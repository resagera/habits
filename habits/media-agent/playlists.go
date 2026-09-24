package main

// Свои плейлисты: треки из любых папок музыки в одном списке.
//
// Хранит агент (playlists.json в кэше), а не браузер приставки — плейлист
// собирают один раз и слушают и в слайд-шоу, и под «Музыку на паузе», и со
// второй приставки. Запись — пути к трекам: пропавший файл при показе
// молча пропускается, а не ломает весь плейлист.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type playlist struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Tracks []string `json:"tracks"` // пути
}

type playlistStore struct {
	mu    sync.Mutex
	path  string
	items []*playlist
}

func newPlaylistStore(cacheDir string) *playlistStore {
	p := &playlistStore{path: filepath.Join(cacheDir, "playlists.json")}
	if data, err := os.ReadFile(p.path); err == nil {
		_ = json.Unmarshal(data, &p.items)
	}
	return p
}

func (p *playlistStore) saveLocked() {
	if data, err := json.Marshal(p.items); err == nil {
		_ = writeAtomic(p.path, data)
	}
}

func (p *playlistStore) find(id string) *playlist {
	for _, pl := range p.items {
		if pl.ID == id {
			return pl
		}
	}
	return nil
}

// playlistTiles — плитки для раздела «Музыка»: плейлисты идут первыми.
func (s *server) playlistTiles() []catTile {
	s.playlists.mu.Lock()
	defer s.playlists.mu.Unlock()
	out := []catTile{}
	for _, pl := range s.playlists.items {
		out = append(out, catTile{ID: pl.ID, Name: pl.Name, Kind: "playlist", Count: len(pl.Tracks)})
	}
	return out
}

// audioUnder — треки папки со всеми вложенными, в «человеческом» порядке:
// поэтому «Играть всё» у альбома с дисками собирает CD1 целиком, потом CD2.
func audioUnder(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && p != dir {
			return filepath.SkipDir
		}
		if !d.IsDir() && audioExt[strings.ToLower(filepath.Ext(p))] {
			out = append(out, p)
		}
		return nil
	})
	sort.SliceStable(out, func(a, b int) bool { return naturalLess(out[a], out[b]) })
	return out
}

// GET /api/tracks?id= — треки папки со всеми вложенными, в том же виде, что
// /api/browse. Альбом бывает разложен по CD1/CD2, и «Играть всё» должно
// собирать его целиком.
func (s *server) tracks(w http.ResponseWriter, r *http.Request) {
	dir, ok := agentStore.pathOf(r.URL.Query().Get("id"))
	if !ok || !s.inRoots(dir) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "папка не найдена"})
		return
	}
	items := []entry{}
	for _, p := range audioUnder(dir) {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		// у альбома с дисками в названии видно, с какого диска трек
		name := filepath.Base(p)
		if rel, err := filepath.Rel(dir, p); err == nil && strings.Contains(rel, string(filepath.Separator)) {
			name = rel
		}
		id := s.remember(p)
		items = append(items, entry{ID: id, Name: name, Size: st.Size(),
			Ready: s.q.ready(id), Info: agentStore.probe(p, st)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.withURLs(items)})
}

func (s *server) playlistRoutes() {
	s.mux.HandleFunc("GET /api/tracks", s.tracks)
	s.mux.HandleFunc("GET /api/playlists", s.listPlaylists)
	s.mux.HandleFunc("POST /api/playlists", s.createPlaylist)
	s.mux.HandleFunc("GET /api/playlists/{id}", s.getPlaylist)
	s.mux.HandleFunc("POST /api/playlists/{id}/add", s.addToPlaylist)
	s.mux.HandleFunc("POST /api/playlists/{id}/remove", s.removeFromPlaylist)
	s.mux.HandleFunc("POST /api/playlists/{id}/rename", s.renamePlaylist)
	s.mux.HandleFunc("DELETE /api/playlists/{id}", s.deletePlaylist)
}

func (s *server) listPlaylists(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"playlists": s.playlistTiles()})
}

func playlistName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		return "", false
	}
	return name, true
}

// POST /api/playlists {name, ids?} — новый плейлист, сразу с треками или папками.
func (s *server) createPlaylist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string   `json:"name"`
		IDs  []string `json:"ids"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужно имя"})
		return
	}
	name, ok := playlistName(req.Name)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "имя плейлиста — от 1 до 120 знаков"})
		return
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	pl := &playlist{ID: "pl" + hex.EncodeToString(b), Name: name, Tracks: s.expandTracks(req.IDs)}
	s.playlists.mu.Lock()
	s.playlists.items = append(s.playlists.items, pl)
	s.playlists.saveLocked()
	s.playlists.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"playlist": catTile{ID: pl.ID, Name: pl.Name, Kind: "playlist", Count: len(pl.Tracks)}})
}

// expandTracks — пути треков по id: трек как есть, папка — всеми треками внутри.
func (s *server) expandTracks(ids []string) []string {
	var out []string
	for _, id := range ids {
		p, ok := agentStore.pathOf(id)
		if !ok || !s.inRoots(p) {
			continue
		}
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if st.IsDir() {
			out = append(out, audioUnder(p)...)
		} else if audioExt[strings.ToLower(filepath.Ext(p))] {
			out = append(out, p)
		}
	}
	return out
}

// GET /api/playlists/{id} — треки плейлиста в том же виде, что /api/browse:
// страница играет их тем же кодом, что и папку.
func (s *server) getPlaylist(w http.ResponseWriter, r *http.Request) {
	s.playlists.mu.Lock()
	pl := s.playlists.find(r.PathValue("id"))
	var tracks []string
	name := ""
	if pl != nil {
		tracks = append(tracks, pl.Tracks...)
		name = pl.Name
	}
	s.playlists.mu.Unlock()
	if pl == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "плейлист не найден"})
		return
	}
	items := []entry{}
	for _, p := range tracks {
		st, err := os.Stat(p)
		if err != nil || !s.inRoots(p) {
			continue // файл удалили или переименовали — пропускаем, а не ломаем плейлист
		}
		id := s.remember(p)
		items = append(items, entry{ID: id, Name: filepath.Base(p), Size: st.Size(),
			Ready: s.q.ready(id), Info: agentStore.probe(p, st)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "name": name, "items": s.withURLs(items)})
}

// POST /api/playlists/{id}/add {ids} — дописать треки или целые папки в конец
// (уже бывшие в плейлисте не повторяются).
func (s *server) addToPlaylist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужны ids"})
		return
	}
	add := s.expandTracks(req.IDs)
	s.playlists.mu.Lock()
	defer s.playlists.mu.Unlock()
	pl := s.playlists.find(r.PathValue("id"))
	if pl == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "плейлист не найден"})
		return
	}
	have := map[string]bool{}
	for _, t := range pl.Tracks {
		have[t] = true
	}
	added := 0
	for _, t := range add {
		if !have[t] {
			pl.Tracks = append(pl.Tracks, t)
			have[t] = true
			added++
		}
	}
	s.playlists.saveLocked()
	writeJSON(w, http.StatusOK, map[string]any{"added": added, "count": len(pl.Tracks)})
}

// POST /api/playlists/{id}/remove {id: трек}
func (s *server) removeFromPlaylist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужен id"})
		return
	}
	s.playlists.mu.Lock()
	defer s.playlists.mu.Unlock()
	pl := s.playlists.find(r.PathValue("id"))
	if pl == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "плейлист не найден"})
		return
	}
	for i, t := range pl.Tracks {
		if shortID(t) == req.ID {
			pl.Tracks = append(pl.Tracks[:i], pl.Tracks[i+1:]...)
			break
		}
	}
	s.playlists.saveLocked()
	writeJSON(w, http.StatusOK, map[string]any{"count": len(pl.Tracks)})
}

func (s *server) renamePlaylist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req)
	name, ok := playlistName(req.Name)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "имя плейлиста — от 1 до 120 знаков"})
		return
	}
	s.playlists.mu.Lock()
	defer s.playlists.mu.Unlock()
	pl := s.playlists.find(r.PathValue("id"))
	if pl == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "плейлист не найден"})
		return
	}
	pl.Name = name
	s.playlists.saveLocked()
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

func (s *server) deletePlaylist(w http.ResponseWriter, r *http.Request) {
	s.playlists.mu.Lock()
	defer s.playlists.mu.Unlock()
	for i, pl := range s.playlists.items {
		if pl.ID == r.PathValue("id") {
			s.playlists.items = append(s.playlists.items[:i], s.playlists.items[i+1:]...)
			s.playlists.saveLocked()
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "плейлист не найден"})
}
