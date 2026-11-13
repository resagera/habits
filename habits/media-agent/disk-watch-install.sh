#!/bin/bash
# Ставит сторожа дисков (disk-watch.sh) и сразу прогоняет его один раз —
# то есть заодно возвращает диск, который отвалился прямо сейчас.
# НУЖЕН ROOT:  sudo ~/bin/disk-watch-install.sh
# Убрать:      sudo systemctl disable --now habits-disk-watch.service
set -eu
[ "$(id -u)" = 0 ] || { echo "нужен root: sudo $0"; exit 1; }
HERE=$(dirname "$(readlink -f "$0")")
install -m 755 "$HERE/disk-watch.sh" /usr/local/sbin/habits-disk-watch

cat > /etc/systemd/system/habits-disk-watch.service <<'UNIT'
[Unit]
Description=Сторож внешних дисков медиаагента: смонтировать отвалившийся
After=local-fs.target

[Service]
ExecStart=/usr/local/sbin/habits-disk-watch 60
Restart=always
RestartSec=30

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable habits-disk-watch.service
echo "--- первый прогон ---"
/usr/local/sbin/habits-disk-watch
systemctl restart habits-disk-watch.service   # дальше — каждую минуту
echo "--- диски ---"
findmnt -no TARGET,SOURCE,FSTYPE /mnt/media /mnt/mybook || true
echo
echo "журнал сторожа: journalctl -t habits-disk-watch"
