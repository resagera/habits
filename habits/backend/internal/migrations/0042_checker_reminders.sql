-- +goose Up
-- Дедлайны/напоминания: у пункта или у списка — момент напоминания (UTC).
-- Воркер шлёт сообщение бота владельцу и участникам, помечает reminded_at.
ALTER TABLE checker_items ADD COLUMN remind_at TIMESTAMPTZ;
ALTER TABLE checker_items ADD COLUMN reminded_at TIMESTAMPTZ;
ALTER TABLE checker_groups ADD COLUMN remind_at TIMESTAMPTZ;
ALTER TABLE checker_groups ADD COLUMN reminded_at TIMESTAMPTZ;
CREATE INDEX checker_items_remind_idx ON checker_items (remind_at)
    WHERE remind_at IS NOT NULL AND reminded_at IS NULL;
CREATE INDEX checker_groups_remind_idx ON checker_groups (remind_at)
    WHERE remind_at IS NOT NULL AND reminded_at IS NULL;

-- +goose Down
DROP INDEX checker_groups_remind_idx;
DROP INDEX checker_items_remind_idx;
ALTER TABLE checker_groups DROP COLUMN reminded_at;
ALTER TABLE checker_groups DROP COLUMN remind_at;
ALTER TABLE checker_items DROP COLUMN reminded_at;
ALTER TABLE checker_items DROP COLUMN remind_at;
