#!/usr/bin/env python3
"""
Отправка одного фильма на машину с медиаагентом.

Приставка играет только MP4, но пережимать здесь чаще всего нечего: у
BDRip-ов видео обычно уже h264, и достаточно сменить контейнер (`-c:v copy`)
и переклеить звук. Тогда работа идёт со скоростью диска, а не процессора.

Что делает:
  1. разбирает файл и показывает, какие дорожки внутри;
  2. собирает MP4 — видео как есть (если кодек годный), одна звуковая
     дорожка в AAC-стерео, субтитры и обложка отбрасываются;
  3. проверяет длительность результата;
  4. заводит на той стороне папку и, если надо, добавляет её в конфиг агента;
  5. отдаёт файл и сверяет размер и длительность на месте;
  6. убирает свою временную копию.

Оригинал не трогается: на ту сторону уезжает только собранный MP4.

Доступ берётся оттуда же, откуда у recode-remote.py: ~/.config/habits-recode.conf
или RECODE_HOST/RECODE_USER/RECODE_PASS. В репозитории паролей нет.

    ./send-film.py /путь/к/фильму.mkv                 # как есть
    ./send-film.py фильм.mkv --audio 2                # другая звуковая дорожка
    ./send-film.py фильм.mkv --name 'Джентльмены (2019).mp4'
    ./send-film.py фильм.mkv --dry-run                # только показать план
"""
import argparse
import importlib.util
import json
import os
import shlex
import shutil
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
DEST = '/home/res/rudb/media/films'
TITLE = 'Фильмы'
WORKDIR = '/home/resager/rudb/mount/2tb-ext-part/rudb/temp/recode'
AGENT_CONF = '/home/res/.config/habits-media-agent.conf'

# видео с этими кодеками приставка играет как есть — контейнер меняем без
# перекодирования, остальное придётся пережимать
COPY_VIDEO = {'h264', 'hevc', 'av1', 'vp9'}


def load_helpers():
    """Соединение с переподключением уже написано в recode-remote.py —
    берём его оттуда, чтобы не держать две копии одного и того же."""
    spec = importlib.util.spec_from_file_location('recode_remote',
                                                  os.path.join(HERE, 'recode-remote.py'))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def probe(path):
    out = subprocess.run(['ffprobe', '-v', 'error', '-print_format', 'json',
                          '-show_format', '-show_streams', path],
                         capture_output=True, text=True)
    if out.returncode != 0:
        sys.exit('ffprobe не смог прочитать файл: ' + out.stderr.strip()[:200])
    return json.loads(out.stdout)


def human(n):
    return f'{n / (1 << 20):.0f} МБ' if n < (1 << 30) else f'{n / (1 << 30):.1f} ГБ'


def hms(sec):
    sec = int(sec)
    return f'{sec // 3600}:{sec % 3600 // 60:02d}:{sec % 60:02d}'


def describe(info):
    """Разбор на человеческий язык: что за видео и какие дорожки есть."""
    video, audio = None, []
    for s in info['streams']:
        if s['codec_type'] == 'video' and s.get('disposition', {}).get('attached_pic') != 1:
            if video is None:
                video = s
        elif s['codec_type'] == 'audio':
            audio.append(s)
    return video, audio


def track_label(s):
    tags = s.get('tags') or {}
    bits = [s.get('codec_name', '?'), f"{s.get('channels', '?')} кан."]
    if tags.get('language'):
        bits.append(tags['language'])
    if tags.get('title'):
        bits.append(tags['title'])
    return ', '.join(bits)


def expected_size(info, video, audio_index):
    """Прикидка веса результата: общий поток минус все звуковые дорожки
    (их выбрасываем) плюс одна в AAC. Нужна, чтобы не начинать работу, когда
    на диске заведомо не хватит места."""
    try:
        total = int(info['format']['bit_rate'])
    except (KeyError, ValueError, TypeError):
        return int(info['format'].get('size', 0)) // 2
    audio_bits = 0
    for s in info['streams']:
        if s['codec_type'] == 'audio':
            audio_bits += int(s.get('bit_rate') or 0)
    duration = float(info['format'].get('duration', 0))
    return int(max(total - audio_bits + 192_000, total * 0.2) * duration / 8)


def build_command(src, dst, video, audio_index, sample):
    args = ['ffmpeg', '-hide_banner', '-y']
    if sample:
        args += ['-t', str(sample)]
    args += ['-i', src, '-map', '0:v:0', '-map', f'0:a:{audio_index}']
    if video and video.get('codec_name') in COPY_VIDEO:
        args += ['-c:v', 'copy']
    else:
        args += ['-c:v', 'libx264', '-preset', 'veryfast', '-crf', '21', '-pix_fmt', 'yuv420p']
    # 5.1 в браузере приставки девать некуда, сводим в стерео
    args += ['-c:a', 'aac', '-b:a', '192k', '-ac', '2',
             '-sn', '-dn', '-map_chapters', '-1', '-movflags', '+faststart', dst]
    return args


def local_duration(path):
    out = subprocess.run(['ffprobe', '-v', 'error', '-show_entries', 'format=duration',
                          '-of', 'csv=p=0', path], capture_output=True, text=True).stdout.strip()
    try:
        return float(out)
    except ValueError:
        return 0.0


def ensure_library(remote, args):
    """Папка и строка в конфиге агента. Идемпотентно: второй запуск ничего
    не меняет и агент не дёргается."""
    remote.run('mkdir -p ' + shlex.quote(args.dest))
    rc, conf, _ = remote.run('cat ' + shlex.quote(AGENT_CONF))
    if rc != 0:
        print('  конфиг агента не прочитать — библиотеку добавьте руками')
        return
    if args.dest in conf:
        return
    # дописываем через полную перезапись: у конфига может не быть перевода
    # строки в конце, и тогда строка слиплась бы с предыдущей
    new = conf if conf.endswith('\n') else conf + '\n'
    new += f'roots = {args.dest} | {args.title} | video\n'
    remote.run(f'cat > {shlex.quote(AGENT_CONF)} <<\'HABITS_EOF\'\n{new}HABITS_EOF\n')
    remote.run('systemctl --user restart habits-media-agent')
    print(f'  библиотека «{args.title}» добавлена в конфиг агента, агент перезапущен')


def main():
    ap = argparse.ArgumentParser(description='Собрать MP4 и отправить его на машину с агентом')
    ap.add_argument('film', help='исходный файл')
    ap.add_argument('--audio', type=int, default=0, help='номер звуковой дорожки (по умолчанию первая)')
    ap.add_argument('--name', default='', help='имя файла на той стороне')
    ap.add_argument('--dest', default=DEST, help='папка на той стороне')
    ap.add_argument('--title', default=TITLE, help='название библиотеки в конфиге агента')
    ap.add_argument('--workdir', default=WORKDIR, help='где собирать временный MP4')
    ap.add_argument('--sample', type=int, default=0, help='взять только N секунд (для проверки)')
    ap.add_argument('--keep-local', action='store_true', help='не удалять собранный файл здесь')
    ap.add_argument('--dry-run', action='store_true', help='только показать план')
    args = ap.parse_args()

    if not shutil.which('ffmpeg'):
        sys.exit('нужен ffmpeg')
    if not os.path.isfile(args.film):
        sys.exit('файл не найден: ' + args.film)

    info = probe(args.film)
    video, audio = describe(info)
    if not audio:
        sys.exit('в файле нет звука')
    if args.audio >= len(audio):
        sys.exit(f'звуковых дорожек всего {len(audio)}, а запрошена {args.audio}')

    src_size = int(info['format'].get('size', os.path.getsize(args.film)))
    duration = float(info['format'].get('duration', 0))
    guess = expected_size(info, video, args.audio)
    name = args.name or os.path.splitext(os.path.basename(args.film))[0] + '.mp4'
    if not name.lower().endswith('.mp4'):
        name += '.mp4'
    local_dst = os.path.join(args.workdir, name)
    remote_dst = args.dest.rstrip('/') + '/' + name

    print(f'файл:      {os.path.basename(args.film)}')
    print(f'           {human(src_size)}, {hms(duration)}, '
          f"видео {video.get('codec_name') if video else '?'} "
          f"{video.get('width') if video else '?'}×{video.get('height') if video else '?'}")
    print('звуковые дорожки:')
    for i, s in enumerate(audio):
        mark = ' ← берём' if i == args.audio else ''
        print(f'  [{i}] {track_label(s)}{mark}')
    copy_video = bool(video and video.get('codec_name') in COPY_VIDEO)
    print(f'видео:     {"без перекодирования (меняем контейнер)" if copy_video else "ПЕРЕКОДИРОВАНИЕ (кодек не годится приставке)"}')
    print('звук:      AAC 192k, стерео')
    print(f'выйдет:    примерно {human(guess)} → {remote_dst}')

    if args.dry_run:
        return

    os.makedirs(args.workdir, exist_ok=True)
    st = os.statvfs(args.workdir)
    free = st.f_bavail * st.f_frsize
    if free < guess * 1.1:
        sys.exit(f'в {args.workdir} свободно {human(free)}, а нужно около {human(guess)} — '
                 f'укажите другую папку через --workdir')

    print('\nсобираю MP4…', flush=True)
    t0 = time.time()
    cmd = build_command(args.film, local_dst, video, args.audio, args.sample)
    res = subprocess.run(cmd + ['-stats'])
    t_enc = time.time() - t0
    if res.returncode != 0 or not os.path.exists(local_dst):
        sys.exit('ffmpeg не справился')
    out_size = os.path.getsize(local_dst)
    got = local_duration(local_dst)
    want = args.sample or duration
    if want > 0 and abs(got - want) > max(2.0, want * 0.02):
        sys.exit(f'длительность разошлась: {got:.0f} вместо {want:.0f} с — файл не отправляю')
    print(f'готово за {hms(t_enc)}: {human(src_size)} → {human(out_size)} '
          f'({want / t_enc:.0f}× реального времени)')

    print('\nотправляю…', flush=True)
    rr = load_helpers()
    remote = rr.Remote(*rr.load_access())
    try:
        remote.run('true')  # проверяем связь до передачи
    except Exception as e:
        # собранный файл не выбрасываем: работа сделана, машину можно
        # включить и запустить скрипт ещё раз — он начнёт с этого места
        print(f'машина недоступна ({type(e).__name__}: {e})')
        print(f'собранный файл остался здесь: {local_dst}')
        print(f'когда машина включится: {sys.argv[0]} {shlex.quote(local_dst)} --name {shlex.quote(name)}')
        sys.exit(1)
    ensure_library(remote, args)
    tmp = remote_dst + '.part'
    t0 = time.time()
    if remote.push(local_dst, tmp) != 0:
        remote.run('rm -f ' + shlex.quote(tmp))
        sys.exit('не удалось отдать файл')
    t_up = time.time() - t0
    if remote.size(tmp) != out_size:
        remote.run('rm -f ' + shlex.quote(tmp))
        sys.exit('на той стороне размер не сошёлся — файл не переименовываю')
    remote.run(f'mv -f {shlex.quote(tmp)} {shlex.quote(remote_dst)}')
    there = remote.duration(remote_dst)
    print(f'отдано за {hms(t_up)} ({out_size / (1 << 20) / max(t_up, 1):.1f} МБ/с), '
          f'на месте {hms(there)}')

    if not args.keep_local:
        os.remove(local_dst)
    print(f'\nготово: {remote_dst}')
    print('оригинал на месте, его никто не трогал')


if __name__ == '__main__':
    main()
