-- +goose Up
CREATE TABLE diary_entries (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    at         TIMESTAMPTZ NOT NULL,
    text       TEXT NOT NULL CHECK (length(text) BETWEEN 1 AND 100000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX diary_entries_user_at_idx ON diary_entries (user_id, at DESC);

-- +goose Down
DROP TABLE diary_entries;
