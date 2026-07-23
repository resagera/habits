#!/usr/bin/env bash
# Деплой streaks/habits на прод-сервер.
#
# Что делает:
#   1. Собирает бэкенд (linux/amd64, CGO off) и фронтенд (base /app/habits/).
#   2. Заливает сборку в /opt/habits на сервере (ssh-ключ habits.key).
#   3. Поднимает изолированный docker-compose стек (postgres + app).
#   4. Идемпотентно добавляет блок habits в Caddyfile стриминга и делает
#      GRACEFUL reload Caddy (стриминг не прерывается; перед reload — validate).
#   5. Проверяет здоровье и habits, и стриминга.
#
# Стек стриминга (home-stream) скрипт не пересобирает и не перезапускает.
#
# Использование:
#   ./deploy.sh            # обычный деплой
#   BOT_TOKEN=... ./deploy.sh   # первый деплой (создаст /opt/habits/.env)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
BACKEND_DIR="$REPO_ROOT/habits/backend"
FRONTEND_DIR="$REPO_ROOT/habits/frontend/app"
DEPLOY_DIR="$REPO_ROOT/habits/deployment/streaks"

SSH_KEY="$REPO_ROOT/habits.key"
SERVER="root@45.129.196.26"
SSH="ssh -i $SSH_KEY -o BatchMode=yes $SERVER"
REMOTE_DIR="/opt/habits"

CADDYFILE="/home/stream/home-stream/Caddyfile"
CADDY_CONTAINER="home-stream-caddy-1"
STREAM_URL="https://event.resager.ru"
HABITS_URL="https://telegram.resager.ru/app/habits"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

export PATH=/usr/local/go/bin:$PATH GOTOOLCHAIN=auto

echo "==> 1/6 Сборка бэкенда (linux/amd64)"
(cd "$BACKEND_DIR" && go test ./... >/dev/null && \
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' \
    -o "$STAGE/server" ./cmd/server)

echo "==> 2/6 Сборка фронтенда"
(cd "$FRONTEND_DIR" && npm run --silent build)
cp -r "$FRONTEND_DIR/dist" "$STAGE/public"
cp "$DEPLOY_DIR/Dockerfile" "$DEPLOY_DIR/docker-compose.yml" "$DEPLOY_DIR/Caddyfile.habits" "$STAGE/"

echo "==> 3/6 Заливка на сервер ($REMOTE_DIR)"
$SSH "mkdir -p $REMOTE_DIR"
tar -C "$STAGE" -czf - . | $SSH "tar -C $REMOTE_DIR -xzf -"

echo "==> 4/6 .env и запуск стека"
$SSH "cd $REMOTE_DIR && \
  if [ ! -f .env ]; then \
    if [ -z '${BOT_TOKEN:-}' ]; then echo 'ERROR: первый деплой — передайте BOT_TOKEN=... ./deploy.sh' >&2; exit 1; fi; \
    umask 077; \
    { echo \"POSTGRES_PASSWORD=\$(openssl rand -hex 24)\"; echo 'BOT_TOKEN=${BOT_TOKEN:-}'; } > .env; \
    echo '  создан новый .env'; \
  fi && \
  docker compose up -d --build --wait"

echo "==> 5/6 Обновление Caddyfile (idempotent) + graceful reload"
$SSH "set -e
  cp $CADDYFILE $CADDYFILE.bak.\$(date +%Y%m%d%H%M%S)
  # Собираем новый конфиг: старый минус прежний habits-блок, плюс актуальный.
  # Пишем через cat > (truncate), а не mv/sed -i: файл вмонтирован в контейнер
  # Caddy, замена inode отвяжет bind mount.
  tmp=\$(mktemp)
  sed '/^# --- habits begin/,/^# --- habits end/d' $CADDYFILE > \$tmp
  cat $REMOTE_DIR/Caddyfile.habits >> \$tmp
  cat \$tmp > $CADDYFILE
  rm -f \$tmp
  # Reload делаем с копии внутри контейнера (docker cp), а не с bind mount —
  # это работает даже если mount уже отвязан, и не требует рестарта Caddy.
  docker cp $CADDYFILE $CADDY_CONTAINER:/tmp/Caddyfile.deploy
  docker exec $CADDY_CONTAINER caddy validate --adapter caddyfile --config /tmp/Caddyfile.deploy
  docker exec $CADDY_CONTAINER caddy reload --adapter caddyfile --config /tmp/Caddyfile.deploy"

echo "==> 6/6 Проверки"
check() {
  local name="$1" url="$2"
  local code
  code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 15 "$url") || code=fail
  echo "  $name: HTTP $code ($url)"
  case "$code" in 200|301|302|307|308) ;; *) echo "  !! ПРОВЕРКА ПРОВАЛЕНА"; exit 1;; esac
}
sleep 3
check "стриминг      " "$STREAM_URL"
check "habits healthz" "$HABITS_URL/healthz"
check "habits app    " "$HABITS_URL/"
check "заглушка      " "https://resager.ru/"

echo "==> Деплой завершён успешно"
