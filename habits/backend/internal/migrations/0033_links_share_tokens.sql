-- +goose Up
-- Ссылки-приглашения для Links: как в Checker/Articles — по токену друг
-- открывает t.me/<bot>?startapp=lnf_<token> (папка) или lnk_<token> (ссылка)
-- и получает копию. Токен на папку/ссылку, уникальный, выдаётся по требованию.
ALTER TABLE links_folders ADD COLUMN share_token TEXT UNIQUE;
ALTER TABLE links ADD COLUMN share_token TEXT UNIQUE;

-- +goose Down
ALTER TABLE links_folders DROP COLUMN share_token;
ALTER TABLE links DROP COLUMN share_token;
