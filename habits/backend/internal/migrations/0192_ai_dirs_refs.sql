-- +goose Up
-- Папки, разрешённые машине ИЗ ПРИЛОЖЕНИЯ (а не из AI_AGENT_DIRS агента), и
-- сессии CLI, добавленные вручную по id.
--
-- Папки: до сих пор белый список приходил только в hello агента (ai_machines.dirs)
-- и менялся правкой ~/.config/habits-ai-agent.env с рестартом сервиса. Теперь
-- папку можно разрешить со страницы AI: сервер хранит её здесь и присылает
-- агенту кадром `dirs` (при подключении и при каждом изменении). Агент держит
-- такие папки отдельно от своих и может вообще их не принимать
-- (AI_AGENT_REMOTE_DIRS=0) — конфиг машины остаётся сильнее приложения.
--
-- Сессии: список старых сессий агент строит сканом файлов CLI и отдаёт только
-- 300 самых свежих из разрешённых папок. Сессию, которой там нет (слишком
-- старая или папка разрешена только что), можно добавить вручную по id —
-- ссылка живёт здесь и остаётся на странице, даже когда агент офлайн.
-- +goose StatementBegin
CREATE TABLE ai_machine_dirs (
  id          BIGSERIAL PRIMARY KEY,
  machine_id  BIGINT NOT NULL REFERENCES ai_machines(id) ON DELETE CASCADE,
  path        TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (machine_id, path)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE ai_session_refs (
  id          BIGSERIAL PRIMARY KEY,
  machine_id  BIGINT NOT NULL REFERENCES ai_machines(id) ON DELETE CASCADE,
  tool        TEXT NOT NULL,
  session_id  TEXT NOT NULL,
  workdir     TEXT NOT NULL,
  title       TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (machine_id, tool, session_id)
);
-- +goose StatementEnd

-- Версия агента из hello: на странице видно, понимает ли агент папки из
-- приложения (кадр `dirs` появился в 1.8.0) — иначе папка молча не работала бы.
-- +goose StatementBegin
ALTER TABLE ai_machines ADD COLUMN IF NOT EXISTS agent_version TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
ALTER TABLE ai_machines DROP COLUMN IF EXISTS agent_version;
DROP TABLE IF EXISTS ai_session_refs;
DROP TABLE IF EXISTS ai_machine_dirs;
