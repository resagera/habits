-- +goose Up
-- Повторяющиеся списки: у группы верхнего уровня — расписание сброса отметок.
-- При сбросе состояние дня сохраняется снимком (day-history / календарь).
ALTER TABLE checker_groups ADD COLUMN reset_period TEXT NOT NULL DEFAULT 'none'; -- none/daily/weekly/monthly
ALTER TABLE checker_groups ADD COLUMN reset_minute INT NOT NULL DEFAULT 360;      -- минут от полуночи (6:00)
ALTER TABLE checker_groups ADD COLUMN reset_dow INT NOT NULL DEFAULT 1;           -- день недели (0=вс..6), weekly
ALTER TABLE checker_groups ADD COLUMN reset_dom INT NOT NULL DEFAULT 1;           -- день месяца, monthly
ALTER TABLE checker_groups ADD COLUMN reset_tz_off INT NOT NULL DEFAULT 0;        -- смещение TZ, минут к востоку от UTC
ALTER TABLE checker_groups ADD COLUMN next_reset_at TIMESTAMPTZ;                  -- когда сработает следующий сброс (UTC)
CREATE INDEX checker_groups_reset_idx ON checker_groups (next_reset_at)
    WHERE reset_period <> 'none' AND next_reset_at IS NOT NULL;

-- снимки состояния списка на день
CREATE TABLE checker_snapshots (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    root_id    BIGINT NOT NULL REFERENCES checker_groups(id) ON DELETE CASCADE,
    day        DATE   NOT NULL,
    done       INT    NOT NULL DEFAULT 0,
    total      INT    NOT NULL DEFAULT 0,
    data       JSONB  NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (root_id, day)
);

-- +goose Down
DROP TABLE checker_snapshots;
DROP INDEX checker_groups_reset_idx;
ALTER TABLE checker_groups DROP COLUMN next_reset_at;
ALTER TABLE checker_groups DROP COLUMN reset_tz_off;
ALTER TABLE checker_groups DROP COLUMN reset_dom;
ALTER TABLE checker_groups DROP COLUMN reset_dow;
ALTER TABLE checker_groups DROP COLUMN reset_minute;
ALTER TABLE checker_groups DROP COLUMN reset_period;
