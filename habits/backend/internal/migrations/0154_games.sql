-- +goose Up
-- Страница «Игры»: рекорды и картинки, выбранные под каждую игру.
--
-- Рекорд — одна строка на игру: что считается лучшим (больше очков или
-- меньше секунд), решает сервер по коду игры, а не колонка.
CREATE TABLE IF NOT EXISTS game_scores (
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  game       TEXT NOT NULL,
  best       INTEGER NOT NULL DEFAULT 0,
  detail     JSONB,
  played     INTEGER NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, game)
);

-- Картинки под игру — ссылки на уже загруженные фоны пользователя, а не
-- копии файлов: удалили картинку в «Оформлении» — она пропадает и из игры
-- (ON DELETE CASCADE), висячих ссылок не остаётся.
CREATE TABLE IF NOT EXISTS game_backgrounds (
  user_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  game     TEXT NOT NULL,
  image_id BIGINT NOT NULL REFERENCES user_backgrounds(id) ON DELETE CASCADE,
  PRIMARY KEY (user_id, game, image_id)
);

-- +goose Down
DROP TABLE IF EXISTS game_backgrounds;
DROP TABLE IF EXISTS game_scores;
