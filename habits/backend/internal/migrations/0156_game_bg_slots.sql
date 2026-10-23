-- +goose Up
-- У картинок игры появилось назначение: фон всей игры и, отдельно, рубашка
-- карт в «найди пару». Раньше набор был один на игру.
ALTER TABLE game_backgrounds
  ADD COLUMN IF NOT EXISTS slot TEXT NOT NULL DEFAULT 'game';

ALTER TABLE game_backgrounds DROP CONSTRAINT IF EXISTS game_backgrounds_pkey;
ALTER TABLE game_backgrounds ADD PRIMARY KEY (user_id, game, slot, image_id);

-- +goose Down
ALTER TABLE game_backgrounds DROP CONSTRAINT IF EXISTS game_backgrounds_pkey;
DELETE FROM game_backgrounds WHERE slot <> 'game';
ALTER TABLE game_backgrounds DROP COLUMN IF EXISTS slot;
ALTER TABLE game_backgrounds ADD PRIMARY KEY (user_id, game, image_id);
