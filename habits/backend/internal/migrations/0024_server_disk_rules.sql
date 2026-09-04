-- +goose Up

-- Пороги по каждому диску отдельно (раньше был один порог на все диски —
-- прилетали ненужные алерты по /boot и т.п.). Правило: {"mount": "/",
-- "min_free_mb": 1024}. Мониторятся только перечисленные точки монтирования;
-- disk_alerted_mounts — множество точек, уже находящихся в состоянии алерта
-- (защита от спама, по одной точке).
ALTER TABLE servers ADD COLUMN disk_rules          JSONB NOT NULL DEFAULT '[]';
ALTER TABLE servers ADD COLUMN disk_alerted_mounts JSONB NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE servers DROP COLUMN disk_alerted_mounts;
ALTER TABLE servers DROP COLUMN disk_rules;
