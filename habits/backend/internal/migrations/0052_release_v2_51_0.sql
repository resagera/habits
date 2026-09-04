-- +goose Up
-- Запись релиза в журнал (notified_at NULL — бот уведомит админа на старте).
-- +goose StatementBegin
INSERT INTO releases (version, released_on, title, public_notes, tech_notes) VALUES (
  '2.51.0',
  '2026-07-29',
  'AI — кнопка «Лимиты»: остаток лимитов Claude Code и Codex на карточках',
  'На карточках инструментов страницы AI появилась кнопка «📊 Лимиты» — аналог /usage в Claude Code и /status в Codex: план подписки и прогресс-бары занятости по окнам (сессия 5 часов, неделя, недельный лимит топ-модели) с временем сброса. Полоски желтеют от 70% и краснеют от 90%. У Codex данные — по состоянию на последний прогон (показывается когда).',
  E'Агент v1.2.0, op "usage" (sync-вызов хаба, 30с). Claude: тот же источник, что у /usage — GET api.anthropic.com/api/oauth/usage с OAuth-токеном из ~/.claude/.credentials.json (заголовок anthropic-beta: oauth-2025-04-20); токен не покидает машину, наружу — только проценты/даты; разбор массива limits (session/weekly_all/weekly_scoped со scope.model.display_name), план — claude auth status --json (subscriptionType). Codex: прямого API нет — последний снапшот rate_limits из роллаутов ~/.codex/sessions/**.jsonl (пишется при каждом прогоне; смотрим до 10 новейших файлов), primary/secondary окна по window_minutes (10080→неделя, ~300→5 часов), plan_type, note с timestamp снапшота. Нормализация usageInfo{plan, windows[{label,percent,resets_at}], note, error}. Бэкенд: POST /ai/machines/{id}/usage {tool} (без кэша — живые данные). Фронт: кнопка на карточках, блок с прогресс-барами (warn≥70, crit≥90), сброс при смене машины.'
);
-- +goose StatementEnd

-- +goose Down
DELETE FROM releases WHERE version = '2.51.0';
