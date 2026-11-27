package main

// Фильмография актёра из интернета: все фильмы и сериалы, где человек есть в
// составе (P161) по Wikidata — один SPARQL-запрос, без ключей. Ответ
// хранится месяц (people/<Q>.films.json): фильмография старых актёров не
// меняется, а новые роли появятся при следующем обновлении.
//
// Каждая строка отмечается: есть ли это у нас (по Q-id описания плитки) или
// в планах.

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	filmsMax     = 300
	filmsTTL     = 30 * 24 * time.Hour
	filmsMissTTL = time.Hour // неудачу не повторяем каждый раз
)

type filmRow struct {
	QID   string `json:"qid"`
	Title string `json:"title"`
	Year  int    `json:"year,omitempty"`
	Kind  string `json:"kind"`           // film | series
	Have  string `json:"have,omitempty"` // id плитки медиатеки
	Plan  string `json:"plan,omitempty"` // id плана
}

const sparqlFilmsQuery = `SELECT ?w ?wLabel ?t (MIN(YEAR(?d)) AS ?y) WHERE {
  ?w wdt:P161 wd:%Q% .
  VALUES ?t { wd:Q11424 wd:Q5398426 }
  ?w wdt:P31/wdt:P279* ?t .
  OPTIONAL { ?w wdt:P577 ?d }
  SERVICE wikibase:label { bd:serviceParam wikibase:language "ru,en". }
} GROUP BY ?w ?wLabel ?t LIMIT 400`

var sparqlHTTP = &http.Client{Timeout: 8 * time.Second}

// sparqlFn — запрос к Wikidata; в тестах подменяется.
var sparqlFn = sparqlQuery

func sparqlQuery(q string) ([]byte, error) {
	req, _ := http.NewRequest(http.MethodGet, "https://query.wikidata.org/sparql?query="+url.QueryEscape(q), nil)
	req.Header.Set("Accept", "application/sparql-results+json")
	req.Header.Set("User-Agent", "habits-media-agent/1.0")
	resp, err := sparqlHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("Wikidata SPARQL: " + resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// parseFilmography — строки ответа: одно произведение один раз (сериал
// главнее фильма), без безымянных, по году вниз, без года — в конце.
func parseFilmography(body []byte) []filmRow {
	var res struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if json.Unmarshal(body, &res) != nil {
		return nil
	}
	byQ := map[string]*filmRow{}
	var order []string
	for _, b := range res.Results.Bindings {
		qid := strings.TrimPrefix(b["w"].Value, "http://www.wikidata.org/entity/")
		title := b["wLabel"].Value
		if !reQID.MatchString(qid) || title == "" || title == qid {
			continue
		}
		kind := "film"
		if strings.HasSuffix(b["t"].Value, "/Q5398426") {
			kind = "series"
		}
		year, _ := strconv.Atoi(b["y"].Value)
		r := byQ[qid]
		if r == nil {
			r = &filmRow{QID: qid, Title: title, Year: year, Kind: kind}
			byQ[qid] = r
			order = append(order, qid)
		}
		if kind == "series" {
			r.Kind = "series"
		}
		if year != 0 && (r.Year == 0 || year < r.Year) {
			r.Year = year
		}
	}
	out := make([]filmRow, 0, len(order))
	for _, q := range order {
		out = append(out, *byQ[q])
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Year, out[j].Year
		if (a == 0) != (b == 0) {
			return b == 0
		}
		if a != b {
			return a > b
		}
		return out[i].Title < out[j].Title
	})
	if len(out) > filmsMax {
		out = out[:filmsMax]
	}
	return out
}

var filmsLocks sync.Map

// filmography — фильмография из кэша, а нет — из Wikidata. Неудача — пустой
// список (и час без повторов).
func (s *server) filmography(qid string) []filmRow {
	path := filepath.Join(s.cfg.cache, "people", qid+".films.json")
	miss := path + ".miss"
	read := func() ([]filmRow, bool) {
		st, err := os.Stat(path)
		if err != nil || time.Since(st.ModTime()) > filmsTTL {
			return nil, false
		}
		var rows []filmRow
		data, _ := os.ReadFile(path)
		return rows, json.Unmarshal(data, &rows) == nil
	}
	if rows, ok := read(); ok {
		return rows
	}
	lock, _ := filmsLocks.LoadOrStore(qid, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	if rows, ok := read(); ok {
		return rows
	}
	if st, err := os.Stat(miss); err == nil && time.Since(st.ModTime()) < filmsMissTTL {
		return []filmRow{}
	}
	body, err := sparqlFn(strings.ReplaceAll(sparqlFilmsQuery, "%Q%", qid))
	rows := parseFilmography(body)
	if err != nil || rows == nil {
		if err == nil {
			err = errors.New("ответ не разобрался")
		}
		log.Printf("фильмография %s не получена: %v", qid, err)
		_ = os.MkdirAll(filepath.Dir(miss), 0o755)
		_ = os.WriteFile(miss, nil, 0o644)
		return []filmRow{}
	}
	if data, err := json.Marshal(rows); err == nil {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = writeAtomic(path, data)
	}
	return rows
}

// markFilms отмечает, что из фильмографии есть у нас и что в планах.
func (s *server) markFilms(rows []filmRow, r *http.Request) {
	have, plans := map[string]string{}, map[string]string{}
	films, series, anime := s.rawVideoTiles()
	for _, t := range append(append(films, series...), anime...) {
		if p, ok := agentStore.pathOf(t.ID); ok && s.visible(p, r) {
			if inf, ok := readInfo(p); ok && inf.QID != "" {
				have[inf.QID] = t.ID
			}
		}
	}
	for _, pl := range s.plans.all() {
		if pl.Archived == 0 && pl.Info != nil && pl.Info.QID != "" {
			plans[pl.Info.QID] = pl.ID
		}
	}
	for i := range rows {
		rows[i].Have, rows[i].Plan = have[rows[i].QID], plans[rows[i].QID]
	}
}
