-- +goose Up
CREATE TABLE links_folders (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    parent_id  BIGINT REFERENCES links_folders(id) ON DELETE CASCADE,
    name       TEXT   NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    position   INT    NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX links_folders_user_idx ON links_folders (user_id, parent_id, position);

CREATE TABLE links (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    folder_id  BIGINT REFERENCES links_folders(id) ON DELETE CASCADE, -- NULL = корень
    name       TEXT   NOT NULL CHECK (length(name) BETWEEN 1 AND 500),
    url        TEXT   NOT NULL CHECK (length(url) BETWEEN 1 AND 2000),
    tags       TEXT[] NOT NULL DEFAULT '{}',
    pinned     BOOLEAN NOT NULL DEFAULT false,
    position   INT    NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX links_user_folder_idx ON links (user_id, folder_id, position);

-- +goose Down
DROP TABLE links;
DROP TABLE links_folders;
