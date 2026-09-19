package main

// Заставка и титры: отмечают в одной серии — работает на весь сериал.
//
// Начало и конец заставки хранятся от начала серии (заставка обычно идёт в
// одно и то же время), титры — от КОНЦА серии: серии разной длины, а титры
// одинаковые. Ключ — тот же, что у «Продолжить просмотр» (папка сериала или
// фильм), поэтому отметка из любой серии попадает в общую запись.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

type seriesMarks struct {
	IntroStart     float64 `json:"intro_start"`
	IntroEnd       float64 `json:"intro_end"`        // 0 — заставка не отмечена
	CreditsFromEnd float64 `json:"credits_from_end"` // 0 — титры не отмечены
	AutoSkip       bool    `json:"auto_skip"`        // пропускать заставку без вопроса
}

type marksStore struct {
	mu    sync.Mutex
	path  string
	items map[string]seriesMarks
}

func newMarksStore(cacheDir string) *marksStore {
	m := &marksStore{path: filepath.Join(cacheDir, "marks.json"), items: map[string]seriesMarks{}}
	if data, err := os.ReadFile(m.path); err == nil {
		_ = json.Unmarshal(data, &m.items)
		if m.items == nil {
			m.items = map[string]seriesMarks{}
		}
	}
	return m
}

// marksKey — чья это серия: ключ сериала (или фильма) по файлу.
func (s *server) marksKey(w http.ResponseWriter, fileID string) (string, string, bool) {
	path, ok := agentStore.pathOf(fileID)
	if !ok || !s.inRoots(path) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "файл не найден"})
		return "", "", false
	}
	o := s.ownerOf(path)
	return o.key, o.kind, true
}

// GET /api/marks?file= — отметки сериала этой серии.
func (s *server) getMarks(w http.ResponseWriter, r *http.Request) {
	key, kind, ok := s.marksKey(w, r.URL.Query().Get("file"))
	if !ok {
		return
	}
	s.marks.mu.Lock()
	m := s.marks.items[key]
	s.marks.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "kind": kind, "marks": m})
}

// PUT /api/marks {file, intro_start?, intro_end?, credits_from_end?, auto_skip?, reset?}
func (s *server) putMarks(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File           string   `json:"file"`
		IntroStart     *float64 `json:"intro_start"`
		IntroEnd       *float64 `json:"intro_end"`
		CreditsFromEnd *float64 `json:"credits_from_end"`
		AutoSkip       *bool    `json:"auto_skip"`
		Reset          bool     `json:"reset"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil || req.File == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужен file"})
		return
	}
	key, kind, ok := s.marksKey(w, req.File)
	if !ok {
		return
	}
	pos := func(p *float64) float64 { return max(0, min(*p, 6*3600)) }
	s.marks.mu.Lock()
	m := s.marks.items[key]
	if req.Reset {
		m = seriesMarks{}
	}
	if req.IntroStart != nil {
		m.IntroStart = pos(req.IntroStart)
	}
	if req.IntroEnd != nil {
		m.IntroEnd = pos(req.IntroEnd)
	}
	if req.CreditsFromEnd != nil {
		m.CreditsFromEnd = pos(req.CreditsFromEnd)
	}
	if req.AutoSkip != nil {
		m.AutoSkip = *req.AutoSkip
	}
	// конец раньше начала — начало не отмечали или отметили не то: считаем с нуля
	if m.IntroEnd > 0 && m.IntroStart >= m.IntroEnd {
		m.IntroStart = 0
	}
	if m == (seriesMarks{}) {
		delete(s.marks.items, key)
	} else {
		s.marks.items[key] = m
	}
	data, _ := json.Marshal(s.marks.items)
	s.marks.mu.Unlock()
	_ = writeAtomic(s.marks.path, data)
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "kind": kind, "marks": m})
}
