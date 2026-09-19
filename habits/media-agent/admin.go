package main

// Админка медиатеки: переименование, удаление через корзину, обслуживание.
//
// Закрыта PIN-кодом (admin_pin в настройках): в домашнем вайфае живёт не
// только приставка, а здесь можно удалить сериал. Без PIN админка выключена
// совсем. Пять неверных попыток подряд — пауза на пять минут: четыре цифры
// иначе перебираются за минуты.
//
// Удаление не стирает сразу. Объект вместе с постером рядом переезжает в
// скрытую папку .habits-trash в корне своей библиотеки (тот же диск — переезд
// мгновенный, место освобождается при стирании) и лежит там trash_days дней.
// За это время его можно вернуть. О переезде и стирании агент пишет
// владельцу приставки в Telegram через прод (POST /api/v1/tv/notify).
//
// Переименование — настоящее, на диске: имя папки — это и название плитки, и
// запрос для поиска постера, и то, что видно в режиме «Файлы». Постер рядом
// переименовывается вместе с объектом, записи «Продолжить просмотр» — тоже.

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const trashDirName = ".habits-trash"

var posterExts = []string{".jpg", ".jpeg", ".png", ".webp"}

// --- доступ ---

var adminGuard struct {
	mu    sync.Mutex
	fails int
	until time.Time
}

const (
	adminMaxFails = 5
	adminLockFor  = 5 * time.Minute
)

// admin оборачивает обработчик проверкой PIN из заголовка X-Admin-Pin.
func (s *server) admin(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.adminPin == "" {
			writeJSON(w, http.StatusForbidden, map[string]string{"code": "disabled",
				"error": "Админка выключена: задайте admin_pin в ~/.config/habits-media-agent.conf на сервере"})
			return
		}
		adminGuard.mu.Lock()
		if wait := time.Until(adminGuard.until); wait > 0 {
			adminGuard.mu.Unlock()
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"code": "locked",
				"error": fmt.Sprintf("Слишком много неверных PIN — подождите %d мин", int(wait.Minutes())+1)})
			return
		}
		ok := hmac.Equal([]byte(r.Header.Get("X-Admin-Pin")), []byte(s.cfg.adminPin))
		if !ok {
			adminGuard.fails++
			if adminGuard.fails >= adminMaxFails {
				adminGuard.fails = 0
				adminGuard.until = time.Now().Add(adminLockFor)
				log.Printf("админка: %d неверных PIN подряд — пауза %s", adminMaxFails, adminLockFor)
			}
		} else {
			adminGuard.fails = 0
		}
		adminGuard.mu.Unlock()
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "pin", "error": "Неверный PIN"})
			return
		}
		fn(w, r)
	}
}

func (s *server) adminRoutes() {
	s.mux.HandleFunc("GET /api/admin/check", s.admin(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}))
	s.mux.HandleFunc("GET /api/admin/overview", s.admin(s.adminOverview))
	s.mux.HandleFunc("GET /api/admin/children", s.admin(s.adminChildren))
	s.mux.HandleFunc("POST /api/admin/rename", s.admin(s.adminRename))
	s.mux.HandleFunc("POST /api/admin/delete", s.admin(s.adminDelete))
	s.mux.HandleFunc("GET /api/admin/trash", s.admin(s.adminTrash))
	s.mux.HandleFunc("POST /api/admin/trash/{id}/restore", s.admin(s.adminRestore))
	s.mux.HandleFunc("POST /api/admin/trash/{id}/purge", s.admin(s.adminPurge))
	s.mux.HandleFunc("POST /api/admin/poster/reset", s.admin(s.adminPosterReset))
	s.mux.HandleFunc("GET /api/admin/service", s.admin(s.adminService))
	s.mux.HandleFunc("POST /api/admin/service/{action}", s.admin(s.adminServiceAction))
	s.mux.HandleFunc("POST /api/admin/settings", s.admin(s.adminSettings))
	s.mux.HandleFunc("POST /api/admin/info", s.admin(s.adminInfo))
	s.mux.HandleFunc("POST /api/admin/image", s.admin(s.adminImage))
	s.mux.HandleFunc("DELETE /api/admin/image", s.admin(s.adminImageDelete))
}

// --- обзор ---

type adminItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	IsDir     bool   `json:"is_dir"`
	Size      int64  `json:"size"`
	Count     int    `json:"count,omitempty"`
	CanPoster bool   `json:"can_poster,omitempty"` // постер ищется в интернете — можно искать заново
	Root      bool   `json:"root,omitempty"`       // корень библиотеки: не переименовать и не удалить
}

type adminLib struct {
	Title     string `json:"title"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	Available bool   `json:"available"`
	Free      int64  `json:"free"`
	Total     int64  `json:"total"`
}

func diskSpace(path string) (free, total int64) {
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) != nil {
		return 0, 0
	}
	return int64(st.Bavail) * int64(st.Bsize), int64(st.Blocks) * int64(st.Bsize)
}

// pathSize — размер файла или папки со всем содержимым (корзину не считаем).
func pathSize(p string) int64 {
	var total int64
	_ = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == trashDirName {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

func (s *server) adminItemFor(t catTile) (adminItem, bool) {
	path, ok := agentStore.pathOf(t.ID)
	if !ok {
		return adminItem{}, false
	}
	st, err := os.Stat(path)
	if err != nil {
		return adminItem{}, false
	}
	return adminItem{ID: t.ID, Name: t.Name, Kind: t.Kind, Path: path, IsDir: st.IsDir(),
		Size: pathSize(path), Count: t.Count, CanPoster: s.libraryOf(path).Kind == "video",
		Root: s.isRoot(path)}, true
}

// GET /api/admin/overview — библиотеки с местом на диске и все разделы
// каталога с путями и размерами.
func (s *server) adminOverview(w http.ResponseWriter, r *http.Request) {
	libs := []adminLib{}
	for _, lib := range s.cfg.roots {
		l := adminLib{Title: lib.Title, Kind: lib.Kind, Path: lib.Path, Available: lib.available()}
		if l.Available {
			l.Free, l.Total = diskSpace(lib.Path)
		}
		libs = append(libs, l)
	}
	cat := s.buildCatalog()
	section := func(tiles []catTile) []adminItem {
		out := []adminItem{}
		for _, t := range tiles {
			if it, ok := s.adminItemFor(t); ok {
				out = append(out, it)
			}
		}
		return out
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"libraries": libs,
		"sections": map[string][]adminItem{
			"series": section(cat.Series), "films": section(cat.Films), "clips": section(cat.Clips),
			"photos": section(cat.Photos), "music": section(cat.Music),
		},
		"trash_count": len(s.trash.list()),
		"trash_days":  s.cfg.trashDays,
	})
}

// GET /api/admin/children?id= — всё, что лежит в папке (кроме скрытого):
// сезоны, серии, постеры, посторонние файлы — удалить можно что угодно.
func (s *server) adminChildren(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.adminPath(w, r.URL.Query().Get("id"))
	if !ok {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "это не папка"})
		return
	}
	items := []adminItem{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		items = append(items, adminItem{ID: s.remember(full), Name: e.Name(), Path: full,
			IsDir: e.IsDir(), Size: pathSize(full), CanPoster: false})
	}
	sort.SliceStable(items, func(a, b int) bool {
		if items[a].IsDir != items[b].IsDir {
			return items[a].IsDir
		}
		return naturalLess(items[a].Name, items[b].Name)
	})
	out := map[string]any{"id": r.URL.Query().Get("id"), "name": filepath.Base(dir), "path": dir,
		"root": s.isRoot(dir), "items": items}
	if parent := filepath.Dir(dir); !s.isRoot(dir) && s.inRoots(parent) {
		out["parent"] = s.remember(parent)
	}
	writeJSON(w, http.StatusOK, out)
}

// adminPath — путь по id, только внутри библиотек и не из корзины.
func (s *server) adminPath(w http.ResponseWriter, id string) (string, bool) {
	p, ok := agentStore.pathOf(id)
	if !ok || !s.inRoots(p) || strings.Contains(p, string(filepath.Separator)+trashDirName) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "не найдено — обновите список"})
		return "", false
	}
	if _, err := os.Stat(p); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "уже нет на диске — обновите список"})
		return "", false
	}
	return p, true
}

// --- переименование ---

// cleanName проверяет новое имя: одна составляющая пути, без скрытых и
// служебных имён.
func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", errors.New("пустое имя")
	case strings.ContainsAny(name, "/\\\x00"):
		return "", errors.New("в имени не должно быть «/»")
	case strings.HasPrefix(name, "."):
		return "", errors.New("имя не может начинаться с точки")
	case len(name) > 200:
		return "", errors.New("слишком длинное имя")
	}
	return name, nil
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// renamePath переименовывает файл или папку на месте. У файла расширение
// сохраняется, если его не написали. Возвращает новый путь.
func (s *server) renamePath(old, name string) (string, error) {
	name, err := cleanName(name)
	if err != nil {
		return "", err
	}
	if s.isRoot(old) {
		return "", errors.New("корень библиотеки переименовывается в настройках агента")
	}
	st, err := os.Stat(old)
	if err != nil {
		return "", err
	}
	isDir := st.IsDir()
	if !isDir && !strings.EqualFold(filepath.Ext(name), filepath.Ext(old)) {
		name += filepath.Ext(old)
	}
	neu := filepath.Join(filepath.Dir(old), name)
	if neu == old {
		return old, nil
	}
	// «fauda» → «Fauda» на exFAT — это тот же файл, а не занятое имя
	if exists(neu) && !strings.EqualFold(neu, old) {
		return "", fmt.Errorf("«%s» уже есть рядом", name)
	}
	side := sidecars(old, isDir)
	if err := os.Rename(old, neu); err != nil {
		return "", err
	}
	// постер и описание рядом с тем же именем едут следом:
	// «Fauda.jpg» → «Fauda (2015).jpg», «Fauda.info.json» → …
	oldBase, newBase := posterBase(old, isDir), posterBase(neu, isDir)
	for _, f := range side {
		if to := newBase + strings.TrimPrefix(f, oldBase); !exists(to) {
			_ = os.Rename(f, to)
		}
	}
	s.remember(neu)
	s.moveProgress(old, neu, isDir)
	log.Printf("админка: %s → %s", old, neu)
	return neu, nil
}

// movedPath — где теперь лежит p после переезда old → neu. У файлов записи
// «Продолжить просмотр» хранят путь без расширения.
func movedPath(p, old, neu string, isDir bool) (string, bool) {
	if isDir {
		if p == old {
			return neu, true
		}
		if strings.HasPrefix(p, old+string(filepath.Separator)) {
			return neu + p[len(old):], true
		}
		return "", false
	}
	if p == strings.TrimSuffix(old, filepath.Ext(old)) {
		return strings.TrimSuffix(neu, filepath.Ext(neu)), true
	}
	return "", false
}

// moveProgress переписывает пути в записях «Продолжить просмотр». Ключ и id
// файла пересчитаются сами при следующем чтении списка (resolve).
func (s *server) moveProgress(old, neu string, isDir bool) {
	s.progress.update(func(items map[string]*progressEntry) {
		for _, e := range items {
			if e.Dir == "" || e.Rel == "" {
				continue
			}
			full, ok := movedPath(filepath.Join(e.Dir, e.Rel), old, neu, isDir)
			if !ok {
				continue
			}
			if isDir {
				if d, ok := movedPath(e.Dir, old, neu, true); ok {
					e.Dir = d
				}
			}
			if rel, err := filepath.Rel(e.Dir, full); err == nil {
				e.Rel = rel
			}
		}
	})
}

// POST /api/admin/rename {id, name}
func (s *server) adminRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужны id и name"})
		return
	}
	old, ok := s.adminPath(w, req.ID)
	if !ok {
		return
	}
	neu, err := s.renamePath(old, req.Name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": s.remember(neu), "path": neu, "name": filepath.Base(neu)})
}

// --- корзина ---

type trashMove struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type trashEntry struct {
	ID        string      `json:"id"`
	Title     string      `json:"title"` // «Сериалы: Fauda/Season 1/E05.mp4»
	Orig      string      `json:"orig"`
	IsDir     bool        `json:"is_dir"`
	Dir       string      `json:"dir"` // папка этой записи в корзине
	Moves     []trashMove `json:"moves"`
	Size      int64       `json:"size"`
	DeletedAt int64       `json:"deleted_at"`
	PurgeAt   int64       `json:"purge_at"`
}

type trashStore struct {
	mu    sync.Mutex
	path  string
	items []trashEntry
}

func newTrashStore(cacheDir string) *trashStore {
	t := &trashStore{path: filepath.Join(cacheDir, "trash.json")}
	if data, err := os.ReadFile(t.path); err == nil {
		_ = json.Unmarshal(data, &t.items)
	}
	return t
}

func (t *trashStore) saveLocked() {
	if data, err := json.Marshal(t.items); err == nil {
		_ = writeAtomic(t.path, data)
	}
}

func (t *trashStore) list() []trashEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	// пустой список — [], а не null: страница перебирает его без проверок
	return append([]trashEntry{}, t.items...)
}

func (t *trashStore) add(e trashEntry) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.items = append(t.items, e)
	t.saveLocked()
}

// take убирает запись из списка и возвращает её.
func (t *trashStore) take(id string) (trashEntry, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, e := range t.items {
		if e.ID == id {
			t.items = append(t.items[:i], t.items[i+1:]...)
			t.saveLocked()
			return e, true
		}
	}
	return trashEntry{}, false
}

func (t *trashStore) get(id string) (trashEntry, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, e := range t.items {
		if e.ID == id {
			return e, true
		}
	}
	return trashEntry{}, false
}

func (s *server) titleOf(p string) string {
	lib := s.libraryOf(p)
	rel, err := filepath.Rel(lib.Path, p)
	if err != nil || lib.Path == "" {
		return filepath.Base(p)
	}
	return lib.Title + ": " + rel
}

// trashPath переносит объект (и постер рядом с ним) в корзину своей библиотеки.
func (s *server) trashPath(p string) (trashEntry, error) {
	lib := s.libraryOf(p)
	if lib.Path == "" || p == lib.Path {
		return trashEntry{}, errors.New("библиотеку целиком удалить нельзя — только то, что в ней")
	}
	st, err := os.Stat(p)
	if err != nil {
		return trashEntry{}, err
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	dir := filepath.Join(lib.Path, trashDirName, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return trashEntry{}, err
	}
	moves := []trashMove{{From: p, To: filepath.Join(dir, filepath.Base(p))}}
	for _, f := range sidecars(p, st.IsDir()) {
		moves = append(moves, trashMove{From: f, To: filepath.Join(dir, filepath.Base(f))})
	}
	size := pathSize(p)
	for i, m := range moves {
		if err := os.Rename(m.From, m.To); err != nil {
			// откатываем то, что успело переехать, — полуудалённого не бывает
			for _, done := range moves[:i] {
				_ = os.Rename(done.To, done.From)
			}
			_ = os.Remove(dir)
			return trashEntry{}, err
		}
	}
	now := time.Now()
	e := trashEntry{ID: id, Title: s.titleOf(p), Orig: p, IsDir: st.IsDir(), Dir: dir, Moves: moves,
		Size: size, DeletedAt: now.Unix(), PurgeAt: now.Add(time.Duration(s.cfg.trashDays) * 24 * time.Hour).Unix()}
	s.trash.add(e)
	log.Printf("админка: в корзину %s (%s)", p, human(size))
	return e, nil
}

// restoreTrash возвращает объект на старое место.
func (s *server) restoreTrash(id string) (trashEntry, error) {
	e, ok := s.trash.get(id)
	if !ok {
		return e, errors.New("в корзине такого нет")
	}
	for _, m := range e.Moves {
		if exists(m.From) {
			return e, fmt.Errorf("на старом месте уже есть «%s» — переименуйте его и повторите", filepath.Base(m.From))
		}
	}
	for i, m := range e.Moves {
		_ = os.MkdirAll(filepath.Dir(m.From), 0o755)
		if err := os.Rename(m.To, m.From); err != nil {
			for _, done := range e.Moves[:i] {
				_ = os.Rename(done.From, done.To)
			}
			return e, err
		}
	}
	_ = os.RemoveAll(e.Dir)
	s.trash.take(id)
	log.Printf("админка: из корзины вернулось %s", e.Orig)
	return e, nil
}

func (s *server) purgeTrash(id string) (trashEntry, error) {
	e, ok := s.trash.get(id)
	if !ok {
		return e, errors.New("в корзине такого нет")
	}
	if err := os.RemoveAll(e.Dir); err != nil {
		return e, err
	}
	s.trash.take(id)
	log.Printf("админка: стёрто %s (%s)", e.Orig, human(e.Size))
	return e, nil
}

// trashWorker стирает то, чей срок в корзине вышел. Проверяет раз в полчаса:
// точность до минуты тут никому не нужна.
func (s *server) trashWorker() {
	check := func() {
		s.cleanUploads()
		now := time.Now().Unix()
		for _, e := range s.trash.list() {
			if e.PurgeAt > now {
				continue
			}
			if _, err := s.purgeTrash(e.ID); err != nil {
				log.Printf("корзина: %s не стирается: %v", e.Dir, err)
				continue
			}
			s.notifyAsync(fmt.Sprintf("🧹 Стёрто из медиатеки: «%s», освободилось %s.", e.Title, human(e.Size)))
		}
	}
	time.Sleep(time.Minute)
	check()
	for range time.Tick(30 * time.Minute) {
		check()
	}
}

func (s *server) adminDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужен id"})
		return
	}
	p, ok := s.adminPath(w, req.ID)
	if !ok {
		return
	}
	e, err := s.trashPath(p)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.notifyAsync(fmt.Sprintf("🗑 В корзине медиатеки: «%s» (%s). Сотрётся %s — до этого можно вернуть в админке на ТВ.",
		e.Title, human(e.Size), time.Unix(e.PurgeAt, 0).Format("02.01 в 15:04")))
	writeJSON(w, http.StatusOK, map[string]any{"entry": e})
}

func (s *server) adminTrash(w http.ResponseWriter, r *http.Request) {
	list := s.trash.list()
	sort.Slice(list, func(a, b int) bool { return list[a].DeletedAt > list[b].DeletedAt })
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "days": s.cfg.trashDays})
}

func (s *server) adminRestore(w http.ResponseWriter, r *http.Request) {
	e, err := s.restoreTrash(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.notifyAsync(fmt.Sprintf("↩ Вернулось из корзины медиатеки: «%s».", e.Title))
	writeJSON(w, http.StatusOK, map[string]any{"entry": e})
}

func (s *server) adminPurge(w http.ResponseWriter, r *http.Request) {
	e, err := s.purgeTrash(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.notifyAsync(fmt.Sprintf("🧹 Стёрто из медиатеки: «%s», освободилось %s.", e.Title, human(e.Size)))
	writeJSON(w, http.StatusOK, map[string]any{"entry": e})
}

// --- постеры и обслуживание ---

// POST /api/admin/poster/reset {id} — найти постер заново: картинка рядом и
// отметка неудачного поиска убираются, следующий показ плитки ищет снова.
func (s *server) adminPosterReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужен id"})
		return
	}
	p, ok := s.adminPath(w, req.ID)
	if !ok {
		return
	}
	if s.libraryOf(p).Kind != "video" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "постеры ищутся только для фильмов и сериалов"})
		return
	}
	st, _ := os.Stat(p)
	removed := 0
	base := posterBase(p, st.IsDir())
	for _, ext := range posterExts {
		if os.Remove(base+ext) == nil {
			removed++
		}
	}
	for _, ext := range append(posterExts, ".miss") {
		_ = os.Remove(filepath.Join(s.cfg.cache, "posters", req.ID+ext))
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": removed})
}

func dirStats(dir string) (files int, size int64) {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files++
			if info, err := d.Info(); err == nil {
				size += info.Size()
			}
		}
		return nil
	})
	return
}

// GET /api/admin/service — кэш агента и прочее обслуживание.
func (s *server) adminService(w http.ResponseWriter, r *http.Request) {
	photoFiles, photoSize := dirStats(filepath.Join(s.cfg.cache, "photos"))
	misses, _ := filepath.Glob(filepath.Join(s.cfg.cache, "posters", "*.miss"))
	_, transSize := dirStats(filepath.Join(s.cfg.cache, "transcoded"))
	s.progress.mu.Lock()
	progress := len(s.progress.items)
	s.progress.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"photo_files": photoFiles, "photo_bytes": photoSize,
		"poster_misses":    len(misses),
		"transcoded_bytes": transSize,
		"progress":         progress,
		"room":             s.cfg.room,
		"trash_days":       s.cfg.trashDays,
	})
}

// POST /api/admin/service/{action}
func (s *server) adminServiceAction(w http.ResponseWriter, r *http.Request) {
	switch r.PathValue("action") {
	case "photos-clear":
		// копии соберутся заново по мере просмотра
		_ = os.RemoveAll(filepath.Join(s.cfg.cache, "photos"))
		photoWarm.Range(func(k, _ any) bool { photoWarm.Delete(k); return true })
	case "posters-retry":
		misses, _ := filepath.Glob(filepath.Join(s.cfg.cache, "posters", "*.miss"))
		for _, m := range misses {
			_ = os.Remove(m)
		}
	case "progress-clear":
		s.progress.update(func(items map[string]*progressEntry) {
			for k := range items {
				delete(items, k)
			}
		})
	case "notify-test":
		if err := s.notify("✅ Проверка связи: медиатека на «" + s.cfg.room + "» умеет писать сюда."); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "нет такого действия"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- уведомления в Telegram ---

var notifyHTTP = &http.Client{Timeout: 15 * time.Second}

// notifyURL — адрес уведомлений на проде: рядом с шиной пульта.
func (s *server) notifyURL() string {
	h := s.cfg.hub
	switch {
	case strings.HasPrefix(h, "wss://"):
		h = "https://" + strings.TrimPrefix(h, "wss://")
	case strings.HasPrefix(h, "ws://"):
		h = "http://" + strings.TrimPrefix(h, "ws://")
	default:
		return ""
	}
	return strings.TrimSuffix(h, "/socket") + "/notify"
}

// notify пишет владельцу приставки в Telegram. Пульт к приставке не
// подключали — прод ответит 404: писать некому.
func (s *server) notify(text string) error {
	u := s.notifyURL()
	if u == "" || s.cfg.room == "" {
		return errors.New("шина не настроена")
	}
	body, _ := json.Marshal(map[string]string{"room": s.cfg.room, "text": text})
	resp, err := notifyHTTP.Post(u, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("прод недоступен: %v", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK:
		return nil
	case http.StatusNotFound:
		return errors.New("к этой приставке ещё не подключали пульт — писать некому")
	case http.StatusTooManyRequests:
		return errors.New("слишком много уведомлений за час")
	}
	return fmt.Errorf("прод ответил %d", resp.StatusCode)
}

func (s *server) notifyAsync(text string) {
	go func() {
		if err := s.notify(text); err != nil {
			log.Printf("уведомление не ушло: %v", err)
		}
	}()
}

func human(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f ГБ", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f МБ", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f КБ", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d Б", n)
}
