-- +goose Up

-- Видимость страниц: по умолчанию (нет строки) — из реестра в коде
-- ('all' для всех, 'personal' для servers). Строка переопределяет.
CREATE TABLE page_settings (
    page TEXT PRIMARY KEY,
    visibility TEXT NOT NULL CHECK (visibility IN ('all', 'personal'))
);

-- Персональные доступы к страницам с visibility='personal'.
CREATE TABLE page_access (
    page TEXT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (page, user_id)
);

-- Доступы к отдельным опциям страниц (например links.dead_check).
CREATE TABLE feature_access (
    feature TEXT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (feature, user_id)
);

-- Servers по умолчанию видна только по персональному доступу.
INSERT INTO page_settings (page, visibility) VALUES ('servers', 'personal');

-- Обращения к админу со страницы Help.
CREATE TABLE help_requests (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    text TEXT NOT NULL CHECK (length(text) BETWEEN 1 AND 4000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Мониторинг серверов: адрес опрашивается поллером раз в минуту.
CREATE TABLE servers (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    url TEXT NOT NULL CHECK (length(url) BETWEEN 1 AND 500),
    token TEXT NOT NULL DEFAULT '',
    last_ok_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    last_data JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX servers_user_idx ON servers (user_id);

-- История CPU/RAM для графиков (поллер чистит старше 48 часов).
CREATE TABLE server_samples (
    server_id BIGINT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    at TIMESTAMPTZ NOT NULL DEFAULT now(),
    cpu_pct REAL NOT NULL,
    ram_used BIGINT NOT NULL,
    ram_total BIGINT NOT NULL
);
CREATE INDEX server_samples_idx ON server_samples (server_id, at);

-- Шаблоны чек-листов: многоразовые списки («сборы в поездку»).
CREATE TABLE checker_templates (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    share_token TEXT UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE checker_template_items (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    template_id BIGINT NOT NULL REFERENCES checker_templates(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 500),
    position INT NOT NULL DEFAULT 0
);
CREATE INDEX checker_template_items_idx ON checker_template_items (template_id, position);

-- Проверка битых ссылок (опция links.dead_check).
ALTER TABLE links ADD COLUMN dead BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE links ADD COLUMN checked_at TIMESTAMPTZ;

-- Выбор хранилища Links живёт на сервере: localStorage в Telegram-webview
-- периодически очищается, из-за чего настройка «слетала» на локальное.
ALTER TABLE user_settings ADD COLUMN links_storage TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE user_settings DROP COLUMN links_storage;
ALTER TABLE links DROP COLUMN checked_at;
ALTER TABLE links DROP COLUMN dead;
DROP TABLE checker_template_items;
DROP TABLE checker_templates;
DROP TABLE server_samples;
DROP TABLE servers;
DROP TABLE help_requests;
DROP TABLE feature_access;
DROP TABLE page_access;
DROP TABLE page_settings;
