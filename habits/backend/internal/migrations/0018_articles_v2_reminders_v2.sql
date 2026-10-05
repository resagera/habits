-- +goose Up

-- Articles v2: публичное чтение, история изменений, позиция чтения.
ALTER TABLE articles ADD COLUMN read_token TEXT UNIQUE;

-- История: при каждом сохранении с изменённым content старая версия
-- уходит сюда (хранится последние 30 ревизий на статью).
CREATE TABLE article_revisions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    article_id BIGINT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    saved_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX article_revisions_idx ON article_revisions (article_id, saved_at DESC);

-- Позиция чтения: доля прокрутки (0..1) на пользователя и статью.
CREATE TABLE article_read_positions (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    article_id BIGINT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    pos REAL NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, article_id)
);

-- Reminders v2: свои категории и ежегодные напоминания.
CREATE TABLE reminder_categories (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    position INT NOT NULL DEFAULT 0
);
CREATE INDEX reminder_categories_user_idx ON reminder_categories (user_id, position);

ALTER TABLE reminders ADD COLUMN group_id BIGINT REFERENCES reminder_categories(id) ON DELETE SET NULL;
-- месяц для kind='yearly' (день берётся из day_of_month)
ALTER TABLE reminders ADD COLUMN month INT CHECK (month BETWEEN 1 AND 12);
ALTER TABLE reminders DROP CONSTRAINT reminders_kind_check;
ALTER TABLE reminders ADD CONSTRAINT reminders_kind_check
    CHECK (kind IN ('once', 'daily', 'weekly', 'monthly', 'yearly', 'interval', 'tracker'));

-- +goose Down
ALTER TABLE reminders DROP CONSTRAINT reminders_kind_check;
ALTER TABLE reminders ADD CONSTRAINT reminders_kind_check
    CHECK (kind IN ('once', 'daily', 'weekly', 'monthly', 'interval', 'tracker'));
ALTER TABLE reminders DROP COLUMN month;
ALTER TABLE reminders DROP COLUMN group_id;
DROP TABLE reminder_categories;
DROP TABLE article_read_positions;
DROP TABLE article_revisions;
ALTER TABLE articles DROP COLUMN read_token;
