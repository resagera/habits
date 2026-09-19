-- +goose Up

-- Пороговые уведомления по серверу: мало места на диске, долгая высокая
-- загрузка RAM/CPU. Флаги *_alerted — защита от спама (одно сообщение на
-- событие, сбрасывается когда условие ушло).
ALTER TABLE servers ADD COLUMN alerts_enabled   BOOLEAN  NOT NULL DEFAULT false;
ALTER TABLE servers ADD COLUMN disk_min_free_mb BIGINT   NOT NULL DEFAULT 1024;  -- < 1 ГБ
ALTER TABLE servers ADD COLUMN ram_pct          SMALLINT NOT NULL DEFAULT 95;
ALTER TABLE servers ADD COLUMN ram_minutes      SMALLINT NOT NULL DEFAULT 5;
ALTER TABLE servers ADD COLUMN cpu_pct          SMALLINT NOT NULL DEFAULT 95;
ALTER TABLE servers ADD COLUMN cpu_minutes      SMALLINT NOT NULL DEFAULT 10;
ALTER TABLE servers ADD COLUMN disk_alerted     BOOLEAN  NOT NULL DEFAULT false;
ALTER TABLE servers ADD COLUMN ram_alerted      BOOLEAN  NOT NULL DEFAULT false;
ALTER TABLE servers ADD COLUMN cpu_alerted      BOOLEAN  NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE servers
    DROP COLUMN cpu_alerted,
    DROP COLUMN ram_alerted,
    DROP COLUMN disk_alerted,
    DROP COLUMN cpu_minutes,
    DROP COLUMN cpu_pct,
    DROP COLUMN ram_minutes,
    DROP COLUMN ram_pct,
    DROP COLUMN disk_min_free_mb,
    DROP COLUMN alerts_enabled;
