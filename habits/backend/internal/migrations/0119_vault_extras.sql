-- +goose Up
-- Сейф, второй заход: временные ссылки на один файл, срок жизни файла,
-- журнал доступа к расшаренному и косметическое скрытие подпапок.
-- Сервер по-прежнему ничего не может расшифровать (см. habits/PLAN-vault.md).
-- Заметки к файлу отдельной колонки не получили: они лежат внутри meta_env,
-- то есть зашифрованы вместе с именем — иначе сервер читал бы их открытым.

-- Косметика: не показывать вложенные папки, пока родитель не открыт паролем.
-- Именно косметика — имена папок в базе открытые, и в интерфейсе это сказано.
ALTER TABLE vault_folders ADD COLUMN hide_children BOOLEAN NOT NULL DEFAULT false;
-- 0 — не удалять; иначе файлам этой папки ставится срок при загрузке
ALTER TABLE vault_folders ADD COLUMN auto_delete_days INT NOT NULL DEFAULT 0;

ALTER TABLE vault_files ADD COLUMN expires_at TIMESTAMPTZ;
CREATE INDEX vault_files_expires_idx ON vault_files (expires_at) WHERE expires_at IS NOT NULL;

-- Временная ссылка на ОДИН файл. Ключ содержимого завёрнут в отдельный
-- конверт под паролем ссылки: ключ папки наружу не уходит, и утёкшая ссылка
-- компрометирует максимум один файл — да и то только вместе с паролем.
CREATE TABLE vault_links (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    file_id    BIGINT NOT NULL REFERENCES vault_files(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- только хеш токена: утечка базы не даёт рабочих ссылок
    token_hash TEXT NOT NULL UNIQUE,
    kdf_salt   TEXT NOT NULL,
    kdf_iter   INT  NOT NULL,
    key_env    TEXT NOT NULL,  -- CK под ключом из пароля ссылки
    meta_env   TEXT NOT NULL,  -- имя и тип под тем же ключом
    expires_at TIMESTAMPTZ NOT NULL,
    max_views  INT NOT NULL DEFAULT 0,  -- 0 — без ограничения
    views      INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX vault_links_file_idx ON vault_links (file_id, created_at DESC);
CREATE INDEX vault_links_expires_idx ON vault_links (expires_at);

-- Журнал: кто и когда брал файл. Пишется только для чужих обращений.
CREATE TABLE vault_access_log (
    id      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    file_id BIGINT NOT NULL REFERENCES vault_files(id) ON DELETE CASCADE,
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL, -- пусто у ссылок
    via     TEXT NOT NULL,  -- share | link
    at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX vault_access_log_file_idx ON vault_access_log (file_id, at DESC);

-- +goose Down
DROP TABLE vault_access_log;
DROP TABLE vault_links;
DROP INDEX vault_files_expires_idx;
ALTER TABLE vault_files DROP COLUMN expires_at;
ALTER TABLE vault_folders DROP COLUMN auto_delete_days;
ALTER TABLE vault_folders DROP COLUMN hide_children;
