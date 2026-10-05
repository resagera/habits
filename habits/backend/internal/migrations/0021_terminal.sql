-- +goose Up
-- Terminal: домашние машины с shell-агентом. Агент держит исходящий WebSocket
-- (у машин нет внешнего IP) и по запросу открывает PTY-сессии — веб-консоль
-- к своей машине из любой точки.
CREATE TABLE terminal_machines (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    token TEXT NOT NULL UNIQUE,
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX terminal_machines_user_idx ON terminal_machines (user_id);

-- +goose Down
DROP TABLE terminal_machines;
