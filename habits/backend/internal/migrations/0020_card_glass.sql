-- +goose Up
-- Полупрозрачные карточки с размытием (frosted glass): уровень непрозрачности
-- (100 = как раньше, сплошной) и радиус размытия фона под карточкой.
ALTER TABLE user_settings
    ADD COLUMN card_opacity SMALLINT NOT NULL DEFAULT 100,
    ADD COLUMN card_blur SMALLINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE user_settings
    DROP COLUMN card_opacity,
    DROP COLUMN card_blur;
