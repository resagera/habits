-- +goose Up
-- История изменений чек-листа (особенно для общих списков): кто что сделал.
-- root_id — группа верхнего уровня (история ведётся на весь список/поддерево).
CREATE TABLE checker_history (
    id      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    root_id BIGINT NOT NULL REFERENCES checker_groups(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action  TEXT   NOT NULL,
    at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX checker_history_root_idx ON checker_history (root_id, at DESC);

-- +goose Down
DROP TABLE checker_history;
