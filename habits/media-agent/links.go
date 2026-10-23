package main

// Ссылки у фильма или сериала: где это ещё можно посмотреть — онлайн-кинотеатр,
// страница на Кинопоиске, раздача.
//
// Хранит агент (links.json), ключ — тот же, что у избранного: путь, а у
// папки-подборки её id. Отдельно от «Инфо» намеренно: описание переписывается
// при повторном поиске в интернете, а ссылки ставят руками и терять их нельзя.
//
// Заголовка может не быть — тогда берём его из самой страницы (<title>), а
// если она не отвечает или закрылась от бота, остаётся хост: «rezka.ag»
// всё равно говорит больше, чем голый адрес на три строки.

import (
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

const maxLinks = 20

type itemLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type linkStore struct {
	mu    sync.Mutex
	path  string
	items map[string][]itemLink
}

func newLinkStore(cacheDir string) *linkStore {
	l := &linkStore{path: filepath.Join(cacheDir, "links.json"), items: map[string][]itemLink{}}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, &l.items)
	}
	return l
}

func (ls *linkStore) saveLocked() {
	if data, err := json.MarshalIndent(ls.items, "", " "); err == nil {
		_ = writeAtomic(ls.path, data)
	}
}

func (ls *linkStore) get(ref string) []itemLink {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return append([]itemLink{}, ls.items[ref]...)
}

func (ls *linkStore) count(ref string) int {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return len(ls.items[ref])
}

func (ls *linkStore) set(ref string, links []itemLink) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if len(links) == 0 {
		delete(ls.items, ref)
	} else {
		ls.items[ref] = links
	}
	ls.saveLocked()
}

// move — переименовали в админке: ссылки едут за объектом вместе с путём.
func (ls *linkStore) move(old, neu string, isDir bool) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	changed := false
	for ref, links := range ls.items {
		if p, ok := movedCollPath(ref, old, neu, isDir); ok {
			delete(ls.items, ref)
			ls.items[p] = links
			changed = true
		}
	}
	if changed {
		ls.saveLocked()
	}
}

// --- разбор ссылки ---

// cleanURL приводит адрес к виду, по которому браузер приставки точно пойдёт.
// Без схемы ссылка в <a href> считается относительной и ведёт в никуда.
func cleanURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2000 {
		return "", false
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", false
	}
	// только http(s): javascript: и data: в href пускать нельзя
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	return u.String(), true
}

var (
	reTitleTag = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reHTMLTag  = regexp.MustCompile(`(?s)<[^>]*>`)
)

// hostTitle — запасной заголовок: хост без «www.».
func hostTitle(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

// pageTitle достаёт <title> страницы. Онлайн-кинотеатры часто закрыты от
// ботов, поэтому неудача здесь — не ошибка, а повод взять хост.
func pageTitle(raw string) string {
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", posterUA)
	resp, err := posterHTTP.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	// заголовок лежит в первых килобайтах — качать страницу целиком незачем
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	m := reTitleTag.FindSubmatch(body)
	if m == nil {
		return ""
	}
	title := html.UnescapeString(string(reHTMLTag.ReplaceAll(m[1], nil)))
	title = strings.TrimSpace(reSpaces.ReplaceAllString(title, " "))
	if !utf8.ValidString(title) {
		return "" // не UTF-8 (встречается у старых раздач) — разбирать кодировки ради подписи незачем
	}
	return trimRunes(title, 120)
}

func trimRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:max])) + "…"
}

// Заслон вместо страницы отдаётся с кодом 200 и своим заголовком: Кинопоиск
// встречает бота «Вы не робот?», Cloudflare — «Just a moment…». Подписывать
// так ссылку хуже, чем именем сайта.
var reBlockTitle = regexp.MustCompile(`(?i)не робот|robot|captcha|just a moment|checking your browser|` +
	`attention required|access denied|forbidden|доступ (?:ограничен|запрещ)|ошибка 4\d\d|^\s*4\d\d\s*$`)

// linkTitle — заголовок, который покажем: заданный руками, иначе из страницы,
// иначе хост.
func linkTitle(title, u string) string {
	if t := strings.TrimSpace(title); t != "" {
		return trimRunes(t, 120)
	}
	if t := pageTitle(u); t != "" && !reBlockTitle.MatchString(t) {
		return t
	}
	return hostTitle(u)
}

// --- ручки ---

// GET /api/links?id= — ссылки элемента для страницы.
func (s *server) itemLinks(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.favRef(r.URL.Query().Get("id"))
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"links": []itemLink{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": s.linksOf(ref)})
}

func (s *server) linksOf(ref string) []itemLink {
	out := s.links.get(ref)
	if out == nil {
		out = []itemLink{}
	}
	return out
}

// POST /api/admin/links {id, links:[{title,url}]} — список целиком.
// Пустой заголовок добирается из страницы здесь, а не на клиенте: ходить в
// интернет из браузера приставки нельзя (CORS), да и незачем.
func (s *server) adminLinks(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID    string     `json:"id"`
		Links []itemLink `json:"links"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	ref, ok := s.favRef(req.ID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "не найдено"})
		return
	}
	out := []itemLink{}
	for _, l := range req.Links {
		u, ok := cleanURL(l.URL)
		if !ok {
			writeJSON(w, http.StatusBadRequest,
				map[string]string{"error": "не похоже на ссылку: " + trimRunes(l.URL, 60)})
			return
		}
		out = append(out, itemLink{Title: linkTitle(l.Title, u), URL: u})
		if len(out) >= maxLinks {
			break
		}
	}
	s.links.set(ref, out)
	writeJSON(w, http.StatusOK, map[string]any{"links": out})
}
