-- +goose Up
-- Календарь: настройки слоёв пользователя (выбранные трекеры, тумблеры
-- источников). Хранится как непрозрачный JSONB фронтенда.
CREATE TABLE calendar_prefs (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    layers JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE calendar_prefs;
