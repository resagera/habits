-- +goose Up
ALTER TABLE links ADD COLUMN clicks INT NOT NULL DEFAULT 0;

CREATE TABLE user_settings (
    user_id     BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    bg_kind     TEXT NOT NULL DEFAULT 'none' CHECK (bg_kind IN ('none', 'file', 'url')),
    bg_value    TEXT NOT NULL DEFAULT '', -- имя файла или URL
    bg_position TEXT NOT NULL DEFAULT 'cover' CHECK (bg_position IN ('cover', 'repeat', 'center')),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE user_backgrounds (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    filename   TEXT   NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX user_backgrounds_user_idx ON user_backgrounds (user_id);

-- +goose Down
DROP TABLE user_backgrounds;
DROP TABLE user_settings;
ALTER TABLE links DROP COLUMN clicks;
