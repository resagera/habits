package main

// Карточки фильмографии для расширенного вида: постер и пара фраз о фильме
// по-русски. Источник — статья Википедии, на которую ведёт Q-id (ruwiki, а
// нет её — постер из enwiki и короткое описание Wikidata на русском). Всё
// берётся пачками: 50 Q-id одним запросом к Wikidata и по 20 статей за раз —
// фильмография в 300 строк обходится десятком запросов, а не тремя сотнями.
//
// Карточка лежит в кэше агента месяц (films/<Q>.json), постер скачивается,
// когда его впервые попросят, и дальше отдаётся с нашего сервера
// (films/<Q>.jpg) — как фото актёров.

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	filmCardsMax  = 50  // Q-id в одном запросе страницы
	wikiBatch     = 20  // столько вступлений Википедия отдаёт за раз (exlimit)
	filmAboutMax  = 500 // знаков описания
	filmPosterW   = 300
	filmPosterH   = 450
	filmPosterMis = 7 * 24 * time.Hour
)

type filmCard struct {
	About string `json:"about,omitempty"`
	Img   string `json:"img,omitempty"` // откуда скачать постер
	At    int64  `json:"at,omitempty"`
}

// filmCardsFetchFn — карточки из интернета; в тестах подменяется.
var (
	filmCardsFetchFn     = fetchFilmCards
	filmPosterDownloadFn = download
	filmCardsMu          sync.Mutex
	// Викисклад отвечает 429 на всплеск запросов, поэтому постеры качаются
	// по одному и с паузой
	filmPosterSem  = make(chan struct{}, 1)
	filmPosterGap  = 400 * time.Millisecond
	filmPosterLast time.Time
	// новые карточки сразу докачивают постеры в фоне; в тестах выключено
	filmPosterPrefetch = true
	filmPrefetchWG     sync.WaitGroup
)

func (s *server) filmCardPath(qid string) string {
	return filepath.Join(s.cfg.cache, "films", qid+".json")
}

func (s *server) filmPosterPath(qid string) string {
	return filepath.Join(s.cfg.cache, "films", qid+".jpg")
}

func (s *server) readFilmCard(qid string) (filmCard, bool) {
	var c filmCard
	data, err := os.ReadFile(s.filmCardPath(qid))
	if err != nil || json.Unmarshal(data, &c) != nil || time.Since(time.Unix(c.At, 0)) > filmsTTL {
		return filmCard{}, false
	}
	return c, true
}

// filmCards — карточки из кэша, недостающие — из интернета. Не нашлось —
// пустая карточка, и она тоже запоминается; сбой сети — не запоминается.
func (s *server) filmCards(qids []string) map[string]filmCard {
	out := map[string]filmCard{}
	missing := func() []string {
		var m []string
		for _, q := range qids {
			if _, ok := out[q]; ok {
				continue
			}
			if c, ok := s.readFilmCard(q); ok {
				out[q] = c
			} else {
				m = append(m, q)
			}
		}
		return m
	}
	if len(missing()) == 0 {
		return out
	}
	filmCardsMu.Lock()
	defer filmCardsMu.Unlock()
	need := missing() // пока ждали, мог принести другой запрос
	if len(need) == 0 {
		return out
	}
	found, err := filmCardsFetchFn(need)
	if err != nil {
		log.Printf("карточки фильмографии не получены: %v", err)
		return out
	}
	_ = os.MkdirAll(filepath.Join(s.cfg.cache, "films"), 0o755)
	for _, q := range need {
		c := found[q]
		c.At = time.Now().Unix()
		if data, err := json.Marshal(c); err == nil {
			_ = writeAtomic(s.filmCardPath(q), data)
		}
		out[q] = c
	}
	if filmPosterPrefetch {
		filmPrefetchWG.Add(1)
		go func() {
			defer filmPrefetchWG.Done()
			for _, q := range need {
				if out[q].Img != "" {
					s.ensureFilmPoster(q)
				}
			}
		}()
	}
	return out
}

// GET /api/filmcards?ids=Q1,Q2 — карточки для расширенной фильмографии.
func (s *server) filmCardsAPI(w http.ResponseWriter, r *http.Request) {
	var ids []string
	seen := map[string]bool{}
	n := 0
	for _, q := range strings.Split(r.URL.Query().Get("ids"), ",") {
		if q = strings.TrimSpace(q); q == "" {
			continue
		}
		n++
		if reQID.MatchString(q) && !seen[q] {
			seen[q] = true
			ids = append(ids, q)
		}
	}
	if n > filmCardsMax {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "не больше 50 за раз"})
		return
	}
	cards := map[string]any{}
	for q, c := range s.filmCards(ids) {
		m := map[string]string{}
		if c.About != "" {
			m["about"] = c.About
		}
		if c.Img != "" || fileReady(s.filmPosterPath(q)) {
			m["poster"] = s.cfg.base + "/filmposter/" + q
		}
		cards[q] = m
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": cards})
}

// GET /filmposter/{qid} — постер из кэша, а нет — скачать по карточке.
func (s *server) filmPoster(w http.ResponseWriter, r *http.Request) {
	qid := r.PathValue("qid")
	if !reQID.MatchString(qid) || !s.ensureFilmPoster(qid) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=2592000")
	http.ServeFile(w, r, s.filmPosterPath(qid))
}

func (s *server) ensureFilmPoster(qid string) bool {
	dst := s.filmPosterPath(qid)
	if fileReady(dst) {
		return true
	}
	c, ok := s.readFilmCard(qid)
	if !ok || c.Img == "" {
		return false
	}
	miss := dst + ".miss"
	if st, err := os.Stat(miss); err == nil && time.Since(st.ModTime()) < filmPosterMis {
		return false
	}
	filmPosterSem <- struct{}{}
	defer func() { <-filmPosterSem }()
	if fileReady(dst) {
		return true // успел другой запрос
	}
	if st, err := os.Stat(miss); err == nil && time.Since(st.ModTime()) < filmPosterMis {
		return false // пока ждали, другой запрос узнал, что скачать нельзя
	}
	if wait := filmPosterGap - time.Since(filmPosterLast); wait > 0 {
		time.Sleep(wait)
	}
	defer func() { filmPosterLast = time.Now() }()
	err := s.fetchFilmPoster(c.Img, dst)
	if transientErr(err) {
		// Викисклад отвечает 429, когда страница просит много постеров разом
		time.Sleep(filmPosterRetryWait)
		err = s.fetchFilmPoster(c.Img, dst)
	}
	if transientErr(err) {
		log.Printf("постер %s пока не скачан: %v", qid, err)
		return false // в следующий раз попробуем снова
	}
	if err != nil {
		log.Printf("постер %s не скачан: %v", qid, err)
		_ = os.WriteFile(miss, nil, 0o644)
		return false
	}
	return true
}

// filmPosterRetryWait — пауза перед повтором; в тестах ноль.
var filmPosterRetryWait = 3 * time.Second

// transientErr — отказ, который пройдёт сам: перегрузка, сбой сервера, сеть.
func transientErr(err error) bool {
	if err == nil {
		return false
	}
	var se *httpStatusError
	if errors.As(err, &se) {
		return se.code == http.StatusTooManyRequests || se.code >= 500
	}
	var ne net.Error
	return errors.As(err, &ne)
}

func (s *server) fetchFilmPoster(src, dst string) error {
	data, ext, err := filmPosterDownloadFn(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".src" + ext
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	defer os.Remove(tmp)
	part := dst + ".part"
	if err := renderJPEG(tmp, part, filmPosterW, filmPosterH, 85); err != nil {
		_ = os.Remove(part)
		return err
	}
	return os.Rename(part, dst)
}

// --- интернет ---

// fetchFilmCards — карточки по Q-id: ссылки на статьи из Wikidata, потом
// вступления и картинки статей пачками.
func fetchFilmCards(qids []string) (map[string]filmCard, error) {
	var res struct {
		Entities map[string]struct {
			Sitelinks map[string]struct {
				Title string `json:"title"`
			} `json:"sitelinks"`
			Descriptions map[string]struct {
				Value string `json:"value"`
			} `json:"descriptions"`
		} `json:"entities"`
	}
	u := wikidataAPI + "action=wbgetentities&props=sitelinks|descriptions&sitefilter=ruwiki|enwiki&languages=ru&ids=" +
		url.QueryEscape(strings.Join(qids, "|"))
	if err := getJSON(u, &res); err != nil {
		return nil, err
	}
	out := map[string]filmCard{}
	ru, en := map[string]string{}, map[string]string{}
	for q, e := range res.Entities {
		if t := e.Sitelinks["ruwiki"].Title; t != "" {
			ru[t] = q
		}
		if t := e.Sitelinks["enwiki"].Title; t != "" {
			en[t] = q
		}
	}
	got, err := wikiCards("ru", ru, true)
	if err != nil {
		return nil, err
	}
	for q, c := range got {
		out[q] = c
	}
	// без постера в русской статье — из английской, описание оттуда не берём
	noImg := map[string]string{}
	for t, q := range en {
		if out[q].Img == "" {
			noImg[t] = q
		}
	}
	if got, err = wikiCards("en", noImg, false); err != nil {
		return nil, err
	}
	for q, c := range got {
		if c.Img != "" {
			cur := out[q]
			cur.Img = c.Img
			out[q] = cur
		}
	}
	for q, e := range res.Entities {
		if d := strings.TrimSpace(e.Descriptions["ru"].Value); d != "" && out[q].About == "" {
			cur := out[q]
			cur.About = upperFirst(d)
			out[q] = cur
		}
	}
	return out, nil
}

// wikiCards — вступления и картинки статей одного языка, по 20 за запрос.
func wikiCards(lang string, byTitle map[string]string, extracts bool) (map[string]filmCard, error) {
	titles := make([]string, 0, len(byTitle))
	for t := range byTitle {
		titles = append(titles, t)
	}
	out := map[string]filmCard{}
	for i := 0; i < len(titles); i += wikiBatch {
		part := titles[i:min(i+wikiBatch, len(titles))]
		prop := "pageimages"
		if extracts {
			prop = "extracts|pageimages&exintro=1&explaintext=1&exsentences=3&exlimit=20"
		}
		// pilicense=any: постеры несвободные, а по умолчанию их не отдают
		u := "https://" + lang + ".wikipedia.org/w/api.php?format=json&formatversion=2&action=query&redirects=1" +
			"&prop=" + prop + "&piprop=thumbnail&pithumbsize=400&pilicense=any&titles=" +
			url.QueryEscape(strings.Join(part, "|"))
		var raw json.RawMessage
		if err := getJSON(u, &raw); err != nil {
			return nil, err
		}
		for q, c := range cardsFromWiki(raw, byTitle) {
			out[q] = c
		}
	}
	return out, nil
}

// cardsFromWiki — разбор ответа action=query: названия в ответе бывают
// нормализованы и перенаправлены, поэтому Q-id ищется по цепочке назад.
func cardsFromWiki(body []byte, byTitle map[string]string) map[string]filmCard {
	var res struct {
		Query struct {
			Normalized []struct{ From, To string } `json:"normalized"`
			Redirects  []struct{ From, To string } `json:"redirects"`
			Pages      []struct {
				Title     string `json:"title"`
				Extract   string `json:"extract"`
				Missing   bool   `json:"missing"`
				Thumbnail struct {
					Source string `json:"source"`
				} `json:"thumbnail"`
			} `json:"pages"`
		} `json:"query"`
	}
	out := map[string]filmCard{}
	if json.Unmarshal(body, &res) != nil {
		return out
	}
	qOf := map[string]string{}
	for t, q := range byTitle {
		qOf[t] = q
	}
	for _, n := range res.Query.Normalized {
		if q, ok := qOf[n.From]; ok {
			qOf[n.To] = q
		}
	}
	for _, rd := range res.Query.Redirects {
		if q, ok := qOf[rd.From]; ok {
			qOf[rd.To] = q
		}
	}
	for _, p := range res.Query.Pages {
		q := qOf[p.Title]
		if q == "" || p.Missing {
			continue
		}
		c := filmCard{About: clipAbout(p.Extract), Img: p.Thumbnail.Source}
		if c.About != "" || c.Img != "" {
			out[q] = c
		}
	}
	return out
}

func clipAbout(s string) string {
	// знаки ударения из вступлений («Нача́ло») на экране только мешают
	s = strings.ReplaceAll(s, "\u0301", "")
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > filmAboutMax {
		s = strings.TrimSpace(string(r[:filmAboutMax])) + "…"
	}
	return s
}

func upperFirst(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return strings.ToUpper(string(r[0])) + string(r[1:])
}
