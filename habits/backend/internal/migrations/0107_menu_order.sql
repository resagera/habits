-- +goose Up
-- Закрепление страниц В МЕНЮ (не путать с pinned_pages из 0074 — там липкая
-- шапка страницы при прокрутке).
--
-- Меню и плитки главной сортируются сами: закреплённые сверху, дальше
-- страницы с данными, внизу пустые. Закрепление — единственная ручная часть
-- этого порядка, поэтому хранится на сервере: в localStorage оно терялось бы
-- ровно так же, как терялась настройка шапки до 0074.
ALTER TABLE user_settings
    ADD COLUMN menu_pinned_pages JSONB NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE user_settings
    DROP COLUMN menu_pinned_pages;
