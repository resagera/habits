-- +goose Up
-- Запись релиза в журнал. notified_at не указываем (NULL) — на старте после
-- деплоя на прод сервер уведомит админа и проставит notified_at.
-- ШАБЛОН для будущих релизов: копируем этот файл с новым номером и версией.
-- StatementBegin/End — чтобы goose не резал INSERT по «;» внутри текста.
-- +goose StatementBegin
INSERT INTO releases (version, released_on, title, public_notes, tech_notes) VALUES (
  '2.47.0',
  '2026-07-27',
  'страница «Релизы» — журнал версий с публичным и техническим описанием',
  'Новая страница «Релизы» (пока только для админа): список версий сворачиваемыми блоками, у каждой — что нового для пользователя, дата и статус. Уведомление о новом релизе теперь приходит в бот.',
  E'Таблица releases (миграция 0044, seed из git-истории v2.13.0..v2.46.0). Публичное поле public_notes видно всем, tech_notes и comment — только админам (API обычным пользователям их не отдаёт). GET /releases (роль решает набор полей), PATCH /releases/{id} — админ правит комментарий/статус. Страница apps/releases (meta.admin), сворачиваемые блоки; техблок и комментарий видит только админ. Уведомление админу — announceReleases на старте сервера по строкам с notified_at IS NULL. Changelog в habits/CHANGELOG.md (Keep a Changelog).'
);
-- +goose StatementEnd

-- +goose Down
DELETE FROM releases WHERE version = '2.47.0';
