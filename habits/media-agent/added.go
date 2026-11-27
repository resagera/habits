package main

// Сведения для фильтров главной: год, жанры и когда плитка появилась в
// медиатеке («Недавно добавленные»).
//
// Время добавления запоминается при первой встрече (added.json): время
// файлов для этого не годится — отправка с компьютера может сохранить старое
// время изменения, а у папки сериала оно сдвигается с каждой новой серией.
// Первый раз берётся время смены inode (ctime) — когда файл лёг на этот диск.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type addedStore struct {
	mu    sync.Mutex
	path  string
	items map[string]int64 // путь → unix-время добавления
}

func newAddedStore(cacheDir string) *addedStore {
	a := &addedStore{path: filepath.Join(cacheDir, "added.json"), items: map[string]int64{}}
	if data, err := os.ReadFile(a.path); err == nil {
		_ = json.Unmarshal(data, &a.items)
		if a.items == nil {
			a.items = map[string]int64{}
		}
	}
	return a
}

func (a *addedStore) saveLocked() {
	if data, err := json.Marshal(a.items); err == nil {
		_ = writeAtomic(a.path, data)
	}
}

// arrivedAt — когда путь лёг на диск: ctime, а без него — время изменения.
func arrivedAt(p string) int64 {
	st, err := os.Stat(p)
	if err != nil {
		return time.Now().Unix()
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok && sys.Ctim.Sec > 0 {
		return sys.Ctim.Sec
	}
	return st.ModTime().Unix()
}

// when отдаёт время добавления путей, впервые встреченные запоминает.
func (a *addedStore) when(paths []string) map[string]int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[string]int64, len(paths))
	changed := false
	for _, p := range paths {
		t, ok := a.items[p]
		if !ok {
			t = arrivedAt(p)
			a.items[p] = t
			changed = true
		}
		out[p] = t
	}
	if changed {
		a.saveLocked()
	}
	return out
}

// move — переименовали в админке: дата добавления едет за объектом.
func (a *addedStore) move(old, neu string, isDir bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	changed := false
	for p, t := range a.items {
		if to, ok := movedCollPath(p, old, neu, isDir); ok {
			delete(a.items, p)
			a.items[to] = t
			changed = true
		}
	}
	if changed {
		a.saveLocked()
	}
}

// applyFacts дописывает фильмам и сериалам год, жанры и время добавления.
func (s *server) applyFacts(lists ...[]catTile) {
	var paths []string
	var walk func(list []catTile, fn func(t *catTile, p string))
	walk = func(list []catTile, fn func(t *catTile, p string)) {
		for i := range list {
			t := &list[i]
			if t.Kind == "collection" {
				walk(t.Items, fn)
				continue
			}
			if t.Kind != "film" && t.Kind != "series" {
				continue
			}
			if p, ok := agentStore.pathOf(t.ID); ok {
				fn(t, p)
			}
		}
	}
	for _, l := range lists {
		walk(l, func(_ *catTile, p string) { paths = append(paths, p) })
	}
	added := s.added.when(paths)
	for _, l := range lists {
		walk(l, func(t *catTile, p string) {
			t.Added = added[p]
			if inf, ok := readInfo(p); ok {
				t.Year, t.Genres = inf.Year, inf.Genres
			}
			if t.Year == 0 {
				t.Year = pickYear(filepath.Base(p))
			}
		})
	}
}
