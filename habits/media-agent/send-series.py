#!/usr/bin/env python3
"""
Отправка сериала или фильма с этой машины на медиасервер — одной командой.

Скрипт сам решает, что делать с каждым файлом:
  - видео h264/hevc/av1/vp9 копируется как есть (скорость диска), иначе
    перекодируется в h264;
  - звук AAC/MP3 в стерео копируется, остальное (AC3, DTS, 5.1) — в AAC стерео;
  - контейнер всегда MP4 с faststart; субтитры и обложки отбрасываются.
Сначала печатает план — сколько файлов, что копируется, что перекодируется,
куда ляжет, — потом работает конвейером: пока файл уезжает по сети, следующий
уже собирается.

Папка сериала сохраняет структуру сезонов: Fauda/S01/E01.avi уедет в
<библиотека>/Fauda/S01/E01.mp4 — по разбиению на сезоны медиатека и узнаёт
сериал. Отдельный файл считается фильмом и ложится в библиотеку фильмов.

Уже лежащие на той стороне файлы пропускаются: прерванный прогон просто
запускают заново. Оригиналы не трогаются. Пропала сеть — отдача ждёт её до
30 минут и повторяет тот же файл, а не валит все оставшиеся.

    ./send-series.py "/путь/Legion (2017)" --audio 0 --dry-run   # только план
    ./send-series.py "/путь/Legion (2017)" --audio 0             # поехали
    ./send-series.py "/путь/Film.2020.1080p.mkv" --audio 1 --name "Фильм (2020)"  # фильм
    ./send-series.py /путь/Сериал --only "Season 02"             # один сезон
"""
import argparse
import importlib.util
import json
import os
import queue
import re
import shlex
import subprocess
import sys
import threading
import time
from collections import Counter

HERE = os.path.dirname(os.path.abspath(__file__))
SERIES_ROOT = '/mnt/media/rudb/media/serials'
FILMS_ROOT = '/home/res/rudb/media/films'
WORKDIR = '/media/resager/a25e7fed-287a-455d-a4af-40dd1fc70868/home/sah/rudb'
VIDEO_EXT = ('.avi', '.mkv', '.mp4', '.m4v', '.mov', '.ts', '.wmv', '.flv', '.mpg', '.mpeg', '.webm')
COPY_VIDEO = {'h264', 'hevc', 'av1', 'vp9'}
COPY_AUDIO = {'aac', 'mp3'}  # браузер приставки играет их как есть
DONE = object()


def load(name, file):
    spec = importlib.util.spec_from_file_location(name, os.path.join(HERE, file))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def natural_key(s):
    """«S2» раньше «S10», «E9» раньше «E10»."""
    return [int(t) if t.isdigit() else t.lower() for t in re.split(r'(\d+)', s)]


def collect(root, only):
    files = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames.sort(key=natural_key)
        for name in filenames:
            if name.lower().endswith(VIDEO_EXT) and not name.startswith('.'):
                full = os.path.join(dirpath, name)
                if only and only.lower() not in os.path.relpath(full, root).lower():
                    continue
                files.append(full)
    files.sort(key=lambda p: natural_key(os.path.relpath(p, root)))
    return files


def probe(path):
    out = subprocess.run(['ffprobe', '-v', 'error', '-print_format', 'json', '-show_format',
                          '-show_streams', path], capture_output=True, text=True).stdout
    try:
        data = json.loads(out)
    except ValueError:
        return None
    video = next((s for s in data.get('streams', []) if s.get('codec_type') == 'video'
                  and s.get('disposition', {}).get('attached_pic') != 1), None)
    audio = [s for s in data.get('streams', []) if s.get('codec_type') == 'audio']
    return {
        'vcodec': (video or {}).get('codec_name', ''),
        'height': (video or {}).get('height', 0),
        'audio': [(a.get('codec_name', ''), a.get('channels', 0)) for a in audio],
        'duration': float(data.get('format', {}).get('duration') or 0),
        'size': int(data.get('format', {}).get('size') or os.path.getsize(path)),
    }


def plan_file(info, audio_index):
    """Что делать с файлом: копировать ли видео и звук. None — файл не годится."""
    if not info or not info['vcodec']:
        return None
    if audio_index >= 0 and audio_index >= len(info['audio']):
        return None
    tracks = info['audio'] if audio_index < 0 else [info['audio'][audio_index]]
    return {
        'vcopy': info['vcodec'] in COPY_VIDEO,
        # копируем звук, только если ВСЕ берущиеся дорожки играют как есть
        'acopy': bool(tracks) and all(c in COPY_AUDIO and 0 < ch <= 2 for c, ch in tracks),
        'tracks': tracks,
    }


def ffmpeg_args(src, dst, plan, audio_index):
    args = ['ffmpeg', '-v', 'error', '-y', '-i', src, '-map', '0:v:0']
    if plan['tracks']:
        args += ['-map', f'0:a:{audio_index}' if audio_index >= 0 else '0:a']
    args += ['-c:v', 'copy'] if plan['vcopy'] else \
        ['-c:v', 'libx264', '-preset', 'veryfast', '-crf', '21', '-pix_fmt', 'yuv420p']
    if plan['tracks']:
        # 5.1 в браузере приставки девать некуда — стерео
        args += ['-c:a', 'copy'] if plan['acopy'] else ['-c:a', 'aac', '-b:a', '160k', '-ac', '2']
    args += ['-sn', '-dn', '-map_chapters', '-1', '-movflags', '+faststart', dst]
    return args


def duration_of(path):
    out = subprocess.run(['ffprobe', '-v', 'error', '-show_entries', 'format=duration',
                          '-of', 'csv=p=0', path], capture_output=True, text=True).stdout.strip()
    try:
        return float(out)
    except ValueError:
        return 0.0


def hms(sec):
    sec = int(sec)
    return f'{sec // 3600}:{sec % 3600 // 60:02d}:{sec % 60:02d}'


def human(n):
    return f'{n / (1 << 20):.0f} МБ' if n < (1 << 30) else f'{n / (1 << 30):.1f} ГБ'


def free_bytes(path):
    st = os.statvfs(path)
    return st.f_bavail * st.f_frsize


_print_lock = threading.Lock()


def log(msg):
    with _print_lock:
        print(time.strftime('%H:%M:%S ') + msg, flush=True)


def main():
    ap = argparse.ArgumentParser(description='Сериал или фильм на медиасервер: план, сборка MP4, отдача')
    ap.add_argument('path', help='папка сериала (внутри — папки сезонов) или файл фильма')
    ap.add_argument('--audio', type=int, default=-1,
                    help='номер звуковой дорожки (0 — первая); по умолчанию все дорожки')
    ap.add_argument('--dest-root', default='', help=f'библиотека на той стороне '
                    f'(сериалы — {SERIES_ROOT}, фильмы — {FILMS_ROOT})')
    ap.add_argument('--workdir', default=WORKDIR, help='где собирать временные MP4')
    ap.add_argument('--only', default='', help='только пути с этой подстрокой (например "Season 02")')
    ap.add_argument('--name', default='', help='фильм: имя на той стороне, например "Джентльмены (2019)"')
    ap.add_argument('--retries', type=int, default=3, help='попыток отдачи на файл')
    ap.add_argument('--dry-run', action='store_true', help='только показать план')
    args = ap.parse_args()

    src = os.path.abspath(args.path.rstrip('/'))
    is_film = os.path.isfile(src)
    if is_film:
        root, files = os.path.dirname(src), [src]
        dest_base = (args.dest_root or FILMS_ROOT).rstrip('/')
    elif os.path.isdir(src):
        root, files = src, collect(src, args.only)
        dest_base = (args.dest_root or SERIES_ROOT).rstrip('/') + '/' + os.path.basename(src)
    else:
        sys.exit('нет такого файла или папки: ' + src)
    if not files:
        sys.exit('видеофайлов не найдено')

    rr = load('recode_remote', 'recode-remote.py')
    remote = rr.Remote(*rr.load_access())
    if not remote.wait_online(patience=300):
        sys.exit('медиасервер недоступен')
    rc, out, _ = remote.run(f'find {shlex.quote(dest_base)} -name "*.mp4" 2>/dev/null')
    there = set(out.split('\n')) if rc == 0 else set()

    # --- план ---
    plan, skipped, unusable = [], 0, []
    kinds = Counter()
    total_dur = total_size = 0
    for f in files:
        rel = os.path.relpath(f, root)
        name = (args.name if is_film and args.name else os.path.splitext(rel)[0])
        dst = dest_base + '/' + (name[:-4] if name.lower().endswith('.mp4') else name) + '.mp4'
        if dst in there:
            skipped += 1
            continue
        info = probe(f)
        p = plan_file(info, args.audio)
        if p is None:
            unusable.append(rel)
            continue
        total_dur += info['duration']
        total_size += info['size']
        audio_all = ', '.join(f'{c} {ch}к' for c, ch in info['audio'])
        taken = 'все дорожки' if args.audio < 0 else f'дорожка {args.audio}'
        kinds[(f'видео {info["vcodec"]} {info["height"]}p — ' + ('копия' if p['vcopy'] else 'ПЕРЕКОДИРОВАНИЕ в h264'),
               f'звук [{audio_all}], {taken} — ' + ('копия' if p['acopy'] else 'в AAC стерео'))] += 1
        plan.append((f, rel, dst, info, p))

    log(f'{"фильм" if is_film else "сериал"} «{os.path.basename(src)}» → {dest_base}')
    log(f'файлов {len(files)}: к отправке {len(plan)} ({human(total_size)}, {hms(total_dur)} видео), '
        f'уже на месте {skipped}' + (f', не годятся {len(unusable)}' if unusable else ''))
    for (v, a), n in kinds.most_common():
        print(f'   {n} × {v}; {a}')
    if plan:
        # Замеры: перекодирование видео ~20× реального времени; копия упирается в
        # чтение исходника (переносной USB-диск ~25 МБ/с вместе со сборкой звука);
        # отдача по вайфаю ~25 МБ/с. Сборка и отдача идут параллельно — общее
        # время близко к большей из двух.
        build = sum(i['duration'] / 20 if not p['vcopy'] else i['size'] / (25 << 20)
                    for _, _, _, i, p in plan)
        send = total_size * 0.95 / (25 << 20)
        print(f'   оценка: около {hms(max(build, send) * 1.15)} '
              f'(сборка ~{hms(build)}, передача ~{hms(send)}, идут параллельно)')
    for rel in unusable:
        print('   не годится (нет видео или нет такой дорожки):', rel)
    if args.dry_run or not plan:
        remote.close()
        return

    spool = os.path.join(args.workdir, '.spool-series')
    os.makedirs(spool, exist_ok=True)
    ready = queue.Queue(maxsize=1)  # один готовый файл ждёт, пока уезжает другой
    stats = {'ok': 0, 'failed': []}
    stop = threading.Event()
    started = time.time()

    def encoder():
        try:
            for n, (src_file, rel, dst, info, p) in enumerate(plan, 1):
                if stop.is_set():
                    break
                # места мало — ждём, пока отдача освободит (исходник на месте, готового нет)
                while free_bytes(args.workdir) < info['size'] * 1.2 and not ready.empty() and not stop.is_set():
                    time.sleep(10)
                local = os.path.join(spool, re.sub(r'[/\\]', '__', os.path.splitext(rel)[0]) + '.mp4')
                t0 = time.time()
                res = subprocess.run(ffmpeg_args(src_file, local, p, args.audio), capture_output=True, text=True)
                got = duration_of(local) if os.path.exists(local) else 0
                if res.returncode != 0 or abs(got - info['duration']) > max(2.0, info['duration'] * 0.02):
                    want = info['duration']
                    reason = res.stderr.strip()[:160] or f'длительность {got:.0f} вместо {want:.0f} с'
                    log(f'[{n}/{len(plan)}] ОШИБКА сборки {rel}: {reason}')
                    stats['failed'].append(rel)
                    if os.path.exists(local):
                        os.remove(local)
                    continue
                dt = time.time() - t0
                log(f'[{n}/{len(plan)}] собрано {rel} за {dt:.0f} с: {human(info["size"])} → {human(os.path.getsize(local))}')
                ready.put((n, rel, dst, local))
        finally:
            ready.put(DONE)

    def uploader():
        up = rr.Remote(*rr.load_access())
        try:
            while True:
                item = ready.get()
                if item is DONE:
                    return
                n, rel, dst, local = item
                try:
                    sent, note = False, ''
                    for attempt in range(1, args.retries + 1):
                        try:
                            up.run('mkdir -p ' + shlex.quote(os.path.dirname(dst)))
                            tmp = dst + '.part'
                            t0 = time.time()
                            size = os.path.getsize(local)
                            if up.push(local, tmp) == 0 and up.size(tmp) == size:
                                up.run(f'mv -f {shlex.quote(tmp)} {shlex.quote(dst)}')
                                sent = True
                                dt = time.time() - t0
                                note = f'{dt:.0f} с ({size / (1 << 20) / max(dt, 1):.0f} МБ/с)'
                                break
                            note = 'размер на той стороне не сошёлся'
                            up.run('rm -f ' + shlex.quote(tmp))
                        except rr.Remote.BROKEN as e:
                            note = f'{type(e).__name__}: {e}'
                            # сеть пропала надолго — ждём её, а не валим файл за файлом
                            if not up.wait_online(stop):
                                stop.set()
                                break
                        time.sleep(5 * attempt)
                    if sent:
                        stats['ok'] += 1
                        left = len(plan) - n
                        eta = (time.time() - started) / n * left
                        log(f'[{n}/{len(plan)}] отдано {rel}: {note}' + (f', осталось ~{hms(eta)}' if left else ''))
                    else:
                        log(f'[{n}/{len(plan)}] ОШИБКА отдачи {rel}: {note}')
                        stats['failed'].append(rel)
                finally:
                    if os.path.exists(local):
                        os.remove(local)
        finally:
            up.close()

    remote.close()
    threads = [threading.Thread(target=encoder, daemon=True), threading.Thread(target=uploader, daemon=True)]
    for t in threads:
        t.start()
    try:
        for t in threads:
            while t.is_alive():
                t.join(timeout=1)
    except KeyboardInterrupt:
        log('прервано — дожидаюсь уборки временных файлов')
        stop.set()
        for t in threads:
            t.join(timeout=120)

    log(f'итог: отдано {stats["ok"]}, с ошибками {len(stats["failed"])}, времени {hms(time.time() - started)}')
    for rel in stats['failed']:
        print('   не вышло:', rel)
    print('оригиналы на месте, их никто не трогал')


if __name__ == '__main__':
    main()
