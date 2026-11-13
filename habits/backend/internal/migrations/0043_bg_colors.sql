-- +goose Up
-- Свой цвет фона приложения — отдельно для тёмной и светлой темы ('' — цвет темы
-- по умолчанию). Аналогично уже существующим text_color_dark/light.
ALTER TABLE user_settings ADD COLUMN bg_color_dark TEXT NOT NULL DEFAULT '';
ALTER TABLE user_settings ADD COLUMN bg_color_light TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE user_settings DROP COLUMN bg_color_light;
ALTER TABLE user_settings DROP COLUMN bg_color_dark;
