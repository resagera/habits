-- +goose Up
-- Корзина Checker: мягкое удаление групп. Удаление помечает всё поддерево
-- deleted_at; такие группы исчезают из списка, но лежат в корзине и физически
-- удаляются лениво по истечении срока хранения (user_settings.checker_trash_days).
ALTER TABLE checker_groups ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE INDEX checker_groups_trash_idx ON checker_groups (user_id) WHERE deleted_at IS NOT NULL;
ALTER TABLE user_settings ADD COLUMN checker_trash_days INT NOT NULL DEFAULT 30;

-- +goose Down
DROP INDEX checker_groups_trash_idx;
ALTER TABLE checker_groups DROP COLUMN deleted_at;
ALTER TABLE user_settings DROP COLUMN checker_trash_days;
