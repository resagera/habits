-- +goose Up
-- Запись релиза в журнал (notified_at NULL — бот уведомит админа на старте).
-- +goose StatementBegin
INSERT INTO releases (version, released_on, title, public_notes, tech_notes) VALUES (
  '2.48.0',
  '2026-07-28',
  'страница AI — задачи для Claude Code на своих машинах из приложения',
  'Новая страница «AI»: добавьте свою машину (компьютер с установленным Claude Code), и ставьте coding-агенту задачи прямо из приложения — выбрав папку проекта, модель и описав задачу. Задача выполняется на машине, результат приходит в приложение; можно продолжить с тем же контекстом (агент помнит предыдущие прогоны). Поддержка Codex — в разработке (пока проверка установки).',
  E'Релей-архитектура как у My Files/Terminal: агент habits-ai-agent (Go) держит исходящий WS GET /api/v1/ai/agent (Bearer = токен машины), белый список папок AI_AGENT_DIRS, режим прав bypass (--dangerously-skip-permissions, AI_AGENT_BYPASS=0 отключает). Хаб internal/aicoder: sync-вызовы (check — наличие/авторизация инструмента, авторизация проверяется реальным мини-запросом haiku) + async-прогоны (run_status/run_result/run_ack, недоставленные результаты агент повторяет после reconnect, FinishAIRun идемпотентен). Раннер: claude -p --output-format json [--model X] [--resume session_id] + фильтрованные доп. параметры, prompt через stdin, cwd=workdir (двойная проверка белого списка), таймаут 60 мин, meta: cost/turns/elapsed/branch/diffstat. Таблицы ai_machines/ai_tasks/ai_runs (0046); задача = цепочка прогонов с session_id. API: /ai/machines CRUD + /check, /ai/tasks CRUD + /{id}/runs (продолжение, 409 при активном прогоне), подвисшие прогоны (>2ч) — в ошибку при листинге. Страница ai в PagesRegistry (personal) + guardedPages. Фронт apps/ai: машины с инструкцией установки, инструменты (Codex — заглушка), задачи с поллингом 5с, сворачиваемые прогоны, продолжение контекста, справка по CLI-параметрам.'
);
-- +goose StatementEnd

-- +goose Down
DELETE FROM releases WHERE version = '2.48.0';
