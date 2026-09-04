package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

//go:embed player.html
var playerHTML []byte

// agentStore — общий кэш разбора. Глобальный намеренно: очередь задач должна
// уметь превратить id обратно в путь, а таскать хранилище через три структуры
// ради одного вызова смысла нет.
var agentStore *store

type server struct {
	cfg    config
	mux    *http.ServeMux
	q      *queue
	secret []byte
}

func newServer(cfg config) *server {
	agentStore = newStore(cfg.cache)
	s := &server{cfg: cfg, mux: http.NewServeMux(), q: newQueue(cfg.cache),
		secret: loadSecret(cfg.cache)}

	s.mux.HandleFunc("GET /{$}", s.page)
	s.mux.HandleFunc("GET /api/libraries", s.libraries)
	s.mux.HandleFunc("GET /api/browse", s.browse)
	s.mux.HandleFunc("GET /api/jobs", s.jobs)
	s.mux.HandleFunc("POST /api/jobs", s.addJob)
	s.mux.HandleFunc("POST /api/jobs/{id}/stop", s.stopJob)
	s.mux.HandleFunc("GET /media/{id}", s.media)
	s.mux.HandleFunc("HEAD /media/{id}", s.media)
	return s
}

// ServeHTTP срезает внешний префикс, под которым агента видно через nginx.
// Так один и тот же код работает и за проксей на /tv, и напрямую в отладке.
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.cfg.base != "" {
		if r.URL.Path == s.cfg.base {
			http.Redirect(w, r, s.cfg.base+"/", http.StatusMovedPermanently)
			return
		}
		r.URL.Path = strings.TrimPrefix(r.URL.Path, s.cfg.base)
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
	}
	s.mux.ServeHTTP(w, r)
}

// loadSecret — ключ подписи ссылок на файлы. Живёт в кэше и переживает
// перезапуск: иначе открытая на приставке страница после рестарта агента
// теряла бы все ссылки разом.
func loadSecret(dir string) []byte {
	path := filepath.Join(dir, "secret")
	if data, err := os.ReadFile(path); err == nil && len(data) >= 32 {
		return data
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		log.Fatalf("не удалось создать ключ подписи: %v", err)
	}
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		log.Printf("ключ подписи не сохранился (%v) — ссылки протухнут при перезапуске", err)
	}
	return buf
}

// sign — пропуск к файлу. В локальной сети живёт не только приставка: без
// подписи любой сосед по вайфаю перебирал бы ссылки и читал бы папки.
func (s *server) sign(id string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(id))
	return hex.EncodeToString(m.Sum(nil))[:16]
}

func (s *server) validKey(id, key string) bool {
	return hmac.Equal([]byte(s.sign(id)), []byte(key))
}

func (s *server) mediaURL(id string) string {
	return s.cfg.base + "/media/" + id + "?k=" + s.sign(id)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *server) page(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// страница знает свой префикс: иначе за проксей все запросы уедут в корень
	body := strings.ReplaceAll(string(playerHTML), "{{BASE}}", s.cfg.base)
	body = strings.ReplaceAll(body, "{{ROOM}}", s.cfg.room)
	body = strings.ReplaceAll(body, "{{HUB}}", s.cfg.hub)
	_, _ = w.Write([]byte(body))
}

func (s *server) libraries(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"libraries": s.cfg.roots})
}

// browse отдаёт содержимое папки. Наружу ходят только идентификаторы: клиент
// не знает и не может задать путь, поэтому выйти за пределы библиотеки нечем.
func (s *server) browse(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	dir := ""
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужен id"})
		return
	}
	for _, lib := range s.cfg.roots {
		if lib.ID == id {
			dir = lib.Path
		}
	}
	if dir == "" {
		p, ok := agentStore.pathOf(id)
		if !ok || !s.inRoots(p) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "папка не найдена"})
			return
		}
		dir = p
	}
	items, err := agentStore.browse(dir, s.q.ready)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	type outItem struct {
		entry
		URL string `json:"url,omitempty"`
	}
	out := make([]outItem, 0, len(items))
	for _, it := range items {
		o := outItem{entry: it}
		if !it.IsDir {
			o.URL = s.mediaURL(it.ID)
		}
		out = append(out, o)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// inRoots — путь лежит внутри разрешённых папок. Симлинки разворачиваются и
// проверяются повторно: иначе ссылка внутри библиотеки открыла бы весь диск.
func (s *server) inRoots(p string) bool {
	within := func(target string) bool {
		for _, lib := range s.cfg.roots {
			if target == lib.Path || strings.HasPrefix(target, lib.Path+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}
	if !within(p) {
		return false
	}
	if real, err := filepath.EvalSymlinks(p); err == nil && !within(real) {
		return false
	}
	return true
}

func (s *server) jobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"jobs": s.q.list()})
}

func (s *server) addJob(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		Profile string `json:"profile"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужен id"})
		return
	}
	path, ok := agentStore.pathOf(req.ID)
	if !ok || !s.inRoots(path) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "файл не найден"})
		return
	}
	st, err := os.Stat(path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "файл не найден"})
		return
	}
	info := agentStore.probe(path, st)
	profile := req.Profile
	if profile == "" && info != nil {
		profile = info.Verdict // вердикт разбора и есть нужный профиль
	}
	if profile == "" || profile == "ok" {
		profile = "remux"
	}
	duration := 0.0
	if info != nil {
		duration = info.Duration
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"job": s.q.add(req.ID, filepath.Base(path), profile, duration)})
}

func (s *server) stopJob(w http.ResponseWriter, r *http.Request) {
	if !s.q.stop(r.PathValue("id")) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "задача не найдена"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// media отдаёт файл. Range обслуживает http.ServeContent — с ним перемотка и
// докачка работают ровно так, как ждёт <video>.
func (s *server) media(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.validKey(id, r.URL.Query().Get("k")) {
		http.Error(w, "нет доступа", http.StatusForbidden)
		return
	}
	path, ok := agentStore.pathOf(id)
	if !ok || !s.inRoots(path) {
		http.Error(w, "файл не найден", http.StatusNotFound)
		return
	}
	// перекодированная копия главнее оригинала: её и просили
	if s.q.ready(id) {
		path = s.q.output(id)
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "файл не открылся", http.StatusNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.Error(w, "файл не открылся", http.StatusNotFound)
		return
	}
	if ct := contentType(path); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeContent(w, r, filepath.Base(path), st.ModTime(), f)
}

// contentType — тип по расширению. mkv и вебмоподобные добавляем руками:
// системная таблица о них знает не везде, а без типа браузер не берётся играть.
func contentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mkv":
		return "video/x-matroska"
	case ".m4v":
		return "video/mp4"
	case ".ts":
		return "video/mp2t"
	case ".opus":
		return "audio/ogg"
	}
	return mime.TypeByExtension(ext)
}
