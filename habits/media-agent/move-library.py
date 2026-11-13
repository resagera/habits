#!/usr/bin/env python3
"""
Перевозит библиотеку медиаагента на другой диск — вместе с памятью агента о ней.

Запускается НА МЕДИАСЕРВЕРЕ (res-i3), а не с дев-машины: копирование идёт по
месту, иначе 183 ГБ фильмов поехали бы по сети туда и обратно.

Почему не `mv`. Идентификатор файла в агенте — хеш его пути
(`shortID = sha256(путь)[:9]`), и по этим id разложено всё, что агент помнит:
«Продолжить просмотр» (progress.json), заставки серий (marks.json), любимое,
ссылки, подборки, статистика просмотра, постеры (posters/<id>.jpg) и кадры для
«Инфо» (frames/<id>/). Переложив файлы, всё это осиротеет: плитки встанут с
начала, заставки придётся искать заново, подборки опустеют. Поэтому скрипт
переписывает пути и переименовывает id во всём кэше.

Порядок такой, чтобы приставка почти не простаивала и чтобы ни на одном шаге
нельзя было остаться без файлов:

  1. проверки: куда переносим — примонтировано и пишется, места хватает,
     библиотека есть в конфиге агента;
  2. копирование rsync-ом ПРИ РАБОТАЮЩЕМ агенте — самое долгое (183 ГБ по
     USB 2.0 больше часа), и всё это время медиатека работает как обычно;
  3. агент останавливается, rsync повторяется — догнать то, что успело
     появиться за время копирования;
  4. сверка: каждый файл источника есть на той стороне и ровно того же размера;
  5. память агента переписывается под новые пути (на каждый файл — копия
     рядом, с суффиксом `.before-move`),
     файлы и папки кэша с именем из старого id переименовываются в новый;
  6. в конфиге агента правится корень библиотеки, агент запускается;
  7. проверка его же API: библиотека на месте и плиток в ней столько же;
  8. только теперь удаляется исходник — и только если сверка прошла.

Сорвалось на 5–7 — конфиг и память возвращаются из копий, агент поднимается
на старом месте, исходник остаётся. Копия на новом диске при этом остаётся
лежать: она никому не мешает, а следующий прогон её догонит, а не начнёт
заново. Сорвалось на 2–4 — трогать ещё нечего, агент и не останавливался.

Прогон идемпотентен: если источника уже нет, а конфиг смотрит на новое место,
скрипт говорит «уже перевезено» и выходит без ошибки. Поэтому его не страшно
повесить на таймер с `Persistent=true`.

    ./move-library.py --from /home/res/rudb/media/films \\
                      --to /mnt/mybook/rudb/media/films --dry-run
    ./move-library.py --from … --to … --notify     # и написать в Telegram
    ./move-library.py --from … --to … --keep-source # не удалять исходник
"""
import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.request

CONFIG = os.path.expanduser('~/.config/habits-media-agent.conf')
CACHE = os.path.expanduser('~/.cache/habits-media')
UNIT = 'habits-media-agent'
API = 'http://127.0.0.1:8410/tv/api'
# Запас на диске-приёмнике сверх размера библиотеки: забивать его под ноль
# нельзя, там же будут жить новые фильмы.
RESERVE = 5 << 30


def log(msg):
    print(time.strftime('%H:%M:%S ') + msg, flush=True)


def human(n):
    for unit, size in (('ТБ', 1 << 40), ('ГБ', 1 << 30), ('МБ', 1 << 20)):
        if n >= size:
            return f'{n / size:.1f} {unit}'
    return f'{n} Б'


def short_id(path):
    """То же, что shortID в агенте: sha256 пути, первые 9 байт в hex."""
    return hashlib.sha256(path.encode('utf-8')).hexdigest()[:18]


def run(cmd, **kw):
    return subprocess.run(cmd, text=True, capture_output=True, **kw)


class Fail(Exception):
    """Шаг не прошёл. Текст уходит и в журнал, и в уведомление."""


# --- конфиг агента ---

def config_line(text, root):
    """Строка `roots = <root> | Название | вид` для этой библиотеки.
    Путь в конфиге может быть со слешем на конце — агент его всё равно
    приводит к одному виду, поэтому и ищем так же."""
    want = root.rstrip('/')
    for line in text.splitlines():
        body = line.split('#', 1)[0].strip()
        if not body or '=' not in body:
            continue
        key, value = (p.strip() for p in body.split('=', 1))
        if key not in ('roots', 'root'):
            continue
        parts = [p.strip() for p in value.split('|')]
        if parts[0].rstrip('/') == want:
            return line, parts
    return '', []


# --- копирование и сверка ---

def tree(root):
    """Файлы библиотеки: относительный путь → размер. Пустые папки нам
    безразличны — медиатека смотрит на файлы."""
    out = {}
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames.sort()
        for name in sorted(filenames):
            full = os.path.join(dirpath, name)
            if os.path.islink(full):
                continue
            try:
                out[os.path.relpath(full, root)] = os.path.getsize(full)
            except OSError:
                pass
    return out


def copy(src, dst, dry):
    """rsync с ключами под exFAT: права и владельца там менять нельзя, время
    правки оставляем — по нему агент решает, не перечитать ли файл."""
    cmd = ['rsync', '-rt', '--size-only', '--no-perms', '--no-owner', '--no-group',
           '--omit-dir-times', '--partial', src.rstrip('/') + '/', dst.rstrip('/') + '/']
    if dry:
        log('   скопировал бы: ' + ' '.join(cmd))
        return
    res = subprocess.run(cmd, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    if res.returncode != 0:
        tail = (res.stdout or '').strip().splitlines()[-3:]
        raise Fail('rsync вернул %d: %s' % (res.returncode, ' / '.join(tail)))


def compare(src_files, dst_root):
    """Каждый файл источника — на той стороне и того же размера. Лишние файлы
    на приёмнике не беда: туда мог приехать новый фильм, пока шло копирование."""
    missing, wrong = [], []
    for rel, size in src_files.items():
        try:
            got = os.path.getsize(os.path.join(dst_root, rel))
        except OSError:
            missing.append(rel)
            continue
        if got != size:
            wrong.append(f'{rel}: {size} → {got}')
    return missing, wrong


# --- память агента ---

def id_map(src_root, dst_root, src_files):
    """Старый путь → новый и старый id → новый, для корня библиотеки, всех
    папок внутри и всех файлов."""
    paths = {src_root: dst_root}
    for rel in src_files:
        paths[os.path.join(src_root, rel)] = os.path.join(dst_root, rel)
        part = os.path.dirname(rel)
        while part:
            paths[os.path.join(src_root, part)] = os.path.join(dst_root, part)
            part = os.path.dirname(part)
    return paths, {short_id(o): short_id(n) for o, n in paths.items()}


def remap(value, src_root, dst_root, ids):
    """Переписывает строки внутри разобранного JSON: пути — заменой начала
    (в probe.json путь лежит вместе с размером и временем правки, одной
    строкой), id — только целиком, чтобы не задеть чужой хеш."""
    if isinstance(value, str):
        if src_root in value:
            value = value.replace(src_root, dst_root)
        return ids.get(value, value)
    if isinstance(value, list):
        return [remap(v, src_root, dst_root, ids) for v in value]
    if isinstance(value, dict):
        return {remap(k, src_root, dst_root, ids): remap(v, src_root, dst_root, ids)
                for k, v in value.items()}
    return value


def refresh_probe_keys(data, dst_root):
    """Ключ в probe.json — «версия|путь|размер|время правки», и по нему агент
    решает, годится ли разбор. На exFAT время правки округляется до двух
    секунд, поэтому после переезда ключи подновляются по факту: иначе агент
    перечитал бы ffprobe-ом всю библиотеку при первом открытии."""
    fixed = 0
    for entry in (data.get('probes') or {}).values():
        key = entry.get('key') or ''
        parts = key.rsplit('|', 2)
        if len(parts) != 3 or dst_root not in parts[0]:
            continue
        head, size, _ = parts
        path = head.split('|')[-1]
        try:
            st = os.stat(path)
        except OSError:
            continue
        if str(st.st_size) != size:
            continue
        entry['key'] = f'{head}|{size}|{int(st.st_mtime)}'
        fixed += 1
    return fixed


# Суффикс наших копий. Нарочно не «.bak»: у агента есть свой
# progress.json.bak, и копия с тем же именем затёрла бы её.
BACKUP = '.before-move'


def rewrite_memory(cache, src_root, dst_root, ids, dry):
    """Переписывает json-ы кэша и переименовывает файлы и папки, названные
    старым id (постеры, кадры, обложки подборок, перекодированные копии)."""
    touched, renamed = [], []
    for name in sorted(os.listdir(cache)):
        if name.endswith(BACKUP) or not (name.endswith('.json') or name.endswith('.json.bak')):
            continue
        path = os.path.join(cache, name)
        try:
            with open(path, encoding='utf-8') as f:
                data = json.load(f)
        except (OSError, ValueError) as e:
            log(f'   {name}: не разобрал ({e}) — оставляю как есть')
            continue
        fresh = remap(data, src_root, dst_root, ids)
        if name.startswith('probe.json'):
            fixed = refresh_probe_keys(fresh, dst_root)
            if fixed:
                log(f'   probe.json: подновил время правки у {fixed} записей')
        if fresh == data:
            continue
        touched.append(name)
        if dry:
            continue
        shutil.copy2(path, path + BACKUP)
        tmp = path + '.tmp'
        with open(tmp, 'w', encoding='utf-8') as f:
            json.dump(fresh, f, ensure_ascii=False, separators=(',', ':'))
        os.replace(tmp, path)

    # сначала собираем, потом переименовываем от глубоких к ближним: иначе
    # os.walk не спустится в папку, которую мы только что переназвали
    found = []
    for dirpath, dirnames, filenames in os.walk(cache):
        for name in sorted(dirnames + filenames):
            if name[:18] in ids:
                found.append((dirpath, name))
    for dirpath, name in sorted(found, key=lambda it: it[0].count(os.sep), reverse=True):
        new = ids[name[:18]] + name[18:]
        renamed.append(f'{os.path.relpath(os.path.join(dirpath, name), cache)} → {new}')
        if not dry:
            os.replace(os.path.join(dirpath, name), os.path.join(dirpath, new))
    return touched, renamed


def restore_memory(cache, touched):
    for name in touched:
        bak = os.path.join(cache, name + BACKUP)
        if os.path.exists(bak):
            shutil.copy2(bak, os.path.join(cache, name))


# --- агент ---

def agent(action):
    res = run(['systemctl', '--user', action, UNIT])
    if res.returncode != 0:
        raise Fail(f'systemctl --user {action} {UNIT}: {res.stderr.strip()}')


def api(path, patience=30):
    for _ in range(patience):
        try:
            with urllib.request.urlopen(API + path, timeout=5) as r:
                return json.loads(r.read().decode())
        except (urllib.error.URLError, OSError, ValueError):
            time.sleep(1)
    raise Fail(f'агент не ответил на {path}')


def tiles(catalog):
    """Плиток в каталоге: фильмы и сериалы всех видеобиблиотек. Меньше, чем
    было, — значит, переезд что-то потерял."""
    return len(catalog.get('films') or []) + len(catalog.get('series') or [])


def notify(text, cache):
    """Пишет владельцу приставки в Telegram — тем же путём, что и сам агент
    (прод + ключ комнаты из кэша). Сообщение одно за прогон: у прода стоит
    ограничение на число уведомлений в час."""
    try:
        with open(os.path.join(cache, 'room'), encoding='utf-8') as f:
            room = f.read().strip()
    except OSError as e:
        log(f'уведомление не ушло: нет имени комнаты ({e})')
        return
    url = 'https://telegram.resager.ru/app/habits/api/v1/tv/notify'
    body = json.dumps({'room': room, 'text': text}).encode()
    req = urllib.request.Request(url, data=body, headers={'Content-Type': 'application/json'})
    try:
        with urllib.request.urlopen(req, timeout=15) as r:
            log(f'уведомление отправлено ({r.status})')
    except (urllib.error.URLError, OSError) as e:
        log(f'уведомление не ушло: {e}')


def main():
    ap = argparse.ArgumentParser(description='Перенос библиотеки медиаагента на другой диск')
    ap.add_argument('--from', dest='src', required=True, help='откуда (корень библиотеки сейчас)')
    ap.add_argument('--to', dest='dst', required=True, help='куда (корень на новом диске)')
    ap.add_argument('--config', default=CONFIG, help='конфиг агента')
    ap.add_argument('--cache', default=CACHE, help='кэш агента')
    ap.add_argument('--keep-source', action='store_true', help='не удалять исходник после сверки')
    ap.add_argument('--notify', action='store_true', help='написать итог в Telegram')
    ap.add_argument('--dry-run', action='store_true', help='только показать, что сделал бы')
    args = ap.parse_args()

    src = os.path.abspath(args.src).rstrip('/')
    dst = os.path.abspath(args.dst).rstrip('/')
    name = os.path.basename(src)
    started = time.time()
    stage = 'проверки'
    stopped = False
    touched = []
    conf = ''
    try:
        with open(args.config, encoding='utf-8') as f:
            conf = f.read()
        line, parts = config_line(conf, src)
        if not line:
            if config_line(conf, dst)[0] and not os.path.exists(src):
                log(f'библиотека «{name}» уже перевезена: конфиг смотрит на {dst}, источника нет')
                return 0
            raise Fail(f'в конфиге {args.config} нет библиотеки {src}')
        if not os.path.isdir(src):
            raise Fail(f'нет папки {src}')
        if src == dst:
            raise Fail('откуда и куда — одно и то же место')
        parent = os.path.dirname(dst)
        if not os.path.isdir(parent):
            raise Fail(f'нет папки {parent} — диск не примонтирован?')
        probe = os.path.join(parent, '.habits-move-test')
        try:
            with open(probe, 'w') as f:
                f.write('ok')
            os.remove(probe)
        except OSError as e:
            raise Fail(f'в {parent} не пишется: {e}')

        src_files = tree(src)
        size = sum(src_files.values())
        free = shutil.disk_usage(parent).free
        log(f'библиотека: {src} → {dst}')
        log(f'файлов {len(src_files)}, {human(size)}; на приёмнике свободно {human(free)}')
        if not src_files:
            raise Fail('в библиотеке нет файлов — переносить нечего')
        if free < size + RESERVE:
            raise Fail(f'мало места: нужно {human(size + RESERVE)}, свободно {human(free)}')

        title = parts[1] if len(parts) > 1 and parts[1] else name
        before = tiles(api('/catalog'))
        log(f'плиток в каталоге сейчас: {before}')

        stage = 'копирование'
        log('копирую (агент работает, медиатека доступна)…')
        copy(src, dst, args.dry_run)

        stage = 'остановка агента'
        if not args.dry_run:
            agent('stop')
            stopped = True
        log('агент остановлен — догоняю то, что появилось за время копирования')
        stage = 'повторное копирование'
        copy(src, dst, args.dry_run)

        stage = 'сверка'
        src_files = tree(src)
        if args.dry_run:
            log(f'   сверил бы {len(src_files)} файлов')
        else:
            missing, wrong = compare(src_files, dst)
            if missing or wrong:
                raise Fail('не сошлось: нет %d, размер не тот у %d (%s)' %
                           (len(missing), len(wrong), '; '.join((missing + wrong)[:3])))
            log(f'сверка прошла: {len(src_files)} файлов, {human(sum(src_files.values()))}')

        stage = 'память агента'
        paths, ids = id_map(src, dst, src_files)
        log(f'переписываю память: путей {len(paths)}, id {len(ids)}')
        touched, renamed = rewrite_memory(args.cache, src, dst, ids, args.dry_run)
        log('   поправлено: ' + (', '.join(touched) or 'ничего'))
        for r in renamed[:10]:
            log('   переименовано: ' + r)
        if len(renamed) > 10:
            log(f'   …и ещё {len(renamed) - 10}')

        stage = 'конфиг'
        fresh = line.replace(parts[0], dst, 1)
        if args.dry_run:
            log(f'   в конфиге стало бы: {fresh.strip()}')
        else:
            shutil.copy2(args.config, args.config + '.bak-' + time.strftime('%Y%m%d-%H%M'))
            with open(args.config, 'w', encoding='utf-8') as f:
                f.write(conf.replace(line, fresh, 1))
            log(f'   в конфиге: {fresh.strip()}')

        stage = 'запуск агента'
        if not args.dry_run:
            agent('start')
            stopped = False
            libs = api('/libraries')
            titles = [l['title'] for l in libs.get('libraries') or []]
            if title not in titles:
                raise Fail(f'агент не показывает библиотеку: {titles}')
            after = tiles(api('/catalog'))
            log(f'агент поднялся, плиток в каталоге: {after}')
            if after < before:
                raise Fail(f'плиток стало меньше: было {before}, стало {after}')

        stage = 'уборка'
        freed = 0
        if args.keep_source:
            log(f'исходник оставлен (--keep-source): {src}')
        elif args.dry_run:
            log(f'удалил бы исходник: {src}')
        else:
            freed = sum(src_files.values())
            shutil.rmtree(src)
            log(f'исходник удалён, освободилось около {human(freed)}')

        took = int(time.time() - started)
        log(f'готово за {took // 60} мин {took % 60} с')
        if args.notify and not args.dry_run:
            text = (f'📦 Медиатека: «{title}» переехала на новый диск.\n'
                    f'{len(src_files)} файлов, {human(sum(src_files.values()))}, '
                    f'за {took // 60} мин.')
            text += f'\nНа системном диске освободилось {human(freed)}.' if freed \
                else '\nИсходник оставлен на месте.'
            notify(text, args.cache)
        return 0

    except Fail as e:
        log(f'НЕ ВЫШЛО на шаге «{stage}»: {e}')
        if touched and not args.dry_run:
            log(f'возвращаю память агента из {BACKUP}')
            restore_memory(args.cache, touched)
        if conf and not args.dry_run:
            try:
                with open(args.config, 'w', encoding='utf-8') as f:
                    f.write(conf)
            except OSError as oops:
                log(f'конфиг не вернулся: {oops}')
        if stopped:
            try:
                agent('start')
                log('агент поднят на старом месте')
            except Fail as oops:
                log(f'агент не поднялся: {oops}')
        log(f'исходник на месте: {src}')
        if args.notify and not args.dry_run:
            notify(f'⚠ Медиатека: перенос «{name}» на новый диск не вышел на шаге «{stage}»: {e}\n'
                   f'Файлы на месте, медиатека работает как раньше.', args.cache)
        return 1


if __name__ == '__main__':
    sys.exit(main())
