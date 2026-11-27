-- +goose Up
ALTER TABLE users ADD COLUMN banned BOOLEAN NOT NULL DEFAULT false;

-- Уникальные связки IP + устройство с датой первого появления
CREATE TABLE user_devices (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ip         TEXT   NOT NULL,
    device     TEXT   NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, ip, device)
);

CREATE INDEX user_devices_user_idx ON user_devices (user_id, created_at DESC);

-- +goose Down
DROP TABLE user_devices;
ALTER TABLE users DROP COLUMN banned;
