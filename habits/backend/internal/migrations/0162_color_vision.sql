-- +goose Up
-- Результаты теста на цветовосприятие. Таблицы рисует клиент, поэтому храним
-- только итог: сколько таблиц каждого типа показали и сколько из них узнали.
CREATE TABLE IF NOT EXISTS color_vision_results (
  id        BIGSERIAL PRIMARY KEY,
  user_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  taken_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  total     INTEGER NOT NULL,
  correct   INTEGER NOT NULL,
  -- ошибки по типам: red-green (протан/дейтан) и сине-жёлтые таблицы
  protan    INTEGER NOT NULL DEFAULT 0,
  deutan    INTEGER NOT NULL DEFAULT 0,
  tritan    INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS color_vision_results_user_idx
  ON color_vision_results (user_id, taken_at DESC);

-- +goose Down
DROP TABLE IF EXISTS color_vision_results;
