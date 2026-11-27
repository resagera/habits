-- +goose Up
-- Запись релиза в журнал (notified_at NULL — бот уведомит админа на старте).
-- +goose StatementBegin
INSERT INTO releases (version, released_on, title, public_notes, tech_notes) VALUES (
  '2.137.0',
  '2026-11-23',
  'Пульт ТВ — фильмография с постерами',
  E'Пульт ТВ. Фильмографию актёра теперь можно смотреть не только списком, но и карточками: у каждого фильма постер и пара фраз о нём по-русски. Постеры и описания сохраняются на вашем медиасервере, поэтому при следующем открытии всё показывается сразу.',
  E'Медиаагент: filmcards.go — карточки по Q-id (wbgetentities sitelinks/descriptions на 50 Q-id, ruwiki extracts+pageimages по 20, постер из enwiki при отсутствии, описание Wikidata как запасное), кэш films/<Q>.json на 30 дней, постеры films/<Q>.jpg 300×450 — по одному с паузой 400 мс и фоновой докачкой, временные отказы (429/5xx/сеть) — повтор через 3 с без .miss; GET /api/filmcards?ids=, GET /filmposter/{qid}; download возвращает httpStatusError. Страница: переключатель «☰ Список / ▦ С постерами» (htv.filmoView), карточки дополняются на месте без перерисовки. Скрипт link-series.py — сериал из нескольких папок раздачи символическими ссылками.'
);
-- +goose StatementEnd

-- +goose Down
DELETE FROM releases WHERE version = '2.137.0';
