-- +goose Up

-- Шаринг категории статей ДОСТУПОМ (контент не дублируется): получатель
-- видит поддерево владельца вживую (read-only). Каскад: удалил папку /
-- пользователя — доступ исчез.
CREATE TABLE articles_folder_shares (
    folder_id BIGINT NOT NULL REFERENCES articles_folders(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (folder_id, user_id)
);
CREATE INDEX articles_folder_shares_user_idx ON articles_folder_shares (user_id);

-- Свой цвет текста интерфейса отдельно для тёмной и светлой темы
-- ('' — цвет темы по умолчанию).
ALTER TABLE user_settings ADD COLUMN text_color_dark TEXT NOT NULL DEFAULT '';
ALTER TABLE user_settings ADD COLUMN text_color_light TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE user_settings DROP COLUMN text_color_light;
ALTER TABLE user_settings DROP COLUMN text_color_dark;
DROP TABLE articles_folder_shares;
