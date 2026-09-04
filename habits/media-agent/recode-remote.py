#!/usr/bin/env python3
"""
Перекодирование библиотеки удалённой машины силами этой.

Зачем так: на домашнем ПК с медиаагентом (res-i3, Core i3-6100U) x264 идёт
~8× реального времени, на дев-машине (i7-11370H) — ~24×. Гонять по гигабитному
вайфаю (~25 МБ/с) заметно дешевле, чем ждать втрое дольше: серия на 19 минут
там 145 с, а «скачать → перекодировать здесь → вернуть» — 75 с.

Сценарий на файл: забрать → перекодировать → отдать под временным именем →
переименовать → удалить оригинал на той стороне → стереть локальные копии.
Оригинал удаляется ТОЛЬКО после успешной проверки готового файла.

Дёшево и без пересылки делается прямо на той стороне: перепаковка контейнера
и переклейка звука (`-c:v copy`) — сеть там ничего не ускорит.

Доступ: RECODE_HOST/RECODE_USER/RECODE_PASS или ~/.config/habits-recode.conf
(host/user/password, права 0600). В репозитории паролей нет.

    ./recode-remote.py --dry-run            # что будет сделано
    ./recode-remote.py --limit 1            # один файл, для пробы
    ./recode-remote.py --only Discovery     # по подстроке пути
    ./recode-remote.py                      # вся библиотека
"""
import argparse
import os
import re
import shlex
import shutil
import subprocess
import sys
import time

try:
    import paramiko
except ImportError:
    sys.exit('нужен paramiko: apt install python3-paramiko')

CONFIG = os.path.expanduser('~/.config/habits-recode.conf')
WORKDIR = '/home/resager/rudb/mount/2tb-ext-part/rudb/temp/recode'
REMOTE_AGENT_CONF = '~/.config/habits-media-agent.conf'

# Те же таблицы, что у агента (media-agent/library.go): вердикт должен
# совпадать с тем, что показывает пульт, иначе будем «чинить» уже годное.
OK_VIDEO = {'h264', 'hevc', 'vp8', 'vp9', 'av1'}
OK_AUDIO = {'aac', 'mp3', 'opus', 'vorbis', 'flac'}
OK_CONTAINER = {'mp4', 'm4v', 'mov', 'webm', 'mp3', 'm4a', 'flac', 'ogg', 'opus', 'wav', 'aac'}
VIDEO_EXT = ['avi', 'mkv', 'mp4', 'm4v', 'mov', 'ts', 'webm', 'wmv', 'flv', 'mpg', 'mpeg']

# Запас на рабочей папке: исходник + результат + место на манёвр
MIN_FREE_GB = 3
# Свои временные файлы держим в подпапке: в рабочей папке могут лежать чужие
# файлы с теми же именами, и уборка после прогона снесла бы их
SPOOL = '.spool'


def load_access():
    cfg = {}
    if os.path.exists(CONFIG):
        for line in open(CONFIG, encoding='utf-8'):
            line = line.strip()
            if line and not line.startswith('#') and '=' in line:
                k, v = line.split('=', 1)
                cfg[k.strip()] = v.strip()
    host = os.environ.get('RECODE_HOST', cfg.get('host', ''))
    user = os.environ.get('RECODE_USER', cfg.get('user', ''))
    password = os.environ.get('RECODE_PASS', cfg.get('password', ''))
    if not (host and user and password):
        sys.exit(f'нет доступа: задайте RECODE_HOST/USER/PASS или {CONFIG}')
    return host, user, password


class Remote:
    """Одно SSH-соединение на весь прогон: на каждый файл их уходило бы три."""

    def __init__(self, host, user, password):
        self.cl = paramiko.SSHClient()
        self.cl.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        # без этого paramiko лезет в ssh-agent и роняет аутентификацию по паролю
        self.cl.connect(host, username=user, password=password, timeout=30,
                        allow_agent=False, look_for_keys=False)

    def run(self, cmd, timeout=7200):
        stdin, stdout, stderr = self.cl.exec_command(cmd, timeout=timeout)
        out = stdout.read().decode('utf-8', 'replace')
        err = stderr.read().decode('utf-8', 'replace')
        return stdout.channel.recv_exit_status(), out, err

    def pull(self, remote_path, local_path):
        """Скачивание через `cat`: SFTP на одном потоке даёт вдвое меньше."""
        stdin, stdout, stderr = self.cl.exec_command('cat ' + shlex.quote(remote_path), timeout=7200)
        size = 0
        with open(local_path, 'wb') as f:
            while True:
                chunk = stdout.read(1 << 20)
                if not chunk:
                    break
                f.write(chunk)
                size += len(chunk)
        rc = stdout.channel.recv_exit_status()
        return rc, size

    def push(self, local_path, remote_path):
        stdin, stdout, stderr = self.cl.exec_command('cat > ' + shlex.quote(remote_path), timeout=7200)
        with open(local_path, 'rb') as f:
            while True:
                chunk = f.read(1 << 20)
                if not chunk:
                    break
                stdin.write(chunk)
        stdin.flush()
        stdin.channel.shutdown_write()
        return stdout.channel.recv_exit_status()

    def size(self, path):
        rc, out, _ = self.run('stat -c %s ' + shlex.quote(path))
        return int(out.strip()) if rc == 0 and out.strip() else -1

    def duration(self, path):
        rc, out, _ = self.run('ffprobe -v error -show_entries format=duration -of csv=p=0 '
                              + shlex.quote(path))
        try:
            return float(out.strip())
        except ValueError:
            return 0.0


def video_roots(remote):
    """Папки берём из конфига самого агента: тогда мы чиним ровно то, что
    показывает приставка, и список не разъезжается при правке конфига."""
    rc, out, _ = remote.run('cat ' + REMOTE_AGENT_CONF)
    if rc != 0:
        sys.exit('не прочитать конфиг агента на той стороне')
    roots = []
    for line in out.splitlines():
        line = line.strip()
        if line.startswith('#') or not line.lower().startswith('roots'):
            continue
        fields = [p.strip() for p in line.split('=', 1)[1].split('|')]
        if len(fields) >= 3 and fields[2] == 'video':
            roots.append(fields[0].rstrip('/'))
    return roots


def scan(remote, roots):
    """Один удалённый обход с ffprobe: 322 отдельных SSH-команды заняли бы
    минуты только на рукопожатия."""
    names = ' -o '.join(f"-iname '*.{e}'" for e in VIDEO_EXT)
    parts = ' '.join(shlex.quote(r) for r in roots)
    cmd = f"""find {parts} -type f \\( {names} \\) -print0 | sort -z | while IFS= read -r -d '' f; do
        v=$(ffprobe -v error -select_streams v -show_entries stream=codec_name -of csv=p=0 "$f" 2>/dev/null | head -1)
        a=$(ffprobe -v error -select_streams a -show_entries stream=codec_name -of csv=p=0 "$f" 2>/dev/null | paste -sd, -)
        d=$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$f" 2>/dev/null)
        s=$(stat -c %s "$f")
        printf '%s\\t%s\\t%s\\t%s\\t%s\\n' "$f" "$v" "$a" "$d" "$s"
    done"""
    rc, out, err = remote.run(cmd, timeout=3600)
    if rc != 0 and not out:
        sys.exit('обход не удался: ' + err.strip()[:300])
    files = []
    for line in out.splitlines():
        cols = line.split('\t')
        if len(cols) != 5:
            continue
        path, vcodec, acodecs, dur, size = cols
        files.append({
            'path': path,
            'ext': os.path.splitext(path)[1].lstrip('.').lower(),
            'vcodec': vcodec,
            'acodecs': [a for a in acodecs.split(',') if a],
            'duration': float(dur) if dur else 0.0,
            'size': int(size) if size.isdigit() else 0,
        })
    return files


def verdict(f):
    """ok — играет как есть; remux — только контейнер; audio — ещё и звук;
    video — нужно настоящее перекодирование (единственный дорогой случай)."""
    level, why = 'ok', []
    rank = {'ok': 0, 'remux': 1, 'audio': 2, 'video': 3}

    def up(next_level):
        nonlocal level
        if rank[next_level] > rank[level]:
            level = next_level

    if f['vcodec']:
        if f['ext'] not in OK_CONTAINER:
            up('remux')
            why.append('контейнер ' + f['ext'])
        if f['vcodec'] not in OK_VIDEO:
            up('video')
            why.append('видео ' + f['vcodec'])
    for a in f['acodecs']:
        if a not in OK_AUDIO:
            up('audio')
            why.append('звук ' + a)
            break
    return level, ', '.join(why)


def target_path(path):
    return os.path.splitext(path)[0] + '.mp4'


def human(n):
    return f'{n / (1 << 20):.0f} МБ' if n < (1 << 30) else f'{n / (1 << 30):.1f} ГБ'


def hms(sec):
    sec = int(sec)
    return f'{sec // 3600}:{sec % 3600 // 60:02d}:{sec % 60:02d}'


def free_gb(path):
    st = os.statvfs(path)
    return st.f_bavail * st.f_frsize / (1 << 30)


def encode_local(src, dst, first_audio):
    """Кодирование здесь. Звук берём весь: у части серий две дорожки (два
    перевода), и молча выкидывать вторую нельзя. Больше двух каналов сводим
    в стерео — в браузере приставки многоканал всё равно некуда девать."""
    audio_map = '0:a:0' if first_audio else '0:a'
    cmd = ['ffmpeg', '-v', 'error', '-y', '-i', src,
           '-map', '0:v:0', '-map', audio_map,
           '-c:v', 'libx264', '-preset', 'veryfast', '-crf', '21', '-pix_fmt', 'yuv420p',
           '-c:a', 'aac', '-b:a', '160k', '-ac', '2',
           '-movflags', '+faststart', dst]
    return subprocess.run(cmd, capture_output=True, text=True)


def local_duration(path):
    out = subprocess.run(['ffprobe', '-v', 'error', '-show_entries', 'format=duration',
                          '-of', 'csv=p=0', path], capture_output=True, text=True).stdout.strip()
    try:
        return float(out)
    except ValueError:
        return 0.0


def do_remote_only(remote, f, level, args):
    """remux/audio — работа на копирование потоков: гонять файл по сети незачем."""
    src, dst = f['path'], target_path(f['path'])
    tmp = dst + '.part'
    acodec = 'copy' if all(a in OK_AUDIO for a in f['acodecs']) else 'aac'
    audio_map = '0:a:0' if args.first_audio else '0:a'
    cmd = (f'ffmpeg -v error -y -i {shlex.quote(src)} -map 0:v:0 -map {audio_map} '
           f'-c:v copy -c:a {acodec} ' + ('' if acodec == 'copy' else '-b:a 160k -ac 2 ')
           + f'-movflags +faststart {shlex.quote(tmp)}')
    rc, _, err = remote.run(cmd)
    if rc != 0:
        remote.run('rm -f ' + shlex.quote(tmp))
        return False, err.strip()[:200] or 'ffmpeg на той стороне вернул ошибку'
    if not check_remote(remote, tmp, f['duration']):
        remote.run('rm -f ' + shlex.quote(tmp))
        return False, 'результат не прошёл проверку'
    remote.run(f'mv -f {shlex.quote(tmp)} {shlex.quote(dst)}')
    if src != dst and not args.keep_originals:
        remote.run('rm -f ' + shlex.quote(src))
    return True, 'на месте, без пересылки'


def check_remote(remote, path, want_duration):
    size = remote.size(path)
    if size <= 0:
        return False
    if want_duration <= 0:
        return True
    got = remote.duration(path)
    return abs(got - want_duration) <= max(2.0, want_duration * 0.02)


def process(remote, f, level, args):
    src = f['path']
    dst = target_path(src)
    name = os.path.basename(src)
    spool = os.path.join(args.workdir, SPOOL)
    os.makedirs(spool, exist_ok=True)
    local_src = os.path.join(spool, name)
    local_dst = os.path.join(spool, os.path.splitext(name)[0] + '.mp4')

    if level in ('remux', 'audio'):
        return do_remote_only(remote, f, level, args)

    t0 = time.time()
    rc, size = remote.pull(src, local_src)
    if rc != 0 or size != f['size']:
        os.path.exists(local_src) and os.remove(local_src)
        return False, f'не скачался (rc={rc}, {size} из {f["size"]} байт)'
    t_down = time.time() - t0

    t0 = time.time()
    res = encode_local(local_src, local_dst, args.first_audio)
    t_enc = time.time() - t0
    if res.returncode != 0 or not os.path.exists(local_dst):
        cleanup(local_src, local_dst)
        return False, 'ffmpeg: ' + (res.stderr.strip()[:200] or 'ошибка')

    got = local_duration(local_dst)
    if f['duration'] > 0 and abs(got - f['duration']) > max(2.0, f['duration'] * 0.02):
        cleanup(local_src, local_dst)
        return False, f'длительность разошлась: {got:.0f} против {f["duration"]:.0f} с'

    # сначала под временным именем: оборванная передача не должна оставить
    # в библиотеке половину серии
    t0 = time.time()
    tmp = dst + '.part'
    if remote.push(local_dst, tmp) != 0 or not check_remote(remote, tmp, f['duration']):
        remote.run('rm -f ' + shlex.quote(tmp))
        cleanup(local_src, local_dst)
        return False, 'не отдался обратно'
    remote.run(f'mv -f {shlex.quote(tmp)} {shlex.quote(dst)}')
    t_up = time.time() - t0

    if src != dst and not args.keep_originals:
        remote.run('rm -f ' + shlex.quote(src))
    out_size = os.path.getsize(local_dst)
    cleanup(local_src, local_dst)
    speed = f['duration'] / t_enc if t_enc > 0 else 0
    return True, (f'скачал {t_down:.0f} с, кодировал {t_enc:.0f} с ({speed:.0f}× реального), '
                  f'отдал {t_up:.0f} с, {human(f["size"])} → {human(out_size)}')


def cleanup(*paths):
    for p in paths:
        try:
            os.path.exists(p) and os.remove(p)
        except OSError:
            pass


def main():
    ap = argparse.ArgumentParser(description='Перекодировать библиотеку удалённой машины здесь')
    ap.add_argument('--workdir', default=WORKDIR,
                    help=f'рабочая папка; свои файлы кладём в её подпапку {SPOOL}')
    ap.add_argument('--limit', type=int, default=0, help='обработать не больше N файлов')
    ap.add_argument('--only', default='', help='только пути с этой подстрокой')
    ap.add_argument('--dry-run', action='store_true', help='ничего не менять, показать план')
    ap.add_argument('--keep-originals', action='store_true', help='не удалять исходники')
    ap.add_argument('--first-audio', action='store_true', help='оставлять только первую звуковую дорожку')
    args = ap.parse_args()

    if not shutil.which('ffmpeg'):
        sys.exit('нужен ffmpeg')
    os.makedirs(args.workdir, exist_ok=True)

    remote = Remote(*load_access())
    roots = video_roots(remote)
    if not roots:
        sys.exit('в конфиге агента нет видеопапок')
    print('папки:', ', '.join(roots))

    files = scan(remote, roots)
    todo = []
    for f in files:
        level, why = verdict(f)
        if level == 'ok':
            continue
        if args.only and args.only.lower() not in f['path'].lower():
            continue
        todo.append((f, level, why))
    if args.limit:
        todo = todo[:args.limit]

    total_dur = sum(f['duration'] for f, _, _ in todo)
    total_size = sum(f['size'] for f, _, _ in todo)
    heavy = [t for t in todo if t[1] == 'video']
    print(f'всего файлов: {len(files)}, к переработке: {len(todo)} '
          f'({human(total_size)}, {total_dur / 3600:.1f} ч), из них перекодировать: {len(heavy)}')
    if heavy:
        # 24× реального времени — замер на этой машине, x264 veryfast
        print(f'ожидаемое время кодирования: около {hms(sum(f["duration"] for f, _, _ in heavy) / 24)}')

    if args.dry_run:
        for f, level, why in todo:
            print(f'  [{level}] {f["path"]} — {why}')
        return

    ok = failed = 0
    started = time.time()
    for n, (f, level, why) in enumerate(todo, 1):
        if free_gb(args.workdir) < MIN_FREE_GB:
            print(f'мало места в {args.workdir} ({free_gb(args.workdir):.1f} ГБ) — останавливаюсь')
            break
        print(f'[{n}/{len(todo)}] {os.path.basename(f["path"])} [{level}: {why}]', flush=True)
        try:
            done, note = process(remote, f, level, args)
        except KeyboardInterrupt:
            print('  прервано, чищу временные файлы')
            spool = os.path.join(args.workdir, SPOOL)
            cleanup(os.path.join(spool, os.path.basename(f['path'])),
                    os.path.join(spool, os.path.splitext(os.path.basename(f['path']))[0] + '.mp4'))
            break
        except Exception as e:  # соединение, диск, что угодно — файл пропускаем
            done, note = False, f'{type(e).__name__}: {e}'
        print(('  готово: ' if done else '  ОШИБКА: ') + note, flush=True)
        ok += done
        failed += not done

    print(f'\nитог: готово {ok}, с ошибками {failed}, времени {hms(time.time() - started)}')


if __name__ == '__main__':
    main()
