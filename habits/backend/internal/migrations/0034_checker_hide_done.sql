-- +goose Up
-- Настройка группы Checker «скрывать выполненное»: когда включена, выполненные
-- пункты не показываются в списке (в счётчике done/total учитываются).
ALTER TABLE checker_groups ADD COLUMN hide_done BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE checker_groups DROP COLUMN hide_done;
