#!/usr/bin/env bash
# Сборка и деплой habits-agent (метрики для страницы Servers) на сервер
# как systemd-сервис на :9101. Токен генерируется при первом деплое и
# сохраняется в /etc/habits-agent.env (выводится в конце).
set -euo pipefail

cd "$(dirname "$0")/../../.."

SSH_KEY=habits.key
SERVER=root@45.129.196.26
SSH="ssh -i $SSH_KEY -o StrictHostKeyChecking=no $SERVER"

echo "==> 1/3 Сборка (linux/amd64, static)"
export PATH=/usr/local/go/bin:$PATH GOTOOLCHAIN=auto
(cd habits/agent && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags '-s -w' -o /tmp/habits-agent .)

echo "==> 2/3 Заливка и установка сервиса"
scp -i $SSH_KEY -o StrictHostKeyChecking=no /tmp/habits-agent "$SERVER:/tmp/habits-agent"
$SSH bash -s <<'EOF'
set -euo pipefail
if [ ! -f /etc/habits-agent.env ]; then
    echo "AGENT_TOKEN=$(head -c 24 /dev/urandom | base64 | tr -dc 'a-zA-Z0-9' | head -c 32)" > /etc/habits-agent.env
    chmod 600 /etc/habits-agent.env
fi
install -m 755 /tmp/habits-agent /usr/local/bin/habits-agent
cat > /etc/systemd/system/habits-agent.service <<'UNIT'
[Unit]
Description=Habits monitoring agent (/metrics on :9101)
After=network-online.target

[Service]
EnvironmentFile=/etc/habits-agent.env
ExecStart=/usr/local/bin/habits-agent
Restart=always
RestartSec=5
User=nobody
ProtectSystem=strict
ProtectHome=true
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable --now habits-agent
sleep 1
systemctl is-active habits-agent
# порт открыт только для docker-подсетей: из интернета агент недоступен,
# приложение ходит через gateway своей сети (обычно 172.19.0.1)
ufw allow from 172.16.0.0/12 to any port 9101 proto tcp comment "habits-agent (docker only)" >/dev/null
EOF

echo "==> 3/3 Проверка"
TOKEN=$($SSH "grep -oP 'AGENT_TOKEN=\K.*' /etc/habits-agent.env")
$SSH "curl -s -H 'Authorization: Bearer $TOKEN' localhost:9101/metrics | head -c 200"; echo
echo
echo "Адрес для страницы Servers (изнутри приложения): http://172.19.0.1:9101/metrics"
echo "Токен: $TOKEN"
