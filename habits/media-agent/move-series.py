#!/usr/bin/env python3
"""Переезд всех сериалов со старого диска на My Book — без рук, по таймеру.

Запускается НА res-i3 (разовым таймером systemd, см. HANDOFF.md). Сам ничего
не копирует: ставит все папки старой библиотеки в очередь переезда агента —
ровно то, что делает кнопка «Перенести всё» в админке → Задачи → «📦 Переезд
на другой диск». Агент везёт по одной папке, сверяет размеры, переносит
память (позиции, отметки, постеры) и только потом удаляет исходник.

Скрипт ждёт, пока очередь опустеет, и пишет итог в Telegram. PIN админки
берёт из конфига агента и никуда не выводит.

    ./move-series.py                       # «Сериалы» → «Сериалы (новый диск)»
    ./move-series.py --dry-run             # только показать, что поедет
    ./move-series.py --from "…" --to "…"   # другие библиотеки (по заголовку)
"""

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request

CONFIG = os.path.expanduser('~/.config/habits-media-agent.conf')
CACHE = os.path.expanduser('~/.cache/habits-media')
NOTIFY = 'https://telegram.resager.ru/app/habits/api/v1/tv/notify'


def log(msg):
    print(time.strftime('%H:%M:%S'), msg, flush=True)


def conf():
    out = {}
    with open(CONFIG, encoding='utf-8') as f:
        for line in f:
            line = line.strip()
            if line and not line.startswith('#') and '=' in line:
                k, v = line.split('=', 1)
                out.setdefault(k.strip(), v.strip())
    return out


class Agent:
    def __init__(self, cfg):
        addr = cfg.get('addr', '127.0.0.1:8410')
        if addr.startswith(':'):
            addr = '127.0.0.1' + addr
        self.api = 'http://' + addr + cfg.get('base', '/tv').rstrip('/') + '/api'
        self.pin = cfg.get('admin_pin', '')

    def call(self, method, path, body=None, patience=60):
        data = json.dumps(body).encode() if body is not None else None
        last = None
        for _ in range(patience):
            req = urllib.request.Request(self.api + path, data=data, method=method,
                                         headers={'Content-Type': 'application/json',
                                                  'X-Admin-Pin': self.pin})
            try:
                with urllib.request.urlopen(req, timeout=30) as r:
                    return json.loads(r.read().decode())
            except urllib.error.HTTPError as e:
                # ответ агента с ошибкой — повторять бессмысленно
                try:
                    msg = json.loads(e.read().decode()).get('error', '')
                except ValueError:
                    msg = ''
                raise SystemExit(f'агент ответил {e.code} на {path}: {msg}')
            except (urllib.error.URLError, OSError, ValueError) as e:
                last = e
                time.sleep(5)
        raise SystemExit(f'агент не ответил на {path}: {last}')


def notify(text):
    """Одно сообщение за прогон — у прода лимит уведомлений в час."""
    try:
        with open(os.path.join(CACHE, 'room'), encoding='utf-8') as f:
            room = f.read().strip()
        req = urllib.request.Request(NOTIFY, data=json.dumps({'room': room, 'text': text}).encode(),
                                     headers={'Content-Type': 'application/json'})
        with urllib.request.urlopen(req, timeout=15) as r:
            log(f'уведомление отправлено ({r.status})')
    except (OSError, urllib.error.URLError) as e:
        log(f'уведомление не ушло: {e}')


def gb(n):
    return f'{n / 2**30:.0f} ГБ'


def main():
    ap = argparse.ArgumentParser(description=__doc__.split('\n')[0])
    ap.add_argument('--from', dest='src', default='Сериалы', help='заголовок библиотеки-источника')
    ap.add_argument('--to', dest='dst', default='Сериалы (новый диск)', help='заголовок приёмника')
    ap.add_argument('--dry-run', action='store_true')
    ap.add_argument('--no-notify', action='store_true')
    args = ap.parse_args()

    ag = Agent(conf())
    if not ag.pin:
        raise SystemExit('в конфиге агента нет admin_pin')
    state = ag.call('GET', '/admin/migrate')
    libs = {l['title']: l for l in state.get('libs') or []}
    src, dst = libs.get(args.src), libs.get(args.dst)
    if not src or not dst:
        raise SystemExit(f'нет библиотеки «{args.src}» или «{args.dst}»; есть: {", ".join(libs)}')
    if not (src.get('available') and dst.get('available')):
        text = f'⚠ Переезд сериалов не начат: диск «{args.src if not src.get("available") else args.dst}» недоступен.'
        log(text)
        if not args.no_notify:
            notify(text)
        sys.exit(1)

    cand = ag.call('GET', f'/admin/migrate?src={src["id"]}&dst={dst["id"]}')
    items = cand.get('items') or []
    total = sum(i.get('size', 0) for i in items)
    log(f'«{args.src}» → «{args.dst}»: папок {len(items)}, {gb(total)}, свободно на приёмнике {gb(dst.get("free", 0))}')
    for i in items:
        log(f'   {i.get("name")}  {gb(i.get("size", 0))}' + ('  — уже есть на приёмнике, не поедет' if i.get('clash') else ''))
    if args.dry_run:
        return
    if total > dst.get('free', 0):
        raise SystemExit('на приёмнике не хватает места')

    started = time.time()
    out = ag.call('POST', '/admin/migrate', {'src': src['id'], 'dst': dst['id']})
    log(f'в очереди: {out.get("added")}; пропущено: {out.get("skipped") or "—"}')

    # ждём, пока агент не довезёт всё: очередь пуста и задач нет
    idle = 0
    while True:
        time.sleep(60)
        st = ag.call('GET', '/admin/migrate')
        if st.get('queue') or st.get('busy'):
            idle = 0
            continue
        idle += 1
        if idle >= 2:   # два круга подряд тихо — последняя задача точно закончилась
            break

    left = ag.call('GET', f'/admin/migrate?src={src["id"]}&dst={dst["id"]}').get('items') or []
    mins = int((time.time() - started) / 60)
    moved = len(items) - len(left)
    if left:
        text = (f'⚠ Переезд сериалов: перевезено {moved} из {len(items)} за {mins // 60}:{mins % 60:02d}. '
                f'Остались на старом диске: {", ".join(i.get("name", "?") for i in left)} — '
                f'подробности в админке → Задачи.')
    else:
        text = (f'✅ Сериалы переехали на My Book: {moved} папок, {gb(total)} за {mins // 60}:{mins % 60:02d}. '
                f'Старый диск пуст — строку «Сериалы» можно убрать из конфига агента.')
    log(text)
    if not args.no_notify:
        notify(text)


if __name__ == '__main__':
    main()
