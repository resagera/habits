-- +goose Up
-- Заметка чека: подробности, которые не сводятся к списку позиций.
-- Появилась ради отчётов о поездках Яндекс Go — там позиций нет вовсе, а
-- маршрут, машина и тариф и есть весь смысл записи.
ALTER TABLE mail_receipts ADD COLUMN IF NOT EXISTS note TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE mail_receipts DROP COLUMN IF EXISTS note;
