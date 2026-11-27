#!/usr/bin/env python3
"""
Собрать один сериал из нескольких папок — ссылками, не трогая файлы.

Раздачи часто лежат по папке на сезон («House M.D. 1 - LostFilm.TV»,
«House M.D. 2 - LostFilm.TV», …), и переносить файлы нельзя — они раздаются в
торренте. Скрипт делает папку сериала из символических ссылок:

    ~/send/House M.D. (2004)/Season 01/House.M.D.s01e01….avi -> оригинал
    ~/send/House M.D. (2004)/Season 02/…

и печатает команду send-series.py, которая отправит её на приставку.

Номер сезона берётся из имени файла («s01e02», «S08E01», «1x02»), а если там
его нет — из имени папки («Season 2», «Сезон 2» или просто число:
«Euphoria 2 - LostFilm.TV [1080p]» → 2; «1080p», «720p» и годы числом
сезона не считаются).

Папки — списком или шаблоном (относительные — внутри --in):

    ./link-series.py --pattern "House M.D. *" --name "House M.D. (2004)"
    ./link-series.py "Euphoria 1 - LostFilm.TV [1080p]" "Euphoria 2 - LostFilm.TV [1080p]" --name "Euphoria (2019)"
    ./link-series.py --pattern "House M.D. *" --name "House M.D. (2004)" --dry-run   # только план

Ссылки нужны на диске, который их умеет: exFAT/NTFS-диски с раздачами — нет,
поэтому по умолчанию папка собирается в ~/send. Повторный запуск дособирает
недостающее; --clean убирает ссылки, которых в новом плане нет.
"""
import argparse
import glob
import importlib.util
import os
import re
import shlex
import sys
from collections import defaultdict

HERE = os.path.dirname(os.path.abspath(__file__))
SERIALS = '/media/resager/win200/rudb/media/serials'
OUT = os.path.expanduser('~/send')


def load(name, file):
    spec = importlib.util.spec_from_file_location(name, os.path.join(HERE, file))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


ss = load('send_series', 'send-series.py')  # те же расширения и разбор сезонов

# число отдельным словом: «House M.D. 1 - LostFilm.TV» → 1; «720p», «1080p», «2019» — нет
BARE_NUM = re.compile(r'(?<![\w.])0*(\d{1,2})(?![\w])')
EPISODE = re.compile(r's\s*0*(\d{1,2})\s*[\s.\-_]*e\s*0*(\d{1,3})|(?<!\d)0*(\d{1,2})x(\d{2})(?!\d)', re.I)


def folder_season(folder):
    """Номер сезона из имени папки или None."""
    name = os.path.basename(folder.rstrip('/'))
    allowed = ss.season_allowed(name)
    if allowed and len(allowed) == 1:
        return next(iter(allowed))
    nums = BARE_NUM.findall(re.sub(r'\[[^\]]*\]|\([^)]*\)', ' ', name))
    return int(nums[-1]) if len(nums) == 1 else None


def episode_key(name):
    m = EPISODE.search(name)
    if not m:
        return None
    s, e = (m.group(1), m.group(2)) if m.group(1) else (m.group(3), m.group(4))
    return int(s), int(e)


def guess_name(folders):
    """Общее начало имён папок: «House M.D. 1 - …», «House M.D. 2 - …» → «House M.D.»."""
    names = [os.path.basename(f.rstrip('/')) for f in folders]
    prefix = os.path.commonprefix(names) if len(names) > 1 else re.split(r'\s+\d', names[0])[0]
    return re.sub(r'[\s\d_\-\[(]+$', '', prefix).strip() or names[0]


def resolve_folders(args):
    folders = []
    for f in args.folders:
        folders.append(f if os.path.isabs(f) else os.path.join(args.inside, f))
    if args.pattern:
        pat = args.pattern if os.path.isabs(args.pattern) else os.path.join(args.inside, args.pattern)
        folders += sorted((p for p in glob.glob(pat) if os.path.isdir(p)), key=ss.natural_key)
    seen, out = set(), []
    for f in folders:
        f = os.path.abspath(f)
        if f not in seen:
            seen.add(f)
            out.append(f)
    return out


def plan_links(folders, dest):
    """[(ссылка, оригинал)] и список предупреждений; ошибки — исключением."""
    links, warns, errors = [], [], []
    by_ep = defaultdict(list)
    for folder in folders:
        if not os.path.isdir(folder):
            errors.append(f'нет папки: {folder}')
            continue
        files = ss.collect(folder, '')
        if not files:
            warns.append(f'пропущена, видео нет: {folder}')
            continue
        fs = folder_season(folder)
        for f in files:
            name = os.path.basename(f)
            season = ss.season_of(name)
            if season is None:
                season = fs if fs is not None else (1 if len(folders) == 1 else None)
            elif fs is not None and season != fs:
                warns.append(f'{name}: в имени сезон {season}, у папки {fs} — беру {season}')
            if season is None:
                errors.append(f'не понять сезон: {f} (ни в имени файла, ни в имени папки)')
                continue
            link = os.path.join(dest, f'Season {season:02d}', name)
            links.append((link, f))
            ep = episode_key(name)
            if ep:
                by_ep[ep].append(f)
    targets = defaultdict(list)
    for link, f in links:
        targets[link].append(f)
    for link, srcs in targets.items():
        if len(srcs) > 1:
            errors.append(f'одно имя из разных папок: {os.path.relpath(link, dest)} ← ' + ', '.join(srcs))
    for (s, e), srcs in sorted(by_ep.items()):
        if len(srcs) > 1:
            warns.append(f'S{s:02d}E{e:02d} несколько раз: ' + ', '.join(os.path.basename(x) for x in srcs))
    if errors:
        raise SystemExit('\n'.join(['ошибка:'] + ['  ' + e for e in errors]))
    return links, warns


def main():
    ap = argparse.ArgumentParser(description='Собрать сериал из нескольких папок символическими ссылками.')
    ap.add_argument('folders', nargs='*', help='папки сезонов (относительные — внутри --in)')
    ap.add_argument('-p', '--pattern', default='', help='шаблон папок, например "House M.D. *"')
    ap.add_argument('--in', dest='inside', default=SERIALS, help=f'где искать папки (по умолчанию {SERIALS})')
    ap.add_argument('--name', default='', help='имя сериала, лучше с годом: "House M.D. (2004)"')
    ap.add_argument('--to', default=OUT, help=f'куда собрать (по умолчанию {OUT}); диск должен уметь ссылки')
    ap.add_argument('--clean', action='store_true', help='убрать ссылки, которых нет в новом плане')
    ap.add_argument('--dry-run', action='store_true', help='только показать план')
    args = ap.parse_args()

    folders = resolve_folders(args)
    if not folders:
        ap.error('нет папок: укажите их списком или --pattern')
    name = args.name or guess_name(folders)
    dest = os.path.join(os.path.abspath(os.path.expanduser(args.to)), name)
    # своя же папка сборки под шаблон попасть не должна
    folders = [f for f in folders if not (f == dest or f.startswith(dest + os.sep))]

    links, warns = plan_links(folders, dest)
    seasons = defaultdict(int)
    for link, _ in links:
        seasons[os.path.basename(os.path.dirname(link))] += 1
    print(f'Сериал «{name}» → {dest}')
    print('Папки:')
    for f in folders:
        print('  ' + f)
    print('Сезоны: ' + ', '.join(f'{s} — {n}' for s, n in sorted(seasons.items())) + f'; всего {len(links)}')
    for w in warns:
        print('  ! ' + w)
    if not args.name:
        print(f'  ! имя угадано по папкам; с годом поиск постера надёжнее: --name "{name} (ГОД)"')

    want = {link for link, _ in links}
    stale = []
    if os.path.isdir(dest):
        for dp, _, fns in os.walk(dest):
            for fn in fns:
                p = os.path.join(dp, fn)
                if os.path.islink(p) and p not in want:
                    stale.append(p)
    if stale:
        print(f'Лишних ссылок от прошлых сборок: {len(stale)}' + ('' if args.clean else ' (убрать — --clean)'))

    if args.dry_run:
        print('Сухой прогон: ничего не создано.')
        return

    made = same = 0
    for link, src in links:
        if os.path.islink(link):
            if os.readlink(link) == src:
                same += 1
                continue
            os.remove(link)
        elif os.path.exists(link):
            raise SystemExit(f'ошибка: на месте ссылки лежит настоящий файл, не трогаю: {link}')
        os.makedirs(os.path.dirname(link), exist_ok=True)
        try:
            os.symlink(src, link)
        except OSError as e:
            raise SystemExit(f'ошибка: ссылку не создать ({e}). Диск {args.to} не умеет ссылки '
                             f'(exFAT/NTFS) — соберите в ~/send.')
        made += 1
    if args.clean:
        for p in stale:
            os.remove(p)
        for dp, _, _ in sorted(os.walk(dest), key=lambda x: -len(x[0])):
            if dp != dest and not os.listdir(dp):
                os.rmdir(dp)
    print(f'Готово: новых ссылок {made}, уже были {same}' + (f', убрано {len(stale)}' if args.clean and stale else '') + '.')
    cmd = os.path.join(HERE, 'send-series.py')
    print('\nОтправка на приставку (сначала план):')
    print(f'  {shlex.quote(cmd)} {shlex.quote(dest)} --dry-run')
    print(f'  {shlex.quote(cmd)} {shlex.quote(dest)}')


if __name__ == '__main__':
    sys.exit(main())
