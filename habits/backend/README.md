# Streaks backend

Новый бэкенд для Telegram Mini App «Habits» (этап 1 — Tracker).
Go 1.25 (stdlib `net/http` ServeMux) + Postgres (`pgx/v5`), миграции — `goose` из `embed.FS`, применяются автоматически при старте.

Старый бэкенд `habits/bakend` не затронут и может работать параллельно (:8080/:8676); этот сервис слушает **:8081**.

## Запуск локально

```bash
# 1. Postgres должен быть доступен, база создаётся один раз:
docker exec core-postgres-1 psql -U postgres -c "CREATE DATABASE streaks;"

# 2. Сервер (dev-режим, без валидации Telegram):
make run
# или явно:
DATABASE_URL='postgres://postgres:postgres@localhost:5432/streaks?sslmode=disable' \
  DEV_AUTH_BYPASS=true DEV_USER_ID=1 ADDR=:8081 go run ./cmd/server
```

Переменные окружения — см. `.env.example`. В продакшене обязательно задать `BOT_TOKEN`
и **не** задавать `DEV_AUTH_BYPASS`.

## Авторизация

Каждый запрос к `/api/v1/*` несёт заголовок `Authorization: tma <initData>`,
где `initData` — строка `Telegram.WebApp.initData`. Подпись проверяется HMAC-SHA256
по алгоритму Telegram (`internal/auth/telegram.go`), initData старше 24ч отклоняется.
Пользователь апсертится в таблицу `users` при первом запросе.

## API `/api/v1`

| Метод и путь | Тело | Ответ |
|---|---|---|
| `GET /me` | — | `{"id":1,"username":"...","first_name":"..."}` |
| `GET /tracker/categories` | — | `{"categories":[{"id","name","color","position"}]}` |
| `POST /tracker/categories` | `{"name","color?"}` | `201 {"category":{...}}` |
| `PATCH /tracker/categories/{id}` | `{"name?","color?","position?"}` | `{"category":{...}}` |
| `DELETE /tracker/categories/{id}` | — | `204` |
| `GET /tracker/marks?from=&to=[&category_id=]` | — | `{"marks":[{"category_id","days":["YYYY-MM-DD"]}]}` |
| `POST /tracker/marks/toggle` | `{"category_id","day"}` | `{"marked":true\|false}` |

Ошибки: `{"error":{"code","message"}}` — `invalid_request` 400, `unauthorized` 401,
`not_found` 404, `conflict` 409 (дубликат имени категории), `internal` 500.

## Структура

```
cmd/server/          точка входа: config → migrate → pool → router
internal/config/     env-конфиг
internal/auth/       валидация initData + middleware
internal/store/      запросы к Postgres (pgx)
internal/httpapi/    роутер, хендлеры, формат ошибок
internal/migrations/ goose-миграции (embed), 000N_<app>.sql на приложение
```

Следующие мини-приложения (checker, diary, metrics, …) добавляются по шаблону:
миграция `0002_checker.sql` + `internal/store/checker.go` + `internal/httpapi/checker.go`.
