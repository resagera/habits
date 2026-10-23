-- +goose Up
-- Агент сетевого выхода: jur.am за Cloudflare блокирует датацентр-IP прод-
-- сервера, поэтому HTTP-запросы к сайту туннелируются через домашний агент на
-- резидентном IP. Один агент на пользователя, авторизация Bearer-токеном.
CREATE TABLE automation_agents (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    token TEXT NOT NULL UNIQUE,
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE automation_agents;
