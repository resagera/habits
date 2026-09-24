package main

// «Найти по звуку»: разметка заставки и титров для целого сезона одной кнопкой.
//
// Сам поиск — в fingerprint.go, здесь работа целиком: собрать серии, разобрать
// звук, сравнить каждую серию с несколькими другими и записать отметки на
// уровне серии (у каждой заставка начинается в своё время — общая отметка
// сезона тут не годится).
//
// Считаем в одиночку и в фоне: на i3 сезон разбирается несколько минут, а
// страница тем временем спрашивает, как дела.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// scanEpisode — что нашлось в одной серии.
type scanEpisode struct {
	Name           string  `json:"name"`
	Season         string  `json:"season"`
	IntroStart     float64 `json:"intro_start"`
	IntroEnd       float64 `json:"intro_end"`
	CreditsFromEnd float64 `json:"credits_from_end"`
	Note           string  `json:"note"`
}

type scanJob struct {
	mu       sync.Mutex
	running  bool
	cancel   context.CancelFunc
	title    string
	stage    string
	done     int
	total    int
	episodes []scanEpisode
	err      string
	started  time.Time
	ended    time.Time
}

func (j *scanJob) set(stage string, done, total int) {
	j.mu.Lock()
	j.stage, j.done, j.total = stage, done, total
	j.mu.Unlock()
}

func (j *scanJob) state() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := map[string]any{
		"running": j.running, "title": j.title, "stage": j.stage,
		"done": j.done, "total": j.total, "error": j.err,
		// копия: сам список дописывается дальше, а читать его будут уже без замка
		"episodes": append([]scanEpisode{}, j.episodes...),
	}
	if !j.started.IsZero() {
		end := j.ended
		if j.running {
			end = time.Now()
		}
		out["seconds"] = int(end.Sub(j.started).Seconds())
	}
	return out
}

// scanGroups — сезоны, которые надо разобрать: папка сериала делится на сезоны,
// папка сезона — сама себе группа.
func scanGroups(dir string) map[string][]string {
	out := map[string][]string{}
	add := func(name, d string) {
		_, vids, _ := dirContents(d)
		if len(vids) >= 2 {
			out[name] = vids
		}
	}
	add(filepath.Base(dir), dir)
	if len(out) > 0 {
		return out
	}
	subs, _, _ := dirContents(dir)
	for _, sub := range subs {
		add(filepath.Base(sub), sub)
	}
	return out
}

// POST /api/admin/marks/scan {id, from_start} — разобрать сериал или сезон.
func (s *server) startScan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID        string `json:"id"`
		FromStart bool   `json:"from_start"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужен id"})
		return
	}
	dir, ok := s.adminPath(w, req.ID)
	if !ok {
		return
	}
	groups := scanGroups(dir)
	if len(groups) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "нужны хотя бы две серии в сезоне: заставку ищем сравнением"})
		return
	}
	s.scan.mu.Lock()
	if s.scan.running {
		s.scan.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "поиск уже идёт"})
		return
	}
	total := 0
	for _, files := range groups {
		total += len(files)
	}
	ctx, cancel := context.WithCancel(context.Background())
	// поля по одному: присвоить структуру целиком — значит затереть свой же
	// захваченный замок, и ближайший Unlock уронит агента
	s.scan.running, s.scan.cancel, s.scan.err = true, cancel, ""
	s.scan.title, s.scan.stage = filepath.Base(dir), "слушаем серии"
	s.scan.done, s.scan.total, s.scan.episodes = 0, total, []scanEpisode{}
	s.scan.started, s.scan.ended = time.Now(), time.Time{}
	s.scan.mu.Unlock()
	go s.runScan(ctx, groups, req.FromStart)
	writeJSON(w, http.StatusOK, s.scan.state())
}

// GET /api/admin/marks/scan — как идут дела.
func (s *server) scanState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.scan.state())
}

// POST /api/admin/marks/scan/stop
func (s *server) stopScan(w http.ResponseWriter, r *http.Request) {
	s.scan.mu.Lock()
	if s.scan.cancel != nil {
		s.scan.cancel()
	}
	s.scan.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) runScan(ctx context.Context, groups map[string][]string, fromStart bool) {
	defer func() {
		s.scan.mu.Lock()
		s.scan.running, s.scan.ended = false, time.Now()
		if s.scan.err == "" && ctx.Err() != nil {
			s.scan.err = "поиск остановлен"
		}
		s.scan.mu.Unlock()
	}()
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Slice(names, func(a, b int) bool { return naturalLess(names[a], names[b]) })
	done := 0
	for _, name := range names {
		files := groups[name]
		sort.Slice(files, func(a, b int) bool { return naturalLess(files[a], files[b]) })
		if err := s.scanSeason(ctx, name, files, fromStart, &done); err != nil {
			s.scan.mu.Lock()
			s.scan.err = err.Error()
			s.scan.mu.Unlock()
			return
		}
	}
	s.scan.set("готово", done, done)
}

// scanRange — найденный кусок.
type scanRange struct{ start, end float64 }

func (s *server) scanSeason(ctx context.Context, season string, files []string, fromStart bool, done *int) error {
	heads := make([]*track, len(files))
	tails := make([]*track, len(files))
	for i, f := range files {
		if ctx.Err() != nil {
			return nil
		}
		s.scan.set("слушаем "+season+": "+filepath.Base(f), *done, s.scan.total)
		var err error
		if heads[i], err = audioPrints(ctx, f, false); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
		if tails[i], err = audioPrints(ctx, f, true); err != nil && ctx.Err() == nil {
			return fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
		*done++
		s.scan.set("слушаем "+season+": "+filepath.Base(f), *done, s.scan.total)
	}
	if ctx.Err() != nil {
		return nil
	}
	s.scan.set("сравниваем "+season, *done, s.scan.total)
	found := make([]scanEpisode, len(files))
	marks := make([]seriesMarks, len(files))
	for i, f := range files {
		if ctx.Err() != nil {
			return nil
		}
		var intros, credits []scanRange
		for _, ref := range refsFor(i, len(files)) {
			for _, r := range commonRanges(heads[ref], heads[i]) {
				intros = append(intros, scanRange{r.start, snapForward(heads[i], r.start, r.end)})
			}
			credits = append(credits, commonRanges(tails[ref], tails[i])...)
		}
		ep := scanEpisode{Name: filepath.Base(f), Season: season}
		m := seriesMarks{}
		// Заставка: берём кусок с самым поздним концом. Перед ней бывает заставка
		// студии-переводчика — она тоже общая, но пропускать надо ДО конца самой
		// заставки сериала, а значит, до самого дальнего общего куска.
		if best, ok := latestRange(intros, fpMinRun); ok {
			m.IntroStart, m.IntroEnd = best.start, best.end
			if fromStart {
				m.IntroStart = 0 // «пропустить всё сначала»: вместе с повтором прошлых серий
			}
			if best.end < 60 {
				// у некоторых серий своей заставки нет вовсе, и общим оказывается
				// только заставка студии в самом начале файла
				ep.Note = "общим оказалось только начало файла"
			}
		} else {
			ep.Note = "заставка не нашлась"
		}
		// Титры: берём кусок, который начинается позже всех, но не короче 20 с —
		// в конце серии общей бывает и музыка последней сцены, а нужна та, с
		// которой пошли титры.
		if best, ok := lastStarting(credits, 20); ok && tails[i].dur()-best.start <= fpCreditsMax {
			m.CreditsFromEnd = tails[i].dur() - best.start
		} else {
			ep.Note = strings.TrimPrefix(ep.Note+", титры не нашлись", ", ")
		}
		ep.IntroStart, ep.IntroEnd, ep.CreditsFromEnd = m.IntroStart, m.IntroEnd, m.CreditsFromEnd
		found[i], marks[i] = ep, m
	}
	// Титры у всех серий сезона одной длины. Где нашлось иное — там совпала
	// музыка какой-то сцены: берём то, о чём договорилось большинство.
	if mid, ok := medianCredits(marks); ok {
		for i := range marks {
			if math.Abs(marks[i].CreditsFromEnd-mid) > fpCreditsSpread {
				marks[i].CreditsFromEnd, marks[i].CreditsEndFromEnd = mid, 0
				found[i].CreditsFromEnd = mid
				found[i].Note = strings.TrimPrefix(found[i].Note+", титры по сезону", ", ")
			}
		}
	}
	for i, f := range files {
		if !marks[i].empty() {
			s.saveEpisodeMarks(f, marks[i])
		}
		s.scan.mu.Lock()
		s.scan.episodes = append(s.scan.episodes, found[i])
		s.scan.mu.Unlock()
	}
	return nil
}

const (
	// fpCreditsSpread — на сколько секунд титрам одного сезона позволено разойтись.
	fpCreditsSpread = 25
	// fpCreditsMax — дальше этого от конца титры не начинаются. У короткой серии
	// кусок «с конца» захватывает и заставку, и без этого предела титрами стала
	// бы она.
	fpCreditsMax = 600
)

func medianCredits(all []seriesMarks) (float64, bool) {
	vals := []float64{}
	for _, m := range all {
		if m.CreditsFromEnd > 0 {
			vals = append(vals, m.CreditsFromEnd)
		}
	}
	if len(vals) < 3 {
		return 0, false
	}
	sort.Float64s(vals)
	return vals[len(vals)/2], true
}

// latestRange — кусок, который кончается позже всех (из тех, что не короче
// minLen). Одинаковые куски находятся по нескольку раз — это не мешает.
func latestRange(all []scanRange, minLen float64) (scanRange, bool) {
	best, ok := scanRange{}, false
	for _, r := range all {
		if r.end-r.start >= minLen && (!ok || r.end > best.end) {
			best, ok = r, true
		}
	}
	return best, ok
}

// lastStarting — кусок, который начинается позже всех.
func lastStarting(all []scanRange, minLen float64) (scanRange, bool) {
	best, ok := scanRange{}, false
	for _, r := range all {
		if r.end-r.start >= minLen && (!ok || r.start > best.start) {
			best, ok = r, true
		}
	}
	return best, ok
}

// refsFor — с какими сериями сравниваем эту. Берём с разных концов сезона:
// в одной серии заставки может не быть вовсе, в другой она урезана, и по
// единственной опоре это не отличить.
func refsFor(i, n int) []int {
	out := []int{}
	for _, cand := range []int{0, 1, n - 1, n - 2, n / 2} {
		if cand >= 0 && cand < n && cand != i && !slicesHas(out, cand) {
			out = append(out, cand)
		}
	}
	return out
}

func slicesHas(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// saveEpisodeMarks — отметки этой серии (уровень «только эта серия»): у каждой
// свой пролог, поэтому общая отметка сезона тут не подходит.
func (s *server) saveEpisodeMarks(path string, m seriesMarks) {
	if _, err := os.Stat(path); err != nil {
		return
	}
	s.remember(path)
	key := shortID(path)
	s.marks.mu.Lock()
	old := s.marks.items[key]
	if m.IntroEnd == 0 { // нашлись только титры — прежнюю заставку не трогаем
		m.IntroStart, m.IntroEnd = old.IntroStart, old.IntroEnd
	}
	if m.CreditsFromEnd == 0 {
		m.CreditsFromEnd, m.CreditsEndFromEnd = old.CreditsFromEnd, old.CreditsEndFromEnd
	}
	m.AutoSkip, m.SkipOff = old.AutoSkip, old.SkipOff
	m.Scope, m.Path = "episode", path
	s.marks.items[key] = m
	s.marks.saveLocked()
	s.marks.mu.Unlock()
}
