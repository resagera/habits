package main

// Теги файлов и папок, поиск и подборки по тегам.
//
// Тег — метка вида «#отпуск»: показывается блоком с рамкой, по ним фильтруют
// папку в режиме «Файлы». Хранятся рядом с заголовком (filemeta.go), по пути,
// и едут за файлом при переименовании. Регистр при сравнении не важен:
// «#Дилижан» и «#дилижан» — один тег, показывается тот вид, что дали первым.
//
// Поиск ходит не по диску, а по памяти: индексу обхода (fileindex.go) и
// записям заголовков с тегами. Поэтому он мгновенный, но видит файл только
// после обхода — загруженное через агента индекс дописывает сам.
//
// Подборка по тегам — сохранённый поиск: «#отпуск #2024 в Фото». Плиткой
// стоит на главной визуального режима, открывается списком найденного. Файлы
// никуда не копируются: подборка пересобирается при каждом показе.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	tagMaxLen   = 40
	tagsPerFile = 30
	searchLimit = 300
)

// normTag — тег без «#», пробелы внутри — подчёркиванием (тег — одно
// слово). Пусто — тег негодный.
func normTag(t string) string {
	t = strings.TrimSpace(t)
	t = strings.TrimLeft(t, "#")
	t = strings.Join(strings.FieldsFunc(t, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == ';' || r == '#'
	}), "_")
	if t == "" || !utf8.ValidString(t) || utf8.RuneCountInString(t) > tagMaxLen {
		return ""
	}
	return t
}

// normTags — без повторов (без учёта регистра) и пустых, не больше tagsPerFile.
func normTags(list []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range list {
		t = normTag(t)
		k := strings.ToLower(t)
		if t == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, t)
		if len(out) == tagsPerFile {
			break
		}
	}
	return out
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, want) {
			return true
		}
	}
	return false
}

// --- хранилище (поверх metaStore) ---

// updateLocked меняет запись и убирает её, если она опустела.
func (ms *metaStore) updateLocked(path string, fn func(m *fileMeta)) {
	m := ms.items[path]
	fn(&m)
	if m.empty() {
		delete(ms.items, path)
	} else {
		ms.items[path] = m
	}
}

func (ms *metaStore) tags(path string) []string { return ms.get(path).Tags }

func (ms *metaStore) setTags(path string, tags []string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.updateLocked(path, func(m *fileMeta) { m.Tags = normTags(tags) })
	ms.saveLocked()
}

// editTags — разом многим файлам: добавить одни теги и убрать другие.
// Сохраняется один раз, а не на каждый файл.
func (ms *metaStore) editTags(paths []string, add, remove []string) {
	add = normTags(add)
	ms.mu.Lock()
	defer ms.mu.Unlock()
	for _, p := range paths {
		ms.updateLocked(p, func(m *fileMeta) {
			kept := []string{}
			for _, t := range m.Tags {
				drop := false
				for _, r := range remove {
					if strings.EqualFold(t, normTag(r)) {
						drop = true
					}
				}
				if !drop {
					kept = append(kept, t)
				}
			}
			m.Tags = normTags(append(kept, add...))
		})
	}
	ms.saveLocked()
}

// snapshot — копия всех записей: поиск ходит по ней без замка.
func (ms *metaStore) snapshot() map[string]fileMeta {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	out := make(map[string]fileMeta, len(ms.items))
	for p, m := range ms.items {
		out[p] = m
	}
	return out
}

// paths — все пути индекса: файлы и папки (у папок — сводки).
func (x *fileIndex) paths() (files, dirs []string) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	for p := range x.files {
		files = append(files, p)
	}
	for p := range x.dirs {
		dirs = append(dirs, p)
	}
	return
}

// --- API тегов ---

// POST /api/file/tags {ids, add, remove} — теги многим файлам разом
// (выбор в режиме «Файлы»).
func (s *server) fileTags(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs    []string `json:"ids"`
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if len(req.IDs) == 0 || len(req.IDs) > 5000 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "не выбрано ни одного файла"})
		return
	}
	var paths []string
	for _, id := range req.IDs {
		p, ok := agentStore.pathOf(id)
		if !ok || !s.inRoots(p) || !s.visible(p, r) || !exists(p) {
			continue
		}
		paths = append(paths, p)
	}
	if len(paths) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "файлы не найдены"})
		return
	}
	s.meta.editTags(paths, req.Add, req.Remove)
	writeJSON(w, http.StatusOK, map[string]int{"count": len(paths)})
}

type tagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// GET /api/tags — все теги медиатеки с числом файлов: подсказки при вводе.
func (s *server) allTags(w http.ResponseWriter, r *http.Request) {
	counts := map[string]*tagCount{}
	for p, m := range s.meta.snapshot() {
		if len(m.Tags) == 0 || s.libraryOf(p).Path == "" || !s.visible(p, r) {
			continue
		}
		for _, t := range m.Tags {
			k := strings.ToLower(t)
			if counts[k] == nil {
				counts[k] = &tagCount{Tag: t}
			}
			counts[k].Count++
		}
	}
	out := make([]tagCount, 0, len(counts))
	for _, c := range counts {
		out = append(out, *c)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Count != out[b].Count {
			return out[a].Count > out[b].Count
		}
		return naturalLess(out[a].Tag, out[b].Tag)
	})
	writeJSON(w, http.StatusOK, map[string]any{"tags": out})
}

// --- поиск ---

type searchQuery struct {
	Words []string // каждое — в имени, заголовке или теге
	Tags  []string // каждый — у файла есть
	Scope string   // путь папки; пусто — вся медиатека
}

// parseSearch — «дилижан #отпуск» → слово «дилижан» и тег «отпуск».
func parseSearch(q string, tags []string) searchQuery {
	var sq searchQuery
	for _, w := range strings.Fields(q) {
		if strings.HasPrefix(w, "#") {
			if t := normTag(w); t != "" {
				sq.Tags = append(sq.Tags, t)
			}
			continue
		}
		sq.Words = append(sq.Words, strings.ToLower(w))
	}
	for _, t := range tags {
		if t = normTag(t); t != "" {
			sq.Tags = append(sq.Tags, t)
		}
	}
	return sq
}

func (q searchQuery) empty() bool { return len(q.Words) == 0 && len(q.Tags) == 0 }

// searchPaths — пути, подходящие под запрос, без закрытого кодом и без
// служебного рядом с фильмами (того же, что не показывает режим «Файлы»).
func (s *server) searchPaths(q searchQuery, r *http.Request) []string {
	files, dirs := s.index.paths()
	meta := s.meta.snapshot()
	isDir := map[string]bool{}
	cand := map[string]bool{}
	for _, p := range files {
		cand[p] = true
	}
	for _, p := range dirs {
		cand[p] = true
		isDir[p] = true
	}
	for p := range meta {
		cand[p] = true
	}
	// имена без расширения у видео и папок: «Фильм.jpg» рядом с «Фильм.mkv» —
	// его постер, а не снимок
	bases := map[string]bool{}
	for p := range cand {
		if isDir[p] || isMedia(p) {
			bases[strings.TrimSuffix(p, filepath.Ext(p))] = true
		}
	}
	up := map[string]bool{} // библиотека на месте: stat один раз, а не на каждый путь
	for _, lib := range s.cfg.roots {
		up[lib.Path] = lib.available()
	}
	var out []string
	for p := range cand {
		lib := s.libraryOf(p)
		if lib.Path == "" || p == lib.Path || !up[lib.Path] {
			continue
		}
		if q.Scope != "" && !strings.HasPrefix(p, q.Scope+string(filepath.Separator)) {
			continue
		}
		name := filepath.Base(p)
		if strings.HasPrefix(name, ".") || strings.Contains(p, string(filepath.Separator)+".") ||
			strings.HasSuffix(name, infoSuffix) {
			continue
		}
		if lib.Kind != "photo" && !isDir[p] && !isMedia(name) {
			ext := strings.ToLower(filepath.Ext(name))
			if isPosterExt(ext) && bases[strings.TrimSuffix(p, filepath.Ext(p))] {
				continue
			}
			if name == "poster.jpg" || name == "folder.jpg" || name == "cover.jpg" {
				continue
			}
		}
		m := meta[p]
		ok := true
		for _, t := range q.Tags {
			if !hasTag(m.Tags, t) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		hay := strings.ToLower(name + "\n" + m.Title + "\n" + strings.Join(m.Tags, "\n"))
		for _, w := range q.Words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok && s.visible(p, r) {
			out = append(out, p)
		}
	}
	return out
}

// searchEntries — найденное в виде строк режима «Файлы»: с разбором медиа
// (без него не понять, заиграет ли), заголовком, тегами и тем, где лежит.
func (s *server) searchEntries(paths []string) []entry {
	type found struct {
		path string
		st   os.FileInfo
	}
	var list []found
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil {
			list = append(list, found{p, st})
		}
	}
	shown := func(f found) string { return strings.ToLower(s.shownName(f.path)) }
	sort.SliceStable(list, func(a, b int) bool {
		if list[a].st.IsDir() != list[b].st.IsDir() {
			return list[a].st.IsDir()
		}
		return naturalLess(shown(list[a]), shown(list[b]))
	})
	if len(list) > searchLimit {
		list = list[:searchLimit]
	}
	out := make([]entry, len(list))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, f := range list {
		id := shortID(f.path)
		agentStore.rememberPath(id, f.path)
		e := entry{ID: id, Name: filepath.Base(f.path), IsDir: f.st.IsDir(), Where: s.whereOf(f.path)}
		if !e.IsDir {
			e.Size = f.st.Size()
			e.Ready = s.q.ready(id)
		}
		out[i] = e
		if !e.IsDir && isMedia(e.Name) {
			wg.Add(1)
			go func(i int, f found) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				out[i].Info = agentStore.probe(f.path, f.st)
			}(i, f)
		}
	}
	wg.Wait()
	s.decorate(out)
	return out
}

// whereOf — где лежит: «Фото и видео / 2024 / Дилижан» (без самого файла).
func (s *server) whereOf(p string) string {
	lib := s.libraryOf(p)
	rel, err := filepath.Rel(lib.Path, filepath.Dir(p))
	if err != nil {
		return ""
	}
	parts := []string{lib.Title}
	if rel != "." {
		dir := lib.Path
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			dir = filepath.Join(dir, part)
			parts = append(parts, s.shownName(dir))
		}
	}
	return strings.Join(parts, " / ")
}

// GET /api/search?q=&tags=a,b&scope=<id папки>  или  ?view=<id подборки>
func (s *server) search(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	var q searchQuery
	if id := qs.Get("view"); id != "" {
		v := s.tagViews.find(id)
		if v == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "подборки больше нет"})
			return
		}
		q = parseSearch(v.Q, v.Tags)
		q.Scope = v.Scope
	} else {
		var tags []string
		if t := qs.Get("tags"); t != "" {
			tags = strings.Split(t, ",")
		}
		q = parseSearch(qs.Get("q"), tags)
		if id := qs.Get("scope"); id != "" {
			p := s.libPathOrID(id)
			if p == "" || !s.visible(p, r) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "папка не найдена"})
				return
			}
			q.Scope = p
		}
	}
	if q.empty() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "пустой запрос"})
		return
	}
	paths := s.searchPaths(q, r)
	items := s.searchEntries(paths)
	writeJSON(w, http.StatusOK, map[string]any{"items": s.withURLs(items), "total": len(paths),
		"truncated": len(paths) > len(items)})
}

// libPathOrID — путь папки по id: корень библиотеки или запомненная папка.
func (s *server) libPathOrID(id string) string {
	for _, lib := range s.cfg.roots {
		if lib.ID == id && lib.available() {
			return lib.Path
		}
	}
	if p, ok := agentStore.pathOf(id); ok && s.inRoots(p) {
		return p
	}
	return ""
}

// --- подборки по тегам ---

type tagView struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Tags  []string `json:"tags"`
	Q     string   `json:"q,omitempty"`
	Scope string   `json:"scope,omitempty"` // путь папки; пусто — вся медиатека
}

type tagViewStore struct {
	mu    sync.Mutex
	path  string
	items []tagView
}

func newTagViewStore(cacheDir string) *tagViewStore {
	v := &tagViewStore{path: filepath.Join(cacheDir, "tagviews.json")}
	if data, err := os.ReadFile(v.path); err == nil {
		_ = json.Unmarshal(data, &v.items)
	}
	return v
}

func (vs *tagViewStore) saveLocked() {
	if data, err := json.MarshalIndent(vs.items, "", " "); err == nil {
		_ = writeAtomic(vs.path, data)
	}
}

func (vs *tagViewStore) all() []tagView {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	return append([]tagView{}, vs.items...)
}

func (vs *tagViewStore) find(id string) *tagView {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	for _, v := range vs.items {
		if v.ID == id {
			v := v
			return &v
		}
	}
	return nil
}

func (vs *tagViewStore) add(v tagView) tagView {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	v.ID = "tv" + hex.EncodeToString(b)
	vs.mu.Lock()
	vs.items = append(vs.items, v)
	vs.saveLocked()
	vs.mu.Unlock()
	return v
}

func (vs *tagViewStore) remove(id string) bool {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	for i, v := range vs.items {
		if v.ID == id {
			vs.items = append(vs.items[:i], vs.items[i+1:]...)
			vs.saveLocked()
			return true
		}
	}
	return false
}

// move — папка подборки переехала: подборка едет за ней.
func (vs *tagViewStore) move(old, neu string, isDir bool) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	changed := false
	for i, v := range vs.items {
		if v.Scope == "" {
			continue
		}
		if p, ok := movedCollPath(v.Scope, old, neu, isDir); ok {
			vs.items[i].Scope = p
			changed = true
		}
	}
	if changed {
		vs.saveLocked()
	}
}

// tagViewTiles — плитки подборок для главной. Обложка — первый найденный
// снимок; число — сколько всего нашлось.
func (s *server) tagViewTiles(r *http.Request) []catTile {
	out := []catTile{}
	for _, v := range s.tagViews.all() {
		if v.Scope != "" && !s.visible(v.Scope, r) {
			continue
		}
		q := parseSearch(v.Q, v.Tags)
		q.Scope = v.Scope
		paths := s.searchPaths(q, r)
		t := catTile{ID: v.ID, Name: v.Name, Kind: "tagview", Count: len(paths), Tags: v.Tags}
		sort.Strings(paths)
		for _, p := range paths {
			if photoExt[strings.ToLower(filepath.Ext(p))] {
				t.Poster = s.photoURL(p, "thumb")
				break
			}
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(a, b int) bool { return naturalLess(out[a].Name, out[b].Name) })
	return out
}

// POST /api/tagviews {name, tags, q, scope} — сохранить фильтр подборкой.
func (s *server) tagViewCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string   `json:"name"`
		Tags  []string `json:"tags"`
		Q     string   `json:"q"`
		Scope string   `json:"scope"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	name, ok := playlistName(req.Name)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "название — от 1 до 120 знаков"})
		return
	}
	v := tagView{Name: name, Tags: normTags(req.Tags), Q: strings.Join(strings.Fields(req.Q), " ")}
	if parseSearch(v.Q, v.Tags).empty() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужен хотя бы один тег"})
		return
	}
	if req.Scope != "" {
		p := s.libPathOrID(req.Scope)
		if p == "" || !s.visible(p, r) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "папка не найдена"})
			return
		}
		v.Scope = p
	}
	v = s.tagViews.add(v)
	writeJSON(w, http.StatusOK, map[string]any{"id": v.ID, "name": v.Name})
}

// DELETE /api/tagviews/{id}
func (s *server) tagViewDelete(w http.ResponseWriter, r *http.Request) {
	if !s.tagViews.remove(r.PathValue("id")) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "подборки нет"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) tagRoutes() {
	s.mux.HandleFunc("POST /api/file/tags", s.fileTags)
	s.mux.HandleFunc("GET /api/tags", s.allTags)
	s.mux.HandleFunc("GET /api/search", s.search)
	s.mux.HandleFunc("POST /api/tagviews", s.tagViewCreate)
	s.mux.HandleFunc("DELETE /api/tagviews/{id}", s.tagViewDelete)
}
