-- +goose Up

-- Внешние контакты: людей, которых ещё нет в боте, тоже можно добавлять.
-- contact_id (FK на users) становится NULL для «не в боте»; известные данные
-- храним рядом: tg_id (числовой Telegram id, если добавляли по id),
-- ext_username / ext_name (что удалось узнать через Bot API getChat или из
-- введённого @логина). Когда человек откроет бота, TouchUser привяжет
-- contact_id по tg_id или совпадению логина.
ALTER TABLE contacts ALTER COLUMN contact_id DROP NOT NULL;
ALTER TABLE contacts ADD COLUMN tg_id        BIGINT;
ALTER TABLE contacts ADD COLUMN ext_username TEXT NOT NULL DEFAULT '';
ALTER TABLE contacts ADD COLUMN ext_name     TEXT NOT NULL DEFAULT '';

-- Несколько фото на контакт (галерея) вместо одного.
CREATE TABLE contact_photos (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    contact_id BIGINT NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    photo      TEXT   NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX contact_photos_contact_idx ON contact_photos (contact_id, id);

INSERT INTO contact_photos (contact_id, photo)
    SELECT id, photo FROM contacts WHERE photo <> '';
ALTER TABLE contacts DROP COLUMN photo;

-- +goose Down
ALTER TABLE contacts ADD COLUMN photo TEXT NOT NULL DEFAULT '';
UPDATE contacts c SET photo = COALESCE(
    (SELECT photo FROM contact_photos p WHERE p.contact_id = c.id ORDER BY p.id LIMIT 1), '');
DROP TABLE contact_photos;
DELETE FROM contacts WHERE contact_id IS NULL;
ALTER TABLE contacts DROP COLUMN ext_name;
ALTER TABLE contacts DROP COLUMN ext_username;
ALTER TABLE contacts DROP COLUMN tg_id;
ALTER TABLE contacts ALTER COLUMN contact_id SET NOT NULL;
