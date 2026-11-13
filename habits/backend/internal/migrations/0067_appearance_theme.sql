-- +goose Up
-- Тема интерфейса переезжает на сервер: раньше жила только в localStorage,
-- поэтому веб-версия и расширение (у них своё хранилище) всегда стартовали
-- с «auto» и брали тему системы — в Telegram тёмная, в браузере светлая.
--
-- theme — основная (то, что выбрано в Telegram).
-- web_theme — переопределение ТОЛЬКО для входа по токену (веб, расширение):
--   '' = как в Telegram. Меняется лишь из токен-сессии, поэтому настройки
--   браузера не влияют на мини-приложение и наоборот.
-- web_bg_off — не показывать фоновую картинку в браузере/расширении
--   (в попапе 400x600 фон часто мешает).
ALTER TABLE user_settings
    ADD COLUMN theme TEXT NOT NULL DEFAULT 'auto'
        CHECK (theme IN ('auto', 'light', 'dark')),
    ADD COLUMN web_theme TEXT NOT NULL DEFAULT ''
        CHECK (web_theme IN ('', 'auto', 'light', 'dark')),
    ADD COLUMN web_bg_off BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE user_settings
    DROP COLUMN web_bg_off,
    DROP COLUMN web_theme,
    DROP COLUMN theme;
