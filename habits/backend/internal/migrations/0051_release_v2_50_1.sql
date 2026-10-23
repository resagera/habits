-- +goose Up
-- Запись релиза в журнал (notified_at NULL — бот уведомит админа на старте).
-- +goose StatementBegin
INSERT INTO releases (version, released_on, title, public_notes, tech_notes) VALUES (
  '2.50.1',
  '2026-07-28',
  'AI — модель Fable в выборе для Claude Code',
  'В форме задачи для Claude Code добавлена модель Fable (флагман семейства Claude 5) — раньше в списке были только Opus/Sonnet/Haiku.',
  E'Алиас fable поддерживается claude CLI нативно (--model fable → claude-fable-5, подтверждено пингом с разбором modelUsage). Добавлена option в select формы задачи (AIView.vue); бэкенд/агент без изменений — model передаётся как есть.'
);
-- +goose StatementEnd

-- +goose Down
DELETE FROM releases WHERE version = '2.50.1';
