-- +goose Up
-- Сейф: файлы шифруются в браузере паролем папки, сервер хранит только
-- двоичный шифротекст и конверты ключей. Сервер НЕ может ни прочитать файл,
-- ни узнать его имя и тип — у него нет пароля (см. habits/PLAN-vault.md).

CREATE TABLE vault_folders (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    parent_id   BIGINT REFERENCES vault_folders(id) ON DELETE CASCADE,
    -- имя папки открытое: иначе нечего показать до ввода пароля и нечего
    -- написать в уведомлении о шаринге
    name        TEXT NOT NULL,
    hint        TEXT NOT NULL DEFAULT '',   -- подсказка к паролю (открытая)
    thumbs      BOOLEAN NOT NULL DEFAULT true, -- делать превью картинкам
    -- параметры вывода ключа и обёрнутый ключ папки (base64); у каждой папки
    -- свои, даже при одинаковом пароле — так любая папка самодостаточна
    kdf_salt    TEXT NOT NULL,
    kdf_iter    INT  NOT NULL,
    wrapped_key TEXT NOT NULL,
    wrap_iv     TEXT NOT NULL,
    position    INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX vault_folders_user_idx ON vault_folders (user_id, parent_id, position);

CREATE TABLE vault_files (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    folder_id  BIGINT NOT NULL REFERENCES vault_folders(id) ON DELETE CASCADE,
    blob_name  TEXT NOT NULL,               -- файл в DATA_DIR/vault
    thumb_name TEXT NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL,             -- шифротекст на диске
    plain_size BIGINT NOT NULL,             -- исходный размер (лимиты и UI)
    key_env    TEXT NOT NULL,               -- AES-GCM(FK, ключ содержимого)
    meta_env   TEXT NOT NULL,               -- AES-GCM(FK, {имя, тип, …})
    chunk_size INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX vault_files_user_idx ON vault_files (user_id);
CREATE INDEX vault_files_folder_idx ON vault_files (folder_id);

-- Доступ, а не копия: владелец удалил файл — он пропал у всех.
CREATE TABLE vault_shares (
    kind       TEXT NOT NULL,               -- folder | file
    target_id  BIGINT NOT NULL,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (kind, target_id, user_id)
);
CREATE INDEX vault_shares_user_idx ON vault_shares (user_id);

-- Лимиты сейфа по типу пользователя (правятся в админке).
ALTER TABLE user_type_limits
    ADD COLUMN vault_file_mb  INT NOT NULL DEFAULT 3,
    ADD COLUMN vault_total_mb INT NOT NULL DEFAULT 10;

UPDATE user_type_limits SET vault_file_mb = 3,  vault_total_mb = 10   WHERE type = 'regular';
UPDATE user_type_limits SET vault_file_mb = 10, vault_total_mb = 300  WHERE type = 'payed1';
UPDATE user_type_limits SET vault_file_mb = 50, vault_total_mb = 1024 WHERE type = 'payed2';
UPDATE user_type_limits SET vault_file_mb = 50, vault_total_mb = 1024 WHERE type = 'vip';

-- +goose Down
ALTER TABLE user_type_limits DROP COLUMN vault_file_mb, DROP COLUMN vault_total_mb;
DROP TABLE vault_shares;
DROP TABLE vault_files;
DROP TABLE vault_folders;
