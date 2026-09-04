-- +goose Up
-- «Пункт взят в работу» — необязательная отметка между «не начато» и
-- «сделано». Отдельным флагом, а не значением done: done остаётся булевым,
-- по нему считается прогресс группы, история и повторяющиеся сбросы.
--
-- Отмеченный пункт «в работе» быть не может: при done = true флаг снимается
-- (см. UpdateCheckItem и BulkSetItemsDone), а повторяющийся сброс группы
-- очищает и его — новый цикл начинается с чистого листа.
ALTER TABLE checker_items
    ADD COLUMN in_progress BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE checker_items
    DROP COLUMN in_progress;
