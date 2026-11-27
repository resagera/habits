package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 120, 80)))
	return b.Bytes()
}

func get(t *testing.T, s *server, url string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

// Материалы фильма-файла: добавить снимком из «Фото», увидеть в «Инфо»,
// переименовать фильм — материалы едут следом; удалить.
func TestMaterialsFilm(t *testing.T) {
	s, photos, films := filesLibrary(t)
	s.cfg.adminPin = testPin
	film := filepath.Join(films, "Film (2020).mkv")
	id := s.remember(film)
	photo := s.remember(filepath.Join(photos, "Trip", "IMG_1.png"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/tv/api/admin/image?target=material&id="+id,
		strings.NewReader(`{"photo_id":"`+photo+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Pin", testPin)
	s.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("добавить: %d %s", rec.Code, rec.Body.String())
	}
	// и файлом с компьютера (тело — картинка)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/tv/api/admin/image?target=material&id="+id, bytes.NewReader(pngBytes(t)))
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("X-Admin-Pin", testPin)
	s.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("файлом: %d %s", rec.Code, rec.Body.String())
	}
	info := get(t, s, "/tv/api/info?id="+id)
	mats := info["materials"].([]any)
	if len(mats) != 2 {
		t.Fatalf("материалы в инфо: %v", info["materials"])
	}
	u := mats[0].(map[string]any)["url"].(string)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", u, nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("картинка: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", strings.Replace(u, "&n=", "&n=../", 1), nil))
	if rec.Code != 403 {
		t.Fatalf("путь в имени: %d", rec.Code)
	}
	// каталог и обход файлов скрытую папку не видят
	walk(t, s)
	for p := range s.index.files {
		if strings.Contains(p, materialsDirName) {
			t.Fatalf("материалы в индексе: %s", p)
		}
	}

	neu, err := s.renamePath(film, "Фильм (2020).mkv")
	if err != nil {
		t.Fatal(err)
	}
	if n := materialNames(neu); len(n) != 2 {
		t.Fatalf("после переименования: %v", n)
	}
	nid := s.remember(neu)
	code, _ := adminCall(t, s, "DELETE", "/api/admin/materials?id="+nid+"&n="+materialNames(neu)[0], testPin, nil)
	if code != 200 || len(materialNames(neu)) != 1 {
		t.Fatalf("удалить: %d %v", code, materialNames(neu))
	}
	// в корзину — вместе с фильмом, и обратно
	e, err := s.trashPath(neu)
	if err != nil {
		t.Fatal(err)
	}
	if exists(materialsDir(neu, false)) {
		t.Fatal("материалы остались на месте фильма")
	}
	if _, err := s.restoreTrash(e.ID); err != nil {
		t.Fatal(err)
	}
	if len(materialNames(neu)) != 1 {
		t.Fatal("материалы не вернулись из корзины")
	}
}

// Слайд-шоу на паузе: свои материалы главнее папки «Фото», без них — папка.
func TestPauseShowSources(t *testing.T) {
	s, photos, films := filesLibrary(t)
	show := filepath.Join(films, "Show")
	ep := filepath.Join(show, "S01", "E01.mkv")
	_ = os.MkdirAll(filepath.Dir(ep), 0o755)
	_ = os.WriteFile(ep, []byte("x"), 0o644)
	epID := s.remember(ep)

	out := get(t, s, "/tv/api/pauseshow?id="+epID)
	if out["on"] != true || out["minutes"].(float64) != 10 || len(out["urls"].([]any)) != 0 {
		t.Fatalf("по умолчанию: %v", out)
	}
	s.looks.update(func(v *looks) { v.PauseAlbum = filepath.Join(photos, "Trip") })
	out = get(t, s, "/tv/api/pauseshow?id="+epID)
	if out["source"] != "album:Trip" || len(out["urls"].([]any)) != 1 {
		t.Fatalf("папка: %v", out)
	}
	src := filepath.Join(t.TempDir(), "a.png")
	_ = os.WriteFile(src, pngBytes(t), 0o644)
	if _, err := addMaterial(src, show); err != nil {
		t.Fatal(err)
	}
	out = get(t, s, "/tv/api/pauseshow?id="+epID)
	if out["source"] != "materials" || !strings.Contains(out["urls"].([]any)[0].(string), "/material/") {
		t.Fatalf("материалы сериала: %v", out)
	}
	off := false
	s.looks.update(func(v *looks) { v.PauseShow = &off })
	if out = get(t, s, "/tv/api/pauseshow?id="+epID); out["on"] != false {
		t.Fatalf("выключено: %v", out)
	}
}

// Фильмография: по идентификатору Wikidata, а у старых описаний — по имени.
func TestPersonFilmography(t *testing.T) {
	s, _, films := filesLibrary(t)
	film := filepath.Join(films, "Film (2020).mkv")
	show := filepath.Join(films, "Show")
	_ = os.MkdirAll(filepath.Join(show, "S01"), 0o755)
	_ = os.WriteFile(filepath.Join(show, "S01", "E01.mkv"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(show, "S01", "E02.mkv"), []byte("x"), 0o644)
	_ = writeInfo(film, &itemInfo{Title: "Film", Cast: []castMember{{"Том Харди", "Q208026"}}, CastAt: 1})
	_ = writeInfo(show, &itemInfo{Title: "Show", Actors: []string{"Том Харди", "Кто-то"}})
	// каталог раздаёт id плиток
	get(t, s, "/tv/api/catalog")

	out := get(t, s, "/tv/api/person?qid=Q208026&name="+"Том%20Харди")
	if len(out["ids"].([]any)) != 2 || out["name"] != "Том Харди" || !strings.HasSuffix(out["photo"].(string), "/person/Q208026") {
		t.Fatalf("по qid и имени: %v", out)
	}
	out = get(t, s, "/tv/api/person?qid=Q1")
	if len(out["ids"].([]any)) != 0 {
		t.Fatalf("чужой qid: %v", out)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/tv/person/Qabc", nil))
	if rec.Code != 404 {
		t.Fatalf("плохой qid: %d", rec.Code)
	}
}
