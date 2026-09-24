package main

// «Продолжить просмотр»: где остановились в каждом сериале и фильме.
//
// Хранится на агенте, а не в браузере приставки: хранилище вебвью бывает
// очищено, а позиция нужна и со второй приставки, и потом с пульта. Одна
// запись на сериал (ключ — папка сериала, как у плитки каталога) или на фильм.
//
// Плеер присылает только id файла и секунду; к какому сериалу относится
// серия, агент выясняет по папкам сам — так запись работает одинаково и в
// визуальном режиме, и в «Файлах», и при запуске с телефона.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	progressMax     = 20
	progressMaxAge  = 180 * 24 * time.Hour
	progressMinPos  = 60.0  // первые секунды — случайное включение, не запись
	progressTailSec = 120.0 // осталось меньше двух минут — это титры
)

type progressEntry struct {
	Key      string  `json:"key"`  // id плитки: папка сериала или фильм
	Kind     string  `json:"kind"` // series | film
	Title    string  `json:"title"`
	File     string  `json:"file"`    // id файла, на котором остановились
	Episode  string  `json:"episode"` // имя файла без расширения
	Season   string  `json:"season"`  // имя папки сезона, у фильма пусто
	SeasonID string  `json:"season_id"`
	Position float64 `json:"position"`
	Duration float64 `json:"duration"`
	Updated  int64   `json:"updated"`
	Poster   string  `json:"poster,omitempty"`
	// Пути — только для самого агента, наружу не отдаются. Rel нужен, чтобы
	// найти серию после перекодирования: S01E05.avi → S01E05.mp4 меняет id
	// (это хеш пути), и без запасного поиска позиция терялась бы.
	Dir string `json:"dir,omitempty"`
	Rel string `json:"rel,omitempty"`
}

type progressStore struct {
	mu    sync.Mutex
	path  string
	items map[string]*progressEntry
}

func newProgressStore(cacheDir string) *progressStore {
	p := &progressStore{path: filepath.Join(cacheDir, "progress.json"), items: map[string]*progressEntry{}}
	if data, err := os.ReadFile(p.path); err == nil {
		_ = json.Unmarshal(data, &p.items)
		if p.items == nil {
			p.items = map[string]*progressEntry{}
		}
	}
	return p
}

// update меняет записи под замком, подрезает лишнее и сохраняет на диск.
func (p *progressStore) update(fn func(items map[string]*progressEntry)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fn(p.items)
	cutoff := time.Now().Add(-progressMaxAge).Unix()
	var list []*progressEntry
	for k, e := range p.items {
		if e.Updated < cutoff {
			delete(p.items, k)
			continue
		}
		list = append(list, e)
	}
	if len(list) > progressMax {
		sort.Slice(list, func(a, b int) bool { return list[a].Updated > list[b].Updated })
		for _, e := range list[progressMax:] {
			delete(p.items, e.Key)
		}
	}
	if data, err := json.Marshal(p.items); err == nil {
		_ = writeAtomic(p.path, data)
	}
}

// owner — чья это серия: сериал или фильм и его папка.
type owner struct {
	kind   string
	key    string
	dir    string // от неё считается относительный путь
	title  string
	season string // папка сезона; у фильма пусто
}

// ownerOf повторяет раскладку каталога (catalog.go), чтобы ключ записи
// совпадал с id плитки на главной.
func (s *server) ownerOf(path string) owner {
	parent := filepath.Dir(path)
	name := func(p string, isFile bool) string {
		clean, _ := cleanTitle(filepath.Base(p), isFile)
		return displayName(filepath.Base(p), isFile, clean)
	}
	if s.isRoot(parent) {
		// файл прямо в корне библиотеки — фильм
		return owner{kind: "film", key: s.remember(path), dir: parent, title: name(path, true)}
	}
	grand := filepath.Dir(parent)
	if s.isRoot(grand) {
		sub, vids, _ := dirContents(parent)
		if len(vids) == 1 && !hasSeasonDirs(sub) {
			return owner{kind: "film", key: s.remember(parent), dir: parent, title: name(parent, false)}
		}
		// серии прямо в папке сериала: мини-сериал или «Без сезона»
		return owner{kind: "series", key: s.remember(parent), dir: parent,
			title: name(parent, false), season: parent}
	}
	return owner{kind: "series", key: s.remember(grand), dir: grand,
		title: name(grand, false), season: parent}
}

func (s *server) entryFor(o owner, path string, pos, dur float64) *progressEntry {
	rel, _ := filepath.Rel(o.dir, path)
	e := &progressEntry{
		Key: o.key, Kind: o.kind, Title: o.title, File: s.remember(path),
		Episode:  strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Position: pos, Duration: dur, Updated: time.Now().Unix(),
		Dir: o.dir, Rel: strings.TrimSuffix(rel, filepath.Ext(rel)),
	}
	if o.kind == "series" {
		season := filepath.Dir(path)
		e.Season = filepath.Base(season)
		e.SeasonID = s.remember(season)
	}
	return e
}

// nextEpisode — следующая серия: в том же сезоне, иначе первая серия
// следующего сезона того же сериала.
func (s *server) nextEpisode(path string, o owner) (string, bool) {
	season := filepath.Dir(path)
	_, vids, _ := dirContents(season)
	for i, v := range vids {
		if v == path && i+1 < len(vids) {
			return vids[i+1], true
		}
	}
	if o.kind != "series" || season == o.dir {
		return "", false // мини-сериал: папка кончилась — кончился и сериал
	}
	dirs, _, _ := dirContents(o.dir)
	for i, d := range dirs {
		if d != season {
			continue
		}
		for _, next := range dirs[i+1:] {
			if _, nv, _ := dirContents(next); len(nv) > 0 {
				return nv[0], true
			}
		}
	}
	return "", false
}

func isFinished(pos, dur float64) bool {
	if dur <= 0 {
		return false
	}
	// у коротких роликов «осталось две минуты» — это почти весь ролик
	if dur < 5*60 {
		return pos >= dur*0.97
	}
	return dur-pos < progressTailSec || pos >= dur*0.97
}

// PUT|POST /api/progress {file, position, duration}. POST — ради
// navigator.sendBeacon, которым плеер отправляет позицию при закрытии.
func (s *server) saveProgress(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File     string  `json:"file"`
		Position float64 `json:"position"`
		Duration float64 `json:"duration"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil || req.File == "" ||
		req.Position < 0 || req.Duration < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужны file, position, duration"})
		return
	}
	path, ok := agentStore.pathOf(req.File)
	if !ok || !s.inRoots(path) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "файл не найден"})
		return
	}
	if !videoExt[strings.ToLower(filepath.Ext(path))] {
		// музыка позицию не запоминает: её слушают по кругу, а не «досматривают»
		writeJSON(w, http.StatusOK, map[string]any{"skipped": true})
		return
	}
	o := s.ownerOf(path)
	s.statsStore.record(req.File, o, req.Position, time.Now())
	result := map[string]any{}
	s.progress.update(func(items map[string]*progressEntry) {
		cur := items[o.key]
		if isFinished(req.Position, req.Duration) {
			if next, ok := s.nextEpisode(path, o); ok && o.kind == "series" {
				items[o.key] = s.entryFor(o, next, 0, 0)
				result["entry"] = items[o.key]
				return
			}
			delete(items, o.key)
			result["removed"] = true
			return
		}
		// первая минута — не повод затирать место в сериале: чаще всего это
		// случайно открытая серия. Но свою же запись (например, следующую
		// серию после досмотренной) обновляем с любой секунды.
		if req.Position < progressMinPos && (cur == nil || cur.File != req.File) {
			result["skipped"] = true
			return
		}
		items[o.key] = s.entryFor(o, path, req.Position, req.Duration)
		result["entry"] = items[o.key]
	})
	if e, ok := result["entry"].(*progressEntry); ok {
		out := s.publicEntry(*e)
		result["entry"] = out
	}
	writeJSON(w, http.StatusOK, result)
}

// resolve проверяет, что файл записи на месте. Нет — ищет тот же путь с
// другим расширением (серию перекодировали). Не нашёлся и так — запись не
// нужна.
func (s *server) resolve(e *progressEntry) bool {
	if p, ok := agentStore.pathOf(e.File); ok && s.inRoots(p) {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return true
		}
	}
	if e.Dir == "" || e.Rel == "" {
		return false
	}
	base := filepath.Join(e.Dir, e.Rel)
	for ext := range videoExt {
		candidate := base + ext
		st, err := os.Stat(candidate)
		if err != nil || st.IsDir() || !s.inRoots(candidate) {
			continue
		}
		o := s.ownerOf(candidate)
		fresh := s.entryFor(o, candidate, e.Position, e.Duration)
		fresh.Updated = e.Updated
		*e = *fresh
		return true
	}
	return false
}

func (s *server) publicEntry(e progressEntry) progressEntry {
	e.Dir, e.Rel = "", ""
	e.Poster = s.posterURL(e.Key, e.Kind)
	return e
}

// GET /api/progress — свежие сверху.
func (s *server) listProgress(w http.ResponseWriter, r *http.Request) {
	out := []progressEntry{}
	s.progress.update(func(items map[string]*progressEntry) {
		for key, e := range items {
			if !s.resolve(e) {
				delete(items, key)
				continue
			}
			if e.Key != key {
				// фильм прямо в корне библиотеки: его ключ — id файла, и после
				// перекодирования он тоже сменился
				delete(items, key)
				items[e.Key] = e
			}
			out = append(out, s.publicEntry(*e))
		}
	})
	sort.Slice(out, func(a, b int) bool { return out[a].Updated > out[b].Updated })
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// DELETE /api/progress/{key} — «✕ Убрать» с главной.
func (s *server) deleteProgress(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	s.progress.update(func(items map[string]*progressEntry) { delete(items, key) })
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
