-- +goose Up
-- AI: режим «план → выполнить» и живой лог прогона.
-- mode: '' — обычный прогон; 'plan' — только план (claude --permission-mode
-- plan / codex --sandbox read-only), правок файлов нет.
-- log — ход выполнения (строки-события от агента, кап 256КБ на сервере).
ALTER TABLE ai_runs ADD COLUMN mode TEXT NOT NULL DEFAULT '';
ALTER TABLE ai_runs ADD COLUMN log TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE ai_runs DROP COLUMN log;
ALTER TABLE ai_runs DROP COLUMN mode;
