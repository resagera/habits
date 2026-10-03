package main

// Папки фильмов и сериалов: «Гарри Поттер», «Фантастические твари» — то, что
// относится к одной вселенной и смотрится подряд, в своём порядке.
//
// Папка ВИРТУАЛЬНАЯ: файлы на диске не двигаются. Иначе пришлось бы перекладывать
// десятки гигабайт на внешнем диске (и ломать «Продолжить просмотр», отметки
// заставки и кадры, которые привязаны к пути), а один и тот же фильм нельзя было
// бы держать и в папке, и в общем списке — а это ровно то, что нужно: порядок
// просмотра у подборки свой, но «Элизиум» никуда из «Фильмов» уходить не должен.
//
// Хранится список в кэше агента (collections.json), запись — пути членов: как у
// плейлистов, пропавший файл при показе молча пропускается. Порядок членов —
// порядок просмотра, его задают в админке.
//
// Своё «Инфо» у папки заполняется только руками: в интернете «Гарри Поттера
// Коллекции» нет, а поиск по такому названию принёс бы первый фильм серии.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type collMember struct {
	Path string `json:"path"`
	Only bool   `json:"only"` // показывать только в папке, не в общем списке
}

type collection struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Members []collMember `json:"members"`
	Info    *itemInfo    `json:"info,omitempty"` // «Инфо» папки, заполняется в админке
}

type collStore struct {
	mu    sync.Mutex
	path  string
	items []*collection
}

func newCollStore(cacheDir string) *collStore {
	c := &collStore{path: filepath.Join(cacheDir, "collections.json")}
	if data, err := os.ReadFile(c.path); err == nil {
		_ = json.Unmarshal(data, &c.items)
	}
	return c
}

func (cs *collStore) saveLocked() {
	if data, err := json.MarshalIndent(cs.items, "", "  "); err == nil {
		_ = writeAtomic(cs.path, data)
	}
}

// find — папка по id. Отдаётся копия: наружу уходит в обход замка.
func (cs *collStore) find(id string) *collection {
	if !strings.HasPrefix(id, "cl") {
		return nil // id файла — не наш, не держим замок зря
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for _, c := range cs.items {
		if c.ID == id {
			out := *c
			out.Members = append([]collMember{}, c.Members...)
			return &out
		}
	}
	return nil
}

func (cs *collStore) all() []collection {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	out := make([]collection, 0, len(cs.items))
	for _, c := range cs.items {
		cp := *c
		cp.Members = append([]collMember{}, c.Members...)
		out = append(out, cp)
	}
	return out
}

func (cs *collStore) create(name string) *collection {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	c := &collection{ID: "cl" + hex.EncodeToString(b), Name: name, Members: []collMember{}}
	cs.mu.Lock()
	cs.items = append(cs.items, c)
	cs.saveLocked()
	cs.mu.Unlock()
	return c
}

// update меняет папку под замком: fn получает саму запись.
func (cs *collStore) update(id string, fn func(c *collection)) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for _, c := range cs.items {
		if c.ID == id {
			fn(c)
			cs.saveLocked()
			return true
		}
	}
	return false
}

func (cs *collStore) remove(id string) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for i, c := range cs.items {
		if c.ID == id {
			cs.items = append(cs.items[:i], cs.items[i+1:]...)
			cs.saveLocked()
			return true
		}
	}
	return false
}

// move переписывает пути членов после переименования в админке: иначе фильм
// молча выпал бы из папки.
func (cs *collStore) move(old, neu string, isDir bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	changed := false
	for _, c := range cs.items {
		for i, m := range c.Members {
			if p, ok := movedCollPath(m.Path, old, neu, isDir); ok {
				c.Members[i].Path = p
				changed = true
			}
		}
	}
	if changed {
		cs.saveLocked()
	}
}

// movedCollPath — где теперь лежит путь члена. У файлов, в отличие от записей
// «Продолжить просмотр», храним путь целиком, вместе с расширением.
func movedCollPath(p, old, neu string, isDir bool) (string, bool) {
	if p == old {
		return neu, true
	}
	if isDir && strings.HasPrefix(p, old+string(filepath.Separator)) {
		return neu + p[len(old):], true
	}
	return "", false
}

// --- обложка ---

// collCoverPath — своя обложка папки. Класть её некуда, кроме кэша агента: у
// виртуальной папки нет места на диске.
func (s *server) collCoverPath(id string) string {
	return filepath.Join(s.cfg.cache, "collections", id+".jpg")
}

func (s *server) collCover(id string) string {
	f := s.collCoverPath(id)
	if st, err := os.Stat(f); err == nil && st.Size() > 0 {
		return f
	}
	return ""
}

// --- плитки ---

// collTiles превращает папки в плитки каталога. Члены ищутся среди уже
// разложенных фильмов и сериалов по id (он от пути), поэтому у плитки папки
// внутри лежат настоящие плитки — с постером, сезонами и ссылкой на файл.
// Второй ответ — что убрать из общего списка («показывать только в папке»).
func (s *server) collTiles(films, series []catTile) ([]catTile, map[string]bool) {
	byID := map[string]catTile{}
	for _, t := range append(append([]catTile{}, films...), series...) {
		byID[t.ID] = t
	}
	hide := map[string]bool{}
	out := []catTile{}
	for _, c := range s.colls.all() {
		items := []catTile{}
		for _, m := range c.Members {
			t, ok := byID[shortID(m.Path)]
			if !ok {
				continue // фильм удалили или переименовали мимо админки
			}
			items = append(items, t)
			if m.Only {
				hide[t.ID] = true
			}
		}
		of := "film"
		if len(items) > 0 {
			of = items[0].Kind
		}
		out = append(out, catTile{ID: c.ID, Name: c.Name, Kind: "collection", Of: of,
			Count: len(items), Items: items, Poster: s.collPoster(c.ID, items)})
	}
	return out, hide
}

// collPoster — своя обложка, иначе постер первого фильма («картинка берётся у
// первого»). Время правки в ссылке: постеры отдаются с кэшем на сутки.
func (s *server) collPoster(id string, items []catTile) string {
	if f := s.collCover(id); f != "" {
		v := ""
		if st, err := os.Stat(f); err == nil {
			v = "&v=" + strconv.FormatInt(st.ModTime().Unix(), 36)
		}
		return s.cfg.base + "/poster/" + id + "?k=" + s.sign(id) + "&kind=collection" + v
	}
	for _, t := range items {
		if t.Poster != "" {
			return t.Poster
		}
	}
	return ""
}

// --- админка ---

func (s *server) collRoutes() {
	s.mux.HandleFunc("GET /api/admin/collections", s.admin(s.adminColls))
	s.mux.HandleFunc("POST /api/admin/collections", s.admin(s.adminCollCreate))
	s.mux.HandleFunc("POST /api/admin/collections/{id}", s.admin(s.adminCollUpdate))
	s.mux.HandleFunc("DELETE /api/admin/collections/{id}", s.admin(s.adminCollDelete))
}

type collMemberView struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Only  bool   `json:"only"`
	Count int    `json:"count,omitempty"`
}

type collView struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Of      string           `json:"of"`
	Cover   bool             `json:"cover"`
	Info    bool             `json:"info"`
	Members []collMemberView `json:"members"`
}

// GET /api/admin/collections — папки и всё, из чего их можно собрать.
// Фильмы и сериалы берутся ПОЛНЫМ списком, вместе со спрятанными: иначе
// фильм, отмеченный «только в папке», нельзя было бы оттуда убрать.
func (s *server) adminColls(w http.ResponseWriter, r *http.Request) {
	films, series := s.rawVideoTiles()
	byID := map[string]catTile{}
	for _, t := range append(append([]catTile{}, films...), series...) {
		byID[t.ID] = t
	}
	views := []collView{}
	for _, c := range s.colls.all() {
		v := collView{ID: c.ID, Name: c.Name, Of: "film", Cover: s.collCover(c.ID) != "",
			Info: c.Info != nil, Members: []collMemberView{}}
		for _, m := range c.Members {
			t, ok := byID[shortID(m.Path)]
			if !ok {
				continue
			}
			v.Members = append(v.Members, collMemberView{ID: t.ID, Name: t.Name, Kind: t.Kind,
				Only: m.Only, Count: t.Count})
		}
		if len(v.Members) > 0 {
			v.Of = v.Members[0].Kind
		}
		views = append(views, v)
	}
	short := func(list []catTile) []collMemberView {
		out := []collMemberView{}
		for _, t := range list {
			out = append(out, collMemberView{ID: t.ID, Name: t.Name, Kind: t.Kind, Count: t.Count})
		}
		return out
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"collections": views, "films": short(films), "series": short(series)})
}

func (s *server) adminCollCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	name, ok := playlistName(req.Name)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "название папки — от 1 до 120 знаков"})
		return
	}
	c := s.colls.create(name)
	writeJSON(w, http.StatusOK, map[string]any{"id": c.ID, "name": c.Name})
}

// POST /api/admin/collections/{id} {name?, members?} — имя и состав целиком.
// Состав приходит списком, а не по одному изменению: порядок членов — это
// порядок просмотра, и переставлять его проще разом.
func (s *server) adminCollUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Members []struct {
			ID   string `json:"id"`
			Only bool   `json:"only"`
		} `json:"members"`
		SetMembers bool `json:"set_members"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	if s.colls.find(id) == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "папка не найдена"})
		return
	}
	var members []collMember
	if req.SetMembers {
		members = []collMember{}
		seen := map[string]bool{}
		for _, m := range req.Members {
			p, ok := agentStore.pathOf(m.ID)
			if !ok || !s.inRoots(p) || seen[p] {
				continue
			}
			seen[p] = true
			members = append(members, collMember{Path: p, Only: m.Only})
		}
	}
	name := ""
	if strings.TrimSpace(req.Name) != "" {
		n, ok := playlistName(req.Name)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "название папки — от 1 до 120 знаков"})
			return
		}
		name = n
	}
	s.colls.update(id, func(c *collection) {
		if name != "" {
			c.Name = name
		}
		if req.SetMembers {
			c.Members = members
		}
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) adminCollDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.colls.remove(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "папка не найдена"})
		return
	}
	_ = os.Remove(s.collCoverPath(id))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
