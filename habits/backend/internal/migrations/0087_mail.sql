-- +goose Up
-- Почта на resager.ru: приём писем прямо в приложение.
--
-- Не полноценный почтовый сервер, а приёмник (receive-only): ящиков, IMAP и
-- отправки нет — только SMTP на входе, разбор и хранение. Цель — читать и
-- разбирать письма приложением (чеки магазинов для Finance), а не держать
-- переписку, поэтому Postfix+Dovecot были бы лишней поверхностью атаки на
-- машине с 2 ГБ свободной памяти.
--
-- Ключевое решение против спама: адрес получателя проверяется на RCPT TO по
-- белому списку. Словарный перебор ботов отбивается до DATA и вообще не
-- попадает в базу — это дешевле любой фильтрации после приёма.

-- Адреса и одноразовые алиасы. Алиас заводится под конкретный магазин: если
-- он начал течь спамом, видно, кто продал адрес, и алиас выключается.
CREATE TABLE mail_addresses (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    address TEXT NOT NULL CHECK (length(address) BETWEEN 3 AND 320),
    label TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL DEFAULT 'address' CHECK (kind IN ('address', 'alias')),
    -- принимать только от этого домена отправителя: для алиаса магазина это
    -- самый сильный фильтр — всё остальное отбивается на RCPT TO
    only_from TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT true,
    received INT NOT NULL DEFAULT 0,
    rejected INT NOT NULL DEFAULT 0,
    last_at TIMESTAMPTZ,
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX mail_addresses_addr_idx ON mail_addresses (lower(address));
CREATE INDEX mail_addresses_user_idx ON mail_addresses (user_id, id);

-- Принятые письма. Сырое письмо лежит на диске (raw_path): разбор MIME можно
-- повторить новым кодом, а исходник в базе раздул бы её на порядок.
CREATE TABLE mail_messages (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    address_id BIGINT REFERENCES mail_addresses(id) ON DELETE SET NULL,
    rcpt TEXT NOT NULL,
    mail_from TEXT NOT NULL DEFAULT '',
    from_name TEXT NOT NULL DEFAULT '',
    from_addr TEXT NOT NULL DEFAULT '',
    subject TEXT NOT NULL DEFAULT '',
    message_id TEXT NOT NULL DEFAULT '',
    sent_at TIMESTAMPTZ,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    size_bytes INT NOT NULL DEFAULT 0,
    text_body TEXT NOT NULL DEFAULT '',
    html_body TEXT NOT NULL DEFAULT '',
    -- откуда пришло: для разбора инцидентов и блокировок
    remote_ip TEXT NOT NULL DEFAULT '',
    helo TEXT NOT NULL DEFAULT '',
    ptr TEXT NOT NULL DEFAULT '',
    tls BOOLEAN NOT NULL DEFAULT false,
    spf TEXT NOT NULL DEFAULT '',
    spam_score INT NOT NULL DEFAULT 0,
    spam_reasons TEXT NOT NULL DEFAULT '',
    is_spam BOOLEAN NOT NULL DEFAULT false,
    is_read BOOLEAN NOT NULL DEFAULT false,
    starred BOOLEAN NOT NULL DEFAULT false,
    archived_at TIMESTAMPTZ,
    raw_path TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX mail_messages_user_idx ON mail_messages (user_id, is_spam, received_at DESC, id DESC);
CREATE INDEX mail_messages_addr_idx ON mail_messages (address_id, received_at DESC);

CREATE TABLE mail_attachments (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    message_id BIGINT NOT NULL REFERENCES mail_messages(id) ON DELETE CASCADE,
    filename TEXT NOT NULL DEFAULT '',
    content_type TEXT NOT NULL DEFAULT '',
    size_bytes INT NOT NULL DEFAULT 0,
    path TEXT NOT NULL DEFAULT ''
);
CREATE INDEX mail_attachments_msg_idx ON mail_attachments (message_id);

-- Кто и как к нам ломится. Счётчики по IP, а не журнал событий: порт 25
-- перебирают круглосуточно, и построчный журнал занял бы больше места, чем
-- сама почта.
CREATE TABLE mail_ip_stats (
    ip TEXT PRIMARY KEY,
    first_seen TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen TIMESTAMPTZ NOT NULL DEFAULT now(),
    connections INT NOT NULL DEFAULT 0,
    accepted INT NOT NULL DEFAULT 0,
    rejected INT NOT NULL DEFAULT 0,
    blocked_until TIMESTAMPTZ,
    last_reason TEXT NOT NULL DEFAULT '',
    ptr TEXT NOT NULL DEFAULT ''
);
CREATE INDEX mail_ip_stats_seen_idx ON mail_ip_stats (last_seen DESC);
CREATE INDEX mail_ip_stats_blocked_idx ON mail_ip_stats (blocked_until) WHERE blocked_until IS NOT NULL;

-- Уведомлять в бота о новом письме (спам не тревожит).
ALTER TABLE user_settings
    ADD COLUMN mail_notify BOOLEAN NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE user_settings DROP COLUMN mail_notify;
DROP TABLE mail_ip_stats;
DROP TABLE mail_attachments;
DROP TABLE mail_messages;
DROP TABLE mail_addresses;
