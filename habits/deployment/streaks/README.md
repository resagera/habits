# Деплой streaks/habits

Прод: **https://telegram.resager.ru/app/habits/** (сервер 45.129.196.26, Ubuntu 24.04).

```bash
./deploy.sh                 # обычный деплой (сборка → заливка → compose up → Caddy reload → проверки)
BOT_TOKEN=... ./deploy.sh   # только первый деплой: создаст /opt/habits/.env
```

Доступ — по ключу `habits.key` из корня репозитория (публичный ключ добавлен в
`authorized_keys` root на сервере).

## Как это устроено на сервере

- На сервере уже работает стек стриминга `home-stream`
  (`/home/stream/home-stream`, docker compose: Caddy 80/443, backend, LiveKit,
  Redis). **Деплой habits его не пересобирает и не перезапускает.**
- habits живёт отдельным compose-стеком в `/opt/habits`:
  `habits-db-1` (postgres:17-alpine, volume `habits_pgdata`) +
  `habits-app-1` (Go-бинарник, раздаёт и API `/api/v1`, и статику фронтенда).
  Наружу порты не публикуются.
- Контейнер `habits-app-1` дополнительно подключён к сети `home-stream_default`
  под алиасом `habits-backend` — так до него дотягивается Caddy стриминга.
- Роутинг — управляемый блок в `/home/stream/home-stream/Caddyfile`
  (между маркерами `# --- habits begin/end ---`, скрипт заменяет его целиком):
  - `telegram.resager.ru`: `handle_path /app/habits/*` → `habits-backend:8081`
    (префикс срезается), всё остальное — редирект на `/app/habits/`;
  - `resager.ru`, `www.resager.ru`: статическая заглушка (inline `respond`).
  - TLS-сертификаты Caddy выпускает сам (Let's Encrypt).

## Важные грабли

- `Caddyfile` вмонтирован в контейнер **как файл**: править его через
  `sed -i`/`mv` нельзя — заменится inode и контейнер перестанет видеть
  изменения. Скрипт пишет через `cat >` и делает reload с копии,
  скопированной внутрь контейнера (`docker cp` + `caddy reload --config
  /tmp/Caddyfile.deploy`) — рестарт Caddy не нужен, стриминг не прерывается.
- Перед reload всегда выполняется `caddy validate`; бэкап Caddyfile кладётся
  рядом (`Caddyfile.bak.<timestamp>`).
- `/opt/habits/.env` (BOT_TOKEN, POSTGRES_PASSWORD) создаётся один раз и при
  деплоях не перезаписывается.

## Проверка после деплоя

Скрипт сам проверяет: стриминг (`event.resager.ru`), `/app/habits/healthz`,
приложение и заглушку. Вручную:

```bash
curl https://telegram.resager.ru/app/habits/healthz     # ok
curl -s https://telegram.resager.ru/app/habits/api/v1/me # 401 вне Telegram — норма
```

Mini App открывается внутри Telegram: URL веб-приложения у бота (@BotFather →
Bot Settings → Menu Button / Web App) должен указывать на
`https://telegram.resager.ru/app/habits/`.
