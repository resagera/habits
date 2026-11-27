#!/usr/bin/env python3
"""Озвучки и субтитры, которых у сериала на приставке нет, — с этой машины.

Смотрит локальную папку сериала: дорожки внутри серий и отдельные файлы рядом
(«S01E01.mka», «Rus Sound/LostFilm/S01E01.ac3», «Subs/S01E01.srt»), — и
сравнивает с тем же сериалом на приставке. Печатает, каких озвучек и субтитров
там не хватает и в скольких сериях. С `--send` довозит недостающее отдельными
файлами рядом с сериями на приставке: отдельный файл едет как есть, дорожка
изнутри серии сначала вынимается в .mka/.srt без перекодирования. Плеер
приставки покажет их в «🔊» и «💬» после обхода файлов — скрипт просит агента
обойти их сразу.

Серии сопоставляются по номеру (S01E02, 1x02); сериал на приставке — по имени
папки. Если там он называется иначе — `--series "Имя на приставке"`.

    ./send-tracks.py "/путь/Legion (2017)"                    # что отсутствует
    ./send-tracks.py "/путь/Legion.S01.1080p" --series "Legion (2017)"
    ./send-tracks.py "/путь/Legion (2017)" --send             # довезти всё недостающее
    ./send-tracks.py "/путь/Legion (2017)" --send --tracks lostfilm,eng   # только эти
    ./send-tracks.py "/путь/Legion (2017)" --only S02 --no-subs

Доступ к приставке — тот же, что у send-series.py (~/.config/habits-recode.conf).
"""
import argparse
import importlib.util
import json
import os
import re
import shlex
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
VIDEO_EXT = ('.avi', '.mkv', '.mp4', '.m4v', '.mov', '.ts', '.wmv', '.flv', '.mpg', '.mpeg', '.webm')
AUDIO_EXT = ('.mka', '.ac3', '.eac3', '.dts', '.mp3', '.aac', '.m4a', '.flac', '.opus', '.ogg', '.wav', '.mp2')
SUB_EXT = ('.srt', '.vtt', '.ass', '.ssa')
SIDE_DEPTH = 3
TEXT_SUB = {'subrip': '.srt', 'mov_text': '.srt', 'webvtt': '.srt', 'text': '.srt', 'ass': '.ass', 'ssa': '.ass'}
LANGS = {'ru', 'rus', 'en', 'eng', 'uk', 'ukr', 'ua', 'hy', 'arm', 'de', 'ger', 'fr', 'fre', 'es', 'spa',
         'ja', 'jpn', 'it', 'ita', 'ko', 'kor', 'zh', 'chi'}
LANG_NORM = {'ru': 'rus', 'en': 'eng', 'uk': 'ukr', 'ua': 'ukr', 'hy': 'arm', 'de': 'ger', 'fr': 'fre',
             'es': 'spa', 'ja': 'jpn', 'it': 'ita', 'ko': 'kor', 'zh': 'chi'}
# «Rus Sound», «Audio», «Subs» — о дорожке говорят только языком (как в агенте, tracks.go)
GENERIC_DIR = re.compile(r'(?i)^\s*(rus|eng|ukr|ru|en|ua)?[\s._-]*(sounds?|audio|dub|tracks?|subs?|subtitles?|'
                         r'звук|озвучк[аи]|дорожки|субтитры)?[\s._-]*(rus|eng|ukr|ru|en|ua)?\s*$')
EPISODE = re.compile(r'(?i)(?:s(\d{1,2})[\s._-]*[eе](\d{1,3})|(?<!\d)(\d{1,2})x(\d{2,3})(?!\d))')


def load(name, file):
    spec = importlib.util.spec_from_file_location(name, os.path.join(HERE, file))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def natural_key(s):
    return [int(t) if t.isdigit() else t.lower() for t in re.split(r'(\d+)', s)]


def stem(p):
    return os.path.splitext(os.path.basename(p))[0]


def side_matches(side_stem, video_stem):
    a, b = side_stem.lower(), video_stem.lower()
    return a == b or a.startswith(b + '.')


def episode_key(rel):
    m = EPISODE.search(os.path.basename(rel)) or EPISODE.search(rel)
    if not m:
        return None
    s, e = (m.group(1), m.group(2)) if m.group(1) else (m.group(3), m.group(4))
    return int(s), int(e)


def norm(text):
    return re.sub(r'[\W_]+', '', text.lower())


def side_label(video, side):
    """Подпись и язык отдельного файла дорожки — тем же разбором, что в агенте."""
    names, lang = [], ''
    rel = os.path.relpath(os.path.dirname(side), os.path.dirname(video))
    if rel != '.':
        for part in rel.split(os.sep):
            m = GENERIC_DIR.match(part)
            if m:
                lang = lang or (m.group(1) or m.group(3) or '').lower()
                continue
            names.append(part)
        names = names[-1:]
    mid = stem(side)[len(stem(video)):].strip('. ')
    for tok in mid.split('.'):
        tok = tok.strip('[]() ')
        if not tok:
            continue
        if tok.lower() in LANGS:
            lang = tok.lower()
        else:
            names.append(tok)
    return ' · '.join(names), LANG_NORM.get(lang, lang)


class Track:
    """Озвучка или субтитры серии: внутри файла (n — номер среди своего вида)
    или отдельным файлом (path)."""

    def __init__(self, kind, title, lang, codec='', n=-1, path=''):
        self.kind, self.title, self.codec, self.n, self.path = kind, title.strip(), codec, n, path
        lang = (lang or '').lower()
        self.lang = '' if lang == 'und' else LANG_NORM.get(lang, lang)

    def key(self):
        # одна и та же озвучка внутри mkv и файлом рядом: «LostFilm» = «LostFilm»
        if self.title:
            return self.kind + ':' + norm(self.title)
        if self.lang:
            return self.kind + ':lang:' + self.lang
        return None

    def name(self):
        t = self.title or {'rus': 'русская', 'eng': 'английская'}.get(self.lang, self.lang) or 'без подписи'
        return ('🔊 ' if self.kind == 'audio' else '💬 ') + t + (f' ({self.lang})' if self.title and self.lang else '')

    def can_send(self):
        return self.kind == 'audio' or bool(self.path) or self.codec in TEXT_SUB


def parse_streams(data):
    out, na, ns = [], 0, 0
    for s in data.get('streams', []):
        tags = {k.lower(): v for k, v in (s.get('tags') or {}).items()}
        if s.get('codec_type') == 'audio':
            out.append(Track('audio', title_of(tags), tags.get('language', ''), s.get('codec_name', ''), na))
            na += 1
        elif s.get('codec_type') == 'subtitle':
            out.append(Track('sub', title_of(tags), tags.get('language', ''), s.get('codec_name', ''), ns))
            ns += 1
    return out


PROBE = ['ffprobe', '-v', 'error', '-show_entries', 'stream=codec_type,codec_name:stream_tags=language,title,handler_name',
         '-of', 'json']
# MP4 хранит название дорожки в имени обработчика; штатные имена — не название
STOCK_HANDLER = re.compile(r'(?i)handler|^iso media|core media|gpac|mainconcept|l-smash|bento4|^mp4box|^apple|^$')


def title_of(tags):
    if tags.get('title', '').strip():
        return tags['title']
    h = tags.get('handler_name', '').strip()
    return '' if STOCK_HANDLER.search(h) else h


def local_streams(path):
    out = subprocess.run(PROBE + [path], capture_output=True, text=True).stdout
    try:
        return parse_streams(json.loads(out))
    except ValueError:
        return []


def sidecars_for(videos, files):
    """Отдельные файлы дорожек к сериям: рядом или в подпапках до трёх уровней."""
    by_dir = {}
    for v in videos:
        by_dir.setdefault(os.path.dirname(v), []).append(v)
    side = {v: [] for v in videos}
    for f in files:
        if not f.lower().endswith(AUDIO_EXT + SUB_EXT):
            continue
        d = os.path.dirname(f)
        for _ in range(SIDE_DEPTH + 1):
            hit = [v for v in by_dir.get(d, []) if side_matches(stem(f), stem(v))]
            for v in hit:
                side[v].append(f)
            up = os.path.dirname(d)
            if hit or up == d:
                break
            d = up
    return side


def tracks_of(video, sides, embedded):
    out = list(embedded)
    for f in sides:
        title, lang = side_label(video, f)
        kind = 'sub' if f.lower().endswith(SUB_EXT) else 'audio'
        out.append(Track(kind, title, lang, os.path.splitext(f)[1].lstrip('.'), path=f))
    return out


def agent_roots(remote):
    rc, out, _ = remote.run('cat ~/.config/habits-media-agent.conf')
    if rc != 0:
        sys.exit('не прочитать конфиг агента на приставке')
    roots = []
    for line in out.splitlines():
        line = line.strip()
        if line.startswith('#') or not line.lower().startswith('roots'):
            continue
        f = [p.strip() for p in line.split('=', 1)[1].split('|')]
        if len(f) >= 3 and f[2] in ('video', 'anime'):
            roots.append(f[0].rstrip('/'))
    return roots


def find_series(remote, name):
    roots = agent_roots(remote)
    listing = {}
    for root in roots:
        rc, out, _ = remote.run('ls -1 ' + shlex.quote(root))
        if rc == 0:
            for n in out.splitlines():
                if n and not n.startswith('.'):
                    listing.setdefault(n, []).append(root)
    exact = listing.get(name)
    if exact and len(exact) == 1:
        return exact[0] + '/' + name
    want = norm(re.sub(r'\(\d{4}\)', '', name))
    near = [n for n in listing if norm(n) == norm(name)] or \
           [n for n in listing if want and norm(re.sub(r'\(\d{4}\)', '', n)) == want]
    if len(near) == 1 and len(listing[near[0]]) == 1:
        return listing[near[0]][0] + '/' + near[0]
    similar = near or [n for n in listing if want and (want in norm(n) or norm(n) in want)]
    hint = ('\n  похожие: ' + ', '.join(sorted(similar)[:10])) if similar else ''
    sys.exit(f'на приставке нет сериала «{name}» — укажите --series "Имя на приставке"{hint}')


def remote_inventory(remote, root):
    rc, out, _ = remote.run('find ' + shlex.quote(root) + ' -type f -not -path "*/.*"')
    if rc != 0:
        sys.exit('не прочитать папку сериала на приставке')
    files = sorted(out.splitlines(), key=natural_key)
    videos = [f for f in files if f.lower().endswith(VIDEO_EXT)]
    # одна команда на все серии: по SSH-запросу на серию было бы долго
    script = 'for f in "$@"; do printf "@@%s\\n" "$f"; ' + ' '.join(map(shlex.quote, PROBE)) + ' "$f"; done'
    embedded = {}
    for chunk in range(0, len(videos), 50):
        part = videos[chunk:chunk + 50]
        rc, out, _ = remote.run('sh -c ' + shlex.quote(script) + ' _ ' + ' '.join(map(shlex.quote, part)))
        for block in out.split('@@')[1:]:
            path, _, body = block.partition('\n')
            try:
                embedded[path] = parse_streams(json.loads(body))
            except ValueError:
                embedded[path] = []
    side = sidecars_for(videos, files)
    return {v: tracks_of(v, side[v], embedded.get(v, [])) for v in videos}


def local_inventory(root, only):
    files = []
    for d, dirs, names in os.walk(root):
        dirs[:] = [x for x in dirs if not x.startswith('.')]
        files += [os.path.join(d, n) for n in names if not n.startswith('.')]
    videos = sorted((f for f in files if f.lower().endswith(VIDEO_EXT)), key=lambda p: natural_key(os.path.relpath(p, root)))
    if only:
        videos = [v for v in videos if only.lower() in os.path.relpath(v, root).lower()]
    side = sidecars_for(videos, files)
    return {v: tracks_of(v, side[v], local_streams(v)) for v in videos}


def side_name(box_video, tr, ext):
    """Имя на приставке: «S01E01.rus.LostFilm.mka» — агент прочтёт его как
    «LostFilm», язык rus."""
    parts = [stem(box_video)]
    if tr.lang:
        parts.append(tr.lang)
    title = re.sub(r'\s+', ' ', re.sub(r'[\\/:*?"<>|.\x00-\x1f]+', ' ', tr.title)).strip()[:60]
    if title:
        parts.append(title)
    if len(parts) == 1:
        parts.append('track' if tr.kind == 'audio' else 'subs')
    return '.'.join(parts) + ext


def prepare(tr, local_video, tmpdir):
    """Локальный файл для отправки и его расширение."""
    if tr.path:
        return tr.path, os.path.splitext(tr.path)[1].lower()
    if tr.kind == 'audio':
        out = os.path.join(tmpdir, 'track.mka')
        args = ['-map', f'0:a:{tr.n}', '-vn', '-sn', '-dn', '-c', 'copy', '-f', 'matroska', out]
        ext = '.mka'
    else:
        ext = TEXT_SUB[tr.codec]
        out = os.path.join(tmpdir, 'subs' + ext)
        codec, fmt = ('copy', 'ass') if ext == '.ass' else ('srt', 'srt')
        args = ['-map', f'0:s:{tr.n}', '-vn', '-an', '-dn', '-c:s', codec, '-f', fmt, out]
    r = subprocess.run(['ffmpeg', '-nostdin', '-v', 'error', '-y', '-i', local_video] + args,
                       capture_output=True, text=True)
    if r.returncode != 0:
        raise RuntimeError(r.stderr.strip()[-200:] or 'ffmpeg не справился')
    return out, ext


def reindex(remote):
    """Попросить агента обойти файлы сейчас. PIN берётся из конфига на самой
    приставке и сюда не приходит."""
    code = r"""
import os, urllib.request
cfg = {}
for line in open(os.path.expanduser('~/.config/habits-media-agent.conf'), encoding='utf-8'):
    if '=' in line and not line.strip().startswith('#'):
        k, v = line.split('=', 1); cfg.setdefault(k.strip(), v.strip())
addr = cfg.get('addr', '127.0.0.1:8410')
addr = '127.0.0.1' + addr if addr.startswith(':') else addr
req = urllib.request.Request('http://' + addr + cfg.get('base', '/tv').rstrip('/') + '/api/admin/index',
                             data=b'', method='POST', headers={'X-Admin-Pin': cfg.get('admin_pin', '')})
try:
    print(urllib.request.urlopen(req, timeout=15).status)
except Exception as e:
    print(getattr(e, 'code', 'нет ответа'))
"""
    cmd = 'python3 -c ' + shlex.quote(code)
    rc, out, _ = remote.run(cmd)
    return out.strip()


def main():
    ap = argparse.ArgumentParser(description=__doc__.split('\n')[0])
    ap.add_argument('folder', help='локальная папка сериала')
    ap.add_argument('--series', help='как сериал называется на приставке, если иначе, чем папка')
    ap.add_argument('--only', help='только серии, в пути которых есть это («S02», «Season 2»)')
    ap.add_argument('--tracks', help='только эти дорожки: части названий через запятую («lostfilm,eng»)')
    ap.add_argument('--no-subs', action='store_true', help='субтитры не трогать')
    ap.add_argument('--no-audio', action='store_true', help='озвучки не трогать')
    ap.add_argument('--send', action='store_true', help='довезти недостающее (без него — только отчёт)')
    args = ap.parse_args()

    folder = os.path.abspath(args.folder.rstrip('/'))
    if not os.path.isdir(folder):
        sys.exit(f'нет папки {folder}')
    print(f'Смотрю дорожки в {folder}…', flush=True)
    local = local_inventory(folder, args.only)
    if not local:
        sys.exit('в папке нет серий')

    rr = load('recode_remote', 'recode-remote.py')
    remote = rr.Remote(*rr.load_access())
    box_root = find_series(remote, args.series or os.path.basename(folder))
    print(f'На приставке: {box_root}', flush=True)
    box = remote_inventory(remote, box_root)
    box_by_ep = {}
    for v in box:
        k = episode_key(os.path.relpath(v, box_root))
        if k:
            box_by_ep.setdefault(k, []).append(v)

    wanted = [w.strip().lower() for w in (args.tracks or '').split(',') if w.strip()]
    missing = {}      # ключ дорожки → [(локальная серия, дорожка, серия на приставке)]
    names = {}
    unmatched = []
    for v, tracks in local.items():
        k = episode_key(os.path.relpath(v, folder))
        targets = box_by_ep.get(k, []) if k else []
        if not targets:
            stem_hit = [b for b in box if norm(stem(b)) == norm(stem(v))]
            targets = stem_hit
        if len(targets) != 1:
            unmatched.append(os.path.relpath(v, folder))
            continue
        target = targets[0]
        have = {t.key() for t in box[target]}
        for tr in tracks:
            key = tr.key()
            if key is None or key in have:
                continue
            if (tr.kind == 'sub' and args.no_subs) or (tr.kind == 'audio' and args.no_audio):
                continue
            label = tr.name().lower()
            if wanted and not any(w in label for w in wanted):
                continue
            names.setdefault(key, tr.name())
            missing.setdefault(key, []).append((v, tr, target))
            have.add(key)   # две одинаковые локальные дорожки — везём одну

    total = len(local)
    if unmatched:
        print(f'\nНе нашлось пары на приставке ({len(unmatched)}): ' + ', '.join(unmatched[:8]) +
              (' …' if len(unmatched) > 8 else ''))
    if not missing:
        print(f'\nВсё на месте: у всех {total - len(unmatched)} сопоставленных серий на приставке есть те же дорожки.')
        return
    print('\nНа приставке не хватает:')
    for key, items in missing.items():
        bad = sum(1 for _, tr, _ in items if not tr.can_send())
        note = f' — {bad} картинкой, файлом не отправить' if bad else ''
        print(f'  {names[key]}: в {len(items)} из {total} серий{note}')
    if not args.send:
        print('\nДовезти: добавьте --send (выбрать — --tracks lostfilm,eng; субтитры не трогать — --no-subs).')
        return

    sent = failed = 0
    with tempfile.TemporaryDirectory(prefix='send-tracks-') as tmp:
        for key, items in missing.items():
            for v, tr, target in items:
                if not tr.can_send():
                    continue
                part = ''
                try:
                    src, ext = prepare(tr, v, tmp)
                    dst = os.path.join(os.path.dirname(target), side_name(target, tr, ext))
                    part = os.path.join(os.path.dirname(dst), '.' + os.path.basename(dst) + '.part')
                    if remote.run('test -e ' + shlex.quote(dst))[0] == 0:
                        continue
                    if remote.push(src, part) != 0 or remote.size(part) != os.path.getsize(src):
                        raise RuntimeError('файл доехал не целиком')
                    remote.run('mv -n ' + shlex.quote(part) + ' ' + shlex.quote(dst))
                    sent += 1
                    print(f'  ✅ {os.path.relpath(dst, box_root)}', flush=True)
                except (RuntimeError, OSError) as e:
                    failed += 1
                    if part:
                        remote.run('rm -f ' + shlex.quote(part))
                    print(f'  ❌ {os.path.basename(v)} {names[key]}: {e}', flush=True)
                finally:
                    for f in os.listdir(tmp):
                        os.remove(os.path.join(tmp, f))
    print(f'\nОтправлено: {sent}, не вышло: {failed}.')
    if sent:
        code = reindex(remote)
        print('Агент обходит файлы — через минуту дорожки появятся в плеере.' if code == '200'
              else f'Агент не принял просьбу обойти файлы ({code or "нет ответа"}) — дорожки появятся после ближайшего обхода.')


if __name__ == '__main__':
    main()
