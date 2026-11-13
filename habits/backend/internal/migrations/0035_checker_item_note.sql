-- +goose Up
-- Заметка/описание у пункта чек-листа (окно редактирования пункта расширяется).
ALTER TABLE checker_items ADD COLUMN note TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE checker_items DROP COLUMN note;
