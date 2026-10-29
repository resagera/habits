-- +goose Up
-- Комнаты ТВ-плеера: приставка и пульт встречаются по строке-ключу, которую
-- объявляет агент (по умолчанию — его адрес в локальной сети).
--
-- Владелец закрепляется за комнатой при первом подключении пульта. Без этого
-- ключ вида «192.168.0.226» угадал бы любой прохожий: приставка сидит в
-- локальной сети, а вот шина стоит на общедоступном сервере.
CREATE TABLE IF NOT EXISTS tv_rooms (
    key         TEXT PRIMARY KEY CHECK (length(key) BETWEEN 1 AND 120),
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    label       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS tv_rooms_user_idx ON tv_rooms (user_id, last_seen_at DESC);

-- +goose Down
DROP TABLE IF EXISTS tv_rooms;
