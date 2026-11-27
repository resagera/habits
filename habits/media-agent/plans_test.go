package main

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeSearch подменяет поиск сведений и постеров; found решает, что
// «находится» по названию и типу.
func fakeSearch(t *testing.T, found func(title, kind string) bool) *int {
	t.Helper()
	calls := 0
	si, sp := fetchInfoFn, findPosterFn
	t.Cleanup(func() { fetchInfoFn, findPosterFn = si, sp })
	fetchInfoFn = func(title string, year int, kind string) (*itemInfo, error) {
		calls++
		if !found(title, kind) {
			return nil, errors.New("не нашлось")
		}
		return &itemInfo{Title: "Found " + title, Year: 2024, Description: "описание " + kind}, nil
	}
	findPosterFn = func(title string, year int, kind string, strict bool) ([]byte, string, error) {
		if !found(strings.TrimPrefix(title, "Found "), kind) {
			return nil, "", errors.New("нет постера")
		}
		return pngBytes(t), ".png", nil
	}
	return &calls
}

func plansServer(t *testing.T) (*server, string, string) {
	s, photos, films := filesLibrary(t)
	s.cfg.adminPin = testPin
	return s, photos, films
}

func waitPlans(t *testing.T, s *server) {
	t.Helper()
	for i := 0; i < 400; i++ {
		busy := false
		for _, p := range s.plans.all() {
			busy = busy || p.State == "searching"
		}
		if !busy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("поиск планов не закончился")
}

func plansList(t *testing.T, s *server, archived string) []map[string]any {
	t.Helper()
	code, out := adminCall(t, s, "GET", "/api/admin/plans?archived="+archived, testPin, nil)
	if code != 200 {
		t.Fatalf("список: %d %v", code, out)
	}
	var list []map[string]any
	for _, x := range out["plans"].([]any) {
		list = append(list, x.(map[string]any))
	}
	return list
}

func TestPlansFromLines(t *testing.T) {
	s, _, _ := plansServer(t)
	fakeSearch(t, func(string, string) bool { return true })
	body := map[string]any{"lines": []string{"Дюна 2 (2024)", "", "  ", "Severance"}, "kind": "series"}
	if code, out := adminCall(t, s, "POST", "/api/admin/plans", testPin, body); code != 200 {
		t.Fatalf("пачка: %d %v", code, out)
	}
	waitPlans(t, s)
	list := plansList(t, s, "0")
	if len(list) != 2 {
		t.Fatalf("планов: %d", len(list))
	}
	for _, p := range list {
		info := p["info"].(map[string]any)
		if p["type"] != "series" || p["kind"] != "series" || !strings.HasPrefix(info["title"].(string), "Found ") ||
			p["poster"] == "" || !strings.Contains(p["poster_url"].(string), "/planposter/") {
			t.Fatalf("план: %v", p)
		}
	}
	// тот же список ещё раз — без дублей
	adminCall(t, s, "POST", "/api/admin/plans", testPin, body)
	waitPlans(t, s)
	if n := len(plansList(t, s, "0")); n != 2 {
		t.Fatalf("дубли: %d", n)
	}
	if code, _ := adminCall(t, s, "POST", "/api/admin/plans", testPin, map[string]any{"lines": []string{" "}, "kind": "film"}); code != 400 {
		t.Fatalf("пустой список: %d", code)
	}
}

func TestPlanTypeFallback(t *testing.T) {
	s, _, _ := plansServer(t)
	fakeSearch(t, func(_, kind string) bool { return kind == "film" })
	adminCall(t, s, "POST", "/api/admin/plans", testPin, map[string]any{"lines": []string{"Spirited Away"}, "kind": "anime"})
	waitPlans(t, s)
	p := s.plans.all()[0]
	if p.Kind != "anime" || p.Type != "film" || p.State != "" {
		t.Fatalf("тип: %+v", p)
	}
}

func TestPlanNotFound(t *testing.T) {
	s, _, _ := plansServer(t)
	fakeSearch(t, func(string, string) bool { return false })
	adminCall(t, s, "POST", "/api/admin/plans", testPin, map[string]any{"lines": []string{"Нечто (2031)"}, "kind": "film"})
	waitPlans(t, s)
	p := s.plans.all()[0]
	if p.State != "notfound" || p.Info == nil || p.Info.Title != "Нечто" || p.Info.Year != 2031 {
		t.Fatalf("не нашлось: %+v %+v", p, p.Info)
	}
}

func TestPlanForTile(t *testing.T) {
	s, _, films := plansServer(t)
	calls := fakeSearch(t, func(string, string) bool { return true })
	film := filepath.Join(films, "Film (2020).mkv")
	_ = writeInfo(film, &itemInfo{Title: "Фильм", Year: 2020, Description: "свой"})
	code, out := adminCall(t, s, "POST", "/api/admin/plans", testPin, map[string]any{"tile": s.remember(film), "note": "в 4K"})
	if code != 200 {
		t.Fatalf("для плитки: %d %v", code, out)
	}
	waitPlans(t, s)
	p := s.plans.all()[0]
	if p.Tile != s.remember(film) || p.Kind != "film" || p.Note != "в 4K" || p.Info.Description != "свой" || p.Poster == "" || *calls != 0 {
		t.Fatalf("план плитки: %+v %+v, поисков %d", p, p.Info, *calls)
	}
	if n := s.planPosters(p.ID); len(n) != 1 {
		t.Fatalf("постер плитки: %v", n)
	}
}

func TestPlanEditArchivePosters(t *testing.T) {
	s, _, _ := plansServer(t)
	fakeSearch(t, func(string, string) bool { return true })
	adminCall(t, s, "POST", "/api/admin/plans", testPin, map[string]any{"plan": map[string]any{"query": "Arrival", "kind": "film", "note": "посмотреть"}})
	waitPlans(t, s)
	p := s.plans.all()[0]
	if p.Note != "посмотреть" || p.Type != "film" {
		t.Fatalf("один: %+v", p)
	}
	if code, out := adminCall(t, s, "PUT", "/api/admin/plans/"+p.ID, testPin,
		map[string]any{"note": "срочно", "info": map[string]any{"title": "Прибытие", "year": 2016, "description": p.Info.Description}}); code != 200 {
		t.Fatalf("правка: %d %v", code, out)
	}
	adminCall(t, s, "POST", "/api/admin/plans/"+p.ID+"/refetch", testPin, map[string]any{})
	waitPlans(t, s)
	p, _ = s.plans.find(p.ID)
	if p.Note != "срочно" || p.Info.Title != "Прибытие" || p.Info.Description != "описание film" || p.Info.Year != 2016 {
		t.Fatalf("после поиска: %+v %+v", p, p.Info)
	}
	// свой постер файлом
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/tv/api/admin/image?target=plan&id="+p.ID, bytes.NewReader(pngBytes(t)))
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("X-Admin-Pin", testPin)
	s.ServeHTTP(rec, req)
	names := s.planPosters(p.ID)
	if rec.Code != 200 || len(names) != 2 {
		t.Fatalf("свой постер: %d %s %v", rec.Code, rec.Body.String(), names)
	}
	adminCall(t, s, "PUT", "/api/admin/plans/"+p.ID, testPin, map[string]any{"poster": names[1]})
	if p, _ = s.plans.find(p.ID); p.Poster != names[1] {
		t.Fatalf("главный: %s", p.Poster)
	}
	// картинка и запрет путей
	u := s.cfg.base + "/planposter/" + p.ID + "?k=" + s.sign(p.ID) + "&n="
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", u+names[1], nil))
	if rec.Code != 200 {
		t.Fatalf("картинка: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", u+"../plans.json", nil))
	if rec.Code != 403 {
		t.Fatalf("путь: %d", rec.Code)
	}
	adminCall(t, s, "DELETE", "/api/admin/plans/"+p.ID+"/poster?n="+names[1], testPin, nil)
	if p, _ = s.plans.find(p.ID); p.Poster != names[0] || len(s.planPosters(p.ID)) != 1 {
		t.Fatalf("убрать главный: %+v", p)
	}
	// архив
	adminCall(t, s, "PUT", "/api/admin/plans/"+p.ID, testPin, map[string]any{"archived": true})
	if len(plansList(t, s, "0")) != 0 || len(plansList(t, s, "1")) != 1 {
		t.Fatal("архив")
	}
	adminCall(t, s, "PUT", "/api/admin/plans/"+p.ID, testPin, map[string]any{"archived": false})
	if len(plansList(t, s, "0")) != 1 {
		t.Fatal("из архива")
	}
}

// Удалили план, пока шёл поиск: поиск его не воскрешает.
func TestPlanDeleteDuringSearch(t *testing.T) {
	s, _, _ := plansServer(t)
	fakeSearch(t, func(string, string) bool { return true })
	gate := make(chan struct{})
	inner := fetchInfoFn
	fetchInfoFn = func(title string, year int, kind string) (*itemInfo, error) {
		<-gate
		return inner(title, year, kind)
	}
	adminCall(t, s, "POST", "/api/admin/plans", testPin, map[string]any{"lines": []string{"Tenet"}, "kind": "film"})
	id := s.plans.all()[0].ID
	if code, _ := adminCall(t, s, "DELETE", "/api/admin/plans/"+id, testPin, nil); code != 200 {
		t.Fatal("удалить")
	}
	close(gate)
	time.Sleep(100 * time.Millisecond)
	if len(s.plans.all()) != 0 {
		t.Fatal("план воскрес")
	}
	if exists(s.planDir(id)) {
		t.Fatal("папка плана осталась")
	}
}

func TestPlansNeedPin(t *testing.T) {
	s, _, _ := plansServer(t)
	if code, _ := adminCall(t, s, "GET", "/api/admin/plans", "", nil); code == 200 {
		t.Fatalf("без PIN: %d", code)
	}
}
