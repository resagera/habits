-- +goose Up
-- Коннектор: правила «источник → условия → результат», связывающие страницы.
--
-- Условия лежат деревом в JSONB, а не отдельными строками-таблицами: вложенные
-- И/ИЛИ в реляционной схеме пришлось бы собирать рекурсивным запросом на каждую
-- проверку, а правило всегда читается и пишется целиком.
--   группа: {"op":"and"|"or","nodes":[ ... ]}
--   лист:   {"field":"from","cmp":"contains","value":"taxi.yandex.ru"}
-- Результаты — массивом объектов: {"type":"tracker_mark","category_id":1,...}.
CREATE TABLE IF NOT EXISTS connector_rules (
  id          BIGSERIAL PRIMARY KEY,
  user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  enabled     BOOLEAN NOT NULL DEFAULT true,
  -- коды источников: ["mail"]. Их несколько, потому что одно правило может
  -- ловить одно и то же событие из разных мест
  sources     JSONB NOT NULL DEFAULT '[]',
  -- дерево условий; пустая группа значит «подходит любое событие источника»
  match       JSONB NOT NULL DEFAULT '{"op":"and","nodes":[]}',
  actions     JSONB NOT NULL DEFAULT '[]',
  -- часовой пояс человека: по нему считается «сегодня» для отметки в трекере.
  -- Как у напоминаний — пояс приходит с устройства, глобального нет
  tz_offset_minutes INT NOT NULL DEFAULT 0
    CHECK (tz_offset_minutes BETWEEN -840 AND 840),
  runs        INT NOT NULL DEFAULT 0,
  last_run_at TIMESTAMPTZ,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS connector_rules_user_idx ON connector_rules (user_id, id);

-- Журнал срабатываний. Пишутся только сработавшие правила и ошибки: писать
-- строку на каждое «не подошло» значит заполнить журнал мусором, а на вопрос
-- «почему не сработало» отвечает кнопка «Проверить на письме».
CREATE TABLE IF NOT EXISTS connector_runs (
  id       BIGSERIAL PRIMARY KEY,
  rule_id  BIGINT NOT NULL REFERENCES connector_rules(id) ON DELETE CASCADE,
  user_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source   TEXT NOT NULL,
  -- событие-источник: id письма. Уникальность вместе с правилом не даёт
  -- отметить одно письмо дважды — ни повторным разбором, ни кнопкой «применить»
  event_id BIGINT,
  -- чем событие было видно человеку: тема письма и отправитель
  event_label TEXT NOT NULL DEFAULT '',
  status   TEXT NOT NULL DEFAULT 'done', -- done | error
  detail   TEXT NOT NULL DEFAULT '',
  at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS connector_runs_event_uidx
  ON connector_runs (rule_id, source, event_id) WHERE event_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS connector_runs_user_idx ON connector_runs (user_id, at DESC);

-- +goose Down
DROP TABLE IF EXISTS connector_runs;
DROP TABLE IF EXISTS connector_rules;
