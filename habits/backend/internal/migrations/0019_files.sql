-- +goose Up
-- My Files: домашние машины с файловым агентом. Папки и режим доступа
-- задаются в конфиге агента и присылаются им при подключении (hello),
-- здесь кэшируются для показа, пока машина офлайн.
CREATE TABLE file_machines (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    token TEXT NOT NULL UNIQUE,
    roots JSONB NOT NULL DEFAULT '[]'::jsonb,
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX file_machines_user_idx ON file_machines (user_id);

-- +goose Down
DROP TABLE file_machines;
