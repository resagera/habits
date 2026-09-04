-- +goose Up
CREATE TABLE checker_groups (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT   NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    position   INT    NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX checker_groups_user_pos_idx ON checker_groups (user_id, position);

CREATE TABLE checker_items (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    group_id   BIGINT NOT NULL REFERENCES checker_groups(id) ON DELETE CASCADE,
    name       TEXT   NOT NULL CHECK (length(name) BETWEEN 1 AND 500),
    done       BOOLEAN NOT NULL DEFAULT false,
    position   INT    NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX checker_items_group_pos_idx ON checker_items (group_id, position);

-- +goose Down
DROP TABLE checker_items;
DROP TABLE checker_groups;
