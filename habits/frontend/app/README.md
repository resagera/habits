# Streaks frontend

Новый фронтенд Telegram Mini App «Habits»: Vue 3 + Vite + TypeScript.
Каждое мини-приложение — отдельный лениво загружаемый роут (`src/apps/<name>/`),
чанки собираются раздельно. Этап 1 реализован Tracker, остальные вкладки — заглушки.

Старый фронтенд `habits/frontend/webapp` не затронут.

## Запуск

```bash
npm install
npm run dev     # http://localhost:5173, /api проксируется на бэкенд :8081
npm run build   # прод-сборка в dist/
```

Бэкенд должен быть запущен (`make run` в `habits/backend`). Вне Telegram
(в обычном браузере, dev-режим) клиент шлёт `Authorization: tma dev` —
бэкенд принимает это только с `DEV_AUTH_BYPASS=true`.

## Структура

```
src/
  main.ts                  init Telegram SDK + тема + mount
  router/index.ts          ленивые роуты всех мини-приложений
  shared/
    telegram.ts            типизированный wrapper над Telegram.WebApp
    api/client.ts          fetch-обёртка: /api/v1, заголовок tma, ApiError
    theme/theme.css        CSS-переменные, тема из Telegram.colorScheme
    toast.ts               глобальный тост
  components/              AppNav (таб-бар), AppToast
  apps/
    tracker/               сетка 8 недель, календарь месяца, настройки категории
    <прочие>/StubView.vue  заглушки, доказывают code splitting
```

## Как добавить следующее мини-приложение

1. Создать `src/apps/<name>/<Name>View.vue` (+ api.ts/types.ts по образцу tracker).
2. Заменить `StubView.vue` на реальный компонент в `src/router/index.ts`.
3. Эндпоинты дергать через `shared/api/client.ts`.
