-- +goose Up
-- Projects: страница-сборник — проект состоит из блоков (текст, картинки,
-- файлы, гео, ссылки на живые сущности: чек-листы, статьи, задачи, категории
-- задач), группируется в категории, шарится для совместного редактирования.
-- project_views хранит последний просмотр каждого участника — «изменён другим»
-- в списке помечается звёздочкой.

CREATE TABLE project_categories (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT   NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    position   INT    NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX project_categories_user_idx ON project_categories (user_id, position);

CREATE TABLE projects (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE, -- владелец
    category_id BIGINT REFERENCES project_categories(id) ON DELETE SET NULL,
    name        TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
    icon        TEXT NOT NULL DEFAULT '' CHECK (length(icon) <= 16),    -- эмодзи
    color       TEXT NOT NULL DEFAULT '#607d8b',
    cover       TEXT NOT NULL DEFAULT '',                               -- uploads/projects/<файл>
    ptype       TEXT NOT NULL DEFAULT '' CHECK (length(ptype) <= 100),  -- тип: рабочий/личный/…
    status      TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft','planned','active','paused','done','cancelled','archived')),
    tags        TEXT[] NOT NULL DEFAULT '{}',
    start_date  DATE,
    due_date    DATE,
    tz          TEXT NOT NULL DEFAULT '' CHECK (length(tz) <= 64),
    position    INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  BIGINT NOT NULL DEFAULT 0                               -- кто менял последним
);
CREATE INDEX projects_user_idx ON projects (user_id, position);

CREATE TABLE project_shares (
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);
CREATE INDEX project_shares_user_idx ON project_shares (user_id);

CREATE TABLE project_views (
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    viewed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);

CREATE TABLE project_blocks (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL,  -- кто добавил; для ref-блоков — владелец сущности
    kind       TEXT NOT NULL CHECK (kind IN
        ('text','images','file','location','checker_group','article','task','task_category')),
    position   INT NOT NULL DEFAULT 0,
    collapsed  BOOLEAN NOT NULL DEFAULT false,
    bg         TEXT NOT NULL DEFAULT '',   -- фон блока (#rrggbb или '')
    -- kind-специфика: text {text,rich}; images {images:[url]}; file {url,name,size};
    -- location {lat,lon,label}; ref-блоки {ref_id}
    content    JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX project_blocks_project_idx ON project_blocks (project_id, position);

CREATE TABLE project_history (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL,
    action     TEXT NOT NULL,
    at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX project_history_project_idx ON project_history (project_id, at DESC);

-- +goose Down
DROP TABLE project_history;
DROP TABLE project_blocks;
DROP TABLE project_views;
DROP TABLE project_shares;
DROP TABLE projects;
DROP TABLE project_categories;
