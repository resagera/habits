-- +goose Up
-- Текстуры стен для игр: админ пополняет набор картинками-плитками, игра
-- берёт случайную. Файлы лежат в DATA_DIR/textures и раздаются как остальные
-- загрузки — по невосстановимому случайному имени.
CREATE TABLE IF NOT EXISTS game_textures (
  id          BIGSERIAL PRIMARY KEY,
  game        TEXT NOT NULL,
  title       TEXT NOT NULL DEFAULT '',
  filename    TEXT NOT NULL,
  uploaded_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS game_textures_game_idx ON game_textures (game, id);

-- +goose Down
DROP TABLE IF EXISTS game_textures;
