-- +goose Up

-- Страница Contacts: список контактов пользователя (те, с кем делился, или
-- добавленные вручную) с примечанием, фото и галочкой auto_accept —
-- «принимать расшаренные данные от этого контакта сразу».
CREATE TABLE contacts (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    contact_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    note        TEXT   NOT NULL DEFAULT '',
    photo       TEXT   NOT NULL DEFAULT '',  -- uploads/contacts/<файл>
    auto_accept BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, contact_id)
);

-- Входящие шаринги, ждущие подтверждения. Если у получателя отправитель
-- в контактах с auto_accept — применяются сразу и сюда не попадают.
-- kind: checker_template | checker_group | reminder_category | article |
--       tracker | task_project; ref_id — id источника у отправителя,
-- применение (копия/доступ) происходит в момент «Принять».
CREATE TABLE incoming_shares (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    from_user  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    to_user    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT   NOT NULL,
    ref_id     BIGINT NOT NULL,
    title      TEXT   NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX incoming_shares_to_idx ON incoming_shares (to_user, created_at DESC);

-- +goose Down
DROP TABLE incoming_shares;
DROP TABLE contacts;
