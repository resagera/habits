package main

// «Инфо» о фильме или сериале: год, жанры, рейтинги, описание, актёры.
//
// Берётся из открытых источников без ключей — тех же, что для постеров:
//   - подсказки IMDb: id фильма, год, двое главных актёров (страница фильма
//     на IMDb ботам не отдаётся — 202 и пустое тело);
//   - Wikidata по id из IMDb: жанры, актёры, дата, оценки критиков (Rotten
//     Tomatoes, Metacritic — у кого есть);
//   - Википедия по ссылке из Wikidata: описание (русское, иначе английское);
//   - TVmaze для сериалов: рейтинг зрителей, жанры и описание, если в
//     Википедии пусто.
// Кириллическое название IMDb не ищет — тогда страница находится поиском
// Википедии, как у постеров.
//
// Кэш — рядом с постером: «Fauda.info.json» рядом с «Fauda.jpg». Переезжает
// и удаляется вместе с сериалом. Правка в админке помечает запись как ручную:
// автоматический поиск её больше не перезаписывает без явной команды.

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type infoRating struct {
	Source string `json:"source"`
	Value  string `json:"value"`
}

type itemInfo struct {
	Title       string       `json:"title,omitempty"`
	Year        int          `json:"year,omitempty"`
	Genres      []string     `json:"genres,omitempty"`
	Ratings     []infoRating `json:"ratings,omitempty"`
	Description string       `json:"description,omitempty"`
	Actors      []string     `json:"actors,omitempty"`
	IMDb        string       `json:"imdb,omitempty"`
	Wiki        string       `json:"wiki,omitempty"` // страница Википедии, откуда описание
	Fetched     int64        `json:"fetched,omitempty"`
	Edited      bool         `json:"edited,omitempty"` // правили руками
}

const infoSuffix = ".info.json"

func infoPath(p string) string {
	st, err := os.Stat(p)
	return posterBase(p, err == nil && st.IsDir()) + infoSuffix
}

func readInfo(p string) (*itemInfo, bool) {
	data, err := os.ReadFile(infoPath(p))
	if err != nil {
		return nil, false
	}
	var inf itemInfo
	if json.Unmarshal(data, &inf) != nil {
		return nil, false
	}
	return &inf, true
}

func writeInfo(p string, inf *itemInfo) error {
	data, err := json.MarshalIndent(inf, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(infoPath(p), data)
}

// sidecars — файлы-спутники объекта: постеры и описание с тем же именем. Они
// переименовываются и уходят в корзину вместе с ним.
func sidecars(p string, isDir bool) []string {
	base := posterBase(p, isDir)
	var out []string
	for _, ext := range append(append([]string{}, posterExts...), infoSuffix) {
		if f := base + ext; f != p && exists(f) {
			out = append(out, f)
		}
	}
	return out
}

var (
	infoLocks sync.Map
	infoSem   = make(chan struct{}, 2)
)

// GET /api/info?id= — сведения о плитке. Нет в кэше — ищем сейчас (как
// постер при первом показе); не нашлось — помним сутки.
func (s *server) info(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	p, ok := agentStore.pathOf(id)
	if !ok || !s.inRoots(p) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "не найдено"})
		return
	}
	if inf, ok := readInfo(p); ok {
		writeJSON(w, http.StatusOK, map[string]any{"info": inf})
		return
	}
	if s.libraryOf(p).Kind != "video" {
		writeJSON(w, http.StatusOK, map[string]any{"info": nil})
		return
	}
	lock, _ := infoLocks.LoadOrStore(id, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	if inf, ok := readInfo(p); ok {
		writeJSON(w, http.StatusOK, map[string]any{"info": inf})
		return
	}
	miss := filepath.Join(s.cfg.cache, "posters", id+".info.miss")
	if st, err := os.Stat(miss); err == nil && time.Since(st.ModTime()) < 24*time.Hour {
		writeJSON(w, http.StatusOK, map[string]any{"info": nil})
		return
	}
	inf, err := s.lookupInfo(p)
	if err != nil {
		log.Printf("инфо для %s не нашлось: %v", filepath.Base(p), err)
		_ = os.MkdirAll(filepath.Dir(miss), 0o700)
		_ = os.WriteFile(miss, nil, 0o600)
		writeJSON(w, http.StatusOK, map[string]any{"info": nil})
		return
	}
	if err := writeInfo(p, inf); err != nil {
		log.Printf("инфо для %s не сохранилось: %v", filepath.Base(p), err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"info": inf})
}

// lookupInfo ищет сведения по имени файла или папки.
func (s *server) lookupInfo(p string) (*itemInfo, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	title, year := cleanTitle(filepath.Base(p), !st.IsDir())
	kind := "film"
	if st.IsDir() {
		// папка с сезонами или несколькими сериями — сериал
		sub, vids, _ := dirContents(p)
		if hasSeasonDirs(sub) || len(vids) > 1 {
			kind = "series"
		}
	}
	infoSem <- struct{}{}
	defer func() { <-infoSem }()
	return fetchInfo(title, year, kind)
}

func fetchInfo(title string, year int, kind string) (*itemInfo, error) {
	if title == "" {
		return nil, errors.New("пустое название")
	}
	inf := &itemInfo{Title: title, Year: year, Fetched: time.Now().Unix()}
	qid, wikiLang, wikiTitle := "", "", ""
	if !hasCyrillic(title) {
		if hit, err := imdbFind(title, year, kind); err == nil {
			inf.IMDb, inf.Title = hit.ID, hit.Title
			if hit.Year != 0 {
				inf.Year = hit.Year
			}
			if hit.Stars != "" {
				inf.Actors = splitList(hit.Stars)
			}
			qid, _ = wikidataByIMDb(hit.ID)
		}
	}
	if qid == "" {
		// кириллица или IMDb молчит — страница Википедии поиском, как у постеров
		for _, lang := range []string{"ru", "en"} {
			if t, err := wikiSearchTitle(lang, title, year, kind); err == nil {
				wikiLang, wikiTitle = lang, t
				qid, _ = wikidataByWiki(lang, t)
				break
			}
		}
	}
	if qid != "" {
		if e, err := wikidataEntity(qid); err == nil {
			e.fill(inf)
			if wikiTitle == "" {
				wikiLang, wikiTitle = e.wikiPage()
			}
		}
	}
	if wikiTitle != "" {
		if extract, err := wikiSummary(wikiLang, wikiTitle); err == nil {
			inf.Description = extract
			inf.Wiki = "https://" + wikiLang + ".wikipedia.org/wiki/" + url.PathEscape(strings.ReplaceAll(wikiTitle, " ", "_"))
		}
	}
	if kind == "series" {
		tvmazeFill(inf, title)
	}
	if inf.Description == "" && len(inf.Genres) == 0 && len(inf.Ratings) == 0 && inf.IMDb == "" {
		return nil, errors.New("ни один источник не ответил")
	}
	return inf, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// --- Wikidata ---

const wikidataAPI = "https://www.wikidata.org/w/api.php?format=json&"

func wikidataByIMDb(id string) (string, error) {
	var res struct {
		Query struct {
			Search []struct {
				Title string `json:"title"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := getJSON(wikidataAPI+"action=query&list=search&srsearch="+url.QueryEscape("haswbstatement:P345="+id), &res); err != nil {
		return "", err
	}
	if len(res.Query.Search) == 0 {
		return "", errors.New("Wikidata: нет записи с этим IMDb")
	}
	return res.Query.Search[0].Title, nil
}

func wikidataByWiki(lang, title string) (string, error) {
	var res struct {
		Query struct {
			Pages map[string]struct {
				PageProps struct {
					Item string `json:"wikibase_item"`
				} `json:"pageprops"`
			} `json:"pages"`
		} `json:"query"`
	}
	u := "https://" + lang + ".wikipedia.org/w/api.php?action=query&format=json&prop=pageprops&titles=" + url.QueryEscape(title)
	if err := getJSON(u, &res); err != nil {
		return "", err
	}
	for _, p := range res.Query.Pages {
		if p.PageProps.Item != "" {
			return p.PageProps.Item, nil
		}
	}
	return "", errors.New("у страницы нет записи в Wikidata")
}

type wdSnak struct {
	DataValue struct {
		Value json.RawMessage `json:"value"`
	} `json:"datavalue"`
}

type wdClaim struct {
	MainSnak   wdSnak              `json:"mainsnak"`
	Qualifiers map[string][]wdSnak `json:"qualifiers"`
}

type wdEntity struct {
	Claims    map[string][]wdClaim `json:"claims"`
	Sitelinks map[string]struct {
		Title string `json:"title"`
	} `json:"sitelinks"`
	labels map[string]string
}

func wikidataEntity(qid string) (*wdEntity, error) {
	var res struct {
		Entities map[string]*wdEntity `json:"entities"`
	}
	u := wikidataAPI + "action=wbgetentities&props=claims|sitelinks&sitefilter=ruwiki|enwiki&ids=" + url.QueryEscape(qid)
	if err := getJSON(u, &res); err != nil {
		return nil, err
	}
	e := res.Entities[qid]
	if e == nil {
		return nil, errors.New("Wikidata: пустой ответ")
	}
	// подписи жанров, актёров и критиков — одним запросом
	var ids []string
	for _, prop := range []string{"P136", "P161"} {
		for i, c := range e.Claims[prop] {
			if prop == "P161" && i >= 12 {
				break
			}
			if id := snakID(c.MainSnak); id != "" {
				ids = append(ids, id)
			}
		}
	}
	for _, c := range e.Claims["P444"] {
		for _, q := range c.Qualifiers["P447"] {
			if id := snakID(q); id != "" {
				ids = append(ids, id)
			}
		}
	}
	e.labels = wikidataLabels(ids)
	return e, nil
}

func snakID(s wdSnak) string {
	var v struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(s.DataValue.Value, &v)
	return v.ID
}

func wikidataLabels(ids []string) map[string]string {
	out := map[string]string{}
	for len(ids) > 0 {
		n := min(len(ids), 50)
		chunk := ids[:n]
		ids = ids[n:]
		var res struct {
			Entities map[string]struct {
				Labels map[string]struct {
					Value string `json:"value"`
				} `json:"labels"`
			} `json:"entities"`
		}
		u := wikidataAPI + "action=wbgetentities&props=labels&languages=ru|en&ids=" + url.QueryEscape(strings.Join(chunk, "|"))
		if getJSON(u, &res) != nil {
			continue
		}
		for id, e := range res.Entities {
			if l, ok := e.Labels["ru"]; ok {
				out[id] = l.Value
			} else if l, ok := e.Labels["en"]; ok {
				out[id] = l.Value
			}
		}
	}
	return out
}

func (e *wdEntity) fill(inf *itemInfo) {
	label := func(id string) string { return e.labels[id] }
	var genres []string
	for _, c := range e.Claims["P136"] {
		if l := label(snakID(c.MainSnak)); l != "" {
			genres = append(genres, l)
		}
	}
	if len(genres) > 0 {
		inf.Genres = genres
	}
	var actors []string
	for i, c := range e.Claims["P161"] {
		if i >= 12 {
			break
		}
		if l := label(snakID(c.MainSnak)); l != "" {
			actors = append(actors, l)
		}
	}
	if len(actors) > 0 {
		inf.Actors = actors
	}
	if inf.Year == 0 {
		for _, prop := range []string{"P577", "P580"} {
			for _, c := range e.Claims[prop] {
				var v struct {
					Time string `json:"time"`
				}
				_ = json.Unmarshal(c.MainSnak.DataValue.Value, &v)
				if len(v.Time) >= 5 {
					if y, err := strconv.Atoi(v.Time[1:5]); err == nil && (inf.Year == 0 || y < inf.Year) {
						inf.Year = y
					}
				}
			}
		}
	}
	// оценки критиков: «Rotten Tomatoes: 75% · 6.6/10», «Metacritic: 51/100»
	byWho := map[string][]string{}
	var order []string
	for _, c := range e.Claims["P444"] {
		var v string
		if json.Unmarshal(c.MainSnak.DataValue.Value, &v) != nil || v == "" {
			continue
		}
		who := "Критики"
		for _, q := range c.Qualifiers["P447"] {
			if l := label(snakID(q)); l != "" {
				who = l
			}
		}
		if _, seen := byWho[who]; !seen {
			order = append(order, who)
		}
		if !contains(byWho[who], v) {
			byWho[who] = append(byWho[who], v)
		}
	}
	for _, who := range order {
		inf.Ratings = append(inf.Ratings, infoRating{Source: who, Value: strings.Join(byWho[who], " · ")})
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (e *wdEntity) wikiPage() (lang, title string) {
	if l, ok := e.Sitelinks["ruwiki"]; ok {
		return "ru", l.Title
	}
	if l, ok := e.Sitelinks["enwiki"]; ok {
		return "en", l.Title
	}
	return "", ""
}

// --- Википедия ---

func wikiSummary(lang, title string) (string, error) {
	var res struct {
		Extract string `json:"extract"`
	}
	u := "https://" + lang + ".wikipedia.org/api/rest_v1/page/summary/" + url.PathEscape(strings.ReplaceAll(title, " ", "_"))
	if err := getJSON(u, &res); err != nil {
		return "", err
	}
	if strings.TrimSpace(res.Extract) == "" {
		return "", errors.New("пустое описание")
	}
	return strings.TrimSpace(res.Extract), nil
}

// wikiSearchTitle — страница Википедии о фильме или сериале; название страницы
// обязано совпасть с искомым (урок «Чернобыля» у постеров).
func wikiSearchTitle(lang, title string, year int, kind string) (string, error) {
	hint := map[string]map[string]string{
		"ru": {"film": "фильм", "series": "сериал"},
		"en": {"film": "film", "series": "TV series"},
	}[lang][kind]
	query := strings.TrimSpace(title + " " + hint)
	if year != 0 {
		query += fmt.Sprintf(" %d", year)
	}
	var res struct {
		Query struct {
			Search []struct {
				Title string `json:"title"`
			} `json:"search"`
		} `json:"query"`
	}
	u := "https://" + lang + ".wikipedia.org/w/api.php?action=query&format=json&list=search&srlimit=5&srsearch=" + url.QueryEscape(query)
	if err := getJSON(u, &res); err != nil {
		return "", err
	}
	want := strings.ToLower(title)
	best, bestScore := "", 0
	for _, p := range res.Query.Search {
		t := strings.ToLower(p.Title)
		bare := strings.TrimSpace(reParens.ReplaceAllString(t, ""))
		score := 0
		switch {
		case bare == want:
			score = 3
		case strings.HasPrefix(bare, want):
			score = 1
		default:
			continue
		}
		if hint != "" && strings.Contains(t, strings.ToLower(hint)) {
			score++
		}
		if score > bestScore {
			best, bestScore = p.Title, score
		}
	}
	if best == "" {
		return "", errors.New("Википедия: подходящей страницы нет")
	}
	return best, nil
}

// --- TVmaze (сериалы) ---

var reTags2 = regexp.MustCompile(`<[^>]+>`)

func tvmazeFill(inf *itemInfo, title string) {
	var show struct {
		Genres  []string `json:"genres"`
		Summary string   `json:"summary"`
		Rating  struct {
			Average float64 `json:"average"`
		} `json:"rating"`
		Premiered string `json:"premiered"`
	}
	u := "https://api.tvmaze.com/singlesearch/shows?q=" + url.QueryEscape(title)
	if inf.IMDb != "" {
		u = "https://api.tvmaze.com/lookup/shows?imdb=" + url.QueryEscape(inf.IMDb)
	}
	if getJSON(u, &show) != nil {
		return
	}
	if show.Rating.Average > 0 {
		inf.Ratings = append(inf.Ratings, infoRating{Source: "TVmaze", Value: fmt.Sprintf("%.1f/10", show.Rating.Average)})
	}
	if len(inf.Genres) == 0 {
		inf.Genres = show.Genres
	}
	if inf.Description == "" && show.Summary != "" {
		inf.Description = strings.TrimSpace(html.UnescapeString(reTags2.ReplaceAllString(show.Summary, "")))
	}
	if inf.Year == 0 && len(show.Premiered) >= 4 {
		inf.Year, _ = strconv.Atoi(show.Premiered[:4])
	}
}

// --- админка ---

// POST /api/admin/info {id, fetch?: true, info?: {...}} — найти в интернете
// заново или сохранить поправленное руками.
func (s *server) adminInfo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID    string    `json:"id"`
		Fetch bool      `json:"fetch"`
		Title string    `json:"title"` // поискать под другим названием
		Year  int       `json:"year"`
		Info  *itemInfo `json:"info"`
		Clear bool      `json:"clear"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный запрос"})
		return
	}
	p, ok := s.adminPath(w, req.ID)
	if !ok {
		return
	}
	switch {
	case req.Clear:
		_ = os.Remove(infoPath(p))
		_ = os.Remove(filepath.Join(s.cfg.cache, "posters", req.ID+".info.miss"))
		writeJSON(w, http.StatusOK, map[string]any{"info": nil})
	case req.Fetch:
		var inf *itemInfo
		var err error
		if req.Title != "" {
			kind := "film"
			if st, e := os.Stat(p); e == nil && st.IsDir() {
				kind = "series"
			}
			infoSem <- struct{}{}
			inf, err = fetchInfo(req.Title, req.Year, kind)
			<-infoSem
		} else {
			inf, err = s.lookupInfo(p)
		}
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "не нашлось: " + err.Error()})
			return
		}
		_ = writeInfo(p, inf)
		writeJSON(w, http.StatusOK, map[string]any{"info": inf})
	case req.Info != nil:
		inf := req.Info
		inf.Edited = true
		inf.Genres = cleanList(inf.Genres)
		inf.Actors = cleanList(inf.Actors)
		if err := writeInfo(p, inf); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"info": inf})
	default:
		inf, _ := readInfo(p)
		writeJSON(w, http.StatusOK, map[string]any{"info": inf})
	}
}

func cleanList(list []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, x := range list {
		x = strings.TrimSpace(x)
		if x != "" && !seen[strings.ToLower(x)] {
			seen[strings.ToLower(x)] = true
			out = append(out, x)
		}
	}
	return out
}
