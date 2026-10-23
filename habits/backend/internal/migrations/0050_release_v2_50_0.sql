-- +goose Up
-- Запись релиза в журнал (notified_at NULL — бот уведомит админа на старте).
-- +goose StatementBegin
INSERT INTO releases (version, released_on, title, public_notes, tech_notes) VALUES (
  '2.50.0',
  '2026-07-28',
  'AI — полная поддержка Codex (OpenAI) наряду с Claude Code',
  'На странице AI теперь можно ставить задачи не только Claude Code, но и Codex (OpenAI): своя кнопка «＋ Задача» у карточки Codex, выбор модели (пусто — модель из config.toml машины), продолжение с тем же контекстом, своя справка по параметрам CLI. Проверка «Проверить» стала честной — реальный мини-запрос вместо проверки файла авторизации. Установка и вход в Codex — самостоятельно на машине (npm i -g @openai/codex, codex login).',
  E'Агент v1.1.0: executeCodex — `codex exec --json --skip-git-repo-check [-m X] [--dangerously-bypass-approvals-and-sandbox | --sandbox workspace-write]`, промпт через stdin («-»), продолжение — `codex exec resume <thread_id>`. Разбор JSONL-событий: thread.started→session_id, item.completed(agent_message)→output (последнее), turn.completed→num_turns/tokens_in/tokens_out (meta; cost нет — подписка). checkCodex — реальный пинг с --ephemeral (не плодит сессий). Per-tool фильтры параметров (filterBlocked): для codex блокируются --json/-o/--output-last-message/-C/--cd/-m/--model/«-». Бэкенд: createTask принимает tool из aiTools (заглушка 400 снята). Фронт: openCreateTask(tool), datalist моделей codex (gpt-5.5/gpt-5.5-codex/gpt-5.1-codex-mini), per-tool справка параметров, бейдж инструмента в списке задач и карточке, tokens в meta-строке. E2E: check codex (installed+authorized), задача (файл создан, thread_id сохранён), resume помнит контекст. Бинарники ai-v1.1.0 в habits-agent (в latest — все агенты).'
);
-- +goose StatementEnd

-- +goose Down
DELETE FROM releases WHERE version = '2.50.0';
