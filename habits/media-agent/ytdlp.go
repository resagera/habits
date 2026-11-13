package main

// Музыка по ссылке: yt-dlp достаёт из страницы звук, агент кладёт файл в
// выбранную папку музыки — ту же, что и «📥 Добавить», включая вложенную.
//
// Почему не прямо в библиотеку: yt-dlp оставляет рядом недокачанные «.part», а
// имя файла узнаётся только в конце (его даёт название страницы). Поэтому
// качаем во временную папку кэша и переносим оттуда только готовый звук —
// оборвалось на середине, и в альбоме ничего не появилось. Перенос идёт тем же
// transferFile, что и «собрать альбом»: на одном диске это rename, на разных —
// копия с удалением исходника.
//
// Ссылок за раз можно дать сколько угодно: они встают в очередь и качаются по
// одной. Очередь лежит на диске (fetch-queue.json), а не в памяти задачи:
// агента перезапускают деплоем, а десяток набранных руками адресов терять
// обидно — после старта их поднимет fetchWorker. Он же берётся за очередь,
// если в момент добавления агент был занят другой работой. Задача при этом
// одна, как и у всей остальной фоновой работы: две закачки разом только
// отбирали бы сеть друг у друга.
//
// Своего бинарника у агента нет: yt-dlp ломается вслед за YouTube и живёт
// отдельными обновлениями. Путь берётся из настроек (ytdlp), иначе ищется
// рядом — ~/bin/yt-dlp_linux, ~/bin/yt-dlp, PATH. Кнопка «⬆ Обновить» зовёт
// «-U»: без этого скачивание однажды просто перестанет работать.
//
//	GET  /api/admin/fetch          → есть ли yt-dlp, какой версии, что в очереди
//	POST /api/admin/fetch          {urls[] | url, id, playlist} — в очередь
//	POST /api/admin/fetch/drop     {id} или {all:true} — снять из очереди
//	POST /api/admin/fetch/update   обновить сам yt-dlp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// Запас на диске: один трек — десятки мегабайт, но подборка идёт в ту же
	// папку, а на системном диске res-i3 место кончается.
	fetchMinFree = 1 << 30
	// Сколько позиций берём из подборки: ссылку со «list=» легко скопировать
	// случайно, и без предела приехал бы весь чужой плейлист.
	fetchPlaylistMax = 100
	fetchTimeout     = 2 * time.Hour
	// Сколько ссылок держим в очереди: столько за раз не наберёшь руками, а
	// предел не даёт одним запросом завалить агента работой на сутки.
	fetchQueueMax = 100
	// Как часто смотрим, не пора ли взяться за очередь.
	fetchKickEvery = 30 * time.Second
)

// --- очередь ---

// fetchItem — одна ссылка в очереди. Папка хранится путём, а не id: id —
// хэш пути, и очередь всё равно пришлось бы сверять с диском, а путь видно
// глазами в самом файле очереди.
type fetchItem struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Dir      string `json:"dir"`
	Where    string `json:"where"` // имя папки для подписи
	Playlist bool   `json:"playlist"`
	Added    int64  `json:"added"`
}

type fetchStore struct {
	mu    sync.Mutex
	path  string
	items []fetchItem
}

func newFetchStore(cacheDir string) *fetchStore {
	f := &fetchStore{path: filepath.Join(cacheDir, "fetch-queue.json")}
	if data, err := os.ReadFile(f.path); err == nil {
		_ = json.Unmarshal(data, &f.items)
	}
	return f
}

func (fs *fetchStore) saveLocked() {
	if data, err := json.MarshalIndent(fs.items, "", " "); err == nil {
		_ = writeAtomic(fs.path, data)
	}
}

// add ставит ссылки в хвост. Тот же адрес в ту же папку второй раз не берём:
// нажать «Скачать» дважды легко, а приедет он тогда дважды, второй раз с «(2)».
func (fs *fetchStore) add(items []fetchItem) (added int, queue []fetchItem) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for _, it := range items {
		if len(fs.items) >= fetchQueueMax {
			break
		}
		if fs.hasLocked(it) {
			continue
		}
		fs.items = append(fs.items, it)
		added++
	}
	if added > 0 {
		fs.saveLocked()
	}
	return added, append([]fetchItem{}, fs.items...)
}

func (fs *fetchStore) hasLocked(it fetchItem) bool {
	for _, have := range fs.items {
		if have.URL == it.URL && have.Dir == it.Dir && have.Playlist == it.Playlist {
			return true
		}
	}
	return false
}

// take снимает голову очереди и говорит, сколько осталось после неё.
func (fs *fetchStore) take() (fetchItem, int, bool) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(fs.items) == 0 {
		return fetchItem{}, 0, false
	}
	it := fs.items[0]
	fs.items = fs.items[1:]
	fs.saveLocked()
	return it, len(fs.items), true
}

func (fs *fetchStore) list() []fetchItem {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return append([]fetchItem{}, fs.items...)
}

// drop снимает одну ссылку по id, пустой id — всю очередь.
func (fs *fetchStore) drop(id string) int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	kept, gone := fs.items[:0], 0
	for _, it := range fs.items {
		if id == "" || it.ID == id {
			gone++
			continue
		}
		kept = append(kept, it)
	}
	fs.items = kept
	if gone > 0 {
		fs.saveLocked()
	}
	return gone
}

// --- сам yt-dlp ---

// ytdlpBin — где лежит yt-dlp. Пусто в настройках — ищем сами.
func (s *server) ytdlpBin() (string, error) {
	if p := s.cfg.ytdlp; p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
		return "", fmt.Errorf("в настройках агента ytdlp = %s, но файла там нет", p)
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		for _, name := range []string{"yt-dlp_linux", "yt-dlp"} {
			p := filepath.Join(home, "bin", name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, nil
			}
		}
	}
	for _, name := range []string{"yt-dlp_linux", "yt-dlp"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", errors.New("yt-dlp не установлен")
}

// ytdlpHint — что сделать на сервере. Через wget: curl на res-i3 нет.
const ytdlpHint = "На сервере: mkdir -p ~/bin && " +
	"wget -O ~/bin/yt-dlp_linux https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux && " +
	"chmod +x ~/bin/yt-dlp_linux"

// Версия yt-dlp запоминается: сам он разворачивается из однофайловой сборки
// и на «--version» тратит полторы секунды, а спрашивает её каждое открытие
// «Обслуживания». Ключ — путь, размер и время файла: обновление («-U»)
// перезаписывает бинарник, и ответ сменится сам.
var ytdlpVer struct {
	mu   sync.Mutex
	key  string
	text string
}

func ytdlpVersion(ctx context.Context, bin string) (string, error) {
	st, err := os.Stat(bin)
	if err != nil {
		return "", err
	}
	key := fmt.Sprintf("%s|%d|%d", bin, st.Size(), st.ModTime().UnixNano())
	ytdlpVer.mu.Lock()
	if ytdlpVer.key == key {
		text := ytdlpVer.text
		ytdlpVer.mu.Unlock()
		return text, nil
	}
	ytdlpVer.mu.Unlock()

	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(out))
	ytdlpVer.mu.Lock()
	ytdlpVer.key, ytdlpVer.text = key, text
	ytdlpVer.mu.Unlock()
	return text, nil
}

// --- ручки ---

// GET /api/admin/fetch — состояние для страницы: можно ли качать, чем и что
// уже стоит в очереди.
func (s *server) adminFetchState(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"queue": s.fetchq.list(), "busy": s.tasks.busy()}
	bin, err := s.ytdlpBin()
	if err != nil {
		out["ready"], out["error"], out["hint"] = false, err.Error(), ytdlpHint
		writeJSON(w, http.StatusOK, out)
		return
	}
	out["path"] = bin
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	version, err := ytdlpVersion(ctx, bin)
	if err != nil {
		out["ready"], out["error"], out["hint"] = false, "yt-dlp не запускается: "+err.Error(), ytdlpHint
		writeJSON(w, http.StatusOK, out)
		return
	}
	out["ready"], out["version"] = true, version
	writeJSON(w, http.StatusOK, out)
}

// POST /api/admin/fetch {urls[] | url, id, playlist} — поставить в очередь и
// взяться за неё, если агент свободен.
func (s *server) adminFetchStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL      string   `json:"url"`
		URLs     []string `json:"urls"`
		ID       string   `json:"id"`
		Playlist bool     `json:"playlist"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	urls, err := fetchURLs(append(req.URLs, req.URL))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	dir, ok := s.adminPath(w, req.ID)
	if !ok {
		return
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "скачивать можно только в папку"})
		return
	}
	// Сам бинарник проверяем здесь, чтобы отказать сразу, а не задачей.
	// Путь перед запуском берётся заново: yt-dlp могли поставить или
	// обновить, пока ссылка стояла в очереди.
	if _, err := s.ytdlpBin(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error(), "hint": ytdlpHint})
		return
	}
	// место нужно и там, где папка, и в кэше: скачивается сначала туда
	for _, p := range []string{dir, s.cfg.cache} {
		if free, _ := diskSpace(p); free > 0 && free < fetchMinFree {
			writeJSON(w, http.StatusInsufficientStorage,
				map[string]string{"error": "мало места: в " + p + " свободно " + human(free)})
			return
		}
	}

	now := time.Now()
	items := make([]fetchItem, 0, len(urls))
	for i, raw := range urls {
		items = append(items, fetchItem{
			ID:       fmt.Sprintf("q%d-%d", now.UnixNano(), i),
			URL:      raw,
			Dir:      dir,
			Where:    filepath.Base(dir),
			Playlist: req.Playlist && strings.Contains(raw, "list="),
			Added:    now.Unix(),
		})
	}
	added, queue := s.fetchq.add(items)
	out := map[string]any{"added": added, "skipped": len(items) - added, "queue": queue}
	// Занят — ссылки просто ждут: их поднимет fetchWorker. Отказом это не
	// считается, иначе набрать очередь во время перекодирования было бы нельзя.
	t, err := s.fetchKick()
	switch {
	case err != nil:
		out["busy"] = err.Error()
	case t != nil:
		out["task"] = t.snapshot()
	}
	writeJSON(w, http.StatusOK, out)
}

// fetchURLs разбирает то, что набрали в поле: ссылки разделены переводом
// строки или пробелом — ни того, ни другого внутри адреса быть не может.
func fetchURLs(chunks []string) ([]string, error) {
	out := []string{}
	for _, chunk := range chunks {
		for _, part := range strings.Fields(chunk) {
			// Точка в имени сайта нужна отдельно от cleanURL: та дописывает
			// схему и пускает «rezka.ag» без «https://», а значит, и случайное
			// слово из одной строки. У ссылок это не беда, а здесь набранное
			// наугад слово завело бы задачу, которой нечего делать.
			raw, ok := cleanURL(part)
			if !ok || !strings.Contains(hostTitle(raw), ".") {
				return nil, errors.New("это не похоже на ссылку: " + trimRunes(part, 60))
			}
			out = append(out, raw)
			if len(out) > fetchQueueMax {
				return nil, fmt.Errorf("больше %d ссылок за раз не возьму", fetchQueueMax)
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("не вижу ни одной ссылки")
	}
	return out, nil
}

// POST /api/admin/fetch/drop {id} | {all:true} — снять из очереди. Идущую
// закачку это не трогает: её останавливают кнопкой у самой задачи.
func (s *server) adminFetchDrop(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID  string `json:"id"`
		All bool   `json:"all"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.ID == "" && !req.All {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нечего снимать"})
		return
	}
	gone := s.fetchq.drop(req.ID)
	writeJSON(w, http.StatusOK, map[string]any{"dropped": gone, "queue": s.fetchq.list()})
}

// POST /api/admin/fetch/update — «yt-dlp -U». Обновление идёт задачей: оно
// лезет в интернет и занимает полминуты.
func (s *server) adminFetchUpdate(w http.ResponseWriter, r *http.Request) {
	bin, err := s.ytdlpBin()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error(), "hint": ytdlpHint})
		return
	}
	s.startTask(w, "fetch", "обновление yt-dlp", func(ctx context.Context, t *task) error {
		t.set("спрашиваю новую версию", 0, 1)
		err := runLines(ctx, exec.Command(bin, "-U"), func(line string) {
			t.say("%s", line)
		})
		t.step("готово")
		return err
	})
}

// --- очередь в работе ---

// fetchKick берётся за очередь, если есть за что и агент свободен.
func (s *server) fetchKick() (*task, error) {
	items := s.fetchq.list()
	if len(items) == 0 {
		return nil, nil
	}
	return s.tasks.start("fetch", fetchTitle(items), s.fetchDrain)
}

// fetchTitle — имя задачи. Оно пишется один раз при запуске, поэтому у очереди
// это первая ссылка и счёт остальных: что именно качается сейчас, видно в
// строке состояния.
func fetchTitle(items []fetchItem) string {
	if len(items) == 0 {
		return "музыка по ссылке"
	}
	title := hostTitle(items[0].URL) + " → " + items[0].Where
	if n := len(items) - 1; n > 0 {
		title += fmt.Sprintf(" и ещё %d", n)
	}
	return title
}

// fetchWorker поднимает очередь, когда агент освободился: ссылки могли лечь в
// неё, пока шла другая задача, или остаться с прошлого запуска.
func (s *server) fetchWorker() {
	sweepFetchTemp(s.cfg.cache)
	for {
		time.Sleep(fetchKickEvery)
		if len(s.fetchq.list()) == 0 || s.tasks.busy() {
			continue
		}
		// Ошибка здесь значит только одно: пока спали, задачу занял кто-то
		// другой. Поговорим об этом на следующем круге.
		_, _ = s.fetchKick()
	}
}

// sweepFetchTemp убирает временные папки, оставшиеся с прошлого запуска:
// агента перезапускают деплоем, и если это случилось на середине закачки,
// «defer» в fetchAudio уже не сработал. Своих папок у только что
// запустившегося агента нет, поэтому сносим все.
func sweepFetchTemp(cache string) {
	entries, _ := os.ReadDir(cache)
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "fetch-") {
			_ = os.RemoveAll(filepath.Join(cache, e.Name()))
		}
	}
}

// fetchDrain качает очередь по одной ссылке. Неудача одной не останавливает
// остальные: в отчёте задачи остаётся строка с причиной.
func (s *server) fetchDrain(ctx context.Context, t *task) error {
	at, bad := 0, 0
	var last error
	for {
		item, left, ok := s.fetchq.take()
		if !ok {
			break
		}
		at++
		of := at + left
		if of > 1 {
			t.say("— %s → %s", hostTitle(item.URL), item.Where)
		}
		err := s.fetchOne(ctx, t, item, at, of)
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			// «Остановить» снимает всю очередь: иначе fetchWorker поднял бы её
			// заново через полминуты, и кнопка выглядела бы сломанной.
			if n := s.fetchq.drop(""); n > 0 {
				t.say("очередь снята, в ней оставалось: %d", n)
			}
			return ctx.Err()
		}
		bad, last = bad+1, err
		if of > 1 {
			t.say("не вышло: %v", err)
		}
	}
	switch {
	case at == 0:
		return errors.New("очередь пуста")
	case bad == at:
		// Одна ссылка — причина уходит в саму задачу, как и раньше.
		return last
	case bad > 0:
		// Строкой, а не состоянием: состояние задачи после возврата всё равно
		// станет «готово», а в отчёте счёт неудач остаётся.
		t.say("не вышло: %d из %d", bad, at)
		t.set(fmt.Sprintf("готово, не вышло: %d из %d", bad, at), at, at)
	}
	return nil
}

// fetchOne — одна ссылка очереди: папку сверяем перед запуском, её могли
// переименовать или удалить, пока ссылка ждала.
func (s *server) fetchOne(ctx context.Context, t *task, item fetchItem, at, of int) error {
	bin, err := s.ytdlpBin()
	if err != nil {
		return err
	}
	if st, err := os.Stat(item.Dir); err != nil || !st.IsDir() || !s.inRoots(item.Dir) {
		return errors.New("папки «" + item.Where + "» больше нет")
	}
	return s.fetchAudio(ctx, t, bin, item.URL, item.Dir, item.Playlist, at, of)
}

var (
	reFetchPercent = regexp.MustCompile(`\[download\]\s+(\d+(?:\.\d+)?)%`)
	reFetchItem    = regexp.MustCompile(`\[download\] Downloading (?:item|video) (\d+) of (\d+)`)
	reFetchDest    = regexp.MustCompile(`(?:\[ExtractAudio\] Destination:|\[download\] Destination:)\s*(.+)$`)
)

// fetchAudio качает звук во временную папку и переносит готовое в dir.
// at и of — место ссылки в очереди: у одной это полоска самой закачки, у
// очереди полоска считает ссылки, а проценты уходят в подпись.
func (s *server) fetchAudio(ctx context.Context, t *task, bin, raw, dir string, playlist bool, at, of int) error {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	tmp, err := os.MkdirTemp(s.cfg.cache, "fetch-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	show := func(stage string, done, total int) {
		if of > 1 {
			t.set(fmt.Sprintf("%d из %d: %s", at, of, stage), at-1, of)
			return
		}
		t.set(stage, done, total)
	}

	// «-x --audio-quality 0» — только звук и лучшее, что есть, без
	// перекодирования: дорожка переписывается в свой контейнер как есть.
	// Имя — как у yt-dlp по умолчанию, с «[идентификатором]» в конце: вся
	// музыка в медиатеке собрана им же, и новое не должно выделяться.
	args := []string{
		"-x", "--audio-quality", "0", "--embed-metadata",
		"--newline", "--no-colors", "--no-mtime", "--ignore-config",
		"-o", "%(title).150B [%(id)s].%(ext)s", "-P", tmp,
	}
	if playlist {
		args = append(args, "--yes-playlist", "--playlist-end", fmt.Sprint(fetchPlaylistMax))
	} else {
		args = append(args, "--no-playlist")
	}
	args = append(args, "--", raw)

	show("спрашиваю страницу", 0, 100)
	items, pos := 0, 0
	err = runLines(ctx, exec.Command(bin, args...), func(line string) {
		switch {
		case reFetchItem.MatchString(line):
			m := reFetchItem.FindStringSubmatch(line)
			if n := atoiSafe(m[2]); n > 0 {
				items, pos = n, atoiSafe(m[1])
				show("качаю "+m[1]+" из "+m[2], pos-1, n)
			}
		case reFetchPercent.MatchString(line):
			m := reFetchPercent.FindStringSubmatch(line)
			if items > 0 {
				show(fmt.Sprintf("качаю %d из %d — %s%%", pos, items, m[1]), pos-1, items)
			} else {
				show("качаю "+m[1]+"%", int(parseFloatSafe(m[1])), 100)
			}
		case reFetchDest.MatchString(line):
			if items > 0 {
				show(fmt.Sprintf("достаю звук %d из %d", pos, items), pos-1, items)
			} else {
				show("достаю звук", 100, 100)
			}
		case strings.HasPrefix(line, "ERROR"):
			t.say("%s", line)
		}
	})
	// Файлы могли и доехать до ошибки на следующей позиции подборки —
	// забираем, что получилось, и только потом разбираемся с ошибкой.
	moved, names := s.collectFetched(tmp, dir)
	for _, n := range names {
		t.say("%s", n)
	}
	if moved > 0 {
		log.Printf("админка: по ссылке %s скачано %d в %s", raw, moved, dir)
	}
	if err != nil {
		if moved == 0 {
			return err
		}
		t.say("дальше не пошло: %v", err)
	}
	if moved == 0 {
		return errors.New("звука не получилось — смотрите строки выше")
	}
	if items > 0 {
		show(fmt.Sprintf("готово, файлов: %d", moved), items, items)
	} else {
		show(fmt.Sprintf("готово, файлов: %d", moved), 100, 100)
	}
	return nil
}

// collectFetched переносит из временной папки всё, что оказалось звуком.
func (s *server) collectFetched(tmp, dir string) (int, []string) {
	entries, _ := os.ReadDir(tmp)
	moved, names := 0, []string{}
	for _, e := range entries {
		if e.IsDir() || !audioExt[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		name := fetchFileName(e.Name())
		dst := freeName(filepath.Join(dir, name))
		if err := transferFile(filepath.Join(tmp, e.Name()), dst, true); err != nil {
			names = append(names, name+": "+err.Error())
			continue
		}
		s.remember(dst)
		moved++
		names = append(names, filepath.Base(dst))
	}
	return moved, names
}

// fetchFileName приводит имя от yt-dlp к тому, что годится в библиотеке: без
// разделителей пути и не скрытым файлом. Название страницы может быть каким
// угодно, а каталог прячет всё, что начинается с точки.
func fetchFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimLeft(strings.TrimSpace(name), ". ")
	if name == "" || strings.HasPrefix(name, ".") {
		return "track-" + fmt.Sprint(time.Now().Unix()) + filepath.Ext(name)
	}
	return name
}

// parseFloatSafe — проценты из вывода yt-dlp («45.2»). Негодное — ноль:
// полоска прогресса не повод возвращать ошибку.
func parseFloatSafe(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 || v > 100 {
		return 0
	}
	return v
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// runLines запускает процесс и отдаёт его вывод построчно. Прогресс yt-dlp
// идёт и в stdout, и в stderr, поэтому оба в одну трубу.
//
// Группа процессов своя: yt-dlp зовёт ffmpeg, и при отмене задачи убить надо
// обоих — иначе «Остановить» оставляло бы ffmpeg доделывать работу.
func runLines(ctx context.Context, cmd *exec.Cmd, onLine func(string)) error {
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		_ = pw.Close()
		done <- err
	}()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		case <-stop:
		}
	}()
	// Код возврата yt-dlp ничего не объясняет, а причина у него всегда в
	// строке «ERROR: …». Хвост вывода берём только если её не было: иначе в
	// отказе оказывались бы «Extracting URL» и «Downloading webpage».
	var tail []string
	said := ""
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "ERROR") {
			said = line
		}
		if tail = append(tail, line); len(tail) > 5 {
			tail = tail[1:]
		}
		onLine(line)
	}
	_ = pr.Close()
	err := <-done
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		switch {
		case said != "":
			return errors.New(strings.TrimPrefix(strings.TrimPrefix(said, "ERROR:"), " "))
		case len(tail) > 0:
			return errors.New(strings.Join(tail, "; "))
		}
		return err
	}
	return nil
}
