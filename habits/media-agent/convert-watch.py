#!/usr/bin/env python3
"""
Сторож папки «положил и забыл»: что бросили в convert_and_del — уезжает на
приставку.

Запускается по таймеру раз в десять минут, сам ничего не ждёт и ничего не
спрашивает. За один прогон:

  1. смотрит convert_and_del/films и convert_and_del/serials;
  2. берёт только то, что УСТОЯЛОСЬ — размер и время правки не менялись с
     прошлого прогона: иначе подхватил бы наполовину скачанный торрент;
  3. проверяет, что на приставке есть место под эту библиотеку;
  4. зовёт send-series.py — тот сам решает, что копировать, а что
     перекодировать, и отдаёт файлы по сети;
  5. убеждается, что файлы ЛЕГЛИ на той стороне (имя и ненулевой размер), и
     только тогда убирает исходник — папка называется convert_and_del.

Два прогона разом не идут: lock-файл. Если прошлый ещё конвертирует, этот
пишет строчку в журнал и выходит — файлы уже забраны, трогать их нельзя.
systemd и сам не запустит второй экземпляр oneshot-юнита, но сторожа
запускают и руками.

Разложение папки — то же, что у send-series.py:
  films/Фильм.mkv        → фильм отдельной плиткой
  films/Коллекция/       → набор фильмов (--films), плитка на файл
  films/Фильм (2020)/    → ровно одно видео внутри: одна плитка по имени папки
  serials/Сериал/        → сериал, структура сезонов сохраняется

    ./convert-watch.py --dry-run     # что взял бы и что сделал бы
    ./convert-watch.py               # один прогон (так его зовёт таймер)
    ./convert-watch.py --settle 0    # не ждать устаканивания (ручной запуск)
"""
import argparse
import fcntl
import json
import os
import re
import shlex
import shutil
import subprocess
import sys
import time
import importlib.util

HERE = os.path.dirname(os.path.abspath(__file__))
WATCH = '/home/resager/rudb/mount/2tb-ext-part/rudb/temp/convert_and_del'
WORKDIR = '/home/resager/rudb/mount/2tb-ext-part/rudb/temp'
STATE_DIR = os.path.expanduser('~/.local/state/habits-convert')
# запас на приставке сверх размера исходника: перекодирование обычно ужимает,
# но забивать диск под ноль нельзя — там же кэш агента и перекодированные копии
RESERVE = 5 << 30
KEEP_LOGS = 200
MAX_FAILS = 3


def load(name, file):
    spec = importlib.util.spec_from_file_location(name, os.path.join(HERE, file))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


class Log:
    """В файл и в stdout разом: файл — чтобы читать потом, stdout — чтобы
    строки попадали в journalctl вместе с остальным юнитом."""

    def __init__(self, path):
        self.f = open(path, 'a', encoding='utf-8', buffering=1) if path else None

    def __call__(self, msg):
        line = time.strftime('%Y-%m-%d %H:%M:%S ') + msg
        print(line, flush=True)
        if self.f:
            self.f.write(line + '\n')

    def close(self):
        if self.f:
            self.f.close()


def open_log(log_dir):
    if not log_dir:
        return Log(''), ''
    os.makedirs(log_dir, exist_ok=True)
    path = os.path.join(log_dir, time.strftime('convert-%Y%m%d-%H%M%S.log'))
    # старые прогоны не копим: их сотни, а интересны последние
    old = sorted(f for f in os.listdir(log_dir) if f.startswith('convert-') and f.endswith('.log'))
    for f in old[:-KEEP_LOGS]:
        try:
            os.remove(os.path.join(log_dir, f))
        except OSError:
            pass
    return Log(path), path


def read_state(path):
    try:
        with open(path, encoding='utf-8') as f:
            return json.load(f)
    except (OSError, ValueError):
        return {}


def write_state(path, state):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    tmp = path + '.tmp'
    with open(tmp, 'w', encoding='utf-8') as f:
        json.dump(state, f, ensure_ascii=False, indent=1)
    os.replace(tmp, path)


def videos_under(path, ss):
    if os.path.isfile(path):
        return [path] if path.lower().endswith(ss.VIDEO_EXT) else []
    return ss.collect(path, '')


def local_free(path):
    st = os.statvfs(path)
    return st.f_bavail * st.f_frsize


def fingerprint(files):
    """Отпечаток набора файлов: сколько, сколько весят, когда правили в
    последний раз. Качается торрент — отпечаток меняется каждую минуту."""
    total = 0
    newest = 0.0
    for f in files:
        try:
            st = os.stat(f)
        except OSError:
            continue
        total += st.st_size
        newest = max(newest, st.st_mtime)
    return {'count': len(files), 'size': total, 'mtime': int(newest)}


def scan(watch, ss, log):
    """Что лежит в папке: по элементу на плитку будущей медиатеки."""
    items = []
    for kind, sub in (('film', 'films'), ('series', 'serials')):
        root = os.path.join(watch, sub)
        if not os.path.isdir(root):
            continue
        for name in sorted(os.listdir(root)):
            if name.startswith('.') or name.startswith('_'):
                continue
            path = os.path.join(root, name)
            files = videos_under(path, ss)
            if not files:
                continue
            if kind == 'series' and os.path.isfile(path):
                log(f'пропускаю {sub}/{name}: это файл, а сериал — папка с сезонами')
                continue
            mode = 'film'
            if kind == 'film' and os.path.isdir(path):
                # папка с одним видео — это один фильм, и имя у него от папки;
                # папка с несколькими — коллекция, плитка на файл
                mode = 'film-dir' if len(files) == 1 else 'films'
            elif kind == 'series':
                mode = 'series'
            items.append({'key': sub + '/' + name, 'path': path, 'mode': mode, 'files': files})
    return items


def dest_root(mode, ss):
    return ss.SERIES_ROOT if mode == 'series' else ss.FILMS_ROOT


def film_title(path, ss):
    """Имя плитки для одиночного файла. Без него send-series.py взял бы имя
    файла как есть, и в медиатеке висело бы
    «Fantastic.Beasts.2022.Lic.BDRip.MegaPeer»."""
    return ss.film_name(path) or os.path.splitext(os.path.basename(path))[0]


def destinations(item, ss):
    """Куда лягут файлы на той стороне — теми же правилами, что у
    send-series.py. По ним потом и проверяем, что всё доехало."""
    mode, path, files = item['mode'], item['path'], item['files']
    if mode == 'film':
        return [ss.FILMS_ROOT.rstrip('/') + '/' + film_title(path, ss) + '.mp4']
    if mode == 'film-dir':
        return [ss.FILMS_ROOT.rstrip('/') + '/' + os.path.basename(path) + '.mp4']
    if mode == 'films':
        return [ss.FILMS_ROOT.rstrip('/') + '/' + ss.film_name(f) + '.mp4' for f in files]
    base = ss.SERIES_ROOT.rstrip('/') + '/' + os.path.basename(path)
    out = []
    for f in files:
        rel = os.path.splitext(os.path.relpath(f, path))[0]
        out.append(base + '/' + rel + '.mp4')
    return out


def send_args(item, args, ss):
    cmd = [sys.executable, os.path.join(HERE, 'send-series.py'), item['path'],
           '--audio', args.audio, '--workdir', args.workdir]
    if item['mode'] == 'film':
        cmd += ['--name', film_title(item['path'], ss)]
    if item['mode'] == 'films':
        cmd.append('--films')
    if item['mode'] == 'film-dir':
        # одно видео в папке: send-series.py сам посчитал бы её сериалом
        cmd = [sys.executable, os.path.join(HERE, 'send-series.py'), item['files'][0],
               '--audio', args.audio, '--workdir', args.workdir,
               '--name', os.path.basename(item['path'])]
    for flag, value in (('--scale', args.scale), ('--bitrate', args.bitrate), ('--preset', args.preset)):
        if value:
            cmd += [flag, value]
    if args.crf is not None:
        cmd += ['--crf', str(args.crf)]
    return cmd


def remote_free(remote, path):
    """Сколько свободно на той стороне под этой папкой. -1 — не узнали."""
    rc, out, _ = remote.run('df -P -B1 ' + shlex.quote(path) + ' | tail -1')
    if rc != 0:
        return -1
    parts = out.split()
    try:
        return int(parts[3])
    except (IndexError, ValueError):
        return -1


def verify(remote, dests, log):
    """Всё ли доехало. Проверяем размер, а не только имя: .part-файл от
    оборванной отдачи send-series.py убирает сам, но мало ли."""
    missing = []
    for d in dests:
        if remote.size(d) <= 0:
            missing.append(d)
    if missing:
        log(f'   на приставке не хватает {len(missing)} из {len(dests)}: ' +
            ', '.join(os.path.basename(m) for m in missing[:3]) + ('…' if len(missing) > 3 else ''))
    return not missing


def drop(path, log, dry):
    if dry:
        log(f'   удалил бы исходник {path}')
        return
    try:
        if os.path.isdir(path):
            shutil.rmtree(path)
        else:
            os.remove(path)
        log(f'   исходник убран: {path}')
    except OSError as e:
        log(f'   исходник не убрался ({e}) — удалите вручную: {path}')


def main():
    ap = argparse.ArgumentParser(description='Сторож папки convert_and_del: конвертировать и отправить на приставку')
    ap.add_argument('--watch', default=WATCH, help='папка со вложенными films/ и serials/')
    ap.add_argument('--workdir', default=WORKDIR, help='где собирать временные MP4')
    ap.add_argument('--log-dir', default=os.path.join(STATE_DIR, 'log'), help='куда писать журнал прогонов')
    ap.add_argument('--state', default=os.path.join(STATE_DIR, 'state.json'), help='файл памяти между прогонами')
    ap.add_argument('--audio', default='0', help='звуковые дорожки для send-series.py (по умолчанию первая)')
    ap.add_argument('--scale', default='', help='уменьшить картинку: «720p», «70%%»')
    ap.add_argument('--bitrate', default='', help='битрейт видео: «4M»')
    ap.add_argument('--preset', default='', help='пресет x264: fast, medium, slow')
    ap.add_argument('--crf', type=int, default=None, help='качество при перекодировании')
    ap.add_argument('--settle', type=int, default=600,
                    help='сколько секунд файл должен не меняться, чтобы его взять (0 — брать сразу)')
    ap.add_argument('--reserve', type=int, default=RESERVE // (1 << 30),
                    help='сколько ГБ оставлять свободными на приставке')
    ap.add_argument('--keep', action='store_true', help='не удалять исходники после отправки')
    ap.add_argument('--dry-run', action='store_true', help='только показать, что сделал бы')
    args = ap.parse_args()

    log, log_path = open_log(args.log_dir)
    rc = run(args, log)
    if log_path:
        log(f'журнал: {log_path}')
    log.close()
    return rc


def run(args, log):
    os.makedirs(STATE_DIR, exist_ok=True)
    lock_path = os.path.join(STATE_DIR, 'lock')
    lock = open(lock_path, 'w')
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError:
        log('прошлый прогон ещё идёт — файлы уже забраны им, выхожу')
        return 0

    # папку заводим сами, но только если диск на месте: без него os.makedirs
    # создал бы её в точке монтирования, и потом она мешала бы монтированию
    if not os.path.isdir(args.watch):
        if not os.path.isdir(os.path.dirname(args.watch)):
            log(f'папки {args.watch} нет и заводить её негде (диск не смонтирован?) — ничего не делаю')
            return 0
        log(f'завожу папку {args.watch}')
    for sub in ('films', 'serials'):
        os.makedirs(os.path.join(args.watch, sub), exist_ok=True)

    ss = load('send_series', 'send-series.py')
    rr = load('recode_remote', 'recode-remote.py')
    state = read_state(args.state)
    items = scan(args.watch, ss, log)
    if not items:
        log('пусто')
        return 0

    now = int(time.time())
    ready = []
    for it in items:
        fp = fingerprint(it['files'])
        was = state.get(it['key'], {})
        if was.get('fails', 0) >= MAX_FAILS and was.get('fp') == fp:
            continue    # уже падало трижды подряд — не мучаем ни диск, ни сеть
        if was.get('fp') != fp:
            # ещё качается или только что положили — запоминаем и ждём
            state[it['key']] = {'fp': fp, 'seen': now, 'fails': 0}
            log(f'{it["key"]}: появилось или ещё пишется ({ss.human(fp["size"])}, файлов {fp["count"]}) — подожду')
            continue
        waited = now - was.get('seen', now)
        if waited < args.settle:
            log(f'{it["key"]}: устаканивается, прошло {waited} из {args.settle} с')
            continue
        it['fp'] = fp
        ready.append(it)
    write_state(args.state, state)
    if not ready:
        return 0

    remote = rr.Remote(*rr.load_access())
    try:
        if not remote.wait_online(patience=120):
            log('приставка недоступна — попробую в следующий раз')
            return 0
        for it in ready:
            handle(it, args, ss, remote, state, log)
            write_state(args.state, state)
    finally:
        remote.close()
    return 0


def handle(it, args, ss, remote, state, log):
    size = it['fp']['size']
    root = dest_root(it['mode'], ss)
    log(f'{it["key"]}: {it["mode"]}, файлов {it["fp"]["count"]}, {ss.human(size)} → {root}')

    # временный MP4 собирается на этой машине по одному файлу за раз, поэтому
    # нужен не весь сериал, а самый большой файл с запасом
    biggest = max((os.path.getsize(f) for f in it['files'] if os.path.exists(f)), default=0)
    here = local_free(args.workdir)
    if here < biggest * 1.5:
        log(f'   мало места в {args.workdir}: свободно {ss.human(here)}, '
            f'на сборку нужно около {ss.human(int(biggest * 1.5))} — пропускаю')
        return

    free = remote_free(remote, root)
    reserve = args.reserve << 30
    if free < 0:
        log('   не узнал, сколько места на приставке — пропускаю до следующего раза')
        return
    # исходник — верхняя оценка: перекодирование ужимает, копия весит столько же
    if free < size + reserve:
        log(f'   на приставке мало места: свободно {ss.human(free)}, нужно {ss.human(size)} '
            f'плюс запас {args.reserve} ГБ — пропускаю')
        return
    log(f'   место есть: свободно {ss.human(free)}')

    cmd = send_args(it, args, ss)
    if args.dry_run:
        log('   запустил бы: ' + ' '.join(shlex.quote(c) for c in cmd))
        for d in destinations(it, ss)[:5]:
            log('   → ' + d)
        return

    t0 = time.time()
    # вывод тянем построчно, а не в конце: сериал идёт часами, и всё это время
    # в журнале не было бы ни строчки — не понять, работает оно вообще или висит
    proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                            text=True, bufsize=1)
    for line in proc.stdout:
        line = line.rstrip()
        if line:
            log('   | ' + line)
    rc = proc.wait()
    if rc != 0:
        log(f'   send-series.py вернул {rc}')

    if verify(remote, destinations(it, ss), log):
        log(f'   готово за {ss.hms(time.time() - t0)}')
        state[it['key']] = {'fp': it['fp'], 'seen': int(time.time()), 'fails': 0, 'done': True}
        if args.keep:
            log('   исходник оставлен (--keep)')
        else:
            drop(it['path'], log, args.dry_run)
            state.pop(it['key'], None)
    else:
        fails = state.get(it['key'], {}).get('fails', 0) + 1
        state[it['key']] = {'fp': it['fp'], 'seen': int(time.time()), 'fails': fails}
        log(f'   не доехало целиком, попытка {fails} из {MAX_FAILS}; исходник на месте')


if __name__ == '__main__':
    sys.exit(main())
