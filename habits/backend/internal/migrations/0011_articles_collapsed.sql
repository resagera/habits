-- +goose Up

-- Articles: markdown-статьи с вложенными категориями (как папки Links).
CREATE TABLE articles_folders (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    parent_id BIGINT REFERENCES articles_folders(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX articles_folders_user_idx ON articles_folders (user_id);

CREATE TABLE articles (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    folder_id BIGINT REFERENCES articles_folders(id) ON DELETE CASCADE,
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 300),
    content TEXT NOT NULL DEFAULT '' CHECK (length(content) <= 1048576),
    -- токен приглашения (t.me/...?startapp=art_<токен>)
    share_token TEXT UNIQUE,
    -- токен публичной ссылки на скачивание .md (без авторизации)
    download_token TEXT UNIQUE,
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX articles_user_idx ON articles (user_id);

-- Свёрнутые группы Checker/Tracker: {"checker":[ids],"tracker":[ids]}.
-- Хранится на сервере — localStorage в Telegram-webview очищается.
ALTER TABLE user_settings ADD COLUMN ui_collapsed JSONB NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE user_settings DROP COLUMN ui_collapsed;
DROP TABLE articles;
DROP TABLE articles_folders;
