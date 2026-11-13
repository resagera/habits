#!/usr/bin/env python3
"""Подготовка библиотеки к просмотру на приставке: имена под Plex и MP4.

Приставка играет только MP4 (проверено на Xiaomi Mi TV): MKV и AVI она не
открывает, хотя кодеки внутри чаще всего подходящие. Значит MKV достаточно
ПЕРЕПАКОВАТЬ — это копирование потоков, оно идёт со скоростью диска и не
трогает качество. Настоящее перекодирование нужно только там, где не годится
сам кодек (звук AC3/DTS или видео MPEG-4/Xvid из AVI).

Ничего не делает без --apply: сначала показывает план.

    python3 tvconvert.py plan    ПАПКА              что нашлось и что будет
    python3 tvconvert.py rename  ПАПКА [--apply]    имена и папки под Plex
    python3 tvconvert.py convert ПАПКА [--apply]    в MP4 (сохраняя оригиналы)

Полезные ключи:
    --jobs N       сколько файлов конвертировать разом (по умолчанию 2)
    --out ПАПКА    складывать результат отдельно, не рядом с оригиналом
    --delete       удалять оригинал после успешной конвертации
    --show "Имя"   название сериала, если из папки оно не угадывается
"""

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor

VIDEO_EXT = {'.mkv', '.mp4', '.avi', '.m4v', '.mov', '.ts', '.webm', '.wmv', '.flv', '.mpg', '.mpeg'}

# Кодеки, которые приставка берёт как есть (по её же отчёту: HEVC и 10 бит — да,
# AC3/EAC3 — нет).
OK_VIDEO = {'h264', 'hevc', 'vp8', 'vp9', 'av1'}
OK_AUDIO = {'aac', 'mp3', 'opus', 'vorbis', 'flac'}
OK_CONTAINER = {'.mp4', '.m4v', '.mov'}

# --- разбор имён ---

# Порядок важен: сначала однозначные виды («S01E02»), потом расплывчатые
# («- 02 -»), иначе «Show 2016 - 05» отдаст сезон 20 из года.
EPISODE_PATTERNS = [
    re.compile(r'[Ss](\d{1,2})[\s._-]*[EeЕе](\d{1,3})'),          # S01E02, s1e2
    re.compile(r'(?<!\d)(\d{1,2})[xх](\d{2,3})(?!\d)'),            # 1x02
    re.compile(r'(?i)сезон[\s._-]*(\d{1,2}).*?серия[\s._-]*(\d{1,3})'),
]
# Номер серии без сезона — сезон берём из папки
EPISODE_ONLY = [
    re.compile(r'(?i)(?:серия|episode|ep|e)[\s._-]*(\d{1,3})(?!\d)'),
    re.compile(r'(?<!\d)\[(\d{1,3})\](?!\d)'),
    re.compile(r'^(\d{1,3})[\s._-]'),                              # «02. Название»
    re.compile(r'[\s._-](\d{1,3})[\s._-]'),                        # «- 02 -»
]
SEASON_DIR = re.compile(r'(?i)(?:season|сезон|s)[\s._-]*(\d{1,2})')
YEAR = re.compile(r'(?:19|20)\d{2}')

# Мусор из имён релизов: он не помогает узнать сериал, но мешает.
JUNK = re.compile(
    r'(?i)\b(1080p|720p|2160p|480p|4k|bluray|blu-ray|bdrip|webrip|web-dl|webdl|hdtv|'
    r'dvdrip|x264|x265|h\.?264|h\.?265|hevc|avc|aac|ac3|dts|flac|10bit|hdr|'
    r'rus|eng|dub|sub|multi|repack|proper)\b')


def clean_title(s):
    s = re.sub(r'\.(?=[^\d])', ' ', s)      # точки-разделители, но не «5.1»
    s = s.replace('_', ' ')
    s = JUNK.sub(' ', s)
    s = re.sub(r'[\[\(\{].*?[\]\)\}]', ' ', s)   # скобочные хвосты релизёров
    s = re.sub(r'[\s.-]+', ' ', s)
    return s.strip(' -.')


def parse_episode(path, root):
    """Сезон и номер серии из имени файла, при нужде — из имени папки."""
    name = os.path.splitext(os.path.basename(path))[0]
    for rx in EPISODE_PATTERNS:
        m = rx.search(name)
        if m:
            return int(m.group(1)), int(m.group(2))

    season = None
    rel = os.path.relpath(os.path.dirname(path), root)
    for part in reversed(rel.split(os.sep)):
        m = SEASON_DIR.search(part)
        if m:
            season = int(m.group(1))
            break

    # год в имени легко принять за номер серии — выкидываем его заранее
    stripped = YEAR.sub(' ', name)
    for rx in EPISODE_ONLY:
        m = rx.search(stripped)
        if m:
            return season if season is not None else 1, int(m.group(1))
    return season, None


def guess_show(root, override):
    if override:
        return override
    return clean_title(os.path.basename(os.path.abspath(root))) or 'Show'


def plex_path(show, season, episode, ext, title=''):
    """Раскладка, которую понимает Plex: Сериал/Season 01/Сериал - S01E02.ext"""
    s = f'{season:02d}'
    e = f'{episode:02d}' if episode < 100 else f'{episode:03d}'
    name = f'{show} - S{s}E{e}'
    if title:
        name += f' - {title}'
    return os.path.join(show, f'Season {s}', name + ext)


# --- разбор содержимого ---

def probe(path):
    try:
        out = subprocess.run(['ffprobe', '-v', 'error', '-print_format', 'json',
                              '-show_format', '-show_streams', path],
                             capture_output=True, text=True, timeout=60)
        if out.returncode:
            return None
        raw = json.loads(out.stdout)
    except Exception:
        return None
    v = a = ''
    dur = float(raw.get('format', {}).get('duration', 0) or 0)
    for s in raw.get('streams', []):
        if s.get('codec_type') == 'video' and not v and s.get('disposition', {}).get('attached_pic') != 1:
            v = s.get('codec_name', '')
        elif s.get('codec_type') == 'audio' and not a:
            a = s.get('codec_name', '')
    return {'vcodec': v, 'acodec': a, 'duration': dur}


def profile_for(path, info):
    """Что нужно сделать с файлом, чтобы приставка его взяла."""
    ext = os.path.splitext(path)[1].lower()
    if not info:
        return None, 'не читается'
    bad_v = info['vcodec'] and info['vcodec'] not in OK_VIDEO
    bad_a = info['acodec'] and info['acodec'] not in OK_AUDIO
    if bad_v:
        return 'video', f'видео {info["vcodec"]}'
    if bad_a:
        return 'audio', f'звук {info["acodec"]}'
    if ext not in OK_CONTAINER:
        return 'remux', f'контейнер {ext.lstrip(".")}'
    return None, 'уже годится'


FFMPEG_ARGS = {
    # копирование потоков: быстро, без потери качества — только смена коробки
    'remux': ['-c', 'copy'],
    # видео как есть, звук в AAC: самый частый и самый дешёвый случай
    'audio': ['-c:v', 'copy', '-c:a', 'aac', '-b:a', '192k', '-ac', '2'],
    'video': ['-c:v', 'libx264', '-preset', 'medium', '-crf', '20',
              '-c:a', 'aac', '-b:a', '192k', '-ac', '2'],
}


def convert(src, dst, prof, delete_src):
    os.makedirs(os.path.dirname(dst) or '.', exist_ok=True)
    tmp = dst + '.part.mp4'
    cmd = (['ffmpeg', '-nostdin', '-y', '-v', 'error', '-i', src]
           + FFMPEG_ARGS[prof]
           # английские дорожки субтитров в mp4 не лезут (ASS/PGS) — выкидываем:
           # иначе ffmpeg падает на ровном месте
           + ['-sn', '-movflags', '+faststart', '-f', 'mp4', tmp])
    r = subprocess.run(cmd, capture_output=True, text=True)
    if r.returncode != 0 or not os.path.exists(tmp):
        if os.path.exists(tmp):
            os.remove(tmp)
        return False, (r.stderr or '').strip().split('\n')[-1][:200]
    os.replace(tmp, dst)
    if delete_src and os.path.abspath(src) != os.path.abspath(dst):
        os.remove(src)
    return True, ''


# --- обход ---

def walk(root):
    out = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = sorted(d for d in dirnames if not d.startswith('.'))
        for fn in sorted(filenames):
            if os.path.splitext(fn)[1].lower() in VIDEO_EXT:
                out.append(os.path.join(dirpath, fn))
    return out


def build(args):
    """Общая подготовка: список файлов с разбором, планом имени и профилем."""
    root = os.path.abspath(os.path.expanduser(args.folder))
    show = guess_show(root, args.show)
    files = walk(root)
    if not files:
        print('Видеофайлов не нашлось.')
        return root, show, []
    print(f'Файлов: {len(files)}. Читаю через ffprobe…', file=sys.stderr)
    with ThreadPoolExecutor(max_workers=8) as pool:
        infos = list(pool.map(probe, files))

    rows = []
    for path, info in zip(files, infos):
        season, episode = parse_episode(path, root)
        prof, why = profile_for(path, info)
        ext = '.mp4' if prof else os.path.splitext(path)[1].lower()
        target = (plex_path(show, season or 1, episode, ext)
                  if episode is not None else None)
        rows.append({'path': path, 'info': info, 'profile': prof, 'why': why,
                     'season': season, 'episode': episode, 'target': target})
    return root, show, rows


def cmd_plan(args):
    root, show, rows = build(args)
    if not rows:
        return
    print(f'\nСериал: {show}')
    by_prof = {}
    unnamed = 0
    for r in rows:
        by_prof[r['why']] = by_prof.get(r['why'], 0) + 1
        if r['target'] is None:
            unnamed += 1
    print('\n=== Что делать с файлами ===')
    for why, n in sorted(by_prof.items(), key=lambda kv: -kv[1]):
        print(f'  {n:5}  {why}')
    print('\n=== Примеры переименования ===')
    shown = 0
    for r in rows:
        if r['target'] and shown < 8:
            print(f'  {os.path.relpath(r["path"], root)}')
            print(f'    → {r["target"]}')
            shown += 1
    if unnamed:
        print(f'\n  ⚠ у {unnamed} файлов не удалось определить номер серии —')
        print('    их переименование пропустится (сами файлы это не трогает)')
    todo = sum(1 for r in rows if r['profile'])
    print(f'\nК конвертации: {todo}. Запуск: convert ПАПКА --apply')


def cmd_rename(args):
    root, show, rows = build(args)
    dest_root = os.path.abspath(os.path.expanduser(args.out)) if args.out else root
    done = skipped = 0
    for r in rows:
        if not r['target']:
            skipped += 1
            continue
        # расширение при переименовании не меняем: конвертация — отдельный шаг
        target = os.path.splitext(r['target'])[0] + os.path.splitext(r['path'])[1].lower()
        dst = os.path.join(dest_root, target)
        if os.path.abspath(dst) == os.path.abspath(r['path']):
            continue
        if os.path.exists(dst):
            print(f'  пропуск (уже есть): {target}')
            skipped += 1
            continue
        print(f'  {os.path.relpath(r["path"], root)}\n    → {target}')
        if args.apply:
            os.makedirs(os.path.dirname(dst), exist_ok=True)
            shutil.move(r['path'], dst)
        done += 1
    print(f'\n{"Перенесено" if args.apply else "Будет перенесено"}: {done}, пропущено: {skipped}')
    if not args.apply:
        print('Это только план. Повторите с --apply.')


def cmd_convert(args):
    root, show, rows = build(args)
    dest_root = os.path.abspath(os.path.expanduser(args.out)) if args.out else None
    todo = [r for r in rows if r['profile']]
    if not todo:
        print('Всё уже годится для приставки — конвертировать нечего.')
        return
    print(f'\nК конвертации: {len(todo)}')
    if not args.apply:
        for r in todo[:15]:
            print(f'  [{r["profile"]}] {os.path.relpath(r["path"], root)} — {r["why"]}')
        if len(todo) > 15:
            print(f'  … и ещё {len(todo)-15}')
        print('\nЭто только план. Повторите с --apply.')
        return

    def dst_for(r):
        if dest_root:
            rel = r['target'] or os.path.relpath(os.path.splitext(r['path'])[0] + '.mp4', root)
            return os.path.join(dest_root, rel)
        return os.path.splitext(r['path'])[0] + '.mp4'

    total, ok, bad = len(todo), 0, []
    def work(i_r):
        i, r = i_r
        dst = dst_for(r)
        if os.path.exists(dst) and os.path.getsize(dst) > 0:
            return ('skip', r, '')
        good, err = convert(r['path'], dst, r['profile'], args.delete)
        return ('ok' if good else 'fail', r, err)

    with ThreadPoolExecutor(max_workers=max(1, args.jobs)) as pool:
        for n, (status, r, err) in enumerate(pool.map(work, enumerate(todo)), 1):
            name = os.path.basename(r['path'])
            if status == 'fail':
                bad.append((name, err))
                print(f'  [{n}/{total}] ✘ {name}: {err}')
            else:
                ok += 1
                print(f'  [{n}/{total}] ✔ {name}' + (' (уже был)' if status == 'skip' else ''))
    print(f'\nГотово: {ok} из {total}.')
    if bad:
        print(f'Не получилось: {len(bad)}')
        for name, err in bad[:10]:
            print(f'  {name}: {err}')


def main():
    p = argparse.ArgumentParser(description=__doc__,
                                formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument('command', choices=['plan', 'rename', 'convert'])
    p.add_argument('folder')
    p.add_argument('--apply', action='store_true', help='выполнить, а не показать план')
    p.add_argument('--jobs', type=int, default=2, help='файлов разом при конвертации')
    p.add_argument('--out', help='складывать результат сюда, а не рядом')
    p.add_argument('--delete', action='store_true', help='удалять оригинал после конвертации')
    p.add_argument('--show', help='название сериала, если из папки не угадывается')
    args = p.parse_args()
    if not shutil.which('ffprobe'):
        sys.exit('нужен ffmpeg с ffprobe в PATH')
    {'plan': cmd_plan, 'rename': cmd_rename, 'convert': cmd_convert}[args.command](args)


if __name__ == '__main__':
    main()
