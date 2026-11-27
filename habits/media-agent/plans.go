package main

// Планы: что хочется посмотреть или скачать, и что сделать с уже лежащим
// («улучшить качество», «докачать сезон»). План живёт сам по себе, файла на
// диске у него нет — всё хранит агент: plans.json и plans/<id>/ с постерами.
//
// Сведения ищутся тем же поиском, что у фильмов медиатеки (fetchInfo), в
// фоне: добавление отвечает сразу, плашка показывает «ищу». Правленые руками
// поля повторный поиск не трогает (mergeRefetch).

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	planPosterW  = 800
	planPosterH  = 1200
	planLinesMax = 200 // строк за раз
)

type plan struct {
	ID       string    `json:"id"`
	Query    string    `json:"query"`
	Kind     string    `json:"kind"` // раздел: film | series | anime
	Type     string    `json:"type"` // как искать: film | series
	Note     string    `json:"note,omitempty"`
	Tile     string    `json:"tile,omitempty"` // план про плитку медиатеки
	Info     *itemInfo `json:"info,omitempty"`
	Poster   string    `json:"poster,omitempty"` // главный постер в plans/<id>/
	State    string    `json:"state,omitempty"`  // "" | searching | notfound
	Archived int64     `json:"archived,omitempty"`
	Created  int64     `json:"created"`
	Updated  int64     `json:"updated"`
}

type planStore struct {
	mu    sync.Mutex
	path  string
	Plans []plan `json:"plans"`
}

func newPlanStore(cacheDir string) *planStore {
	p := &planStore{path: filepath.Join(cacheDir, "plans.json")}
	if data, err := os.ReadFile(p.path); err == nil {
		_ = json.Unmarshal(data, p)
	}
	// поиск, прерванный перезапуском, не висит «ищу» вечно
	for i := range p.Plans {
		if p.Plans[i].State == "searching" {
			p.Plans[i].State = ""
		}
	}
	return p
}

func (p *planStore) saveLocked() {
	if data, err := json.Marshal(p); err == nil {
		_ = writeAtomic(p.path, data)
	}
}

func (p *planStore) all() []plan {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]plan(nil), p.Plans...)
}

func (p *planStore) find(id string) (plan, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, x := range p.Plans {
		if x.ID == id {
			return x, true
		}
	}
	return plan{}, false
}

func (p *planStore) add(pl plan) plan {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	pl.ID = fmt.Sprintf("p-%d", now.UnixNano()/1e6)
	for p.has(pl.ID) {
		now = now.Add(time.Millisecond)
		pl.ID = fmt.Sprintf("p-%d", now.UnixNano()/1e6)
	}
	pl.Created, pl.Updated = now.Unix(), now.Unix()
	p.Plans = append(p.Plans, pl)
	p.saveLocked()
	return pl
}

func (p *planStore) has(id string) bool {
	for _, x := range p.Plans {
		if x.ID == id {
			return true
		}
	}
	return false
}

func (p *planStore) update(id string, fn func(*plan)) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.Plans {
		if p.Plans[i].ID == id {
			fn(&p.Plans[i])
			p.Plans[i].Updated = time.Now().Unix()
			p.saveLocked()
			return true
		}
	}
	return false
}

func (p *planStore) remove(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.Plans {
		if p.Plans[i].ID == id {
			p.Plans = append(p.Plans[:i], p.Plans[i+1:]...)
			p.saveLocked()
			return true
		}
	}
	return false
}

// --- постеры плана ---

var findPosterFn = findPoster

func validPlanID(id string) bool {
	if !strings.HasPrefix(id, "p-") || len(id) < 3 {
		return false
	}
	for _, c := range id[2:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func validPlanPoster(n string) bool {
	return n == filepath.Base(n) && strings.HasPrefix(n, "poster-") && strings.HasSuffix(n, ".jpg")
}

func (s *server) planDir(id string) string { return filepath.Join(s.cfg.cache, "plans", id) }

func (s *server) planPosters(id string) []string {
	list, err := os.ReadDir(s.planDir(id))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range list {
		if !e.IsDir() && validPlanPoster(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// addPlanPoster кладёт картинку src в постеры плана; первый становится главным.
func (s *server) addPlanPoster(id, src string) (string, error) {
	dir := s.planDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("poster-%d.jpg", time.Now().UnixNano()/1e6)
	for exists(filepath.Join(dir, name)) {
		time.Sleep(time.Millisecond)
		name = fmt.Sprintf("poster-%d.jpg", time.Now().UnixNano()/1e6)
	}
	if err := renderJPEG(src, filepath.Join(dir, name), planPosterW, planPosterH, 86); err != nil {
		return "", errors.New("картинка не читается (годятся JPG и PNG): " + err.Error())
	}
	if !s.plans.update(id, func(p *plan) {
		if p.Poster == "" || !exists(filepath.Join(dir, p.Poster)) {
			p.Poster = name
		}
	}) {
		_ = os.RemoveAll(dir) // план удалили, пока клали картинку
		return "", errors.New("плана уже нет")
	}
	return name, nil
}

// addPlanPosterBytes — то же из байтов (найденный в интернете постер).
func (s *server) addPlanPosterBytes(id string, data []byte, ext string) (string, error) {
	tmp, err := os.CreateTemp(s.cfg.cache, ".planposter-*"+ext)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(data)
	tmp.Close()
	if err != nil {
		return "", err
	}
	return s.addPlanPoster(id, tmp.Name())
}

type planView struct {
	plan
	PosterURL string        `json:"poster_url,omitempty"`
	Posters   []materialRef `json:"posters"`
}

func (s *server) planOut(pl plan) planView {
	v := planView{plan: pl, Posters: []materialRef{}}
	u := s.cfg.base + "/planposter/" + pl.ID + "?k=" + s.sign(pl.ID) + "&n="
	for _, n := range s.planPosters(pl.ID) {
		v.Posters = append(v.Posters, materialRef{Name: n, URL: u + n})
		if n == pl.Poster {
			v.PosterURL = u + n
		}
	}
	return v
}

// GET /planposter/{id}?k=&n= — постер плана.
func (s *server) planPoster(w http.ResponseWriter, r *http.Request) {
	id, n := r.PathValue("id"), r.URL.Query().Get("n")
	if !validPlanID(id) || !s.validKey(id, r.URL.Query().Get("k")) || !validPlanPoster(n) {
		http.Error(w, "нет доступа", http.StatusForbidden)
		return
	}
	f := filepath.Join(s.planDir(id), n)
	if !fileReady(f) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, f)
}

// --- поиск ---

func otherType(t string) string {
	if t == "film" {
		return "series"
	}
	return "film"
}

func defaultType(kind string) string {
	if kind == "film" {
		return "film"
	}
	return "series"
}

// searchPlan ищет сведения и постер плана (в фоне). as — искать под другим
// названием; reset — забыть ручные правки.
func (s *server) searchPlan(id, as string, reset bool) {
	pl, ok := s.plans.find(id)
	if !ok {
		return
	}
	query := pl.Query
	if as != "" {
		query = as
	}
	title, year := planTitle(query)
	var found *itemInfo
	typ := pl.Type
	infoSem <- struct{}{}
	for _, t := range []string{pl.Type, otherType(pl.Type)} {
		if inf, err := fetchInfoFn(title, year, t); err == nil {
			found, typ = inf, t
			break
		}
	}
	<-infoSem
	// постер — если своего ещё нет: найденным названием, иначе строкой
	var poster []byte
	var ext string
	if len(s.planPosters(id)) == 0 {
		names := []string{title}
		if found != nil && found.Title != "" && found.Title != title {
			names = append([]string{found.Title}, names...)
		}
		y := year
		if found != nil && found.Year != 0 {
			y = found.Year
		}
		posterSem <- struct{}{}
		for _, n := range names {
			if data, e, err := findPosterFn(n, y, typ, false); err == nil {
				poster, ext = data, e
				break
			}
		}
		<-posterSem
	}
	// постер — до того, как снять «ищу»: страница перестаёт переспрашивать,
	// как только поиск кончился, и плашка осталась бы без картинки
	if poster != nil {
		if _, err := s.addPlanPosterBytes(id, poster, ext); err != nil {
			return // плана уже нет
		}
	}
	s.plans.update(id, func(p *plan) {
		if found == nil {
			p.State = "notfound"
			if p.Info == nil {
				p.Info = &itemInfo{Title: title, Year: year}
			}
			return
		}
		p.State, p.Type = "", typ
		if reset {
			p.Info = found
		} else {
			p.Info = mergeRefetch(p.Info, found)
		}
	})
}

// planTitle — название и год из строки плана. В отличие от имён файлов,
// год плана бывает и будущим: «Дюна 3 (2028)».
func planTitle(q string) (string, int) {
	title, year := cleanTitle(q, false)
	if year == 0 {
		if m := reParenYear.FindStringSubmatch(q); m != nil {
			var y int
			fmt.Sscanf(m[1], "%d", &y)
			if y >= 1900 && y <= 2100 {
				year = y
			}
		}
	}
	return title, year
}

// --- админка ---

var planKinds = map[string]bool{"film": true, "series": true, "anime": true}

// GET /api/admin/plans?archived=0|1
func (s *server) adminPlans(w http.ResponseWriter, r *http.Request) {
	arch := r.URL.Query().Get("archived") == "1"
	out := []planView{}
	list := s.plans.all()
	sort.SliceStable(list, func(i, j int) bool { return list[i].Created > list[j].Created })
	for _, p := range list {
		if (p.Archived != 0) == arch {
			out = append(out, s.planOut(p))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": out})
}

// POST /api/admin/plans — {lines, kind} | {plan:{query,kind,type,note,info}} | {tile, note}
func (s *server) adminPlanAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Lines []string `json:"lines"`
		Kind  string   `json:"kind"`
		Plan  *struct {
			Query string    `json:"query"`
			Kind  string    `json:"kind"`
			Type  string    `json:"type"`
			Note  string    `json:"note"`
			Info  *itemInfo `json:"info"`
		} `json:"plan"`
		Tile string `json:"tile"`
		Note string `json:"note"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный запрос"})
		return
	}
	var made []planView
	switch {
	case req.Tile != "":
		pl, ok := s.planForTile(w, req.Tile, req.Note)
		if !ok {
			return
		}
		made = append(made, s.planOut(pl))
	case req.Plan != nil:
		q := strings.TrimSpace(req.Plan.Query)
		if q == "" && req.Plan.Info != nil {
			q = strings.TrimSpace(req.Plan.Info.Title)
		}
		if q == "" || !planKinds[req.Plan.Kind] {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужны название и раздел"})
			return
		}
		typ := req.Plan.Type
		if typ != "film" && typ != "series" {
			typ = defaultType(req.Plan.Kind)
		}
		pl := plan{Query: q, Kind: req.Plan.Kind, Type: typ, Note: strings.TrimSpace(req.Plan.Note), State: "searching"}
		if inf := req.Plan.Info; inf != nil {
			inf.Genres, inf.Actors = cleanList(inf.Genres), cleanList(inf.Actors)
			markEdited(nil, inf)
			if len(inf.EditedFields) > 0 {
				pl.Info = inf
			}
		}
		pl = s.plans.add(pl)
		go s.searchPlan(pl.ID, "", false)
		made = append(made, s.planOut(pl))
	default:
		if !planKinds[req.Kind] {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "раздел: film, series или anime"})
			return
		}
		seen := map[string]bool{}
		for _, p := range s.plans.all() {
			if p.Archived == 0 {
				seen[p.Kind+"|"+strings.ToLower(p.Query)] = true
			}
		}
		var lines []string
		for _, l := range req.Lines {
			l = strings.TrimSpace(l)
			key := req.Kind + "|" + strings.ToLower(l)
			if l != "" && !seen[key] {
				seen[key] = true
				lines = append(lines, l)
			}
		}
		if !hasText(req.Lines) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "список пуст"})
			return
		}
		if len(lines) > planLinesMax {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("не больше %d строк за раз", planLinesMax)})
			return
		}
		for _, l := range lines {
			pl := s.plans.add(plan{Query: l, Kind: req.Kind, Type: defaultType(req.Kind), State: "searching"})
			go s.searchPlan(pl.ID, "", false)
			made = append(made, s.planOut(pl))
		}
	}
	if made == nil {
		made = []planView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": made})
}

func hasText(lines []string) bool {
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			return true
		}
	}
	return false
}

// planForTile — план про фильм или сериал медиатеки: описание и постер
// копируются сейчас, дальше план живёт сам.
func (s *server) planForTile(w http.ResponseWriter, id, note string) (plan, bool) {
	p, ok := s.materialTarget(w, id)
	if !ok {
		return plan{}, false
	}
	st, err := os.Stat(p)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "не найдено"})
		return plan{}, false
	}
	kind, typ := "film", "film"
	if st.IsDir() {
		kind, typ = "series", "series"
	}
	if s.libraryOf(p).Kind == "anime" {
		kind = "anime"
	}
	title, _ := cleanTitle(filepath.Base(p), !st.IsDir())
	pl := plan{Query: title, Kind: kind, Type: typ, Note: strings.TrimSpace(note), Tile: id}
	if inf, ok := readInfo(p); ok {
		cp := *inf
		pl.Info = &cp
		if cp.Title != "" {
			pl.Query = cp.Title
		}
	} else {
		pl.State = "searching"
	}
	pl = s.plans.add(pl)
	if src := s.existingPoster(p, id); src != "" {
		_, _ = s.addPlanPoster(pl.ID, src)
	}
	if pl.State == "searching" {
		go s.searchPlan(pl.ID, "", false)
	}
	pl, _ = s.plans.find(pl.ID)
	return pl, true
}

// PUT /api/admin/plans/{id}
func (s *server) adminPlanEdit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Note     *string   `json:"note"`
		Kind     *string   `json:"kind"`
		Type     *string   `json:"type"`
		Poster   *string   `json:"poster"`
		Archived *bool     `json:"archived"`
		Info     *itemInfo `json:"info"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный запрос"})
		return
	}
	if req.Kind != nil && !planKinds[*req.Kind] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "раздел: film, series или anime"})
		return
	}
	if req.Type != nil && *req.Type != "film" && *req.Type != "series" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "тип: film или series"})
		return
	}
	if req.Poster != nil && (!validPlanPoster(*req.Poster) || !exists(filepath.Join(s.planDir(id), *req.Poster))) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нет такого постера"})
		return
	}
	ok := s.plans.update(id, func(p *plan) {
		if req.Note != nil {
			p.Note = strings.TrimSpace(*req.Note)
		}
		if req.Kind != nil {
			p.Kind = *req.Kind
		}
		if req.Type != nil {
			p.Type = *req.Type
		}
		if req.Poster != nil {
			p.Poster = *req.Poster
		}
		if req.Archived != nil {
			if *req.Archived {
				p.Archived = time.Now().Unix()
			} else {
				p.Archived = 0
			}
		}
		if inf := req.Info; inf != nil {
			inf.Genres, inf.Actors = cleanList(inf.Genres), cleanList(inf.Actors)
			markEdited(p.Info, inf)
			if p.Info != nil && len(inf.Cast) == 0 {
				inf.QID, inf.Cast, inf.CastAt = p.Info.QID, p.Info.Cast, p.Info.CastAt
			}
			p.Info = inf
		}
	})
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "плана нет"})
		return
	}
	pl, _ := s.plans.find(id)
	writeJSON(w, http.StatusOK, map[string]any{"plan": s.planOut(pl)})
}

// POST /api/admin/plans/{id}/refetch {as?, reset?}
func (s *server) adminPlanRefetch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		As    string `json:"as"`
		Reset bool   `json:"reset"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req)
	if !s.plans.update(id, func(p *plan) { p.State = "searching" }) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "плана нет"})
		return
	}
	go s.searchPlan(id, strings.TrimSpace(req.As), req.Reset)
	pl, _ := s.plans.find(id)
	writeJSON(w, http.StatusOK, map[string]any{"plan": s.planOut(pl)})
}

// DELETE /api/admin/plans/{id}
func (s *server) adminPlanDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validPlanID(id) || !s.plans.remove(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "плана нет"})
		return
	}
	_ = os.RemoveAll(s.planDir(id))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// DELETE /api/admin/plans/{id}/poster?n=
func (s *server) adminPlanPosterDelete(w http.ResponseWriter, r *http.Request) {
	id, n := r.PathValue("id"), r.URL.Query().Get("n")
	if _, ok := s.plans.find(id); !ok || !validPlanPoster(n) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нет такого постера"})
		return
	}
	_ = os.Remove(filepath.Join(s.planDir(id), n))
	rest := s.planPosters(id)
	s.plans.update(id, func(p *plan) {
		if p.Poster == n {
			p.Poster = ""
			if len(rest) > 0 {
				p.Poster = rest[0]
			}
		}
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// adminPlanImage — свой постер плана (target=plan в /api/admin/image).
func (s *server) adminPlanImage(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if _, ok := s.plans.find(id); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "плана нет"})
		return
	}
	src, cleanup, err := s.imageSource(r)
	defer cleanup()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	name, err := s.addPlanPoster(id, src)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}
