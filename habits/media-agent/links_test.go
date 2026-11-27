package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCleanURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://rezka.ag/films/1.html", "https://rezka.ag/films/1.html"},
		{"  kinopoisk.ru/film/326/  ", "https://kinopoisk.ru/film/326/"}, // без схемы ссылка была бы относительной
		{"http://example.com", "http://example.com"},
	}
	for _, c := range cases {
		got, ok := cleanURL(c.in)
		if !ok || got != c.want {
			t.Fatalf("cleanURL(%q) = %q, %v; хотели %q", c.in, got, ok, c.want)
		}
	}
	for _, bad := range []string{"", "   ", "javascript:alert(1)", "data:text/html,x", "https://"} {
		if got, ok := cleanURL(bad); ok {
			t.Fatalf("cleanURL(%q) пропустил %q", bad, got)
		}
	}
}

// Заголовка нет — берём из страницы; страница не отдалась — остаётся хост.
func TestLinkTitleFromPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/closed") {
			http.Error(w, "nope", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("<html><head><title>  Элизиум —\n смотреть онлайн </title></head>"))
	}))
	defer srv.Close()

	if got := linkTitle("", srv.URL+"/film/1"); got != "Элизиум — смотреть онлайн" {
		t.Fatalf("заголовок из страницы: %q", got)
	}
	// онлайн-кинотеатры часто закрыты от ботов — это не ошибка
	if got := linkTitle("", srv.URL+"/closed"); got != "127.0.0.1" {
		t.Fatalf("запасной заголовок: %q", got)
	}
	// заданный руками главнее всего, в интернет за ним не ходим
	if got := linkTitle("Моя ссылка", srv.URL+"/closed"); got != "Моя ссылка" {
		t.Fatalf("ручной заголовок: %q", got)
	}
	if got := hostTitle("https://www.kinopoisk.ru/film/326/"); got != "kinopoisk.ru" {
		t.Fatalf("hostTitle: %q", got)
	}
}

// Заслон отдаётся с кодом 200 и своим заголовком — подписывать им ссылку
// нельзя: это не название фильма.
func TestLinkTitleSkipsBlockPage(t *testing.T) {
	for _, title := range []string{"Вы не робот?", "Just a moment...", "Attention Required! | Cloudflare",
		"Доступ ограничен", "403 Forbidden"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("<html><head><title>" + title + "</title></head>"))
		}))
		if got := linkTitle("", srv.URL+"/film/1"); got != "127.0.0.1" {
			t.Fatalf("заслон «%s» попал в подпись: %q", title, got)
		}
		srv.Close()
	}
	// обычный заголовок при этом не страдает
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><head><title>Робот по имени Чаппи (2015)</title></head>"))
	}))
	defer ok.Close()
	if got := linkTitle("", ok.URL+"/film/2"); got != "Робот по имени Чаппи (2015)" {
		t.Fatalf("обычный заголовок потерялся: %q", got)
	}
}

func linksOfTile(t *testing.T, s *server, id string) []itemLink {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tv/api/links?id="+id, nil))
	var out struct {
		Links []itemLink `json:"links"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out.Links
}

// Ссылки видны странице, считаются в плитке и переживают переименование.
func TestLinksRoundTrip(t *testing.T) {
	s, _ := adminServer(t)
	show := tileNamed(catalogOf(t, s).Series, "Show")
	if show == nil {
		t.Fatal("сериала нет")
	}
	if show.Links != 0 {
		t.Fatalf("ссылок быть не должно: %d", show.Links)
	}

	code, out := adminCall(t, s, "POST", "/api/admin/links", testPin, map[string]any{
		"id": show.ID,
		"links": []map[string]any{
			{"title": "Кинопоиск", "url": "https://www.kinopoisk.ru/series/1/"},
			{"title": "", "url": "rezka.invalid/series/1.html"},
		}})
	if code != http.StatusOK {
		t.Fatalf("сохранение: %d %v", code, out)
	}

	got := linksOfTile(t, s, show.ID)
	if len(got) != 2 || got[0].Title != "Кинопоиск" {
		t.Fatalf("ссылки не сохранились: %+v", got)
	}
	// схема дописалась, заголовок без страницы свёлся к хосту
	if got[1].URL != "https://rezka.invalid/series/1.html" || got[1].Title != "rezka.invalid" {
		t.Fatalf("разбор второй ссылки: %+v", got[1])
	}
	if n := tileNamed(catalogOf(t, s).Series, "Show").Links; n != 2 {
		t.Fatalf("в плитке ссылок %d", n)
	}

	// переименование не теряет ссылки
	if code, out := adminCall(t, s, "POST", "/api/admin/rename", testPin,
		map[string]any{"id": show.ID, "name": "Шоу (2020)"}); code != http.StatusOK {
		t.Fatalf("переименование: %d %v", code, out)
	}
	moved := tileNamed(catalogOf(t, s).Series, "Шоу (2020)")
	if moved == nil || moved.Links != 2 {
		t.Fatalf("после переименования ссылок %+v", moved)
	}

	// пустой список убирает запись целиком
	if code, _ = adminCall(t, s, "POST", "/api/admin/links", testPin,
		map[string]any{"id": moved.ID, "links": []any{}}); code != http.StatusOK {
		t.Fatalf("очистка: %d", code)
	}
	if n := tileNamed(catalogOf(t, s).Series, "Шоу (2020)").Links; n != 0 {
		t.Fatalf("ссылки не убрались: %d", n)
	}
}

// Кривой адрес не сохраняется молча, а возвращает отказ.
func TestLinksRejectBad(t *testing.T) {
	s, _ := adminServer(t)
	film := tileNamed(catalogOf(t, s).Films, "Film")
	code, out := adminCall(t, s, "POST", "/api/admin/links", testPin, map[string]any{
		"id": film.ID, "links": []map[string]any{{"title": "", "url": "javascript:alert(1)"}}})
	if code != http.StatusBadRequest {
		t.Fatalf("javascript: прошёл: %d %v", code, out)
	}
	if n := len(linksOfTile(t, s, film.ID)); n != 0 {
		t.Fatalf("после отказа сохранилось %d", n)
	}
}
