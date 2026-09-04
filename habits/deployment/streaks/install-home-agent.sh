#!/usr/bin/env bash
# Установка habits-agent на ДОМАШНЮЮ машину (без внешнего IP) в push-режиме:
# агент сам шлёт метрики на бэкенд раз в минуту, входящие порты не нужны.
#
# Запускать локально на самой машине (нужны sudo и Go для сборки):
#   ./install-home-agent.sh <PUSH_TOKEN> [PUSH_URL] [LOCAL_ADDR]
#
#   PUSH_TOKEN — токен из приложения: Servers → «＋ Добавить» → «Домашняя машина»
#   PUSH_URL   — endpoint бэкенда (по умолчанию прод)
#   LOCAL_ADDR — опционально, например :9102 — дополнительно отдавать
#                GET /metrics локально (для отладки); пусто = не слушать
set -euo pipefail

TOKEN="${1:-}"
PUSH_URL="${2:-https://telegram.resager.ru/app/habits/api/v1/agent/push}"
LOCAL_ADDR="${3:-}"

if [ -z "$TOKEN" ]; then
    echo "Использование: $0 <PUSH_TOKEN> [PUSH_URL] [LOCAL_ADDR]" >&2
    echo "Токен выдаёт приложение: Servers → ＋ Добавить → Домашняя машина" >&2
    exit 1
fi

cd "$(dirname "$0")/../.."   # → habits/

echo "==> 1/3 Сборка агента"
if ! command -v go >/dev/null; then
    echo "Нужен Go (https://go.dev/dl) — агент собирается из habits/agent" >&2
    exit 1
fi
(cd agent && CGO_ENABLED=0 go build -ldflags '-s -w' -o /tmp/habits-agent .)

echo "==> 2/3 Установка systemd-сервиса (потребуется sudo)"
sudo install -m 755 /tmp/habits-agent /usr/local/bin/habits-agent
sudo tee /etc/habits-agent.env >/dev/null <<ENV
AGENT_TOKEN=$TOKEN
AGENT_PUSH_URL=$PUSH_URL
${LOCAL_ADDR:+AGENT_ADDR=$LOCAL_ADDR}
ENV
sudo chmod 600 /etc/habits-agent.env
sudo tee /etc/systemd/system/habits-agent.service >/dev/null <<'UNIT'
[Unit]
Description=Habits monitoring agent (push mode)
After=network-online.target
Wants=network-online.target

[Service]
EnvironmentFile=/etc/habits-agent.env
ExecStart=/usr/local/bin/habits-agent
Restart=always
RestartSec=10
User=nobody
ProtectSystem=strict
ProtectHome=true
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
UNIT
sudo systemctl daemon-reload
sudo systemctl enable --now habits-agent

echo "==> 3/3 Проверка"
sleep 2
sudo systemctl is-active habits-agent
sudo journalctl -u habits-agent -n 5 --no-pager
echo
echo "Готово: агент шлёт отчёты раз в минуту на $PUSH_URL"
echo "Карточка машины в приложении наполнится в течение минуты."
