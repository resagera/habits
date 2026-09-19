package main

// Визуальный режим плеера: библиотеки, разложенные на фильмы, сериалы,
// видеоролики, фото и музыку, с постерами.
//
// Раскладка берётся из структуры папок, а не из названий:
//   - сериал — папка, внутри которой папки с видео (сезоны);
//   - фильм — видеофайл или папка ровно с одним видеофайлом;
//   - папка с несколькими видео без сезонов — сериал из одного сезона;
//   - музыка — каждая папка, где прямо лежат треки (пустые не в счёт);
//   - видеоролики (библиотека вида clips) — ролик из корня или папка с роликами;
//   - фото — папки первого уровня со снимками (photos.go).
//
// Обход только читает каталоги и ffprobe не запускает: разбор нужен лишь при
// открытии конкретного сериала или фильма, а он и так кэшируется.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

type catSeason struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type catTile struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Kind    string      `json:"kind"` // film | series | music | clip | clips | photos
	Poster  string      `json:"poster,omitempty"`
	File    string      `json:"file,omitempty"` // фильм: id самого видеофайла
	Seasons []catSeason `json:"seasons,omitempty"`
	Count   int         `json:"count"`
}

// dirContents — подпапки и медиафайлы одной папки, в «человеческом» порядке.
func dirContents(dir string) (dirs, videos, audios []string) {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, nil
	}
	for _, it := range items {
		name := it.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(dir, name)
		ext := strings.ToLower(filepath.Ext(name))
		switch {
		case it.IsDir():
			dirs = append(dirs, full)
		case videoExt[ext]:
			videos = append(videos, full)
		case audioExt[ext]:
			audios = append(audios, full)
		}
	}
	byName := func(list []string) {
		sort.SliceStable(list, func(a, b int) bool {
			return naturalLess(filepath.Base(list[a]), filepath.Base(list[b]))
		})
	}
	byName(dirs)
	byName(videos)
	byName(audios)
	return
}

func (s *server) remember(path string) string {
	id := shortID(path)
	agentStore.rememberPath(id, path)
	return id
}

// posterURL — ссылка на постер плитки. Постер отдаётся с кэшем на сутки,
// поэтому в ссылке время правки картинки: поменяли обложку в админке — ссылка
// другая, и приставка не покажет старую.
func (s *server) posterURL(id, kind string) string {
	v := ""
	if p, ok := agentStore.pathOf(id); ok {
		if f := s.existingPoster(p, id); f != "" {
			if st, err := os.Stat(f); err == nil {
				v = "&v=" + strconv.FormatInt(st.ModTime().Unix(), 36)
			}
		}
	}
	return s.cfg.base + "/poster/" + id + "?k=" + s.sign(id) + "&kind=" + kind + v
}

// videoTiles раскладывает видеобиблиотеку на фильмы и сериалы.
func (s *server) videoTiles(lib library) (films, series []catTile) {
	dirs, videos, _ := dirContents(lib.Path)
	for _, v := range videos {
		id := s.remember(v)
		name, _ := cleanTitle(filepath.Base(v), true)
		films = append(films, catTile{ID: id, Name: displayName(filepath.Base(v), true, name),
			Kind: "film", File: id, Count: 1, Poster: s.posterURL(id, "film")})
	}
	for _, d := range dirs {
		sub, vids, _ := dirContents(d)
		var seasons []catSeason
		if len(vids) > 0 && hasSeasonDirs(sub) {
			// серии прямо в папке сериала рядом с сезонами — отдельным «сезоном»
			seasons = append(seasons, catSeason{ID: s.remember(d), Name: "Без сезона", Count: len(vids)})
		}
		for _, sd := range sub {
			_, sv, _ := dirContents(sd)
			if len(sv) == 0 {
				continue
			}
			seasons = append(seasons, catSeason{ID: s.remember(sd), Name: filepath.Base(sd), Count: len(sv)})
		}
		id := s.remember(d)
		title, _ := cleanTitle(filepath.Base(d), false)
		name := displayName(filepath.Base(d), false, title)
		switch {
		case len(seasons) > 0:
			total := 0
			for _, se := range seasons {
				total += se.Count
			}
			series = append(series, catTile{ID: id, Name: name, Kind: "series",
				Seasons: seasons, Count: total, Poster: s.posterURL(id, "series")})
		case len(vids) == 1:
			films = append(films, catTile{ID: id, Name: name, Kind: "film",
				File: s.remember(vids[0]), Count: 1, Poster: s.posterURL(id, "film")})
		case len(vids) > 1:
			// несколько видео без сезонов — мини-сериал из одного сезона
			series = append(series, catTile{ID: id, Name: name, Kind: "series",
				Seasons: []catSeason{{ID: id, Name: "Серии", Count: len(vids)}},
				Count:   len(vids), Poster: s.posterURL(id, "series")})
		}
	}
	return
}

func hasSeasonDirs(dirs []string) bool {
	for _, d := range dirs {
		if _, v, _ := dirContents(d); len(v) > 0 {
			return true
		}
	}
	return false
}

// musicTiles — каждая папка, где прямо лежат треки. Корень библиотеки
// называется её заголовком из конфига: имя папки там часто техническое.
func (s *server) musicTiles(lib library) []catTile {
	var out []catTile
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		dirs, _, audios := dirContents(dir)
		if len(audios) > 0 {
			name := filepath.Base(dir)
			if dir == lib.Path {
				name = lib.Title
			}
			id := s.remember(dir)
			out = append(out, catTile{ID: id, Name: name, Kind: "music", Count: len(audios),
				Poster: s.ownPosterURL(dir, id, "music")})
		}
		if depth >= 6 {
			return
		}
		for _, d := range dirs {
			walk(d, depth+1)
		}
	}
	walk(lib.Path, 0)
	return out
}

// clipTiles — раздел «Видеоролики»: ролик прямо в корне — своей плиткой,
// папка с роликами (на любой глубине) — плиткой на папку. Постеры в интернете
// НЕ ищутся: домашнего ролика там нет, а найденное по имени файла было бы
// чужим. Картинку, положенную рядом руками, показываем.
func (s *server) clipTiles(lib library) []catTile {
	var out []catTile
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		dirs, videos, _ := dirContents(dir)
		if dir == lib.Path {
			for _, v := range videos {
				id := s.remember(v)
				out = append(out, catTile{ID: id, Name: strings.TrimSuffix(filepath.Base(v), filepath.Ext(v)),
					Kind: "clip", File: id, Count: 1, Poster: s.ownPosterURL(v, id, "clip")})
			}
		} else if len(videos) > 0 {
			id := s.remember(dir)
			out = append(out, catTile{ID: id, Name: filepath.Base(dir), Kind: "clips",
				Count: len(videos), Poster: s.ownPosterURL(dir, id, "clips")})
		}
		if depth >= 6 {
			return
		}
		for _, d := range dirs {
			walk(d, depth+1)
		}
	}
	walk(lib.Path, 0)
	return out
}

// ownPosterURL — ссылка на постер, только если картинка уже лежит рядом.
func (s *server) ownPosterURL(path, id, kind string) string {
	if s.existingPoster(path, id) == "" {
		return ""
	}
	return s.posterURL(id, kind)
}

type catalogData struct {
	Playlists []catTile `json:"playlists"`
	Films     []catTile `json:"films"`
	Series    []catTile `json:"series"`
	Music     []catTile `json:"music"`
	Clips     []catTile `json:"clips"`
	Photos    []catTile `json:"photos"`
}

// GET /api/catalog — всё содержимое библиотек для визуального режима.
func (s *server) catalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.buildCatalog())
}

// buildCatalog раскладывает библиотеки по разделам. Им же пользуется
// админка: там те же плитки, только с путями и размерами.
func (s *server) buildCatalog() catalogData {
	films, series, music := []catTile{}, []catTile{}, []catTile{}
	clips, photos := []catTile{}, []catTile{}
	for _, lib := range s.cfg.roots {
		if !lib.available() {
			continue
		}
		switch lib.Kind {
		case "music":
			music = append(music, s.musicTiles(lib)...)
		case "clips":
			clips = append(clips, s.clipTiles(lib)...)
		case "photo":
			photos = append(photos, s.photoTiles(lib)...)
		case "video":
			f, sr := s.videoTiles(lib)
			films = append(films, f...)
			series = append(series, sr...)
		}
	}
	byName := func(list []catTile) {
		sort.SliceStable(list, func(a, b int) bool { return naturalLess(list[a].Name, list[b].Name) })
	}
	byName(films)
	byName(series)
	byName(music)
	byName(clips)
	byName(photos)
	return catalogData{Playlists: s.playlistTiles(), Films: films, Series: series, Music: music, Clips: clips, Photos: photos}
}

// GET /api/item?id= — один файл с разбором и ссылкой (для плитки фильма).
func (s *server) item(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	path, ok := agentStore.pathOf(id)
	if !ok || !s.inRoots(path) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "файл не найден"})
		return
	}
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "файл не найден"})
		return
	}
	it := entry{ID: id, Name: filepath.Base(path), Size: st.Size(),
		Ready: s.q.ready(id), Info: agentStore.probe(path, st)}
	writeJSON(w, http.StatusOK, map[string]any{"item": s.withURLs([]entry{it})[0]})
}

// GET /api/next-season?id=<файл> — серии следующего сезона. Сезон кончился —
// плеер спрашивает, что дальше: соседняя папка того же сериала. Сезоном
// считается только папка внутри папки сериала: файлы в корне библиотеки и в
// папках прямо под ним (фильм в папке, мини-сериал) дальше никуда не идут.
func (s *server) nextSeason(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	path, ok := agentStore.pathOf(id)
	empty := map[string]any{"items": []any{}}
	if !ok || !s.inRoots(path) {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	season := filepath.Dir(path)
	seriesDir := filepath.Dir(season)
	// папка прямо в корне библиотеки — это фильм или мини-сериал, а не сезон:
	// иначе после фильма «Фильм А/» заиграл бы соседний «Фильм Б/»
	if s.isRoot(season) || s.isRoot(seriesDir) || !s.inRoots(seriesDir) {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	dirs, _, _ := dirContents(seriesDir)
	for i, d := range dirs {
		if d != season {
			continue
		}
		for _, next := range dirs[i+1:] {
			if _, v, _ := dirContents(next); len(v) == 0 {
				continue
			}
			items, err := agentStore.browse(next, s.q.ready)
			if err != nil {
				break
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"folder": map[string]string{"id": s.remember(next), "name": filepath.Base(next)},
				"items":  s.withURLs(items),
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, empty)
}

// libraryOf — библиотека, в которой лежит путь (самая глубокая из подходящих:
// папки в конфиге могут быть вложены одна в другую).
func (s *server) libraryOf(path string) library {
	var best library
	for _, lib := range s.cfg.roots {
		if (path == lib.Path || strings.HasPrefix(path, lib.Path+string(filepath.Separator))) &&
			len(lib.Path) > len(best.Path) {
			best = lib
		}
	}
	return best
}

func (s *server) isRoot(dir string) bool {
	for _, lib := range s.cfg.roots {
		if lib.Path == dir {
			return true
		}
	}
	return false
}

type outItem struct {
	entry
	URL string `json:"url,omitempty"`
}

func (s *server) withURLs(items []entry) []outItem {
	out := make([]outItem, 0, len(items))
	for _, it := range items {
		o := outItem{entry: it}
		if !it.IsDir {
			o.URL = s.mediaURL(it.ID)
		}
		out = append(out, o)
	}
	return out
}

// --- названия ---

var (
	reBrackets = regexp.MustCompile(`\[[^\]]*\]`)
	reYear     = regexp.MustCompile(`(?:^|[\s.(_\-])((?:19|20)\d{2})(?:$|[\s.)_\-])`)
	// всё, начиная с первого технического тега, к названию не относится
	reTags   = regexp.MustCompile(`(?i)(?:^|[\s.(_\-])(?:s\d{1,2}(?:e\d{1,3})?|season|сезон|x26[45]|h\.?26[45]|hevc|avc|bd-?rip|br-?rip|web-?dl|web-?rip|hdtv|hd-?rip|dvd-?rip|remux|\d{3,4}p|4k|uhd|lostfilm|newstudio|kyrazh|ollan)(?:$|[\s.)_\-]).*$`)
	reParens = regexp.MustCompile(`\([^)]*\)`)
	reSpaces = regexp.MustCompile(`\s+`)
)

// cleanTitle — название для поиска постера: без расширения, релизных тегов,
// сезонов и года; год возвращается отдельно — по нему отличают фильм 2019 года
// от одноимённого сериала 2024-го.
func cleanTitle(name string, isFile bool) (string, int) {
	if isFile {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	name = reBrackets.ReplaceAllString(name, " ")
	year := pickYear(name)
	// «The.Gentlemen.2019» — точки вместо пробелов у релизных имён
	if !strings.Contains(name, " ") {
		name = strings.NewReplacer(".", " ", "_", " ").Replace(name)
	}
	name = reTags.ReplaceAllString(name, "")
	name = reParens.ReplaceAllString(name, " ")
	if year != 0 {
		name = regexp.MustCompile(`(?:^|\s)`+fmt.Sprint(year)+`(?:\s|$)`).ReplaceAllString(name, " ")
	}
	name = strings.Trim(reSpaces.ReplaceAllString(name, " "), " -._")
	return name, year
}

var reParenYear = regexp.MustCompile(`\(((?:19|20)\d{2})\)`)

// pickYear — год выпуска. Год в скобках главнее всего; иначе берётся ПОСЛЕДНЕЕ
// подходящее число, а не первое: в «Blade Runner 2049 (2017)» или
// «Blade.Runner.2049.2017.1080p» 2049 — часть названия. Будущие годы не годы.
func pickYear(name string) int {
	maxYear := time.Now().Year() + 1
	valid := func(s string) int {
		var y int
		fmt.Sscanf(s, "%d", &y)
		if y < 1900 || y > maxYear {
			return 0
		}
		return y
	}
	if m := reParenYear.FindStringSubmatch(name); m != nil {
		if y := valid(m[1]); y != 0 {
			return y
		}
	}
	year := 0
	// пробелы между совпадениями нужны, чтобы «.2049.2017.» нашлись оба
	spaced := strings.NewReplacer(".", " . ", "_", " _ ").Replace(name)
	for _, m := range reYear.FindAllStringSubmatch(spaced, -1) {
		if y := valid(m[1]); y != 0 {
			year = y
		}
	}
	return year
}

// displayName — подпись плитки. Если название уже человеческое (с пробелами,
// без тегов), оставляем как есть вместе с годом; релизное — чистим.
func displayName(raw string, isFile bool, clean string) string {
	base := raw
	if isFile {
		base = strings.TrimSuffix(raw, filepath.Ext(raw))
	}
	if strings.Contains(base, " ") && !reTags.MatchString(base) && !reBrackets.MatchString(base) {
		return base
	}
	if clean == "" {
		return base
	}
	return clean
}

func hasCyrillic(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

// --- постеры ---

var posterHTTP = &http.Client{Timeout: 12 * time.Second}

// Wikimedia требует в User-Agent контакт владельца: без него после пары
// десятков запросов отвечает «too many requests» (поймано на «Инфо»).
const posterUA = "habits-media-agent/1.0 (home media server; https://github.com/resagera/habits)"

var (
	posterSem   = make(chan struct{}, 2) // в интернет больше двух разом не ходим
	posterLocks sync.Map                 // id → *sync.Mutex: один поиск на плитку
)

// GET /poster/{id}?k=&kind= — постер плитки. Уже лежит рядом с фильмом или
// сериалом картинка с тем же именем — отдаём её (её можно подложить и руками).
// Нет — ищем, кладём рядом и отдаём. Не нашли — помним сутки, чтобы каждая
// перерисовка каталога не ходила в интернет заново.
func (s *server) poster(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.validKey(id, r.URL.Query().Get("k")) {
		http.Error(w, "нет доступа", http.StatusForbidden)
		return
	}
	path, ok := agentStore.pathOf(id)
	if !ok || !s.inRoots(path) {
		http.NotFound(w, r)
		return
	}
	if found := s.existingPoster(path, id); found != "" {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, found)
		return
	}

	lock, _ := posterLocks.LoadOrStore(id, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	// пока ждали очереди, постер мог найти соседний запрос
	if found := s.existingPoster(path, id); found != "" {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, found)
		return
	}
	// искать в интернете — только для фильмов и сериалов: ролик или фото по
	// имени файла нашли бы чужую картинку (и «Продолжить просмотр» ролика
	// тоже приходит сюда)
	if s.libraryOf(path).Kind != "video" {
		http.NotFound(w, r)
		return
	}
	miss := filepath.Join(s.cfg.cache, "posters", id+".miss")
	if st, err := os.Stat(miss); err == nil && time.Since(st.ModTime()) < 24*time.Hour {
		http.NotFound(w, r)
		return
	}

	st, err := os.Stat(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	title, year := cleanTitle(filepath.Base(path), !st.IsDir())
	kind := r.URL.Query().Get("kind")
	posterSem <- struct{}{}
	data, ext, err := findPoster(title, year, kind)
	<-posterSem
	if err != nil {
		log.Printf("постер для %q (%d, %s) не найден: %v", title, year, kind, err)
		_ = os.MkdirAll(filepath.Dir(miss), 0o700)
		_ = os.WriteFile(miss, nil, 0o600)
		http.NotFound(w, r)
		return
	}
	saved := s.savePoster(path, id, data, ext, st.IsDir())
	log.Printf("постер для %q (%d, %s) → %s", title, year, kind, saved)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, saved)
}

// posterBase — путь картинки без расширения: рядом с фильмом или папкой
// сериала, с тем же именем («Fauda» → «Fauda.jpg», «Фильм.mp4» → «Фильм.jpg»).
func posterBase(path string, isDir bool) string {
	if isDir {
		return strings.TrimRight(path, string(filepath.Separator))
	}
	return strings.TrimSuffix(path, filepath.Ext(path))
}

func (s *server) existingPoster(path, id string) string {
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	base := posterBase(path, st.IsDir())
	candidates := []string{base + ".jpg", base + ".jpeg", base + ".png", base + ".webp"}
	if st.IsDir() {
		// обложка из админки — скрытым файлом внутри папки: главнее всего, и в
		// альбоме фото она не покажется лишним снимком
		candidates = append([]string{filepath.Join(path, coverFile)}, candidates...)
		for _, n := range []string{"poster.jpg", "folder.jpg", "cover.jpg"} {
			candidates = append(candidates, filepath.Join(path, n))
		}
	}
	// запасное место — кэш агента: если рядом с файлом писать нельзя
	for _, e := range []string{".jpg", ".png", ".webp"} {
		candidates = append(candidates, filepath.Join(s.cfg.cache, "posters", id+e))
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() && fi.Size() > 0 {
			return c
		}
	}
	return ""
}

func (s *server) savePoster(path, id string, data []byte, ext string, isDir bool) string {
	target := posterBase(path, isDir) + ext
	if writeAtomic(target, data) == nil {
		return target
	}
	// папка библиотеки только для чтения — кладём в кэш агента
	fallback := filepath.Join(s.cfg.cache, "posters", id+ext)
	_ = os.MkdirAll(filepath.Dir(fallback), 0o700)
	_ = writeAtomic(fallback, data)
	return fallback
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// findPoster перебирает источники без ключей. IMDb хорошо ищет латиницей и
// различает фильм и сериал с годом; кириллицу он не понимает, зато Википедия
// находит «Джентльмены (фильм)» с постером в карточке.
func findPoster(title string, year int, kind string) ([]byte, string, error) {
	if title == "" {
		return nil, "", errors.New("пустое название")
	}
	type source func() (string, error)
	imdb := func() (string, error) { return imdbPoster(title, year, kind) }
	wikiRU := func() (string, error) { return wikiPoster("ru", title, year, kind) }
	wikiEN := func() (string, error) { return wikiPoster("en", title, year, kind) }
	order := []source{imdb, wikiEN, wikiRU}
	if hasCyrillic(title) {
		order = []source{wikiRU, imdb}
	}
	var lastErr error = errors.New("ничего не нашлось")
	for _, src := range order {
		u, err := src()
		if err != nil || u == "" {
			if err != nil {
				lastErr = err
			}
			continue
		}
		data, ext, err := download(u)
		if err == nil {
			return data, ext, nil
		}
		lastErr = err
	}
	return nil, "", lastErr
}

// wikimediaGap — не чаще запроса в 300 мс к Википедии и Wikidata: карточка
// «Инфо» — это пять-шесть запросов подряд, а открывают их пачкой.
var (
	wikimediaMu   sync.Mutex
	wikimediaLast time.Time
)

const wikimediaGap = 300 * time.Millisecond

func getJSON(u string, v any) error {
	if strings.Contains(u, "wikipedia.org/") || strings.Contains(u, "wikidata.org/") {
		wikimediaMu.Lock()
		if wait := wikimediaGap - time.Since(wikimediaLast); wait > 0 {
			time.Sleep(wait)
		}
		wikimediaLast = time.Now()
		wikimediaMu.Unlock()
	}
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	req.Header.Set("User-Agent", posterUA)
	resp, err := posterHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(v)
}

var reAmazonSize = regexp.MustCompile(`\._V1_[^.]*\.jpg$`)

// imdbHit — лучшее совпадение в подсказках IMDb: и для постера, и для «Инфо».
type imdbHit struct {
	ID, Title, Stars, Image string
	Year                    int
	Series                  bool
}

func imdbFind(title string, year int, kind string) (imdbHit, error) {
	q := strings.ToLower(title)
	first := string([]rune(q)[0])
	var res struct {
		D []struct {
			ID  string `json:"id"`
			L   string `json:"l"`
			Y   int    `json:"y"`
			S   string `json:"s"`
			QID string `json:"qid"`
			I   struct {
				URL string `json:"imageUrl"`
			} `json:"i"`
		} `json:"d"`
	}
	u := "https://v2.sg.media-imdb.com/suggestion/" + url.PathEscape(first) + "/" + url.PathEscape(q) + ".json"
	if err := getJSON(u, &res); err != nil {
		return imdbHit{}, err
	}
	wantSeries := kind == "series"
	var best imdbHit
	bestScore := -1
	for _, d := range res.D {
		isSeries := d.QID == "tvSeries" || d.QID == "tvMiniSeries"
		isFilm := d.QID == "movie" || d.QID == "tvMovie" || d.QID == "video"
		if !isSeries && !isFilm {
			continue // актёры, эпизоды и прочее
		}
		score := 0
		if isSeries == wantSeries {
			score += 4
		}
		if strings.EqualFold(d.L, title) {
			score += 2
		}
		if year != 0 && d.Y != 0 && (d.Y-year <= 1 && year-d.Y <= 1) {
			score += 3
		}
		if d.I.URL != "" {
			score++
		}
		if score > bestScore {
			best, bestScore = imdbHit{ID: d.ID, Title: d.L, Year: d.Y, Stars: d.S, Image: d.I.URL, Series: isSeries}, score
		}
	}
	if best.ID == "" {
		return best, errors.New("IMDb: подходящего не нашлось")
	}
	return best, nil
}

func imdbPoster(title string, year int, kind string) (string, error) {
	hit, err := imdbFind(title, year, kind)
	if err != nil {
		return "", err
	}
	if hit.Image == "" {
		return "", errors.New("IMDb: у найденного нет постера")
	}
	// оригинал весит мегабайты; амазоновский CDN режет картинку по суффиксу
	return reAmazonSize.ReplaceAllString(hit.Image, "._V1_SX400_.jpg"), nil
}

func wikiPoster(lang, title string, year int, kind string) (string, error) {
	hint := map[string]map[string]string{
		"ru": {"film": "фильм", "series": "сериал"},
		"en": {"film": "film", "series": "TV series"},
	}[lang][kind]
	query := strings.TrimSpace(title + " " + hint)
	if year != 0 {
		query += fmt.Sprintf(" %d", year)
	}
	// pilicense=any обязательно: постеры в Википедии несвободные, а по
	// умолчанию pageimages отдаёт только свободные картинки
	u := "https://" + lang + ".wikipedia.org/w/api.php?action=query&format=json&generator=search" +
		"&gsrlimit=5&prop=pageimages&piprop=thumbnail&pithumbsize=500&pilicense=any&gsrsearch=" +
		url.QueryEscape(query)
	var res struct {
		Query struct {
			Pages map[string]struct {
				Index     int    `json:"index"`
				Title     string `json:"title"`
				Thumbnail struct {
					Source string `json:"source"`
				} `json:"thumbnail"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := getJSON(u, &res); err != nil {
		return "", err
	}
	best, bestScore, bestIndex := "", -1, 1<<30
	want := strings.ToLower(title)
	for _, p := range res.Query.Pages {
		if p.Thumbnail.Source == "" {
			continue
		}
		pageTitle := strings.ToLower(p.Title)
		bare := strings.TrimSpace(reParens.ReplaceAllString(pageTitle, ""))
		score := 0
		switch {
		case bare == want:
			score = 3 // «Джентльмены (фильм)» против «Джентльмены удачи»
		case strings.HasPrefix(bare, want):
			score = 1
		default:
			// без совпадения названия страница не годится, даже если в ней
			// есть «сериал»: иначе на любое имя найдётся «Чернобыль (мини-сериал)»
			continue
		}
		if hint != "" && strings.Contains(pageTitle, strings.ToLower(hint)) {
			score++
		}
		if score > bestScore || (score == bestScore && p.Index < bestIndex) {
			best, bestScore, bestIndex = p.Thumbnail.Source, score, p.Index
		}
	}
	if best == "" || bestScore < 1 {
		return "", fmt.Errorf("Википедия (%s): подходящего не нашлось", lang)
	}
	return best, nil
}

func download(u string) ([]byte, string, error) {
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	req.Header.Set("User-Agent", posterUA)
	resp, err := posterHTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("картинка: HTTP %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}[strings.Split(ct, ";")[0]]
	if ext == "" {
		return nil, "", fmt.Errorf("не картинка: %s", ct)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, "", err
	}
	if len(data) < 1024 {
		return nil, "", errors.New("подозрительно маленькая картинка")
	}
	return data, ext, nil
}
