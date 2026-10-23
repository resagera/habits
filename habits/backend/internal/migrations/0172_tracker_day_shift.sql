-- +goose Up
-- Сдвинутые сутки, несколько отметок в день и настойчивые напоминания.
--
-- day_start_minutes: во сколько у трекера начинается день (минуты от полуночи).
-- 300 = 05:00 — отметка, поставленная в час ночи, относится ко вчерашнему дню.
-- По-человечески сутки переключаются, когда встал, а не когда часы показали ноль.
ALTER TABLE tracker_categories
  ADD COLUMN IF NOT EXISTS day_start_minutes INT NOT NULL DEFAULT 0
    CHECK (day_start_minutes BETWEEN 0 AND 1439),
  -- сколько отметок ждём за день: напоминание сравнивает «не меньше чем»
  ADD COLUMN IF NOT EXISTS daily_target INT NOT NULL DEFAULT 1
    CHECK (daily_target BETWEEN 1 AND 50),
  -- NULL — повторных напоминаний нет; иначе период в минутах
  ADD COLUMN IF NOT EXISTS spam_minutes INT
    CHECK (spam_minutes IS NULL OR spam_minutes BETWEEN 1 AND 720),
  ADD COLUMN IF NOT EXISTS spam_max INT NOT NULL DEFAULT 20
    CHECK (spam_max BETWEEN 1 AND 100);

-- Напоминание знает, сколько отметок должно быть к ЕГО времени: у трекера с
-- двумя отметками в день напоминание в 13:00 ждёт одну, ночное — обе.
ALTER TABLE reminders
  ADD COLUMN IF NOT EXISTS expect_marks INT NOT NULL DEFAULT 1
    CHECK (expect_marks BETWEEN 1 AND 50),
  -- состояние текущего цикла повторов
  ADD COLUMN IF NOT EXISTS spam_sent INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS spam_day DATE;

-- «Хватит напоминать сегодня» — у каждого участника своё: трекер общий, а
-- терпение разное. Отметки при этом остаются общими.
CREATE TABLE IF NOT EXISTS tracker_hushes (
  category_id BIGINT NOT NULL REFERENCES tracker_categories(id) ON DELETE CASCADE,
  user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  day         DATE   NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (category_id, user_id, day)
);

-- +goose Down
DROP TABLE IF EXISTS tracker_hushes;
ALTER TABLE reminders
  DROP COLUMN IF EXISTS expect_marks,
  DROP COLUMN IF EXISTS spam_sent,
  DROP COLUMN IF EXISTS spam_day;
ALTER TABLE tracker_categories
  DROP COLUMN IF EXISTS day_start_minutes,
  DROP COLUMN IF EXISTS daily_target,
  DROP COLUMN IF EXISTS spam_minutes,
  DROP COLUMN IF EXISTS spam_max;
