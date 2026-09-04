-- +goose Up

-- Поделиться категорией напоминаний (ссылка-приглашение rem_<токен>,
-- отправка пользователю). Получатель получает копию категории и её
-- напоминаний (кроме привязанных к привычкам Tracker).
ALTER TABLE reminder_categories ADD COLUMN share_token TEXT UNIQUE;

-- +goose Down
ALTER TABLE reminder_categories DROP COLUMN share_token;
