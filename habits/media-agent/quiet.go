package main

// Тихие часы — когда агенту можно заниматься долгой фоновой работой.
//
// Разметка заставок по звуку, сбор постеров и описаний, работа с дорожками
// грузят i3 и диск — ровно то, на чём идёт просмотр. Поэтому такую работу
// можно не запускать сразу, а поставить «в тихие часы»: она стоит в очереди
// (quiet.json в кэше, переживает перезапуск) и берётся, только когда часы
// наступили и агент ничем другим не занят. Каждая работа — список элементов
// (сезон, плитка), идёт по одному; кончились часы посреди списка — работа
// встаёт на паузу на следующем элементе и ждёт следующей ночи.
//
// Кроме ручных работ каждую ночь агент сам ставит две: разметить заставки у
// неразмеченных сезонов и собрать постеры и описания, которых ещё нет. Тогда
// первая прокрутка каталога не дёргается, а «Пропустить заставку» есть сразу.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type quietConfig struct {
	Enabled      bool   `json:"enabled"`
	From         string `json:"from"` // «01:00»
	To           string `json:"to"`   // «07:00»; меньше From — окно через полночь
	NightMarks   bool   `json:"night_marks"`
	NightPosters bool   `json:"night_posters"`
}

type quietJob struct {
	ID    string   `json:"id"`
	Kind  string   `json:"kind"` // marks | posters
	Title string   `json:"title"`
	Items []string `json:"items"` // пути: сезоны для marks, плитки для posters
	Done  int      `json:"done"`
	Added int64    `json:"added"`
	Auto  bool     `json:"auto,omitempty"` // поставлена ночным обходом, а не руками
	Note  string   `json:"note,omitempty"` // что с ней сейчас
}

type quietStore struct {
	mu   sync.Mutex
	path string
	Cfg  quietConfig `json:"config"`
	Jobs []quietJob  `json:"jobs"`
	Next int         `json:"next"`
	// Night — дата ночи, для которой ночные работы уже поставлены: за одну
	// ночь ставим один раз, а не каждые полминуты, пока идут тихие часы.
	Night string `json:"night"`
	// Tried — сезоны, где разметка уже пробовала и ничего не нашла: чтобы не
	// слушать их каждую ночь заново. Новая серия в сезоне — новый ключ.
	Tried map[string]bool `json:"tried"`
}

var quietDefaults = quietConfig{Enabled: true, From: "01:00", To: "07:00", NightMarks: true, NightPosters: true}

func newQuietStore(cacheDir string) *quietStore {
	q := &quietStore{path: filepath.Join(cacheDir, "quiet.json"), Cfg: quietDefaults, Tried: map[string]bool{}}
	if data, err := os.ReadFile(q.path); err == nil {
		_ = json.Unmarshal(data, q)
	}
	if q.Tried == nil {
		q.Tried = map[string]bool{}
	}
	return q
}

func (q *quietStore) saveLocked() {
	if data, err := json.Marshal(q); err == nil {
		_ = writeAtomic(q.path, data)
	}
}

// hhmm — «07:30» в минуты от полуночи; кривое — -1.
func hhmm(v string) int {
	h, m, ok := strings.Cut(strings.TrimSpace(v), ":")
	hi, e1 := strconv.Atoi(h)
	mi, e2 := strconv.Atoi(m)
	if !ok || e1 != nil || e2 != nil || hi < 0 || hi > 23 || mi < 0 || mi > 59 {
		return -1
	}
	return hi*60 + mi
}

// inWindow — попадает ли время в окно. Окно через полночь (23:00–06:00)
// — обычное дело для «ночных» часов.
func (c quietConfig) inWindow(t time.Time) bool {
	from, to := hhmm(c.From), hhmm(c.To)
	if !c.Enabled || from < 0 || to < 0 || from == to {
		return false
	}
	m := t.Hour()*60 + t.Minute()
	if from < to {
		return m >= from && m < to
	}
	return m >= from || m < to
}

// nightOf — какой ночи принадлежит момент: окно через полночь — одна ночь,
// хотя дата посередине меняется.
func (c quietConfig) nightOf(t time.Time) string {
	if hhmm(c.From) > hhmm(c.To) && t.Hour()*60+t.Minute() < hhmm(c.To) {
		t = t.AddDate(0, 0, -1)
	}
	return t.Format("2006-01-02")
}

func (q *quietStore) config() quietConfig {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.Cfg
}

func (q *quietStore) add(job quietJob) quietJob {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.Next++
	job.ID = "q" + strconv.Itoa(q.Next)
	job.Added = time.Now().Unix()
	q.Jobs = append(q.Jobs, job)
	q.saveLocked()
	return job
}

func (q *quietStore) list() []quietJob {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]quietJob{}, q.Jobs...)
}

func (q *quietStore) drop(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, j := range q.Jobs {
		if j.ID == id {
			q.Jobs = append(q.Jobs[:i], q.Jobs[i+1:]...)
			q.saveLocked()
			return true
		}
	}
	return false
}

// update правит работу по id под замком и сохраняет.
func (q *quietStore) update(id string, fn func(j *quietJob)) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.Jobs {
		if q.Jobs[i].ID == id {
			fn(&q.Jobs[i])
			q.saveLocked()
			return
		}
	}
}

// hasKind — стоит ли уже в очереди работа этого вида (ночная или ручная):
// вторая такая же только повторила бы первую.
func (q *quietStore) hasKind(kind string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, j := range q.Jobs {
		if j.Kind == kind {
			return true
		}
	}
	return false
}

// errQuietOver — тихие часы кончились посреди работы: это пауза, а не ошибка.
var errQuietOver = errors.New("тихие часы кончились — продолжу следующей ночью")

const quietTick = 30 * time.Second

func (s *server) quietWorker() {
	for {
		time.Sleep(quietTick)
		s.quietTickOnce(time.Now())
	}
}

func (s *server) quietTickOnce(now time.Time) {
	cfg := s.quiet.config()
	if !cfg.inWindow(now) {
		return
	}
	s.quietPlanNight(cfg, now)
	if s.tasks.busy() {
		return
	}
	jobs := s.quiet.list()
	if len(jobs) == 0 {
		return
	}
	job := jobs[0]
	title := "🌙 " + job.Title
	if len(jobs) > 1 {
		title += fmt.Sprintf(" (в тихие часы ждут ещё %d)", len(jobs)-1)
	}
	_, err := s.tasks.start("quiet-"+job.Kind, title, func(ctx context.Context, t *task) error {
		return s.runQuietJob(ctx, t, job.ID)
	})
	if err != nil {
		return // пока решали, задачу занял кто-то другой
	}
	s.quiet.update(job.ID, func(j *quietJob) { j.Note = "идёт" })
}

// quietPlanNight — раз за ночь поставить ночные работы.
func (s *server) quietPlanNight(cfg quietConfig, now time.Time) {
	night := cfg.nightOf(now)
	s.quiet.mu.Lock()
	planned := s.quiet.Night == night
	if !planned {
		s.quiet.Night = night
		s.quiet.saveLocked()
	}
	s.quiet.mu.Unlock()
	if planned {
		return
	}
	if cfg.NightPosters && !s.quiet.hasKind("posters") {
		if items := s.tilesWithoutPosters(); len(items) > 0 {
			s.quiet.add(quietJob{Kind: "posters", Auto: true, Items: items,
				Title: fmt.Sprintf("Постеры и описания: %d плиток", len(items))})
		}
	}
	if cfg.NightMarks && !s.quiet.hasKind("marks") {
		if items := s.seasonsWithoutMarks(); len(items) > 0 {
			s.quiet.add(quietJob{Kind: "marks", Auto: true, Items: items,
				Title: fmt.Sprintf("Заставки по звуку: %d сезонов", len(items))})
		}
	}
}

// runQuietJob идёт по элементам работы с места, где остановилась.
func (s *server) runQuietJob(ctx context.Context, t *task, id string) error {
	var job quietJob
	found := false
	for _, j := range s.quiet.list() {
		if j.ID == id {
			job, found = j, true
		}
	}
	if !found {
		return errors.New("работы уже нет в очереди")
	}
	for i := job.Done; i < len(job.Items); i++ {
		if ctx.Err() != nil {
			s.quiet.update(id, func(j *quietJob) { j.Note = "остановлена — продолжу в тихие часы" })
			return nil
		}
		if !s.quiet.config().inWindow(time.Now()) {
			cfg := s.quiet.config()
			s.quiet.update(id, func(j *quietJob) { j.Note = "ждёт тихих часов (с " + cfg.From + ")" })
			t.say("тихие часы кончились на %d из %d — продолжу с %s", i, len(job.Items), cfg.From)
			return errQuietOver
		}
		item := job.Items[i]
		t.set(filepath.Base(item), i, len(job.Items))
		var err error
		switch job.Kind {
		case "marks":
			err = s.quietMarks(ctx, t, item)
		case "posters":
			err = s.quietPoster(t, item)
		default:
			err = fmt.Errorf("неизвестная работа %q", job.Kind)
		}
		if err != nil && ctx.Err() == nil {
			t.say("%s: %v", filepath.Base(item), err)
		}
		if ctx.Err() != nil {
			continue // остановили на этом элементе — его и повторим
		}
		s.quiet.update(id, func(j *quietJob) { j.Done = i + 1 })
	}
	t.set("готово", len(job.Items), len(job.Items))
	s.quiet.drop(id)
	return nil
}

// --- ночные работы ---

// quietMarks — разметить один сезон (или сериал без сезонов) по звуку.
func (s *server) quietMarks(ctx context.Context, t *task, dir string) error {
	_, vids, _ := dirContents(dir)
	if len(vids) < 2 {
		return errors.New("меньше двух серий — сравнивать не с чем")
	}
	sort.Slice(vids, func(a, b int) bool { return naturalLess(vids[a], vids[b]) })
	done := 0
	if err := s.scanSeason(ctx, t, filepath.Base(dir), vids, false, &done); err != nil {
		return err
	}
	if ctx.Err() == nil {
		s.quiet.mu.Lock()
		s.quiet.Tried[triedKey(dir, len(vids))] = true
		s.quiet.saveLocked()
		s.quiet.mu.Unlock()
	}
	return nil
}

func triedKey(dir string, n int) string { return dir + "|" + strconv.Itoa(n) }

func (s *server) quietPoster(t *task, p string) error {
	id := s.remember(p)
	kind := "film"
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		kind = "series"
	}
	_, perr := s.ensurePoster(p, id, kind)
	_, ierr := s.ensureInfo(p, id)
	if ierr == nil {
		s.ensureCast(p) // состав с фото у описаний, собранных до него
	}
	switch {
	case perr != nil && ierr != nil:
		return errors.New("ни постера, ни описания не нашлось")
	case perr != nil:
		t.say("%s: описание есть, постера не нашлось", filepath.Base(p))
	case ierr != nil:
		t.say("%s: постер есть, описания не нашлось", filepath.Base(p))
	}
	return nil
}

// tilesWithoutPosters — плитки фильмов, сериалов и аниме без постера или без
// описания (и без свежей отметки «не нашлось»).
func (s *server) tilesWithoutPosters() []string {
	films, series, anime := s.rawVideoTiles()
	var out []string
	for _, t := range append(append(films, series...), anime...) {
		p, ok := agentStore.pathOf(t.ID)
		if !ok {
			continue
		}
		inf, hasInfo := readInfo(p)
		if s.existingPoster(p, t.ID) != "" && hasInfo && inf.CastAt != 0 {
			continue
		}
		missP := filepath.Join(s.cfg.cache, "posters", t.ID+".miss")
		missI := filepath.Join(s.cfg.cache, "posters", t.ID+".info.miss")
		if exists(missP) && exists(missI) {
			continue
		}
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// seasonsWithoutMarks — сезоны сериалов и аниме, где нет ни одной отметки
// заставки или титров ни на одном уровне, и разметка там ещё не пробовала.
func (s *server) seasonsWithoutMarks() []string {
	_, series, anime := s.rawVideoTiles()
	s.quiet.mu.Lock()
	tried := make(map[string]bool, len(s.quiet.Tried))
	for k := range s.quiet.Tried {
		tried[k] = true
	}
	s.quiet.mu.Unlock()
	var out []string
	for _, t := range append(series, anime...) {
		dir, ok := agentStore.pathOf(t.ID)
		if !ok {
			continue
		}
		for _, files := range scanGroups(dir) {
			season := filepath.Dir(files[0])
			if tried[triedKey(season, len(files))] || s.seasonMarked(files) {
				continue
			}
			out = append(out, season)
		}
	}
	sort.Slice(out, func(a, b int) bool { return naturalLess(out[a], out[b]) })
	return out
}

func (s *server) seasonMarked(files []string) bool {
	for _, f := range files {
		sc, _ := s.scopesFor(f)
		if m := s.marks.merged(sc); m.IntroEnd > 0 || m.CreditsFromEnd > 0 {
			return true
		}
	}
	return false
}

// --- API ---

type quietView struct {
	Config quietConfig `json:"config"`
	Jobs   []quietJob  `json:"jobs"`
	Now    bool        `json:"now"` // тихие часы идут прямо сейчас
}

// GET /api/admin/quiet
func (s *server) adminQuiet(w http.ResponseWriter, r *http.Request) {
	cfg := s.quiet.config()
	writeJSON(w, http.StatusOK, quietView{Config: cfg, Jobs: s.quiet.list(), Now: cfg.inWindow(time.Now())})
}

// PUT /api/admin/quiet {config}
func (s *server) adminQuietSet(w http.ResponseWriter, r *http.Request) {
	var cfg quietConfig
	if !decodeBody(w, r, &cfg) {
		return
	}
	if hhmm(cfg.From) < 0 || hhmm(cfg.To) < 0 || cfg.From == cfg.To {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "часы — в виде 01:00, начало и конец разные"})
		return
	}
	s.quiet.mu.Lock()
	s.quiet.Cfg = cfg
	s.quiet.saveLocked()
	s.quiet.mu.Unlock()
	log.Printf("админка: тихие часы %s–%s, включены: %v", cfg.From, cfg.To, cfg.Enabled)
	s.adminQuiet(w, r)
}

// POST /api/admin/quiet/jobs {kind, ids} — поставить работу в тихие часы.
// marks: id сериала или сезона (сезоны внутри сериала разберутся сами);
// posters: id плиток, пустой список — все плитки без постера или описания.
func (s *server) adminQuietAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind string   `json:"kind"`
		IDs  []string `json:"ids"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	var items []string
	var title string
	switch req.Kind {
	case "marks":
		for _, id := range req.IDs {
			dir, ok := agentStore.pathOf(id)
			if !ok || !s.inRoots(dir) {
				continue
			}
			for _, files := range scanGroups(dir) {
				items = append(items, filepath.Dir(files[0]))
			}
			if title == "" {
				title = "Заставки по звуку: " + filepath.Base(dir)
			}
		}
		if len(req.IDs) > 1 {
			title = fmt.Sprintf("Заставки по звуку: %d сериалов", len(req.IDs))
		}
	case "posters":
		if len(req.IDs) == 0 {
			items = s.tilesWithoutPosters()
		}
		for _, id := range req.IDs {
			if p, ok := agentStore.pathOf(id); ok && s.inRoots(p) {
				items = append(items, p)
			}
		}
		title = fmt.Sprintf("Постеры и описания: %d плиток", len(items))
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неизвестная работа"})
		return
	}
	if len(items) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "делать нечего: нет подходящих сезонов или плиток"})
		return
	}
	sort.Slice(items, func(a, b int) bool { return naturalLess(items[a], items[b]) })
	job := s.quiet.add(quietJob{Kind: req.Kind, Title: title, Items: items, Note: "ждёт тихих часов"})
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

// DELETE /api/admin/quiet/jobs/{id}
func (s *server) adminQuietDrop(w http.ResponseWriter, r *http.Request) {
	if !s.quiet.drop(r.PathValue("id")) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "такой работы нет"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
