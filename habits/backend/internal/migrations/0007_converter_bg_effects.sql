-- +goose Up
ALTER TABLE user_settings ADD COLUMN bg_blur INT NOT NULL DEFAULT 0 CHECK (bg_blur BETWEEN 0 AND 30);
ALTER TABLE user_settings ADD COLUMN bg_dim  INT NOT NULL DEFAULT 0 CHECK (bg_dim BETWEEN -70 AND 70);

-- Валюты пользователя для конвертера
CREATE TABLE user_currencies (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code       TEXT   NOT NULL CHECK (code ~ '^[a-z0-9]{2,10}$'),
    position   INT    NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, code)
);

-- +goose Down
DROP TABLE user_currencies;
ALTER TABLE user_settings DROP COLUMN bg_dim;
ALTER TABLE user_settings DROP COLUMN bg_blur;
