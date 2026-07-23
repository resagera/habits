-- +goose Up

-- Типы графиков — отдельная сущность: новые типы добавляются строкой здесь,
-- без изменения схемы.
CREATE TABLE metrics_chart_types (
    code TEXT PRIMARY KEY,
    name TEXT NOT NULL
);

INSERT INTO metrics_chart_types (code, name) VALUES
    ('line',  'Линейный'),
    ('bars',  'Столбики'),
    ('tubes', 'Трубки');

CREATE TABLE metrics_categories (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT   NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    position   INT    NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX metrics_categories_user_idx ON metrics_categories (user_id, position);

CREATE TABLE metrics_items (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    category_id BIGINT NOT NULL REFERENCES metrics_categories(id) ON DELETE CASCADE,
    name        TEXT   NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    chart_type  TEXT   NOT NULL DEFAULT 'line' REFERENCES metrics_chart_types(code),
    config      JSONB  NOT NULL DEFAULT '{}',
    position    INT    NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX metrics_items_category_idx ON metrics_items (category_id, position);

-- Точки графика. component — ключ серии/компонента ('' для одиночных метрик;
-- 'deep'/'rem' для мультикомпонентных, 'min'/'max' для трубок и т.п.)
CREATE TABLE metrics_values (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    item_id    BIGINT NOT NULL REFERENCES metrics_items(id) ON DELETE CASCADE,
    at         TIMESTAMPTZ NOT NULL,
    component  TEXT   NOT NULL DEFAULT '',
    value      DOUBLE PRECISION NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX metrics_values_item_at_idx ON metrics_values (item_id, at);

-- +goose Down
DROP TABLE metrics_values;
DROP TABLE metrics_items;
DROP TABLE metrics_categories;
DROP TABLE metrics_chart_types;
