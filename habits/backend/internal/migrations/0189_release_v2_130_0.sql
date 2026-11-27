-- +goose Up
-- Запись релиза в журнал (notified_at NULL — бот уведомит админа на старте).
-- +goose StatementBegin
INSERT INTO releases (version, released_on, title, public_notes, tech_notes) VALUES (
  '2.130.0',
  '2026-11-13',
  'Пульт ТВ — тихие часы, «Не читается», сводка сторожа',
  E'Медиатека на приставке. Тихие часы: в админке → «Задачи» задаётся ночное окно (по умолчанию с 01:00 до 07:00), когда агенту можно заниматься долгой работой. Каждую ночь он сам размечает заставки и титры у сезонов, где их ещё нет, и заранее собирает постеры и описания — первая прокрутка каталога больше не дёргается. Любую такую работу можно поставить и руками — «🌙 В тихие часы». Кончились часы посреди работы — она встаёт на паузу и продолжит следующей ночью, в «Задачах» это видно.\n\n«⚠ Не читается»: в админке → «Обслуживание» — битые, оборванные и недокачанные видео, которые раньше молча пропадали из списков. Их можно сразу убрать в корзину, а о новых находках агент пишет в Telegram.\n\nСторож папки convert_and_del теперь присылает сводку: что уехало на приставку, что не доехало и где не хватило места.',
  E'Медиаагент: quiet.go — quietStore (quiet.json: окно, очередь работ, Night, Tried), quietWorker раз в 30 с, работы marks/posters по элементам, errQuietOver → стадия «⏸ ждёт тихих часов»; API GET|PUT /api/admin/quiet, POST /api/admin/quiet/jobs, DELETE /api/admin/quiet/jobs/{id}. ensurePoster/ensureInfo вынесены из обработчиков. broken.go — GET /api/admin/broken по индексу, reportBroken после обхода (broken-seen.json). convert-watch.py — notify() через /api/v1/tv/notify, ключ комнаты по SSH, warn_once, --no-notify.'
);
-- +goose StatementEnd

-- +goose Down
DELETE FROM releases WHERE version = '2.130.0';
