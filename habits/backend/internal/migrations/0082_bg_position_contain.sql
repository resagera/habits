-- +goose Up
-- Режим фона «вписать» (contain): картинка помещается целиком, ничего не
-- обрезается и не растягивается. У «заполнить» (cover) широкая картинка на
-- узком экране увеличивается в разы — это и выглядело как «растянуло».
--
-- Ограничение на колонку было заведено ещё в 0005 со списком из трёх значений,
-- поэтому расширяем его, иначе сохранение падает на уровне БД.
ALTER TABLE user_settings DROP CONSTRAINT IF EXISTS user_settings_bg_position_check;
ALTER TABLE user_settings
    ADD CONSTRAINT user_settings_bg_position_check
    CHECK (bg_position IN ('cover', 'contain', 'repeat', 'center'));

-- +goose Down
ALTER TABLE user_settings DROP CONSTRAINT IF EXISTS user_settings_bg_position_check;
UPDATE user_settings SET bg_position = 'cover' WHERE bg_position = 'contain';
ALTER TABLE user_settings
    ADD CONSTRAINT user_settings_bg_position_check
    CHECK (bg_position IN ('cover', 'repeat', 'center'));
