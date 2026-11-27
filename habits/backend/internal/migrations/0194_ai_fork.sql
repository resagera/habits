-- +goose Up
-- Форк сессии: продолжение дописывает ту же сессию CLI, форк начинает новую с
-- той же историей, а исходная остаётся нетронутой
-- (claude --resume <id> --fork-session; codex exec fork <id> [--worktree]).
--
-- Флаги живут на ПРОГОНЕ, как и mode: прогон может ждать в очереди (агент
-- офлайн) и уходить агенту позже — значит, чем он был, должно храниться.
-- forked_from — на задаче: новый session_id приедет с результатом первого
-- прогона и перепишет session_id задачи, и без этого поля было бы не видно,
-- от какой сессии она отделилась.
-- +goose StatementBegin
ALTER TABLE ai_tasks ADD COLUMN IF NOT EXISTS forked_from TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE ai_runs ADD COLUMN IF NOT EXISTS fork BOOLEAN NOT NULL DEFAULT false;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE ai_runs ADD COLUMN IF NOT EXISTS worktree BOOLEAN NOT NULL DEFAULT false;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE ai_runs DROP COLUMN IF EXISTS worktree;
ALTER TABLE ai_runs DROP COLUMN IF EXISTS fork;
ALTER TABLE ai_tasks DROP COLUMN IF EXISTS forked_from;
