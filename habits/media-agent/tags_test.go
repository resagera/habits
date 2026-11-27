package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tcall(t *testing.T, s *server, method, url, body string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	s.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestNormTags(t *testing.T) {
	got := normTags([]string{"#Отпуск", "отпуск", " горы  зимой ", "", "#", "a,b"})
	if !equalStrings(got, []string{"Отпуск", "горы_зимой", "a_b"}) {
		t.Fatalf("normTags: %v", got)
	}
}

func TestTagsEditBulkAndSearch(t *testing.T) {
	s, photos, films := filesLibrary(t)
	walk(t, s)
	img := filepath.Join(photos, "Trip", "IMG_1.png")
	notes := filepath.Join(photos, "Trip", "notes.txt")
	film := filepath.Join(films, "Film (2020).mkv")

	code, out := tcall(t, s, "POST", "/tv/api/file/edit",
		`{"id":"`+s.remember(img)+`","tags":["#Отпуск","горы"],"title":"Дилижан"}`)
	if code != 200 || len(out["tags"].([]any)) != 2 {
		t.Fatalf("edit: %d %v", code, out)
	}
	code, out = tcall(t, s, "POST", "/tv/api/file/tags",
		`{"ids":["`+s.remember(notes)+`","`+s.remember(img)+`","`+s.remember(film)+`"],"add":["отпуск","2024"],"remove":["горы"]}`)
	if code != 200 || out["count"].(float64) != 3 {
		t.Fatalf("bulk: %d %v", code, out)
	}
	if got := s.meta.tags(img); !equalStrings(got, []string{"Отпуск", "2024"}) {
		t.Fatalf("теги снимка: %v", got)
	}

	// browse отдаёт теги
	for _, it := range browseAllOf(t, s, s.remember(filepath.Join(photos, "Trip"))).Items {
		if it.Name == "notes.txt" && !equalStrings(it.Tags, []string{"отпуск", "2024"}) {
			t.Fatalf("теги в browse: %v", it.Tags)
		}
	}

	search := func(q string) []string {
		t.Helper()
		code, out := tcall(t, s, "GET", "/tv/api/search?"+q, "")
		if code != 200 {
			t.Fatalf("search %s: %d %v", q, code, out)
		}
		var names []string
		for _, it := range out["items"].([]any) {
			names = append(names, it.(map[string]any)["name"].(string))
		}
		return names
	}
	if got := search("q=%23отпуск"); len(got) != 3 {
		t.Fatalf("#отпуск: %v", got)
	}
	if got := search("q=дилижан"); !equalStrings(got, []string{"IMG_1.png"}) {
		t.Fatalf("по заголовку: %v", got)
	}
	if got := search("tags=отпуск&scope=" + shortID(photos)); len(got) != 2 {
		t.Fatalf("в папке фото: %v", got)
	}
	// постер рядом с фильмом и описание поиск не находит
	if got := search("q=film"); !equalStrings(got, []string{"Film (2020).mkv"}) {
		t.Fatalf("film: %v", got)
	}
	if got := search("q=day"); !equalStrings(got, []string{"Day 2"}) {
		t.Fatalf("папка: %v", got)
	}

	// все теги с числом файлов
	_, out = tcall(t, s, "GET", "/tv/api/tags", "")
	tags := out["tags"].([]any)
	first := tags[1].(map[string]any)
	if len(tags) != 2 || first["count"].(float64) != 3 || !strings.EqualFold(first["tag"].(string), "отпуск") {
		t.Fatalf("теги: %v", tags)
	}

	// подборка: плитка в каталоге, переезд папки, удаление
	code, out = tcall(t, s, "POST", "/tv/api/tagviews",
		`{"name":"Отпуск в фото","tags":["отпуск"],"scope":"`+s.remember(filepath.Join(photos, "Trip"))+`"}`)
	if code != 200 {
		t.Fatalf("tagview: %d %v", code, out)
	}
	view := out["id"].(string)
	var cat catalogData
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/tv/api/catalog", nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &cat)
	if len(cat.TagViews) != 1 || cat.TagViews[0].Count != 2 || cat.TagViews[0].Poster == "" {
		t.Fatalf("плитка подборки: %+v", cat.TagViews)
	}
	if _, err := s.renamePath(filepath.Join(photos, "Trip"), "Поездка"); err != nil {
		t.Fatal(err)
	}
	if got := search("view=" + view); len(got) != 2 {
		t.Fatalf("подборка после переезда папки: %v", got)
	}
	if got := s.meta.tags(filepath.Join(photos, "Поездка", "IMG_1.png")); len(got) != 2 {
		t.Fatalf("теги не переехали: %v", got)
	}
	if code, _ := tcall(t, s, "DELETE", "/tv/api/tagviews/"+view, ""); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := tcall(t, s, "GET", "/tv/api/search?view="+view, ""); code != 404 {
		t.Fatalf("удалённая подборка: %d", code)
	}
}

func TestCatalogTileTags(t *testing.T) {
	s, _, films := filesLibrary(t)
	s.meta.setTags(filepath.Join(films, "Film (2020).mkv"), []string{"любимое"})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/tv/api/catalog", nil))
	var cat catalogData
	_ = json.Unmarshal(rec.Body.Bytes(), &cat)
	if len(cat.Films) != 1 || !equalStrings(cat.Films[0].Tags, []string{"любимое"}) {
		t.Fatalf("теги плитки: %+v", cat.Films)
	}
}

func TestSubsNextToVideo(t *testing.T) {
	s, _, films := filesLibrary(t)
	srt := "1\r\n0:00:01,500 --> 00:00:03,000\r\n\xcf\xf0\xe8\xe2\xe5\xf2\r\n" // «Привет» в cp1251
	if err := os.WriteFile(filepath.Join(films, "Film (2020).ru.srt"), []byte(srt), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(films, "Film (2020).srt"), []byte("WEBVTT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/tv/api/browse?id="+shortID(films), nil))
	var out struct {
		Items []outItem `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Items) != 1 || len(out.Items[0].Subs) != 2 {
		t.Fatalf("субтитры: %+v", out.Items)
	}
	var ru subRef
	for _, sb := range out.Items[0].Subs {
		if sb.Lang == "ru" {
			ru = sb
		}
	}
	if ru.Label != "Русские" {
		t.Fatalf("подпись: %+v", out.Items[0].Subs)
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", ru.URL, nil))
	body := rec.Body.String()
	if rec.Code != 200 || !strings.HasPrefix(body, "WEBVTT") || !strings.Contains(body, "00:00:01.500 --> 00:00:03.000") ||
		!strings.Contains(body, "Привет") {
		t.Fatalf("vtt: %d %q", rec.Code, body)
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", strings.Split(ru.URL, "?")[0]+"?k=bad", nil))
	if rec.Code != 403 {
		t.Fatalf("без подписи: %d", rec.Code)
	}
}

func TestBrokenFiles(t *testing.T) {
	s, _, films := filesLibrary(t)
	if err := os.WriteFile(filepath.Join(films, "Broken (2001).mkv"), []byte("not a video at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(films, "Half (2002).mp4.part"), []byte("xx"), 0o644); err != nil {
		t.Fatal(err)
	}
	walk(t, s)
	s.cfg.adminPin = testPin
	code, out := tcall(t, s, "GET", "/tv/api/admin/broken", "")
	if code != 401 {
		t.Fatalf("без PIN: %d", code)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/tv/api/admin/broken", nil)
	req.Header.Set("X-Admin-Pin", testPin)
	s.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	got := map[string]string{}
	for _, it := range out["items"].([]any) {
		m := it.(map[string]any)
		got[m["name"].(string)] = m["reason"].(string)
	}
	// у тестовой библиотеки и «настоящий» фильм — не видео, поэтому считаем по именам
	if !strings.Contains(got["Broken (2001).mkv"], "битый") ||
		!strings.Contains(got["Half (2002).mp4.part"], "недокачано") {
		t.Fatalf("битые: %v", got)
	}
}
