-- +goose Up
-- Запись релиза в журнал (notified_at NULL — бот уведомит админа на старте).
-- +goose StatementBegin
INSERT INTO releases (version, released_on, title, public_notes, tech_notes) VALUES (
  '2.132.0',
  '2026-11-14',
  'Пульт ТВ — материалы, слайд-шоу на паузе, актёры',
  E'Медиатека на приставке. В карточке «Инфо» — актёры с фотографиями. Нажмите на актёра — увидите, в каких ещё ваших фильмах и сериалах он играет.\n\nМатериалы: к фильму или сериалу можно прикрепить свои снимки и постеры (админка → «🖼» у строки) — они видны в «Инфо».\n\nСлайд-шоу на паузе: если фильм стоит на паузе дольше 10 минут, экран листает материалы сериала, а если их нет — снимки выбранной папки «Фото». Любая кнопка возвращает к фильму. Время и папка — в админке → «Оформление».',
  E'Медиаагент: materials.go (.materials рядом с плиткой, /api/materials, /material/{id}, target=material, DELETE /api/admin/materials, перенос при переименовании и в корзину), pauseshow.go (/api/pauseshow, looks.pause_show/pause_min/pause_album), cast.go (itemInfo.qid/cast/cast_at, ensureCast в фоне и в тихие часы, /person/{Q} — фото P18 с кэшем, /api/person — фильмография по своим описаниям). player.html: лента состава, экран актёра, материалы в «Инфо», слой #pauseshow, админка «🖼» и настройки паузы.'
);
-- +goose StatementEnd

-- +goose Down
DELETE FROM releases WHERE version = '2.132.0';
