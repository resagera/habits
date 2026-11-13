-- +goose Up
-- Tasks: проекты (свои статусы и дефолты), задачи, чек-листы, совместные проекты.
CREATE TABLE task_projects (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT   NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    color      TEXT   NOT NULL DEFAULT '#607d8b',
    position   INT    NOT NULL DEFAULT 0,
    -- переопределение статусов: [{"name":"В работе","kind":"open"},...]; NULL = стандартные
    statuses   JSONB,
    -- дефолты новых задач проекта: {"priority":2,"remind":true,"remind_before_min":60,...}
    defaults   JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX task_projects_user_idx ON task_projects (user_id, position);

CREATE TABLE task_project_shares (
    project_id BIGINT NOT NULL REFERENCES task_projects(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);

CREATE INDEX task_project_shares_user_idx ON task_project_shares (user_id);

CREATE TABLE tasks (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE, -- автор
    project_id          BIGINT REFERENCES task_projects(id) ON DELETE SET NULL, -- NULL = «Входящие»
    title               TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 300),
    note                TEXT NOT NULL DEFAULT '',                               -- markdown
    status              TEXT NOT NULL DEFAULT 'Открыта',
    status_kind         TEXT NOT NULL DEFAULT 'open' CHECK (status_kind IN ('open', 'done', 'archived')),
    priority            SMALLINT NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 3),
    due_date            DATE,
    due_time            TIME,
    remind              BOOLEAN NOT NULL DEFAULT FALSE,
    remind_before_min   INT NOT NULL DEFAULT 0 CHECK (remind_before_min BETWEEN 0 AND 10080),
    reminded_at         TIMESTAMPTZ, -- уведомление «скоро срок» отправлено
    overdue_notified_at TIMESTAMPTZ, -- уведомление «просрочена» отправлено
    repeat_kind         TEXT CHECK (repeat_kind IN ('daily', 'weekly', 'monthly', 'interval')),
    repeat_param        INT,         -- дни для interval
    assignee_id         BIGINT REFERENCES users(id) ON DELETE SET NULL,
    tz_offset_minutes   INT NOT NULL DEFAULT 0,
    position            INT NOT NULL DEFAULT 0,
    completed_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX tasks_user_kind_idx ON tasks (user_id, status_kind, due_date);
CREATE INDEX tasks_project_idx ON tasks (project_id);
CREATE INDEX tasks_remind_idx ON tasks (status_kind, due_date) WHERE remind;

CREATE TABLE task_checklist (
    id       BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    task_id  BIGINT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    name     TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 300),
    done     BOOLEAN NOT NULL DEFAULT FALSE,
    position INT NOT NULL DEFAULT 0
);

CREATE INDEX task_checklist_task_idx ON task_checklist (task_id, position);

-- +goose Down
DROP TABLE task_checklist;
DROP TABLE tasks;
DROP TABLE task_project_shares;
DROP TABLE task_projects;
