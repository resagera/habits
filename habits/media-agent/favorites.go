package main

// Избранное: отмеченное поднимается в начало своего раздела.
//
// Хранит агент, а не браузер приставки: отметка — свойство медиатеки, а не
// устройства. Открыли ту же библиотеку со второй приставки или с телефона —
// порядок тот же.
//
// Ключ записи — ПУТЬ, а не id: id это хеш пути, и переименование в админке
// сбросило бы отметку. Исключение — папки-подборки: пути у них нет, они живут
// в кэше агента, и ключом идёт их собственный id.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type favStore struct {
	mu    sync.Mutex
	path  string
	items map[string]bool
}

func newFavStore(cacheDir string) *favStore {
	f := &favStore{path: filepath.Join(cacheDir, "favorites.json"), items: map[string]bool{}}
	if data, err := os.ReadFile(f.path); err == nil {
		var list []string
		if json.Unmarshal(data, &list) == nil {
			for _, ref := range list {
				f.items[ref] = true
			}
		}
	}
	return f
}

// saveLocked пишет список, а не карту: его читают глазами, когда разбираются,
// почему фильм вылез наверх.
func (fs *favStore) saveLocked() {
	list := make([]string, 0, len(fs.items))
	for ref := range fs.items {
		list = append(list, ref)
	}
	sort.Strings(list)
	if data, err := json.MarshalIndent(list, "", " "); err == nil {
		_ = writeAtomic(fs.path, data)
	}
}

func (fs *favStore) has(ref string) bool {
	if ref == "" {
		return false
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.items[ref]
}

func (fs *favStore) set(ref string, on bool) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if on {
		fs.items[ref] = true
	} else {
		delete(fs.items, ref)
	}
	fs.saveLocked()
}

// move — переименовали в админке: отметка едет за объектом вместе с путём.
func (fs *favStore) move(old, neu string, isDir bool) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	changed := false
	for ref := range fs.items {
		if p, ok := movedCollPath(ref, old, neu, isDir); ok {
			delete(fs.items, ref)
			fs.items[p] = true
			changed = true
		}
	}
	if changed {
		fs.saveLocked()
	}
}

// favRef — под каким ключом элемент лежит в избранном.
func (s *server) favRef(id string) (string, bool) {
	if c := s.colls.find(id); c != nil {
		return c.ID, true
	}
	p, ok := agentStore.pathOf(id)
	if !ok || !s.inRoots(p) {
		return "", false
	}
	return p, true
}

// decorateTiles дописывает плиткам то, что лежит не в файловой системе, а в
// кэше агента: отметку избранного и число ссылок. Заодно поднимает избранное
// в начало раздела — порядок внутри остаётся прежним (по названию), поэтому
// хватает устойчивой сортировки по одному признаку.
func (s *server) decorateTiles(lists ...[]catTile) {
	var walk func(list []catTile)
	walk = func(list []catTile) {
		for i := range list {
			ref := list[i].ID
			if list[i].Kind != "collection" {
				if p, ok := agentStore.pathOf(list[i].ID); ok {
					ref = p
				}
			}
			list[i].Fav = s.favs.has(ref)
			list[i].Links = s.links.count(ref)
			walk(list[i].Items) // внутри папки звёздочки и ссылки тоже видно
		}
		sort.SliceStable(list, func(a, b int) bool { return list[a].Fav && !list[b].Fav })
	}
	for _, list := range lists {
		walk(list)
	}
}

// POST /api/favorite {id, on} — отметить или снять отметку.
func (s *server) favorite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
		On bool   `json:"on"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	ref, ok := s.favRef(req.ID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "не найдено"})
		return
	}
	s.favs.set(ref, req.On)
	writeJSON(w, http.StatusOK, map[string]bool{"fav": req.On})
}
