# Медиаагент: «Планы» и «Актёры» — план работ

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** вкладки админки «Планы» (план фильма/сериала с поиском сведений, заметкой, постерами, архивом) и «Актёры» (список, правка, полная фильмография из Wikidata), плюс задача сбора состава по всей медиатеке.

**Architecture:** всё у медиаагента: `plans.json` + `plans/<id>/`, `people.json` + `people/` в кэше; поиск — существующие `fetchInfo`/`findPoster`/`cast.go`; фильмография — Wikidata SPARQL с кэшем; страница — `player.html`.

**Tech Stack:** Go stdlib (без зависимостей), ванильный JS в `player.html`.

**Spec:** `docs/superpowers/specs/2026-11-23-media-plans-actors-design.md`

## Global Constraints

- Сборка: `GOTOOLCHAIN=auto /usr/local/go/bin/go`, из `habits/media-agent`; тесты `go test ./...`.
- Никаких новых зависимостей; страница вшита — после правки `player.html` пересобрать бинарник.
- Сеть в тестах не трогать: внешние вызовы — через переменные-функции, подменяемые в тестах.
- Лимит заметки в плашке — 80 символов; фильмография — не больше 300 строк, кэш 30 дней.
- Ключ актёра без Q-id — `name:Имя`; его файлы — `people/n-<sha1(имя в нижнем регистре)[:12]>…`.
- Новые админские ручки — с `X-Admin-Pin`; имена файлов из запроса — только `filepath.Base`-чистые.
- Тексты интерфейса — по-русски, в стиле существующих.

## Review Focus

- Повторная вставка того же списка → дубли планов не создаются (совпадение `query`+`kind` среди активных).
- Пустые строки и пробелы в списке → пропускаются; список из 0 строк → 400.
- Поиск плана не нашёл ничего → `state:"notfound"`, план остаётся с названием из строки, `info.title = query`.
- Удаление плана во время фонового поиска → поиск не воскрешает план.
- Актёр, у которого своё фото и скачанное → отдаётся своё; удаление своего → снова скачанное.

---

### Task 1: Дата выхода и правки по полям в `itemInfo`

**Files:**
- Modify: `habits/media-agent/info.go` (`itemInfo`, `fill`, `adminInfo`)
- Test: `habits/media-agent/info_fields_test.go` (новый)

**Interfaces:**
- Produces:
  - `itemInfo.Released string` (`json:"released,omitempty"`, `YYYY-MM-DD`), `itemInfo.EditedFields []string` (`json:"edited_fields,omitempty"`).
  - `infoFields = []string{"title","year","released","genres","ratings","description","actors"}`.
  - `func editedFields(old, neu *itemInfo) []string` — какие поля правка изменила (сравнение по полям).
  - `func mergeRefetch(old, found *itemInfo) *itemInfo` — `found`, но поля из `old.EditedFields` (или все, если `old.Edited && len(old.EditedFields)==0`) взяты из `old`; `Edited/EditedFields` переносятся.
  - `adminInfo` запрос получает `Reset bool` (`json:"reset"`): `fetch` без `reset` → `mergeRefetch(старое, найденное)`; с `reset` → найденное как есть.

- [ ] **Step 1: Тесты** `TestReleasedFromWikidata` (claim P577 `+2027-03-12T00:00:00Z` precision 11 → `Released=="2027-03-12"`, `Year==2027`; precision 9 → `Released==""`), `TestMergeRefetchKeepsEdited` (old `{Title:"Моё",Description:"старое",EditedFields:["title"]}`, found `{Title:"Their",Description:"новое"}` → `Title=="Моё"`, `Description=="новое"`; old `Edited:true` без списка → всё старое), `TestAdminInfoRefetchKeepsEdits` (правка `title` через `adminInfo{info}` → `edited_fields` содержит `title`; затем `fetch` с подменённым `fetchInfoFn` → title сохранён; `fetch+reset` → title найденный).
- [ ] **Step 2:** `go test -run 'Released|MergeRefetch|AdminInfoRefetch'` → FAIL.
- [ ] **Step 3:** Реализовать. В `fill` разбирать `precision` из значения времени; дата — самая ранняя P577 с precision ≥ 11. Ввести `var fetchInfoFn = fetchInfo` и звать его в `lookupInfo`/`adminInfo` (подмена в тестах). При ручной правке: `inf.EditedFields = union(old.EditedFields, editedFields(old, inf))`.
- [ ] **Step 4:** тесты PASS, весь `go test ./...` PASS.
- [ ] **Step 5:** коммит не делаем по задачам — релиз одним коммитом в конце (порядок проекта).

### Task 2: Хранилище и API планов

**Files:**
- Create: `habits/media-agent/plans.go`
- Modify: `serve.go` (маршруты, `s.plans`), `looks.go` (`adminImage` `target=plan`), `main.go`/конструктор сервера (создание стора)
- Test: `habits/media-agent/plans_test.go`

**Interfaces:**
- Consumes: `fetchInfoFn`, `mergeRefetch` (Task 1); `findPoster`, `renderJPEG`, `s.imageSource`, `s.sign/validKey`, `s.adminPath`.
- Produces:
  ```go
  type plan struct {
      ID, Query, Kind, Type, Note, Tile, Poster, State string // Kind: film|series|anime; Type: film|series
      Info     *itemInfo
      Archived, Created, Updated int64
  } // json: id, query, kind, type, note, tile, poster, state, info, archived, created, updated
  type planStore struct{ /* mu, path, list []plan */ }
  func newPlanStore(cacheDir string) *planStore
  func (p *planStore) all() []plan; find(id string) (plan, bool); add(pl plan) plan; update(id string, fn func(*plan)) bool; remove(id string) bool
  func (s *server) planDir(id string) string            // <cache>/plans/<id>
  func (s *server) planPosters(id string) []string      // имена poster-*.jpg по порядку
  func (s *server) planOut(pl plan) map[string]any      // + poster_url, posters:[{name,url}]
  func (s *server) searchPlan(id string, as string, reset bool) // фон: info + постер; Type-фолбэк
  var findPosterFn = findPoster
  ```
- Маршруты (админские под PIN, как существующие):
  - `GET /api/admin/plans?archived=0|1` → `{plans:[planOut]}` (новые сверху).
  - `POST /api/admin/plans` — `{lines:[...],kind}` | `{plan:{query,kind,type,note,info?}}` | `{tile,note}` → `{plans:[...]}`.
  - `PUT /api/admin/plans/{id}` — поля `note, kind, type, poster, archived(bool), info(*itemInfo)`; info-правка ставит `EditedFields` как в Task 1.
  - `POST /api/admin/plans/{id}/refetch` `{as?, reset?}`; `DELETE /api/admin/plans/{id}`; `DELETE /api/admin/plans/{id}/poster?n=`.
  - `GET /planposter/{id}?k=&n=` — подпись `s.sign(id)`, имя `validPlanPoster(n)` (`poster-*.jpg`, без путей).
  - `POST /api/admin/image?target=plan&id=` → `{name}`; первый постер становится главным.

- [ ] **Step 1: Тесты** (подменить `fetchInfoFn`, `findPosterFn`; ждать фон хелпером `waitPlans(t,s)` — пока у всех `State=="searching"`):
  - `TestPlansFromLines`: `lines:["Дюна 2 (2024)","", "  ","Severance"]`, `kind:"series"` → 2 плана, `type=="series"`; после поиска `info.title` из фейка, `poster!=""`, `poster_url` содержит `/planposter/`; повтор того же запроса → всё ещё 2.
  - `TestPlanTypeFallback`: фейк находит только `kind=="film"` → план с `kind:"anime"` получает `type=="film"`.
  - `TestPlanNotFound`: фейк всегда ошибка → `state=="notfound"`, `info.title==query`.
  - `TestPlanForTile`: плитка с `*.info.json` и постером → план `tile==id`, info скопировано, постер скопирован в `plans/<id>/`, поиск в интернет не звался.
  - `TestPlanEditArchivePosters`: PUT `note`/`info.title` → refetch сохраняет title; `archived:true` → в `?archived=1`, не в `?archived=0`; загрузка постера PNG через `adminImage` → 2 постера, PUT `poster` меняет главный; `/planposter` с `n=../x` → 403; DELETE плана → каталог `plans/<id>` удалён, и поиск, запущенный до удаления, план не возвращает.
  - `TestPlansNeedPin`: без PIN → 403.
- [ ] **Step 2:** `go test -run Plan` → FAIL.
- [ ] **Step 3:** Реализовать `plans.go`. Сохранение — `writeAtomic` JSON, как у `quiet`. Поиск: `infoSem`, затем `posterSem`; `searchPlan` после сети перечитывает план через `find` и пишет только если он ещё есть. Название строки: `cleanTitle(query, false)` → title+year. Тип по умолчанию: `film` для `kind=film`, иначе `series`.
- [ ] **Step 4:** тесты PASS, `go test ./...` PASS.

### Task 3: Люди — список, правки, своё фото

**Files:**
- Create: `habits/media-agent/people.go`
- Modify: `cast.go` (`personPhoto` — своё фото и `n-` ключи), `looks.go` (`target=person`), `serve.go`
- Test: `habits/media-agent/people_test.go`

**Interfaces:**
- Consumes: `s.rawVideoTiles`, `readInfo`, `s.plans.all()` (Task 2), `castHit`.
- Produces:
  ```go
  type personEdit struct{ Name, About string; Photo bool; Edited int64 } // json name, about, photo, edited
  type peopleStore struct{ /* people.json */ } ; func newPeopleStore(cacheDir string) *peopleStore
  func (p *peopleStore) get(key string) (personEdit, bool); set(key string, fn func(*personEdit))
  func nameKey(name string) string            // "name:"+name
  func personFileID(key string) string        // "Q123" | "n-<sha1[:12]>"
  type personRow struct{ Key, QID, Name, About, Photo string; Have, Plans int } // json key,qid,name,about,photo,have,plans
  func (s *server) peopleIndex(r *http.Request) []personRow // имя→Q-id склейка по точному имени без регистра
  ```
- Маршруты: `GET /api/admin/people` → `{people:[personRow]}` (сорт.: `Have` убыв., имя); `PUT /api/admin/people?key=` `{name,about}`; `POST /api/admin/image?target=person&id=<key>`; `DELETE /api/admin/image?target=person&id=<key>`; `GET /person/{pid}` принимает `Q…` и `n-[0-9a-f]{12}`, своё (`<pid>.custom.jpg`) главнее скачанного; у `n-` без своего — 404.
- `/api/person` добавляет `about` (своё) и `name` (своё главнее); `photo` — и для `name:` ключа, если есть своё.

- [ ] **Step 1: Тесты** `TestPeopleIndex` (фильм с `Cast:[{Том Харди,Q208026}]`, сериал с `Actors:["Том Харди","Кто-то"]`, план с `Cast:[{Том Харди,Q208026}]` → строка Q208026 `have==2, plans==1`; «Кто-то» — `key=="name:Кто-то"`, `have==1`), `TestPersonEditAndPhoto` (PUT about → в `/api/person?qid=Q208026` `about`; загрузка фото на `name:Кто-то` → `GET /person/n-…` 200 image/jpeg; на Q208026 своё фото при наличии скачанного `people/Q208026.jpg` → отдаётся своё (сравнить байты); DELETE → снова скачанное).
- [ ] **Step 2:** FAIL. **Step 3:** реализовать. **Step 4:** PASS + `go test ./...`.

### Task 4: Фильмография из Wikidata

**Files:**
- Create: `habits/media-agent/filmography.go`, `habits/media-agent/testdata/sparql_person.json` (записанный ответ, 3–4 строки: фильм, сериал, дубль одного произведения)
- Modify: `cast.go` (`person` handler)
- Test: `habits/media-agent/filmography_test.go`

**Interfaces:**
- Produces:
  ```go
  type filmRow struct{ QID, Title string; Year int; Kind, Have, Plan string } // json qid,title,year,kind(film|series),have,plan
  var sparqlFn = sparqlQuery // func(q string) ([]byte, error), GET query.wikidata.org/sparql?format=json, User-Agent "habits-media-agent/1.0", таймаут 8 с
  func parseFilmography(body []byte) []filmRow   // дубли по qid склеиваются, сорт. год убыв., ≤300
  func (s *server) filmography(qid string) ([]filmRow, bool) // кэш people/<qid>.films.json 30 дней; false — не успели (pending)
  func (s *server) markFilms(rows []filmRow, r *http.Request) // Have по info.QID плиток, Plan по info.QID планов
  ```
- SPARQL: `?w wdt:P161 wd:<QID>; wdt:P31/wdt:P279* ?t. VALUES ?t {wd:Q11424 wd:Q5398426}` + `MIN(year of P577)` + `rdfs:label` ru, иначе en; `LIMIT 300`.
- `/api/person` при `qid`: `films`, а если ещё не готово — `films_pending:true` и фоновая догрузка (не больше одной на qid одновременно).

- [ ] **Step 1: Тесты** `TestParseFilmography` (из testdata: N уникальных, порядок по году, kind сериала=="series"), `TestPersonFilms` (подменённый `sparqlFn` → `/api/person?qid=Q208026` содержит `films`, у фильма из медиатеки `have==id плитки`, у плана `plan==id плана`; второй запрос не зовёт `sparqlFn` — кэш; `sparqlFn` ошибка → `films:[]`, без падения).
- [ ] **Step 2:** FAIL. **Step 3:** реализовать (запрос бережно: через `wikimediaGap`). **Step 4:** PASS.

### Task 5: Задача «Актёры по всей медиатеке»

**Files:**
- Modify: `cast.go` (`ensureCastInfo(inf *itemInfo) bool` — общая часть для плитки и плана), `quiet.go` (вид `cast`, ночное планирование), `plans.go` (у плана — ensureCast при поиске), `admin.go`/`serve.go` (кнопка)
- Test: `habits/media-agent/cast_job_test.go`

**Interfaces:**
- Consumes: `wikidataEntityFn` (ввести `var wikidataEntityFn = wikidataEntity`), `personPhotoPrefetch(qid)`.
- Produces: quiet-работа `Kind:"cast"`, элементы — пути плиток и `plan:<id>`; `func (s *server) castTargets() []string` (плитки с `CastAt==0` + планы с `Info.CastAt==0` и `Info!=nil`); `POST /api/admin/cast/run` → ставит работу и сразу запускает её задачей (вне тихих часов тоже — флаг `Now bool` у `quietJob`, который `runQuietJob` использует, чтобы не проверять окно).
- `quietPlanNight` добавляет `cast`, если `castTargets()` не пуст и такой работы нет.

- [ ] **Step 1: Тест** `TestCastJob`: две плитки с описанием без `CastAt` и план; подмена `wikidataEntityFn` (возвращает entity с P161) и фото; `POST /api/admin/cast/run` → `waitIdle` → у всех `CastAt!=0`, `Cast` непуст; повторный `castTargets()` пуст.
- [ ] **Step 2:** FAIL. **Step 3:** реализовать. **Step 4:** PASS + `go test ./...`.

### Task 6: Страница — «Планы», «Актёры», экран актёра

**Files:**
- Modify: `habits/media-agent/player.html`

**Interfaces:**
- Consumes: ручки Tasks 2–5.
- Produces (JS): вкладки `data-adtab="plans"` и `data-adtab="people"` после «Медиатеки»; `renderAdminPlans()`, `renderAdminPlan()` (`adminState.plan`), `renderAdminPeople()`, `renderAdminPerson()` (`adminState.person`); в `renderAdminDir` у фильма/сериала кнопка `data-adplanfor="<id>"` «📋 В план»; в «Задачах» карточка «👥 Актёры» с кнопкой `POST /api/admin/cast/run`; `renderPerson` — описание и блок «Фильмография».
- Плашка плана: постер, «Название · ДД.ММ.ГГГГ» (или «(ГГГГ)»), заметка `≤80` символов с «…», ⏳ при `searching`, ⚠ при `notfound`. Разделы: Фильмы / Сериалы / Аниме. Сверху: «＋ Списком» (textarea + раздел), «＋ Один» (форма как `renderAdminInfo` + «Заметка», «Раздел», «Тип»), «Активные / Архив».
- Карточка: лента постеров (★ главный, ✖, «＋ постер» из «Фото» или файлом — пикер `target:'plan'`), поля с ✏, «🔎 Найти заново», «🔎 Искать как…», «🔎 Заново, сбросив правки», «📦 В архив/↩ Из архива», «✖ Удалить», «▶ Открыть в медиатеке» при `tile`; состав — `castHTML`.
- Пока есть план в `searching` — перерисовка раз в 2 с (как `tasksTimer`).

- [ ] **Step 1:** Реализовать. Все новые кликабельные элементы — в общем селекторе кликов (урок v2.132: `[data-material],[data-pname]` забыли).
- [ ] **Step 2:** Собрать scratch-агент, прогон в Chrome: список из 3 строк → плашки по разделам, ожидание поиска, карточка, правка заметки, новый постер, архив и обратно; «📋 В план» у сериала; «Актёры» → правка описания и фото; экран актёра с фильмографией и переходом на плитку «✓ у нас»; карточка «👥 Актёры» в «Задачах». Консоль без ошибок.

### Task 7: Релиз

**Files:** `frontend/app/src/shared/version.ts` (`2.135.0`), `router/index.ts` (tv `'2.135'`), `DOCS.md`, `HelpView.vue`, `CHANGELOG.md`, `backend/.../migrations/NNNN_release_v2_135_0.sql`, `media-agent/HANDOFF.md`, `media-agent/README.md`, `backup.go` (текст README архива: `plans.json`, `plans/`, `people.json`, `people/`).

- [ ] **Step 1:** `go test ./...` в `media-agent` — PASS.
- [ ] **Step 2:** Документы и версия; `deploy.sh` → все HTTP 200.
- [ ] **Step 3:** Коммит `Habits v2.135.0: Пульт ТВ — планы и актёры`, push, публичный снапшот с проверками.
- [ ] **Step 4:** Агент на res-i3 (`.prev`, рестарт, active), `/api/admin/people` отвечает, запустить «👥 Актёры» один раз.
