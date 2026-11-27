package main

import (
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// в тестах Википедию не зовём: кому нужно — подменяет сам
func init() {
	filmCardsFetchFn = func([]string) (map[string]filmCard, error) {
		return nil, errors.New("сеть в тестах выключена")
	}
	filmPosterDownloadFn = func(string) ([]byte, string, error) {
		return nil, "", errors.New("сеть в тестах выключена")
	}
	filmPosterPrefetch, filmPosterGap = false, 0
}

// Ответ Википедии: названия нормализуются и бывают перенаправлениями —
// карточка всё равно находит свой Q-id.
func TestCardsFromWiki(t *testing.T) {
	body := []byte(`{"query":{
	  "normalized":[{"from":"начало (фильм, 2010)","to":"Начало (фильм, 2010)"}],
	  "redirects":[{"from":"Старое имя","to":"Тёмный рыцарь: Возрождение легенды"}],
	  "pages":[
	    {"title":"Начало (фильм, 2010)","extract":"«Нача́ло» — фильм.\n\nВторой абзац.","thumbnail":{"source":"https://upload.wikimedia.org/a.jpg"}},
	    {"title":"Тёмный рыцарь: Возрождение легенды","extract":"Третья часть."},
	    {"title":"Нет такой","missing":true}]}}`)
	byTitle := map[string]string{"начало (фильм, 2010)": "Q25188", "Старое имя": "Q189330", "Нет такой": "Q1"}
	got := cardsFromWiki(body, byTitle)
	if c := got["Q25188"]; c.Img != "https://upload.wikimedia.org/a.jpg" || c.About != "«Начало» — фильм. Второй абзац." {
		t.Fatalf("Начало: %+v", c)
	}
	if c := got["Q189330"]; c.About != "Третья часть." || c.Img != "" {
		t.Fatalf("перенаправление: %+v", c)
	}
	if _, ok := got["Q1"]; ok {
		t.Fatalf("пустая страница дала карточку: %+v", got)
	}
}

func TestFilmCardsCached(t *testing.T) {
	s, _ := peopleSetup(t)
	saved := filmCardsFetchFn
	t.Cleanup(func() { filmCardsFetchFn = saved })
	var asked [][]string
	filmCardsFetchFn = func(q []string) (map[string]filmCard, error) {
		asked = append(asked, q)
		return map[string]filmCard{"Q25188": {About: "Про сны", Img: "https://x/a.jpg"}}, nil
	}
	out := get(t, s, "/tv/api/filmcards?ids=Q25188,Q2,bad")
	cards := out["cards"].(map[string]any)
	in := cards["Q25188"].(map[string]any)
	if in["about"] != "Про сны" || in["poster"] != s.cfg.base+"/filmposter/Q25188" {
		t.Fatalf("карточка: %v", in)
	}
	// не нашлось — тоже ответ: пустая карточка, без постера, и запомнена
	if q2 := cards["Q2"].(map[string]any); q2["poster"] != nil || q2["about"] != nil {
		t.Fatalf("пустая: %v", q2)
	}
	if _, ok := cards["bad"]; ok || len(asked) != 1 || len(asked[0]) != 2 {
		t.Fatalf("запросы: %v, карточки %v", asked, cards)
	}
	get(t, s, "/tv/api/filmcards?ids=Q25188,Q2")
	if len(asked) != 1 {
		t.Fatalf("второй раз снова в сеть: %v", asked)
	}
	// неудача сети карточки не запоминает
	filmCardsFetchFn = func([]string) (map[string]filmCard, error) { return nil, errors.New("нет сети") }
	get(t, s, "/tv/api/filmcards?ids=Q3")
	filmCardsFetchFn = func(q []string) (map[string]filmCard, error) { asked = append(asked, q); return nil, nil }
	get(t, s, "/tv/api/filmcards?ids=Q3")
	if len(asked) != 2 {
		t.Fatalf("после сбоя не повторил: %v", asked)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/tv/api/filmcards?ids="+strings.Repeat("Q1,", 60), nil))
	if rec.Code != 400 {
		t.Fatalf("больше 50: %d", rec.Code)
	}
}

// Постер скачивается один раз и дальше отдаётся с нашего сервера.
func TestFilmPosterServed(t *testing.T) {
	s, _ := peopleSetup(t)
	sf, sd := filmCardsFetchFn, filmPosterDownloadFn
	t.Cleanup(func() { filmCardsFetchFn, filmPosterDownloadFn = sf, sd })
	filmCardsFetchFn = func([]string) (map[string]filmCard, error) {
		return map[string]filmCard{"Q25188": {Img: "https://x/a.png"}}, nil
	}
	downloads := 0
	filmPosterDownloadFn = func(u string) ([]byte, string, error) {
		downloads++
		if u != "https://x/a.png" {
			t.Errorf("адрес: %s", u)
		}
		return pngBytes(t), ".png", nil
	}
	get(t, s, "/tv/api/filmcards?ids=Q25188,Q2")
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", "/tv/filmposter/Q25188", nil))
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/jpeg" {
			t.Fatalf("постер: %d %s", rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	if downloads != 1 || !fileReady(s.filmPosterPath("Q25188")) {
		t.Fatalf("скачиваний %d", downloads)
	}
	for _, q := range []string{"Q2", "Q999", "x1"} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", "/tv/filmposter/"+q, nil))
		if rec.Code != 404 {
			t.Fatalf("%s: %d", q, rec.Code)
		}
	}
	// скачать не вышло — неделю не пробуем
	filmCardsFetchFn = func([]string) (map[string]filmCard, error) {
		return map[string]filmCard{"Q7": {Img: "https://x/b.png"}}, nil
	}
	filmPosterDownloadFn = func(string) ([]byte, string, error) { downloads++; return nil, "", errors.New("404") }
	get(t, s, "/tv/api/filmcards?ids=Q7")
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", "/tv/filmposter/Q7", nil))
		if rec.Code != 404 {
			t.Fatalf("сбой: %d", rec.Code)
		}
	}
	if downloads != 2 {
		t.Fatalf("после сбоя повторял: %d", downloads)
	}
	if _, err := os.Stat(s.filmPosterPath("Q7") + ".miss"); err != nil {
		t.Fatal("нет отметки неудачи")
	}
}

// «Слишком много запросов» от Викисклада — не повод неделю жить без постера:
// один повтор, а не вышло — без отметки неудачи.
func TestFilmPosterRateLimited(t *testing.T) {
	s, _ := peopleSetup(t)
	sf, sd, sw := filmCardsFetchFn, filmPosterDownloadFn, filmPosterRetryWait
	t.Cleanup(func() { filmCardsFetchFn, filmPosterDownloadFn, filmPosterRetryWait = sf, sd, sw })
	filmPosterRetryWait = 0
	filmCardsFetchFn = func([]string) (map[string]filmCard, error) {
		return map[string]filmCard{"Q8": {Img: "https://x/c.png"}, "Q9": {Img: "https://x/d.png"}}, nil
	}
	get(t, s, "/tv/api/filmcards?ids=Q8,Q9")
	calls := 0
	filmPosterDownloadFn = func(string) ([]byte, string, error) {
		calls++
		if calls == 1 {
			return nil, "", &httpStatusError{429}
		}
		return pngBytes(t), ".png", nil
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/tv/filmposter/Q8", nil))
	if rec.Code != 200 || calls != 2 {
		t.Fatalf("повтор: %d, попыток %d", rec.Code, calls)
	}
	filmPosterDownloadFn = func(string) ([]byte, string, error) { return nil, "", &httpStatusError{503} }
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/tv/filmposter/Q9", nil))
	if rec.Code != 404 {
		t.Fatalf("сбой: %d", rec.Code)
	}
	if _, err := os.Stat(s.filmPosterPath("Q9") + ".miss"); err == nil {
		t.Fatal("временный сбой запомнен на неделю")
	}
}

// Новые карточки сразу докачивают свои постеры в фоне: к прокрутке они уже у нас.
func TestFilmPosterPrefetch(t *testing.T) {
	s, _ := peopleSetup(t)
	sf, sd := filmCardsFetchFn, filmPosterDownloadFn
	t.Cleanup(func() { filmCardsFetchFn, filmPosterDownloadFn, filmPosterPrefetch = sf, sd, false })
	filmPosterPrefetch = true
	filmCardsFetchFn = func([]string) (map[string]filmCard, error) {
		return map[string]filmCard{"Q11": {Img: "https://x/e.png"}, "Q12": {About: "без постера"}}, nil
	}
	downloads := 0
	filmPosterDownloadFn = func(string) ([]byte, string, error) { downloads++; return pngBytes(t), ".png", nil }
	get(t, s, "/tv/api/filmcards?ids=Q11,Q12")
	filmPrefetchWG.Wait()
	if !fileReady(s.filmPosterPath("Q11")) || downloads != 1 {
		t.Fatalf("фоновая докачка: %d", downloads)
	}
}
