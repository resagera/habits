-- +goose Up
-- Automation: автоматизированные действия по расписанию (первый тип —
-- заказ воды на jur.am). Учётные данные сайта хранятся в config JSONB в
-- зашифрованном виде (AES-GCM ключом сервера).

CREATE TABLE automations (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind TEXT NOT NULL DEFAULT 'jur_water' CHECK (kind IN ('jur_water')),
    title TEXT NOT NULL DEFAULT '' CHECK (length(title) <= 200),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    config JSONB NOT NULL DEFAULT '{}',
    interval_days INT NOT NULL DEFAULT 10 CHECK (interval_days BETWEEN 1 AND 365),
    next_run_at TIMESTAMPTZ,               -- когда сработает; NULL — только вручную
    last_run_at TIMESTAMPTZ,
    last_status TEXT NOT NULL DEFAULT '',  -- success/failed/dry_run/'' (ещё не запускалась)
    notified_day BOOLEAN NOT NULL DEFAULT FALSE,   -- уведомление за сутки отправлено
    notified_hour BOOLEAN NOT NULL DEFAULT FALSE,  -- уведомление за час отправлено
    running BOOLEAN NOT NULL DEFAULT FALSE,         -- защита от параллельного запуска
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX automations_user_idx ON automations (user_id);
CREATE INDEX automations_due_idx ON automations (enabled, next_run_at);

CREATE TABLE automation_runs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    automation_id BIGINT NOT NULL REFERENCES automations(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'success', 'failed')),
    dry_run BOOLEAN NOT NULL DEFAULT FALSE,
    trigger TEXT NOT NULL DEFAULT 'schedule' CHECK (trigger IN ('schedule', 'manual', 'dry_run')),
    steps JSONB NOT NULL DEFAULT '[]',     -- пошаговый лог [{name, ok, detail}]
    error TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);
CREATE INDEX automation_runs_idx ON automation_runs (automation_id, started_at DESC);

-- +goose Down
DROP TABLE automation_runs;
DROP TABLE automations;
