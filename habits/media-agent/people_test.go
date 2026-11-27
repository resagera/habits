package main

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func peopleSetup(t *testing.T) (*server, string) {
	s, _, films := plansServer(t)
	film := filepath.Join(films, "Film (2020).mkv")
	show := filepath.Join(films, "Show")
	_ = os.MkdirAll(filepath.Join(show, "S01"), 0o755)
	_ = os.WriteFile(filepath.Join(show, "S01", "E01.mkv"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(show, "S01", "E02.mkv"), []byte("x"), 0o644)
	_ = writeInfo(film, &itemInfo{Title: "Film", Cast: []castMember{{"Том Харди", "Q208026"}}, CastAt: 1})
	_ = writeInfo(show, &itemInfo{Title: "Show", Actors: []string{"том харди", "Кто-то"}})
	s.plans.add(plan{Query: "Venom", Kind: "film", Type: "film",
		Info: &itemInfo{Title: "Venom", Cast: []castMember{{"Том Харди", "Q208026"}}}})
	get(t, s, "/tv/api/catalog") // каталог раздаёт id плиток
	return s, film
}

func peopleRows(t *testing.T, s *server) map[string]map[string]any {
	t.Helper()
	code, out := adminCall(t, s, "GET", "/api/admin/people", testPin, nil)
	if code != 200 {
		t.Fatalf("люди: %d %v", code, out)
	}
	rows := map[string]map[string]any{}
	for _, x := range out["people"].([]any) {
		m := x.(map[string]any)
		rows[m["key"].(string)] = m
	}
	return rows
}

// Человек без Q-id с тем же именем — тот же человек; план тоже считается.
func TestPeopleIndex(t *testing.T) {
	s, _ := peopleSetup(t)
	rows := peopleRows(t, s)
	th := rows["Q208026"]
	if th == nil || th["have"].(float64) != 2 || th["plans"].(float64) != 1 || th["name"] != "Том Харди" {
		t.Fatalf("Том Харди: %v (%v)", th, rows)
	}
	other := rows["name:Кто-то"]
	if other == nil || other["have"].(float64) != 1 || other["qid"] != nil && other["qid"] != "" {
		t.Fatalf("без Q-id: %v", other)
	}
	if len(rows) != 2 {
		t.Fatalf("лишние: %v", rows)
	}
}

func uploadPersonPhoto(t *testing.T, s *server, key string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/tv/api/admin/image?target=person&id="+key, bytes.NewReader(pngBytes(t)))
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("X-Admin-Pin", testPin)
	s.ServeHTTP(rec, req)
	return rec.Code
}

func TestPersonEditAndPhoto(t *testing.T) {
	s, film := peopleSetup(t)
	if code, out := adminCall(t, s, "PUT", "/api/admin/people?key=Q208026", testPin,
		map[string]any{"about": "Британский актёр", "name": "Том Харди"}); code != 200 {
		t.Fatalf("правка: %d %v", code, out)
	}
	if out := get(t, s, "/tv/api/person?qid=Q208026"); out["about"] != "Британский актёр" {
		t.Fatalf("описание: %v", out)
	}
	// своё фото человеку без Q-id
	if code := uploadPersonPhoto(t, s, "name:Кто-то"); code != 200 {
		t.Fatalf("фото без Q-id: %d", code)
	}
	pid := personFileID("name:Кто-то")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/tv/person/"+pid, nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("/person/%s: %d", pid, rec.Code)
	}
	if out := get(t, s, "/tv/api/person?name=Кто-то"); !strings.HasPrefix(out["photo"].(string), s.cfg.base+"/person/"+pid+"?v=") {
		t.Fatalf("фото в /api/person: %v", out)
	}
	if out := get(t, s, "/tv/api/info?id="+s.remember(filepath.Join(filepath.Dir(film), "Show"))); out["people_photos"].(map[string]any)["Кто-то"] == nil {
		t.Fatalf("фото в «Инфо»: %v", out["people_photos"])
	}
	// своё фото главнее скачанного, убрали — снова скачанное
	dl := s.personPhotoPath("Q208026")
	_ = os.MkdirAll(filepath.Dir(dl), 0o755)
	_ = os.WriteFile(dl, []byte("downloaded"), 0o644)
	if code := uploadPersonPhoto(t, s, "Q208026"); code != 200 {
		t.Fatalf("фото Q: %d", code)
	}
	body := func() string {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", "/tv/person/Q208026", nil))
		return rec.Body.String()
	}
	if body() == "downloaded" {
		t.Fatal("своё фото не главнее")
	}
	adminCall(t, s, "DELETE", "/api/admin/image?target=person&id=Q208026", testPin, nil)
	if body() != "downloaded" {
		t.Fatal("после удаления своего — не скачанное")
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/tv/person/n-zz", nil))
	if rec.Code != 404 {
		t.Fatalf("кривой ключ: %d", rec.Code)
	}
}
