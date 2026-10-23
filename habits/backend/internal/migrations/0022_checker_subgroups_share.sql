-- +goose Up

-- Подгруппы: parent_id ссылается на родительскую группу того же пользователя.
-- ON DELETE CASCADE — удаление родителя уносит подгруппы (и их пункты).
ALTER TABLE checker_groups
    ADD COLUMN parent_id BIGINT REFERENCES checker_groups(id) ON DELETE CASCADE;
CREATE INDEX checker_groups_parent_idx ON checker_groups (parent_id);

-- Поделиться живой группой: токен-приглашение (аналог шаблонов). Получатель
-- получает копию группы с пунктами (отметки сбрасываются).
ALTER TABLE checker_groups ADD COLUMN share_token TEXT UNIQUE;

-- +goose Down
ALTER TABLE checker_groups DROP COLUMN share_token;
DROP INDEX checker_groups_parent_idx;
ALTER TABLE checker_groups DROP COLUMN parent_id;
