-- +goose Up
-- Автосмена фона: режим, папка-источник для тёмной и светлой темы и дата
-- последней смены (для режима «раз в день»).
-- Папка хранится числом: -1 — все картинки, 0 — «без папки», иначе id папки.
-- Внешнего ключа нет намеренно: удалённая папка просто перестаёт давать
-- картинки, а настройку чинит пользователь, а не каскад.
ALTER TABLE user_settings
  ADD COLUMN IF NOT EXISTS bg_auto_mode  text NOT NULL DEFAULT 'off',
  ADD COLUMN IF NOT EXISTS bg_auto_dark  bigint,
  ADD COLUMN IF NOT EXISTS bg_auto_light bigint,
  ADD COLUMN IF NOT EXISTS bg_auto_day   date;

-- +goose Down
ALTER TABLE user_settings
  DROP COLUMN IF EXISTS bg_auto_mode,
  DROP COLUMN IF EXISTS bg_auto_dark,
  DROP COLUMN IF EXISTS bg_auto_light,
  DROP COLUMN IF EXISTS bg_auto_day;
