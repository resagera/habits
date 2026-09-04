-- +goose Up
-- «Какие страницы мне видны»: пользователь убирает из меню и с плиток главной
-- то, чем не пользуется. Это НЕ доступ (доступами управляет админ через
-- page_access) — только своя видимость: по прямой ссылке страница откроется.
ALTER TABLE user_settings
    ADD COLUMN hidden_pages JSONB NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE user_settings
    DROP COLUMN hidden_pages;
