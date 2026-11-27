-- +goose Up
-- Запись релиза в журнал (notified_at NULL — бот уведомит админа на старте).
-- +goose StatementBegin
INSERT INTO releases (version, released_on, title, public_notes, tech_notes) VALUES (
  '2.136.0',
  '2026-11-23',
  'Пульт ТВ — планы и актёры',
  E'Пульт ТВ. В админке появились «Планы»: что посмотреть или скачать и что сделать с уже лежащим. Вставьте список — по строке на фильм или сериал, — и постер, дата выхода, описание и актёры найдутся сами; к плану можно написать заметку, сменить постер или добавить свой, а выполненное убрать в архив. Рядом «Актёры» — все, кто есть в вашей медиатеке, с правкой фото и описания. На экране актёра теперь его полная фильмография, и видно, что из неё уже есть у вас или в планах. Правки описания больше не теряются при повторном поиске в интернете.',
  E'Медиаагент: plans.go (plans.json + plans/<id>/, фоновый поиск fetchInfo/findPoster с фолбэком типа, /api/admin/plans*, /planposter/{id}, adminImage target=plan); itemInfo.released (P577 precision 11, без deprecated) и edited_fields (markEdited/mergeRefetch, reset в /api/admin/info); people.go (индекс людей с склейкой имени и Q-id, people.json, свои фото people/<Q|n-sha1>.custom.jpg, people_photos в /api/info); filmography.go (Wikidata SPARQL P161, кэш 30 дней, промах — час, отметки have/plan в /api/person); cast.go — fillCast, ensurePlanCast, ensurePersonPhoto, castTargets, работа тихих часов cast и POST /api/admin/cast/run (job.Now идёт вне окна). Страница: вкладки «Планы» и «Актёры», «📋» у строки медиатеки, карточка «👥 Актёры» в «Задачах», фильмография на экране актёра; попутно .mkvid[hidden] — пустое видео редактора отметок больше не висит над админкой.'
);
-- +goose StatementEnd

-- +goose Down
DELETE FROM releases WHERE version = '2.136.0';
