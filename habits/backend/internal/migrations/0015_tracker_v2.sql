-- +goose Up
-- Tracker v2: виды трекеров, цвет/эмодзи у отметок, счётчик, совместный доступ.
ALTER TABLE tracker_categories
    ADD COLUMN kind  TEXT    NOT NULL DEFAULT 'marks'  CHECK (kind IN ('marks', 'counter')),
    ADD COLUMN style TEXT    NOT NULL DEFAULT 'square' CHECK (style IN ('square', 'circle', 'emoji')),
    ADD COLUMN multi BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN emoji TEXT    NOT NULL DEFAULT '✅' CHECK (length(emoji) BETWEEN 1 AND 16);

ALTER TABLE tracker_marks
    ADD COLUMN color TEXT, -- NULL = цвет категории (одноцветные трекеры перекрашиваются сменой цвета)
    ADD COLUMN emoji TEXT, -- NULL = эмодзи категории
    ADD COLUMN count INT NOT NULL DEFAULT 1 CHECK (count >= 1);

CREATE TABLE tracker_shares (
    category_id BIGINT NOT NULL REFERENCES tracker_categories(id) ON DELETE CASCADE,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (category_id, user_id)
);

CREATE INDEX tracker_shares_user_idx ON tracker_shares (user_id);

-- +goose Down
DROP TABLE tracker_shares;
ALTER TABLE tracker_marks DROP COLUMN color, DROP COLUMN emoji, DROP COLUMN count;
ALTER TABLE tracker_categories DROP COLUMN kind, DROP COLUMN style, DROP COLUMN multi, DROP COLUMN emoji;
