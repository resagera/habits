-- +goose Up

-- Опция «Daily» у категории трекера: по ней можно создать напоминание,
-- которое срабатывает только если день ещё не отмечен.
ALTER TABLE tracker_categories ADD COLUMN daily BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE reminders (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    note TEXT NOT NULL DEFAULT '',
    -- once     — один раз в момент at
    -- daily    — каждый день в time_of_day (days_mask сужает до дней недели)
    -- weekly   — по дням недели days_mask в time_of_day
    -- monthly  — day_of_month каждого месяца в time_of_day
    -- interval — каждые interval_minutes минут
    -- tracker  — как daily, но только если категория category_id не отмечена за сегодня
    kind TEXT NOT NULL CHECK (kind IN ('once', 'daily', 'weekly', 'monthly', 'interval', 'tracker')),
    at TIMESTAMPTZ,
    time_of_day TEXT CHECK (time_of_day ~ '^[0-2][0-9]:[0-5][0-9]$'),
    days_mask INT NOT NULL DEFAULT 127, -- бит 0 = Пн … бит 6 = Вс
    day_of_month INT CHECK (day_of_month BETWEEN 1 AND 31),
    interval_minutes INT CHECK (interval_minutes >= 5),
    category_id BIGINT REFERENCES tracker_categories(id) ON DELETE CASCADE,
    -- смещение локального времени пользователя от UTC в минутах
    -- (time_of_day и «сегодня» для tracker-типа считаются в нём)
    tz_offset_minutes INT NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT true,
    next_fire_at TIMESTAMPTZ,
    last_fired_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX reminders_due_idx ON reminders (next_fire_at) WHERE enabled;
CREATE INDEX reminders_user_idx ON reminders (user_id);

-- +goose Down
DROP TABLE reminders;
ALTER TABLE tracker_categories DROP COLUMN daily;
