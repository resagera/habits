#!/usr/bin/env bash
# Бэкап Habits на сервере: дамп Postgres + данные приложения (загруженные
# фоны) + развёрнутый код/конфиги из /opt/habits. Складывает архив в
# /opt/backups на сервере и скачивает копию в habits/backups/ локально.
#
# Запуск: bash habits/deployment/streaks/backup.sh
set -euo pipefail

cd "$(dirname "$0")/../../.."

SSH_KEY=habits.key
SERVER=root@45.129.196.26
SSH="ssh -i $SSH_KEY -o StrictHostKeyChecking=no $SERVER"

STAMP=$(date +%Y%m%d-%H%M%S)
NAME="habits-backup-$STAMP"

echo "==> 1/3 Бэкап на сервере ($NAME)"
$SSH bash -s <<EOF
set -euo pipefail
mkdir -p /opt/backups
WORK=\$(mktemp -d)
trap 'rm -rf "\$WORK"' EXIT

# 1) дамп БД (внутри контейнера postgres)
docker exec habits-db-1 pg_dump -U streaks --clean --if-exists streaks | gzip > "\$WORK/db.sql.gz"

# 2) данные приложения (volume appdata: загруженные фоны и т.п.)
docker run --rm -v habits_appdata:/data -v "\$WORK":/backup alpine \
    tar czf /backup/appdata.tar.gz -C /data .

# 3) развёрнутый код и конфиги (бинарник, статика, compose, .env)
tar czf "\$WORK/opt-habits.tar.gz" -C /opt habits

tar czf "/opt/backups/$NAME.tar.gz" -C "\$WORK" db.sql.gz appdata.tar.gz opt-habits.tar.gz
ls -lh "/opt/backups/$NAME.tar.gz"

# храним последние 10 бэкапов
ls -t /opt/backups/habits-backup-*.tar.gz | tail -n +11 | xargs -r rm -f
EOF

echo "==> 2/3 Скачивание копии локально"
mkdir -p habits/backups
scp -i $SSH_KEY -o StrictHostKeyChecking=no "$SERVER:/opt/backups/$NAME.tar.gz" habits/backups/
ls -lh "habits/backups/$NAME.tar.gz"

echo "==> 3/3 Проверка архива"
tar tzf "habits/backups/$NAME.tar.gz"
echo "==> Бэкап готов: /opt/backups/$NAME.tar.gz (сервер) и habits/backups/$NAME.tar.gz (локально)"
