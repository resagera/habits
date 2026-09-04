-- +goose Up
-- «Закрепить заголовок при прокрутке» переезжает из localStorage на сервер.
--
-- Причина: в localStorage настройка терялась — Telegram чистит хранилище
-- WebView, а у веб-версии и расширения оно вообще своё (та же история была с
-- темой до 0067). Теперь настройка одна на аккаунт и одинаковая везде;
-- localStorage остаётся только кэшем для первого кадра.
--
-- pinned_pages — список имён роутов, у которых шапка липнет к верху.
-- pin_all_headers — общий выключатель: закрепить на всех страницах сразу.
ALTER TABLE user_settings
    ADD COLUMN pinned_pages JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN pin_all_headers BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE user_settings
    DROP COLUMN pinned_pages,
    DROP COLUMN pin_all_headers;
