package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func call(t *testing.T, s *server, method, url string, body any) (int, map[string]any) {
	t.Helper()
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, "/tv"+url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// Плейлист из трека одной папки и целой другой папки; повтор не дублируется,
// пропавший файл пропускается, плейлист виден в каталоге.
func TestPlaylists(t *testing.T) {
	s, _, music, _ := looksServer(t)
	_ = os.MkdirAll(filepath.Join(music, "Второй", "CD2"), 0o755)
	for _, f := range []string{"Второй/01.mp3", "Второй/CD2/02.mp3", "Альбом/02.mp3"} {
		_ = os.WriteFile(filepath.Join(music, f), []byte("x"), 0o644)
	}
	code, out := call(t, s, "POST", "/api/playlists", map[string]any{"name": "Вечер",
		"ids": []string{s.remember(filepath.Join(music, "Альбом", "01.mp3"))}})
	if code != http.StatusOK {
		t.Fatalf("создание: %d %v", code, out)
	}
	id := out["playlist"].(map[string]any)["id"].(string)
	code, out = call(t, s, "POST", "/api/playlists/"+id+"/add", map[string]any{"ids": []string{
		s.remember(filepath.Join(music, "Второй")), s.remember(filepath.Join(music, "Альбом", "01.mp3"))}})
	if code != http.StatusOK || out["added"].(float64) != 2 || out["count"].(float64) != 3 {
		t.Fatalf("добавление папки: %d %v", code, out)
	}
	_ = os.Remove(filepath.Join(music, "Второй", "01.mp3"))
	_, out = call(t, s, "GET", "/api/playlists/"+id, nil)
	items := out["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["name"] != "01.mp3" || items[1].(map[string]any)["url"] == "" {
		t.Fatalf("треки плейлиста: %v", items)
	}
	if cat := s.buildCatalog(); len(cat.Playlists) != 1 || cat.Playlists[0].Name != "Вечер" || cat.Playlists[0].Kind != "playlist" {
		t.Fatalf("плейлист в каталоге: %+v", cat.Playlists)
	}
	call(t, s, "POST", "/api/playlists/"+id+"/remove", map[string]string{"id": items[0].(map[string]any)["id"].(string)})
	call(t, s, "POST", "/api/playlists/"+id+"/rename", map[string]string{"name": "Ночь"})
	if code, _ := call(t, s, "DELETE", "/api/playlists/"+id, nil); code != http.StatusOK || len(s.buildCatalog().Playlists) != 0 {
		t.Fatal("плейлист не удалился")
	}
}

func makeZip(t *testing.T, files map[string]string, nonUTF8 bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		h := &zip.FileHeader{Name: name, Method: zip.Store, NonUTF8: nonUTF8}
		w, _ := zw.CreateHeader(h)
		_, _ = w.Write([]byte(body))
	}
	_ = zw.Close()
	return buf.Bytes()
}

// ZIP частями через админку: одна папка в архиве — альбом, мусор и чужие
// файлы не распаковываются, «../» не выпускается, занятое имя — «(2)».
func TestMusicZipUpload(t *testing.T) {
	s, _, music, _ := looksServer(t)
	archive := makeZip(t, map[string]string{
		"Мой альбом/01 Песня.mp3": "a", "Мой альбом/02.flac": "b", "Мой альбом/cover.jpg": "c",
		"Мой альбом/readme.txt": "junk", "__MACOSX/Мой альбом/._01.mp3": "junk", "Мой альбом/../../evil.mp3": "evil",
	}, false)
	upload := func(data []byte, name string) (int, map[string]any) {
		code, out := adminCall(t, s, "POST", "/api/admin/upload", testPin, map[string]any{"name": name, "size": len(data)})
		if code != http.StatusOK {
			t.Fatalf("начало загрузки: %d %v", code, out)
		}
		id := out["id"].(string)
		for off := 0; off < len(data); off += 100 {
			end := min(off+100, len(data))
			req := httptest.NewRequest("PUT", fmt.Sprintf("/tv/api/admin/upload/%s?offset=%d", id, off), bytes.NewReader(data[off:end]))
			req.Header.Set("X-Admin-Pin", testPin)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("кусок %d: %d %s", off, rec.Code, rec.Body.String())
			}
		}
		return adminCall(t, s, "POST", "/api/admin/upload/"+id+"/finish?target=music-zip", testPin, nil)
	}
	code, out := upload(archive, "archive.zip")
	if code != http.StatusOK || out["album"] != "Мой альбом" || out["tracks"].(float64) != 2 {
		t.Fatalf("распаковка: %d %v", code, out)
	}
	for _, f := range []string{"01 Песня.mp3", "02.flac", "cover.jpg"} {
		if !exists(filepath.Join(music, "Мой альбом", f)) {
			t.Fatalf("нет %s", f)
		}
	}
	if exists(filepath.Join(music, "Мой альбом", "readme.txt")) || exists(filepath.Join(music, "evil.mp3")) ||
		exists(filepath.Join(filepath.Dir(music), "evil.mp3")) {
		t.Fatal("распаковалось лишнее или вышло за папку")
	}
	// второй раз — рядом, а не поверх
	if code, out := upload(archive, "archive.zip"); code != http.StatusOK || out["album"] != "Мой альбом (2)" {
		t.Fatalf("повтор: %d %v", code, out)
	}
	// кусок не с того места отвергается
	code, out = adminCall(t, s, "POST", "/api/admin/upload", testPin, map[string]any{"name": "x.zip", "size": 10})
	req := httptest.NewRequest("PUT", "/tv/api/admin/upload/"+out["id"].(string)+"?offset=5", bytes.NewReader([]byte("12345")))
	req.Header.Set("X-Admin-Pin", testPin)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("кусок не с того места: %d", rec.Code)
	}
	// без треков — внятный отказ
	if code, _ := upload(makeZip(t, map[string]string{"a.txt": "x"}, false), "empty.zip"); code != http.StatusBadRequest {
		t.Fatalf("архив без треков: %d", code)
	}
}

// Архив из Windows: кириллица в CP866 без флага UTF-8.
func TestZipCP866(t *testing.T) {
	name := string([]byte{0x80, 0xAB, 0xEC, 0xA1, 0xAE, 0xAC}) + "/01.mp3" // «Альбом» в CP866
	root := t.TempDir()
	zp := filepath.Join(t.TempDir(), "a.zip")
	_ = os.WriteFile(zp, makeZip(t, map[string]string{name: "x"}, true), 0o644)
	album, n, err := extractMusicZip(zp, root, "a")
	if err != nil || album != "Альбом" || n != 1 {
		t.Fatalf("CP866: %q %d %v", album, n, err)
	}
}

// Статистика: в зачёт идёт только настоящее воспроизведение.
func TestStatsRecord(t *testing.T) {
	st := newStatsStore(t.TempDir())
	o := owner{key: "show", kind: "series", title: "Fauda"}
	t0 := time.Date(2026, 9, 19, 20, 0, 0, 0, time.UTC)
	st.record("e1", o, 100, t0)
	st.record("e1", o, 115, t0.Add(15*time.Second)) // +15 с
	st.record("e1", o, 130, t0.Add(30*time.Second)) // +15 с
	st.record("e1", o, 900, t0.Add(45*time.Second)) // перемотка — не в счёт
	st.record("e1", o, 915, t0.Add(60*time.Second)) // +15 с
	st.record("e1", o, 915, t0.Add(75*time.Second)) // пауза — не в счёт
	days, top, total := st.summary(7, t0)
	if total != 45 || len(top) != 1 || top[0].Title != "Fauda" || len(days) != 7 || days[6].Seconds != 45 {
		t.Fatalf("итог %v, верх %+v, дни %+v", total, top, days)
	}
}

// Описание, поправленное в админке, лежит рядом с постером и ездит вместе с
// сериалом: переименование и корзина.
func TestInfoSidecar(t *testing.T) {
	s, root := adminServer(t)
	show := filepath.Join(root, "Show")
	code, out := adminCall(t, s, "POST", "/api/admin/info", testPin, map[string]any{"id": s.remember(show),
		"info": map[string]any{"year": 2015, "genres": []string{"Триллер", " триллер ", "Драма"}, "description": "Про спецназ"}})
	if code != http.StatusOK || !out["info"].(map[string]any)["edited"].(bool) {
		t.Fatalf("сохранение: %d %v", code, out)
	}
	if g := out["info"].(map[string]any)["genres"].([]any); len(g) != 2 {
		t.Fatalf("жанры не почищены от повторов: %v", g)
	}
	if !exists(filepath.Join(root, "Show"+infoSuffix)) {
		t.Fatal("описание не рядом с постером")
	}
	_, out = adminCall(t, s, "POST", "/api/admin/rename", testPin, map[string]string{"id": s.remember(show), "name": "Fauda"})
	neu := filepath.Join(root, "Fauda")
	if !exists(neu+infoSuffix) || exists(show+infoSuffix) {
		t.Fatal("описание не переехало с сериалом")
	}
	_, got := call(t, s, "GET", "/api/info?id="+s.remember(neu), nil)
	if got["info"].(map[string]any)["description"] != "Про спецназ" {
		t.Fatalf("GET /api/info: %v", got)
	}
	e, err := s.trashPath(neu)
	if err != nil || exists(neu+infoSuffix) || len(e.Moves) != 3 {
		t.Fatalf("корзина без описания: %v %+v", err, e.Moves)
	}
}
