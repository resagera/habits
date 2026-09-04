-- +goose Up
-- Страница AI: запуск coding-агентов (Claude Code, позже Codex) на домашних
-- машинах через релей-агента (исходящий WS, как files/terminal). Разрешённые
-- папки задаются в конфиге агента и приходят в hello (dirs); tools — кэш
-- последней проверки инструментов ({"claude":{"ok":true,"version":...}}).
CREATE TABLE ai_machines (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    token TEXT NOT NULL UNIQUE,
    dirs JSONB NOT NULL DEFAULT '[]'::jsonb,
    tools JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ai_machines_user_idx ON ai_machines (user_id);

-- Задача = цепочка прогонов с общим контекстом (session_id для --resume).
CREATE TABLE ai_tasks (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    machine_id BIGINT NOT NULL REFERENCES ai_machines(id) ON DELETE CASCADE,
    tool TEXT NOT NULL DEFAULT 'claude',
    workdir TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    params TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    session_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ai_tasks_user_idx ON ai_tasks (user_id, machine_id, updated_at DESC);

-- Прогон: очередь -> выполнение -> результат/ошибка. meta: cost/duration/
-- num_turns/branch/diffstat от агента.
CREATE TABLE ai_runs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    task_id BIGINT NOT NULL REFERENCES ai_tasks(id) ON DELETE CASCADE,
    prompt TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    output TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    meta JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ai_runs_task_idx ON ai_runs (task_id, id);

-- +goose Down
DROP TABLE ai_runs;
DROP TABLE ai_tasks;
DROP TABLE ai_machines;
