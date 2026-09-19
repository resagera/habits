-- +goose Up
-- Персональные токены доступа к аккаунту: авторизация вне Telegram
-- (веб-версия, расширение браузера, позже — MCP-клиенты). initData живёт
-- 24 часа и требует мини-апп, поэтому нужен долгоживущий секрет.
--
-- Сам токен НЕ хранится: только sha256-хэш. В списке показывается prefix
-- (первые символы) — чтобы отличать токены друг от друга.
-- Токен-сессия НЕ даёт админ-полномочий и не может выпускать новые токены
-- (проверки в auth.Middleware) — утечка токена не даёт эскалации.
CREATE TABLE access_tokens (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL DEFAULT '' CHECK (length(name) <= 100),
    token_hash TEXT NOT NULL UNIQUE,
    prefix TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    last_device TEXT NOT NULL DEFAULT '',
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX access_tokens_user_idx ON access_tokens (user_id, id DESC);

-- +goose Down
DROP TABLE access_tokens;
