#!/bin/bash
# Сторож внешних дисков приставки: возвращает на место диск, который
# отвалился и вернулся. НУЖЕН ROOT — ставится disk-watch-install.sh.
#
# Зачем. Оба диска — USB, в fstab с nofail. Если мост на секунду
# переподключится (2026-11-02 07:48 так сделал JMicron с /mnt/media), ядро
# заново видит раздел, а systemd — нет: mount-юнит из fstab останавливается
# вместе с пропавшим устройством и сам больше не стартует. Точка монтирования
# пустеет, половина сериалов исчезает из каталога до перезагрузки.
#
# Каждые N секунд (служба habits-disk-watch.service, N=60), для каждой строки
# fstab с UUID= и точкой под /mnt/:
#   смонтировано                → ничего;
#   раздел есть, не смонтирован → systemctl start <юнит>;
#   раздела нет                 → ждём, пока подключат.
# В журнал пишется только смена состояния, а не каждая минута.
#
# fsck сам не запускает: чинить ФС без спроса опаснее, чем смонтировать её
# «грязной» — exFAT это переносит. Проверить руками: fsck.exfat -n (только
# читает). Агента перезапускать не нужно — каталог читает диск на каждый запрос.
#
# Служба с циклом, а не таймер systemd — как mybook-keepalive: таймер раз в
# минуту дал бы 1440 запусков в сутки и столько же пар строк в журнале.
#
#   habits-disk-watch        # один проход (проверить руками)
#   habits-disk-watch 60     # так её держит systemd
#
# Журнал: journalctl -t habits-disk-watch
set -u
EVERY=${1:-0}
TAG=habits-disk-watch
STATE=${STATE:-/run/habits-disk-watch}   # переопределяется для проверки без root
mkdir -p "$STATE"

# state <точка> <состояние> <сообщение> — пишет в журнал, только если
# состояние сменилось (иначе «диска нет» сыпалось бы раз в минуту)
state() {
    local f="$STATE/$(systemd-escape "$1")"
    [ "$(cat "$f" 2>/dev/null)" = "$2" ] && return
    echo "$2" > "$f"
    logger -t "$TAG" -- "$3"
    echo "$3"
}

check() {
grep -vE '^[[:space:]]*(#|$)' "${FSTAB:-/etc/fstab}" | while read -r spec target _; do
    case "$spec" in UUID=*) ;; *) continue ;; esac
    case "$target" in /mnt/*) ;; *) continue ;; esac
    uuid=${spec#UUID=}
    if mountpoint -q "$target"; then
        state "$target" ok "$target на месте"
        continue
    fi
    if [ ! -e "/dev/disk/by-uuid/$uuid" ]; then
        state "$target" missing "$target: диска $uuid нет — жду, когда подключат"
        continue
    fi
    # раздел уже смонтирован в другом месте (руками, через файловый менеджер) —
    # второй раз монтировать ту же exFAT нельзя
    other=$(findmnt -rno TARGET -S "UUID=$uuid" | head -1)
    if [ -n "$other" ]; then
        state "$target" elsewhere "$target: раздел $uuid смонтирован в $other — не трогаю"
        continue
    fi
    unit=$(systemd-escape -p --suffix=mount "$target")
    if systemctl start "$unit" 2>/dev/null && mountpoint -q "$target"; then
        state "$target" ok "$target: диск $uuid вернулся — смонтировал заново"
    else
        state "$target" failed "$target: раздел $uuid есть, но не монтируется: $(journalctl -u "$unit" -n 3 --no-pager -o cat | tr '\n' ' ')"
    fi
done
}

check
[ "$EVERY" -gt 0 ] 2>/dev/null || exit 0
while sleep "$EVERY"; do check; done
