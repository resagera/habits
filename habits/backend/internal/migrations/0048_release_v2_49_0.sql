-- +goose Up
-- Запись релиза в журнал (notified_at NULL — бот уведомит админа на старте).
-- +goose StatementBegin
INSERT INTO releases (version, released_on, title, public_notes, tech_notes) VALUES (
  '2.49.0',
  '2026-07-28',
  'кнопка «копировать в буфер» у всех текстовых полей',
  'У любого текстового поля на любой странице при фокусе справа появляется кнопка-иконка: нажатие копирует содержимое поля в буфер обмена и показывает подтверждение «Скопировано». Поле при этом не теряет фокус — можно продолжать редактирование. Работает и в однострочных полях, и в многострочных (заметки, описания).',
  E'Глобальный механизм без правки компонентов: ОДИН fixed-оверлей на всё приложение — shared/copyField.ts (installCopyField() в main.ts), стиль .copy-field-btn в shared/theme/theme.css. document.focusin определяет текстоподобное поле (input text/search/url/email/tel/number, textarea; password исключён — у Passwords свои кнопки), кнопка позиционируется по getBoundingClientRect у правого края (у высоких textarea — сверху), пересчёт на input/scroll (capture)/resize/visualViewport. Копирование на pointerdown с preventDefault (фокус не уходит), navigator.clipboard с фолбэком execCommand(''copy''), тост «Скопировано». Опт-аут для поля — атрибут data-no-copy. Скрывается у пустых и слишком узких полей.'
);
-- +goose StatementEnd

-- +goose Down
DELETE FROM releases WHERE version = '2.49.0';
