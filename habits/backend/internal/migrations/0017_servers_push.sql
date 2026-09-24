-- +goose Up

-- Push-режим для машин без внешнего IP: агент сам шлёт отчёты на
-- POST /api/v1/agent/push с Bearer push_token. Для kind='push' url пустой.
ALTER TABLE servers ADD COLUMN kind TEXT NOT NULL DEFAULT 'pull'
    CHECK (kind IN ('pull', 'push'));
ALTER TABLE servers ALTER COLUMN url DROP NOT NULL;
ALTER TABLE servers ADD COLUMN push_token TEXT UNIQUE;
-- Флаг «уведомление об offline уже отправлено» — защита от спама ботом.
ALTER TABLE servers ADD COLUMN offline_notified BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE servers DROP COLUMN offline_notified;
ALTER TABLE servers DROP COLUMN push_token;
UPDATE servers SET url = '' WHERE url IS NULL;
ALTER TABLE servers ALTER COLUMN url SET NOT NULL;
ALTER TABLE servers DROP COLUMN kind;
