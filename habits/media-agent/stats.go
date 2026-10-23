package main

// Статистика просмотра: сколько смотрят и что чаще.
//
// Считается по тем же отметкам позиции, что «Продолжить просмотр» (плеер шлёт
// их раз в 15 секунд): время между двумя отметками одного файла идёт в зачёт,
// если позиция сдвинулась вперёд примерно на столько же, сколько прошло
// времени. Перемотка, пауза на час и повтор той же секунды не считаются.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

const statsKeepDays = 400

type statsTitle struct {
	Title string `json:"title"`
	Kind  string `json:"kind"`
}

type statsData struct {
	Days   map[string]map[string]float64 `json:"days"` // дата → ключ → секунды
	Titles map[string]statsTitle         `json:"titles"`
}

type statsStore struct {
	mu   sync.Mutex
	path string
	d    statsData
	last map[string]struct { // файл → последняя отметка
		pos float64
		at  time.Time
	}
}

func newStatsStore(cacheDir string) *statsStore {
	st := &statsStore{path: filepath.Join(cacheDir, "stats.json"),
		d: statsData{Days: map[string]map[string]float64{}, Titles: map[string]statsTitle{}}}
	st.last = map[string]struct {
		pos float64
		at  time.Time
	}{}
	if data, err := os.ReadFile(st.path); err == nil {
		_ = json.Unmarshal(data, &st.d)
		if st.d.Days == nil {
			st.d.Days = map[string]map[string]float64{}
		}
		if st.d.Titles == nil {
			st.d.Titles = map[string]statsTitle{}
		}
	}
	return st
}

// record учитывает отметку позиции. now — ради тестов.
func (st *statsStore) record(file string, o owner, pos float64, now time.Time) {
	st.mu.Lock()
	defer st.mu.Unlock()
	prev, ok := st.last[file]
	st.last[file] = struct {
		pos float64
		at  time.Time
	}{pos, now}
	if !ok {
		return
	}
	delta := pos - prev.pos
	wall := now.Sub(prev.at).Seconds()
	// вперёд, не больше двух минут и не быстрее, чем шло время (+10 с на
	// неточность): иначе это перемотка
	if delta <= 0 || delta > 120 || delta > wall+10 {
		return
	}
	day := now.Format("2006-01-02")
	if st.d.Days[day] == nil {
		st.d.Days[day] = map[string]float64{}
	}
	st.d.Days[day][o.key] += delta
	st.d.Titles[o.key] = statsTitle{Title: o.title, Kind: o.kind}
	cutoff := now.AddDate(0, 0, -statsKeepDays).Format("2006-01-02")
	for d := range st.d.Days {
		if d < cutoff {
			delete(st.d.Days, d)
		}
	}
	if data, err := json.Marshal(st.d); err == nil {
		_ = writeAtomic(st.path, data)
	}
}

type statsDay struct {
	Date    string  `json:"date"`
	Seconds float64 `json:"seconds"`
}

type statsTop struct {
	Title   string  `json:"title"`
	Kind    string  `json:"kind"`
	Seconds float64 `json:"seconds"`
}

// summary — по дням за последние n дней и самое смотримое за это время.
func (st *statsStore) summary(n int, now time.Time) (days []statsDay, top []statsTop, total float64) {
	st.mu.Lock()
	defer st.mu.Unlock()
	per := map[string]float64{}
	for i := n - 1; i >= 0; i-- {
		date := now.AddDate(0, 0, -i).Format("2006-01-02")
		sum := 0.0
		for key, sec := range st.d.Days[date] {
			sum += sec
			per[key] += sec
		}
		days = append(days, statsDay{Date: date, Seconds: sum})
		total += sum
	}
	for key, sec := range per {
		t := st.d.Titles[key]
		top = append(top, statsTop{Title: t.Title, Kind: t.Kind, Seconds: sec})
	}
	sort.Slice(top, func(a, b int) bool { return top[a].Seconds > top[b].Seconds })
	if len(top) > 10 {
		top = top[:10]
	}
	return
}

// GET /api/stats?days=7
func (s *server) stats(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if n <= 0 || n > 366 {
		n = 7
	}
	days, top, total := s.statsStore.summary(n, time.Now())
	if top == nil {
		top = []statsTop{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": days, "top": top, "total": total})
}
