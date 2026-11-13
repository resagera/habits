package main

// Заголовки и теги файлов и папок — то, что вводит человек, а не собирает
// обход диска.
//
// Заголовок — то, что показывается вместо имени файла: «Отпуск в Дилижане»
// вместо «IMG_20240812_101530.jpg». Файл на диске он не трогает. Теги —
// метки для фильтра (tags.go).
//
// Ключ — ПУТЬ, как у избранного: id это хеш пути, и переименование сбросило
// бы запись. Поэтому хранилище стоит в moveMemory и едет за файлом при
// переименовании, переезде на другой диск и при «переезде мимо агента»,
// который замечает обход (fileindex.go). В копию разметки файл попадает: это
// ручная работа, её не восстановить обходом.
//
// Править разрешено всем, кто смотрит, — без PIN админки (так решил хозяин
// медиатеки): заголовок и тег ничего не удаляют.

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

type fileMeta struct {
	Title string   `json:"title,omitempty"`
	Tags  []string `json:"tags,omitempty"`
}

func (m fileMeta) empty() bool { return m.Title == "" && len(m.Tags) == 0 }

type metaStore struct {
	mu    sync.Mutex
	path  string
	items map[string]fileMeta
}

func newMetaStore(cacheDir string) *metaStore {
	m := &metaStore{path: filepath.Join(cacheDir, "filemeta.json"), items: map[string]fileMeta{}}
	if data, err := os.ReadFile(m.path); err == nil {
		_ = json.Unmarshal(data, &m.items)
		if m.items == nil {
			m.items = map[string]fileMeta{}
		}
	}
	return m
}

func (ms *metaStore) saveLocked() {
	if data, err := json.MarshalIndent(ms.items, "", " "); err == nil {
		_ = writeAtomic(ms.path, data)
	}
}

func (ms *metaStore) get(path string) fileMeta {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.items[path]
}

func (ms *metaStore) title(path string) string { return ms.get(path).Title }

func (ms *metaStore) setTitle(path, title string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.updateLocked(path, func(m *fileMeta) { m.Title = title })
	ms.saveLocked()
}

// move — запись едет за файлом или папкой (у папки — и всё, что внутри).
func (ms *metaStore) move(old, neu string, isDir bool) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	type change struct{ from, to string }
	var list []change
	for p := range ms.items {
		if np, ok := movedCollPath(p, old, neu, isDir); ok {
			list = append(list, change{p, np})
		}
	}
	for _, c := range list {
		m := ms.items[c.from]
		delete(ms.items, c.from)
		ms.items[c.to] = m
	}
	if len(list) > 0 {
		ms.saveLocked()
	}
}

// cleanDisplayTitle — заголовок без переводов строк и лишних пробелов. Пустой —
// значит «показывать имя файла».
func cleanDisplayTitle(t string) (string, error) {
	t = strings.Join(strings.Fields(t), " ")
	if !utf8.ValidString(t) {
		return "", errors.New("заголовок не в UTF-8")
	}
	if utf8.RuneCountInString(t) > 200 {
		return "", errors.New("слишком длинный заголовок")
	}
	return t, nil
}

// applyTitles подставляет заголовки и теги в плитки каталога: заголовок заменяет
// название и в визуальном режиме. Порядок раздела пересчитывается по
// показанному названию — иначе «Дилижан» стоял бы там, где был «IMG_2024».
func (s *server) applyTitles(lists ...[]catTile) {
	var walk func(list []catTile)
	walk = func(list []catTile) {
		changed := false
		for i := range list {
			// фильм в своей папке: плитка — папка, но заголовок могли дать и
			// самому файлу (в режиме «Файлы» его и правят); папка главнее
			titled := false
			for _, id := range []string{list[i].ID, list[i].File} {
				if id == "" || list[i].Kind == "collection" {
					continue
				}
				if p, ok := agentStore.pathOf(id); ok {
					m := s.meta.get(p)
					if m.Title != "" && !titled {
						list[i].Name = m.Title
						changed, titled = true, true
					}
					// теги и папки, и самого файла фильма — вместе
					list[i].Tags = normTags(append(list[i].Tags, m.Tags...))
				}
			}
			if len(list[i].Tags) == 0 {
				list[i].Tags = nil
			}
			for j := range list[i].Seasons {
				if p, ok := agentStore.pathOf(list[i].Seasons[j].ID); ok {
					if t := s.meta.title(p); t != "" {
						list[i].Seasons[j].Name = t
					}
				}
			}
			walk(list[i].Items)
		}
		if changed {
			sort.SliceStable(list, func(a, b int) bool {
				if list[a].Fav != list[b].Fav {
					return list[a].Fav
				}
				return naturalLess(list[a].Name, list[b].Name)
			})
		}
	}
	for _, list := range lists {
		walk(list)
	}
}

// POST /api/file/edit {id, name?, title?} — переименовать на диске и/или
// задать заголовок. Поле, которого нет в запросе, не трогается. Ответ —
// новый id (он меняется вместе с путём), имя и заголовок.
func (s *server) fileEdit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID    string    `json:"id"`
		Name  *string   `json:"name"`
		Title *string   `json:"title"`
		Tags  *[]string `json:"tags"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	path, ok := agentStore.pathOf(req.ID)
	if !ok || !s.inRoots(path) || !s.visible(path, r) || !exists(path) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "не найдено"})
		return
	}
	var title string
	if req.Title != nil {
		t, err := cleanDisplayTitle(*req.Title)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		title = t
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) != "" && strings.TrimSpace(*req.Name) != filepath.Base(path) {
		neu, err := s.renamePath(path, *req.Name)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		path = neu
	}
	if req.Title != nil {
		// заголовок, совпавший с именем, ничего не добавляет — не храним
		if title == filepath.Base(path) || title == strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) {
			title = ""
		}
		s.meta.setTitle(path, title)
	}
	if req.Tags != nil {
		s.meta.setTags(path, *req.Tags)
	}
	m := s.meta.get(path)
	writeJSON(w, http.StatusOK, map[string]any{"id": s.remember(path), "name": filepath.Base(path),
		"title": m.Title, "tags": m.Tags})
}

// shownName — заголовок, если задан, иначе имя файла.
func (s *server) shownName(p string) string {
	if t := s.meta.title(p); t != "" {
		return t
	}
	return filepath.Base(p)
}
