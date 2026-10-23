#!/usr/bin/env python3
"""
Альбом фотографий из ZIP-архива — на медиасервер, в раздел «Фото».

Архив уходит на мини-сервер одним файлом (снимки в JPEG не сжимаются, а один
поток по SSH вдвое быстрее сотни мелких) и распаковывается уже там. Внутри
архива обычно одна папка со снимками — она и становится альбомом. Если снимки
лежат в корне архива, альбом называется по имени архива (или --name).

Распаковка идёт во временную скрытую папку рядом с библиотекой и переезжает на
место одним mv: медиатека не увидит альбом наполовину. Мусор архиваторов
(__MACOSX, .DS_Store, Thumbs.db) не распаковывается. Альбом с таким именем
уже есть — скрипт остановится; --merge дописывает в него недостающие файлы.

После распаковки скрипт просит агента открыть альбом: агент в фоне готовит
превью и копии под экран, и на ТВ альбом листается сразу, без ожидания.

    ./send-photos.py "/путь/Свадьба.zip" --dry-run      # только план
    ./send-photos.py "/путь/Свадьба.zip"                # поехали
    ./send-photos.py "/путь/снимки.zip" --name "Отпуск 2026"
    ./send-photos.py "/путь/Свадьба-2.zip" --merge      # дописать в существующий альбом
"""
import argparse
import hashlib
import importlib.util
import json
import os
import posixpath
import shlex
import sys
import tempfile
import time
import urllib.parse
import urllib.request
import uuid
import zipfile

HERE = os.path.dirname(os.path.abspath(__file__))
PHOTO_ROOT = '/home/res/rudb/media/foto'
AGENT_BASE = '/tv'
JUNK_DIRS = {'__MACOSX'}
JUNK_FILES = {'.ds_store', 'thumbs.db', 'desktop.ini'}
PHOTO_EXT = ('.jpg', '.jpeg', '.png', '.webp', '.gif')

# Распаковщик, который выполняется на мини-сервере: берёт список файлов,
# составленный здесь, и не выпускает файлы за пределы альбома («../» в именах).
REMOTE_UNPACK = r'''
import json, os, shutil, sys, zipfile
with open(sys.argv[1], encoding="utf-8") as f:
    spec = json.load(f)
os.makedirs(spec["tmp"], exist_ok=True)
z = zipfile.ZipFile(spec["zip"])
done = skipped = 0
for m in spec["members"]:
    target = os.path.normpath(os.path.join(spec["tmp"], m["dest"]))
    if not target.startswith(spec["tmp"] + os.sep):
        continue
    final = os.path.join(spec["album"], m["dest"])
    if spec["merge"] and os.path.exists(final):
        skipped += 1
        continue
    os.makedirs(os.path.dirname(target), exist_ok=True)
    with z.open(m["name"]) as src, open(target, "wb") as dst:
        shutil.copyfileobj(src, dst, 1 << 20)
    done += 1
z.close()
if spec["merge"] and os.path.isdir(spec["album"]):
    for root, dirs, files in os.walk(spec["tmp"]):
        for f in files:
            src = os.path.join(root, f)
            dst = os.path.join(spec["album"], os.path.relpath(src, spec["tmp"]))
            os.makedirs(os.path.dirname(dst), exist_ok=True)
            os.replace(src, dst)
    shutil.rmtree(spec["tmp"])
else:
    os.rename(spec["tmp"], spec["album"])
print(json.dumps({"done": done, "skipped": skipped}))
'''


def load(name, file):
    spec = importlib.util.spec_from_file_location(name, os.path.join(HERE, file))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def log(msg):
    print(time.strftime('%H:%M:%S'), msg, flush=True)


def human(n):
    for unit in ('Б', 'КБ', 'МБ', 'ГБ'):
        if n < 1024 or unit == 'ГБ':
            return f'{n:.0f} {unit}' if unit in ('Б', 'КБ') else f'{n:.1f} {unit}'
        n /= 1024


def is_junk(parts):
    return any(p in JUNK_DIRS for p in parts) or parts[-1].lower() in JUNK_FILES or parts[-1].startswith('._')


def plan_members(zf, name_override, archive_path):
    """Что распаковывать и куда: (имя альбома, [{name, dest, size}])."""
    files = []
    for info in zf.infolist():
        if info.is_dir():
            continue
        parts = [p for p in info.filename.replace('\\', '/').split('/') if p not in ('', '.')]
        if not parts or '..' in parts or is_junk(parts):
            continue
        files.append((info, parts))
    if not files:
        sys.exit('в архиве нет файлов')
    tops = {parts[0] for _, parts in files}
    single_folder = len(tops) == 1 and all(len(parts) > 1 for _, parts in files)
    if single_folder:
        album = name_override or next(iter(tops))
        members = [{'name': i.filename, 'dest': '/'.join(p[1:]), 'size': i.file_size} for i, p in files]
    else:
        album = name_override or os.path.splitext(os.path.basename(archive_path))[0]
        members = [{'name': i.filename, 'dest': '/'.join(p), 'size': i.file_size} for i, p in files]
    album = album.strip().replace('/', '-')
    if not album or album.startswith('.'):
        sys.exit(f'негодное имя альбома: {album!r} — задайте --name')
    return album, members


def warm_up(host, album_path):
    """Попросить агента открыть альбом: он в фоне соберёт превью и копии.

    id папки — хеш пути, но агент узнаёт путь по id, только когда сам его
    видел, поэтому сначала каталог (он обходит библиотеки), потом альбом.
    """
    base = f'http://{host}{AGENT_BASE}'
    try:
        with urllib.request.urlopen(base + '/api/catalog', timeout=60) as r:
            r.read()
        album_id = hashlib.sha256(album_path.encode()).hexdigest()[:18]
        with urllib.request.urlopen(base + '/api/photos?id=' + urllib.parse.quote(album_id), timeout=60) as r:
            data = json.load(r)
    except Exception as e:
        log(f'агент не ответил ({e}) — превью соберутся при первом открытии альбома')
        return
    n = len(data.get('photos') or [])
    sub = len(data.get('dirs') or [])
    log(f'агент видит альбом: {n} снимков' + (f', вложенных папок {sub}' if sub else '') +
        f' — превью готовятся в фоне (около {max(1, n * 11 // 10 // 60)} мин на мини-сервере)')


def main():
    ap = argparse.ArgumentParser(description='Альбом из ZIP-архива на медиасервер: отдача и распаковка')
    ap.add_argument('archive', help='ZIP-архив: внутри папка со снимками или снимки прямо в корне')
    ap.add_argument('--name', default='', help='имя альбома на сервере (по умолчанию — папка в архиве)')
    ap.add_argument('--dest-root', default=PHOTO_ROOT, help=f'библиотека фото на сервере ({PHOTO_ROOT})')
    ap.add_argument('--merge', action='store_true', help='альбом уже есть — дописать недостающие файлы')
    ap.add_argument('--dry-run', action='store_true', help='только показать план')
    args = ap.parse_args()

    src = os.path.abspath(args.archive)
    if not zipfile.is_zipfile(src):
        sys.exit('это не ZIP-архив: ' + src)
    with zipfile.ZipFile(src) as zf:
        album, members = plan_members(zf, args.name, src)
    dest_root = args.dest_root.rstrip('/')
    album_path = posixpath.join(dest_root, album)
    photos = sum(1 for m in members if m['dest'].lower().endswith(PHOTO_EXT))
    total = sum(m['size'] for m in members)
    dirs = sorted({posixpath.dirname(m['dest']) for m in members} - {''})
    archive_size = os.path.getsize(src)

    rr = load('recode_remote', 'recode-remote.py')
    remote = rr.Remote(*rr.load_access())
    if not remote.wait_online(patience=300):
        sys.exit('медиасервер недоступен')
    rc, _, _ = remote.run('test -d ' + shlex.quote(album_path))
    exists = rc == 0
    rc, out, _ = remote.run(f'mkdir -p {shlex.quote(dest_root)} && df -B1 --output=avail {shlex.quote(dest_root)} | tail -1')
    free = int(out.strip() or 0) if rc == 0 else 0

    log(f'альбом «{album}» → {album_path}')
    log(f'файлов {len(members)} (снимков {photos}), {human(total)}; архив {human(archive_size)}'
        + (f'; вложенных папок {len(dirs)}' if dirs else ''))
    other = len(members) - photos
    if other:
        print(f'   не снимков: {other} — распакуются, но в разделе «Фото» не покажутся')
    if exists:
        print('   альбом уже есть на сервере — ' + ('допишу недостающие файлы' if args.merge
                                                   else 'остановлюсь (дописать: --merge, другое имя: --name)'))
    need = archive_size + total
    if free and free < need:
        remote.close()
        sys.exit(f'на сервере свободно {human(free)}, нужно около {human(need)} (архив + распакованное)')
    print(f'   оценка: передача ~{max(1, archive_size // (25 << 20))} с, свободно на сервере {human(free)}')
    if args.dry_run or (exists and not args.merge):
        remote.close()
        return

    tag = uuid.uuid4().hex[:8]
    remote_zip = posixpath.join(dest_root, f'.upload-{tag}.zip')
    remote_tmp = posixpath.join(dest_root, f'.upload-{tag}')
    remote_spec = posixpath.join(dest_root, f'.upload-{tag}.json')
    started = time.time()
    try:
        for attempt in range(1, 4):
            try:
                t0 = time.time()
                if remote.push(src, remote_zip) == 0 and remote.size(remote_zip) == archive_size:
                    dt = max(time.time() - t0, 0.1)
                    log(f'архив на сервере: {dt:.0f} с ({archive_size / (1 << 20) / dt:.0f} МБ/с)')
                    break
                log(f'размер на сервере не сошёлся [{attempt}/3]')
            except rr.Remote.BROKEN as e:
                log(f'связь оборвалась при отдаче ({type(e).__name__}: {e}) [{attempt}/3]')
                if not remote.wait_online():
                    sys.exit('сеть не вернулась')
        else:
            sys.exit('архив не удалось отдать')

        # список файлов — отдельным файлом, а не аргументом: у альбома на тысячу
        # снимков он больше предела длины аргумента в Linux (128 КБ)
        spec = {'zip': remote_zip, 'tmp': remote_tmp, 'album': album_path,
                'merge': exists and args.merge, 'members': members}
        with tempfile.NamedTemporaryFile('w', encoding='utf-8', suffix='.json', delete=False) as f:
            json.dump(spec, f, ensure_ascii=False)
            local_spec = f.name
        try:
            if remote.push(local_spec, remote_spec) != 0:
                sys.exit('не удалось отдать список файлов')
        finally:
            os.remove(local_spec)
        cmd = 'python3 -c ' + shlex.quote(REMOTE_UNPACK) + ' ' + shlex.quote(remote_spec)
        rc, out, err = remote.run(cmd, timeout=3600)
        if rc != 0:
            sys.exit('распаковка не удалась:\n' + err.strip())
        res = json.loads(out.strip().splitlines()[-1])
        log(f'распаковано {res["done"]}' + (f', уже были {res["skipped"]}' if res['skipped'] else '')
            + f' — всего {time.time() - started:.0f} с')
    finally:
        remote.run(f'rm -rf {shlex.quote(remote_zip)} {shlex.quote(remote_tmp)} {shlex.quote(remote_spec)}')
        host = remote.host
        remote.close()

    warm_up(host, album_path)
    print('архив на этой машине не тронут')


if __name__ == '__main__':
    main()
