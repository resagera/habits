-- +goose Up
-- Доступ к приставке для других аккаунтов. Раньше приставка закреплялась за
-- тем, кто первым подключил пульт, и второй телефон с другим аккаунтом
-- получал «закреплена за другим аккаунтом». Теперь владелец сам открывает
-- доступ тем, кому доверяет (Контакты → поделиться, вид «tv_room»).
--
-- id нужен общему механизму шаринга: он ссылается на объект числом.
ALTER TABLE tv_rooms ADD COLUMN IF NOT EXISTS id BIGSERIAL;
CREATE UNIQUE INDEX IF NOT EXISTS tv_rooms_id_idx ON tv_rooms (id);

CREATE TABLE IF NOT EXISTS tv_room_members (
    room_key    TEXT NOT NULL REFERENCES tv_rooms(key) ON DELETE CASCADE ON UPDATE CASCADE,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (room_key, user_id)
);
CREATE INDEX IF NOT EXISTS tv_room_members_user_idx ON tv_room_members (user_id);

-- +goose Down
DROP TABLE IF EXISTS tv_room_members;
DROP INDEX IF EXISTS tv_rooms_id_idx;
ALTER TABLE tv_rooms DROP COLUMN IF EXISTS id;
