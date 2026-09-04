-- +goose Up
-- Метка пункта чек-листа: эмодзи-маркер (приоритет/цвет/эмодзи) для визуального
-- разделения — показывается перед названием пункта.
ALTER TABLE checker_items ADD COLUMN label TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE checker_items DROP COLUMN label;
