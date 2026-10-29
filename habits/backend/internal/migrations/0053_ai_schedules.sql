-- +goose Up
-- Расписания страницы AI: периодический запуск задачи для coding-агента
-- (например, «каждое утро — прогони тесты и пришли отчёт»). Каждый запуск
-- создаёт НОВУЮ задачу (свой контекст) с прогоном; при офлайн-агенте прогон
-- остаётся в очереди и доставляется на подключении.
-- period: daily | weekly | hours. at_minute — минуты от полуночи (daily/
-- weekly), dow — день недели 0=вс (weekly), every_hours — интервал (hours),
-- tz_off — смещение таймзоны в минутах (как у checker_recurring).
CREATE TABLE ai_schedules (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    machine_id BIGINT NOT NULL REFERENCES ai_machines(id) ON DELETE CASCADE,
    tool TEXT NOT NULL DEFAULT 'claude',
    workdir TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    params TEXT NOT NULL DEFAULT '',
    prompt TEXT NOT NULL,
    period TEXT NOT NULL DEFAULT 'daily',
    at_minute INT NOT NULL DEFAULT 540,
    dow INT NOT NULL DEFAULT 1,
    every_hours INT NOT NULL DEFAULT 24,
    tz_off INT NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    next_run_at TIMESTAMPTZ,
    last_task_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ai_schedules_due_idx ON ai_schedules (next_run_at) WHERE enabled;
CREATE INDEX ai_schedules_user_idx ON ai_schedules (user_id, machine_id);

-- +goose Down
DROP TABLE ai_schedules;
