-- +goose Up
-- Типы пользователей (regular/vip/payed1/payed2, назначаются админом) и
-- лимиты на тип: блоки в проекте, число картинок/файлов на пользователя,
-- размеры картинки/файла (МБ). project_uploads — учёт загрузок для лимитов.

ALTER TABLE users ADD COLUMN user_type TEXT NOT NULL DEFAULT 'regular';

CREATE TABLE user_type_limits (
    type         TEXT PRIMARY KEY,
    max_blocks   INT NOT NULL,   -- блоков в одном проекте
    max_images   INT NOT NULL,   -- картинок у пользователя (все проекты)
    max_files    INT NOT NULL,   -- файлов у пользователя
    max_image_mb INT NOT NULL,   -- размер одной картинки
    max_file_mb  INT NOT NULL    -- размер одного файла
);

INSERT INTO user_type_limits (type, max_blocks, max_images, max_files, max_image_mb, max_file_mb) VALUES
    ('regular', 100,  200,  100,  5,  20),
    ('vip',     1000, 2000, 1000, 20, 200),
    ('payed1',  300,  600,  300,  10, 50),
    ('payed2',  600,  1200, 600,  15, 100);

CREATE TABLE project_uploads (
    url        TEXT PRIMARY KEY,
    user_id    BIGINT NOT NULL,
    image      BOOLEAN NOT NULL,
    size       BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX project_uploads_user_idx ON project_uploads (user_id, image);

-- +goose Down
DROP TABLE project_uploads;
DROP TABLE user_type_limits;
ALTER TABLE users DROP COLUMN user_type;
