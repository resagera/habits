-- +goose Up
CREATE TABLE users (
    id           BIGINT PRIMARY KEY, -- Telegram user id
    username     TEXT,
    first_name   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE tracker_categories (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT   NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    color      TEXT   NOT NULL DEFAULT '#4caf50',
    position   INT    NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, name)
);

CREATE INDEX tracker_categories_user_pos_idx ON tracker_categories (user_id, position);

CREATE TABLE tracker_marks (
    category_id BIGINT NOT NULL REFERENCES tracker_categories(id) ON DELETE CASCADE,
    day         DATE   NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (category_id, day)
);

-- +goose Down
DROP TABLE tracker_marks;
DROP TABLE tracker_categories;
DROP TABLE users;
