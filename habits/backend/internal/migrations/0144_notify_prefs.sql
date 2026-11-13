-- +goose Up
-- Настройки уведомлений бота: тихие часы и выключенные виды.
-- Одним JSONB — набор видов знает приложение, и новая рассылка не должна
-- требовать миграции.
ALTER TABLE user_settings
  ADD COLUMN IF NOT EXISTS notify_prefs JSONB;

-- Отложенные на конец тихих часов сообщения. Очередь, а не пропуск:
-- напоминание, которое просто не пришло, — потерянное напоминание.
CREATE TABLE IF NOT EXISTS notify_queue (
  id         BIGSERIAL PRIMARY KEY,
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL,
  text       TEXT NOT NULL,
  send_after TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  sent_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS notify_queue_due_idx
  ON notify_queue (send_after) WHERE sent_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS notify_queue;
ALTER TABLE user_settings DROP COLUMN IF EXISTS notify_prefs;
