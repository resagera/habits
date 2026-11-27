-- +goose Up
-- Запись релиза в журнал (notified_at NULL — бот уведомит админа на старте).
-- +goose StatementBegin
INSERT INTO releases (version, released_on, title, public_notes, tech_notes) VALUES (
  '2.131.0',
  '2026-11-14',
  'Пульт ТВ — озвучки и субтитры',
  E'Медиатека на приставке. Озвучки: если у серии несколько дорожек — встроенных или отдельными файлами рядом (S01E01.LostFilm.mka, .ac3, .mp3, в том числе в подпапках вроде «Rus Sound/LostFilm») — кнопка «🔊» в плеере и в пульте переключает их на ходу, выбор помнится для следующих серий. Звук, который браузер приставки не играет сам (AC3, DTS), агент перегоняет на лету, места на диске это не занимает.\n\nСубтитры: «💬» включает и выключает, «💬 ⇄» — другой язык; теперь понимаются и .ass.\n\nДобавить дорожку к серии — в админке «🔊＋» у серии или «📥» у папки сезона. Спрятанные внутри файлов озвучки и субтитры можно выгрузить рядом отдельными файлами: админка → «Задачи» → «🎧 Дорожки в отдельные файлы».',
  E'Медиаагент: tracks.go — fileIndex.side (видео → файлы дорожек, по индексу), sideLabel, trackRef в outItem.Audio, GET /trackaudio/{id}?a=&t= (ffmpeg → ADTS AAC, до 3 одновременно), dropSidecars в seasonItems, upload target=track, /api/admin/tracks/titles|scan|extract (задача tracks, ключ вид|язык|название, -c copy). trackTitle: handler_name у MP4. player.html: <audio id=va>, altTick, кнопки audio/subs/subsnext, карточка «🎧». RemoteView: кнопки 🔊/💬. send-tracks.py, send-series.py пишет title/handler_name.'
);
-- +goose StatementEnd

-- +goose Down
DELETE FROM releases WHERE version = '2.131.0';
