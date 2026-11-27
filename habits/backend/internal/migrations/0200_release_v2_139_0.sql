-- +goose Up
-- Запись релиза в журнал (notified_at NULL — бот уведомит админа на старте).
-- +goose StatementBegin
INSERT INTO releases (version, released_on, title, public_notes, tech_notes) VALUES (
  '2.139.0',
  '2026-11-28',
  'Пульт ТВ — альбом плиткой',
  E'Пульт ТВ. Альбомы «Фото и видео» теперь можно смотреть плиткой: все снимки и ролики папки маленькими превью на весь экран. Нажали на снимок — он открывается крупно, «Назад» возвращает к плитке. Превью медиасервер готовит заранее, ночью, поэтому плитка показывается сразу.',
  E'Медиаагент: thumbs.go — работа тихих часов thumbs (ночью при night_posters) и POST /api/admin/thumbs/run собирают превью 480 px всех альбомов в кэш photos/ (photoCopyPaths), неудача — .miss. Страница: вид tiles у записи photos (mode/fromTiles/shown), renderPhotoTiles по 120 превью, «Назад» из галереи с fromTiles — к плитке, htv.photoView: gallery | tiles | files.'
);
-- +goose StatementEnd

-- +goose Down
DELETE FROM releases WHERE version = '2.139.0';
