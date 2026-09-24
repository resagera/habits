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
	"strings"
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
	album, n, err := extractAlbumZip(zp, root, "a", "music", false)
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

// Альбом, разложенный по дискам: одна плитка на альбом, подпапки — частями,
// «Играть всё» собирает их по порядку.
func TestMusicAlbumWithDiscs(t *testing.T) {
	s, _, music, _ := looksServer(t)
	for _, f := range []string{"Концерт/CD1/01.mp3", "Концерт/CD1/02.mp3", "Концерт/CD2/01.mp3"} {
		p := filepath.Join(music, f)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
	}
	var album *catTile
	for i, t2 := range s.buildCatalog().Music {
		if t2.Name == "Концерт" {
			album = &s.buildCatalog().Music[i]
		}
		if t2.Name == "CD1" || t2.Name == "CD2" {
			t.Fatalf("диски не должны быть отдельными плитками: %+v", t2)
		}
	}
	if album == nil || album.Count != 3 || len(album.Seasons) != 2 ||
		album.Seasons[0].Name != "CD1" || album.Seasons[0].Count != 2 {
		t.Fatalf("плитка альбома с дисками: %+v", album)
	}
	// «Играть всё» — треки обоих дисков по порядку
	_, out := call(t, s, "GET", "/api/tracks?id="+s.remember(filepath.Join(music, "Концерт")), nil)
	items := out["items"].([]any)
	if len(items) != 3 || items[0].(map[string]any)["url"] == "" {
		t.Fatalf("треки альбома: %v", items)
	}
	// отдельный диск играется как обычная папка
	_, out = call(t, s, "GET", "/api/tracks?id="+s.remember(filepath.Join(music, "Концерт", "CD2")), nil)
	if len(out["items"].([]any)) != 1 {
		t.Fatalf("треки диска: %v", out["items"])
	}
}

// ZIP с фото и видео: раскладывается альбомом в библиотеку «Фото и видео»,
// подпапки сохраняются, посторонние файлы отбрасываются.
func TestPhotoZipUpload(t *testing.T) {
	s, _, _, photo := looksServer(t)
	jpeg1 := func() []byte {
		p := filepath.Join(t.TempDir(), "x.jpg")
		writeTestJPEG(t, p, 40, 30)
		b, _ := os.ReadFile(p)
		return b
	}()
	archive := makeZip(t, map[string]string{
		"Поездка/День 1/IMG_1.jpg": string(jpeg1), "Поездка/День 2/IMG_2.jpg": string(jpeg1),
		"Поездка/VID_1.mp4": "video", "Поездка/notes.txt": "junk",
	}, false)
	code, out := adminCall(t, s, "POST", "/api/admin/upload", testPin, map[string]any{"name": "Поездка.zip", "size": len(archive)})
	if code != http.StatusOK {
		t.Fatalf("начало загрузки: %d %v", code, out)
	}
	id := out["id"].(string)
	req := httptest.NewRequest("PUT", "/tv/api/admin/upload/"+id+"?offset=0", bytes.NewReader(archive))
	req.Header.Set("X-Admin-Pin", testPin)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("кусок: %d %s", rec.Code, rec.Body.String())
	}
	code, out = adminCall(t, s, "POST", "/api/admin/upload/"+id+"/finish?target=photo-zip", testPin, nil)
	if code != http.StatusOK || out["album"] != "Поездка" || out["tracks"].(float64) != 3 {
		t.Fatalf("распаковка: %d %v", code, out)
	}
	if !exists(filepath.Join(photo, "Поездка", "День 1", "IMG_1.jpg")) || !exists(filepath.Join(photo, "Поездка", "VID_1.mp4")) {
		t.Fatal("файлы не на месте")
	}
	if exists(filepath.Join(photo, "Поездка", "notes.txt")) {
		t.Fatal("посторонний файл распаковался")
	}
	var album *catTile
	for i, tile := range s.buildCatalog().Photos {
		if tile.Name == "Поездка" {
			album = &s.buildCatalog().Photos[i]
		}
	}
	if album == nil || album.Count != 3 {
		t.Fatalf("альбом в каталоге: %+v", album)
	}
	// в библиотеке музыки такому архиву делать нечего
	code, out = adminCall(t, s, "POST", "/api/admin/upload", testPin, map[string]any{"name": "Поездка.zip", "size": len(archive)})
	id = out["id"].(string)
	req = httptest.NewRequest("PUT", "/tv/api/admin/upload/"+id+"?offset=0", bytes.NewReader(archive))
	req.Header.Set("X-Admin-Pin", testPin)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if code, out := adminCall(t, s, "POST", "/api/admin/upload/"+id+"/finish?target=music-zip", testPin, nil); code != http.StatusBadRequest ||
		!strings.Contains(out["error"].(string), "нет треков") {
		t.Fatalf("архив с фото как музыка: %d %v", code, out)
	}
}

// upload — загрузка частями через админку: возвращает ответ на finish.
func upload(t *testing.T, s *server, data []byte, name, finishQuery string) (int, map[string]any) {
	t.Helper()
	code, out := adminCall(t, s, "POST", "/api/admin/upload", testPin, map[string]any{"name": name, "size": len(data)})
	if code != http.StatusOK {
		t.Fatalf("начало загрузки: %d %v", code, out)
	}
	id := out["id"].(string)
	req := httptest.NewRequest("PUT", "/tv/api/admin/upload/"+id+"?offset=0", bytes.NewReader(data))
	req.Header.Set("X-Admin-Pin", testPin)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("кусок: %d %s", rec.Code, rec.Body.String())
	}
	return adminCall(t, s, "POST", "/api/admin/upload/"+id+"/finish?"+finishQuery, testPin, nil)
}

// Дозагрузка в существующий альбом: отдельным файлом и архивом; занятое имя
// получает «(2)», чужое для библиотеки не берётся.
func TestUploadIntoAlbum(t *testing.T) {
	s, _, music, photo := looksServer(t)
	album := filepath.Join(music, "Альбом")
	id := s.remember(album)
	if code, out := upload(t, s, []byte("track"), "Новый трек.mp3", "target=into&id="+id); code != http.StatusOK || out["added"].(float64) != 1 {
		t.Fatalf("файл в альбом: %d %v", code, out)
	}
	if !exists(filepath.Join(album, "Новый трек.mp3")) {
		t.Fatal("трек не добавился")
	}
	// второй раз тем же именем — рядом, а не поверх
	if _, out := upload(t, s, []byte("track2"), "Новый трек.mp3", "target=into&id="+id); out["name"] != "Новый трек (2).mp3" {
		t.Fatalf("повтор имени: %v", out)
	}
	// текст в музыку не годится
	if code, out := upload(t, s, []byte("x"), "заметка.txt", "target=into&id="+id); code != http.StatusBadRequest ||
		!strings.Contains(out["error"].(string), "не для этой библиотеки") {
		t.Fatalf("чужой файл: %d %v", code, out)
	}
	// архив в альбом: папка-обёртка снимается, подпапки остаются
	archive := makeZip(t, map[string]string{"Ещё/01.mp3": "a", "Ещё/Бонус/02.mp3": "b", "Ещё/readme.txt": "junk"}, false)
	if code, out := upload(t, s, archive, "Ещё.zip", "target=into-zip&id="+id); code != http.StatusOK || out["added"].(float64) != 2 {
		t.Fatalf("архив в альбом: %d %v", code, out)
	}
	if !exists(filepath.Join(album, "01.mp3")) || !exists(filepath.Join(album, "Бонус", "02.mp3")) ||
		exists(filepath.Join(album, "readme.txt")) {
		t.Fatal("архив разложился не так")
	}
	// в альбом фото — снимки, а не треки
	jpegPath := filepath.Join(t.TempDir(), "x.jpg")
	writeTestJPEG(t, jpegPath, 40, 30)
	jpegBytes, _ := os.ReadFile(jpegPath)
	photoAlbum := s.remember(filepath.Join(photo, "Свадьба"))
	if code, _ := upload(t, s, jpegBytes, "IMG_9.jpg", "target=into&id="+photoAlbum); code != http.StatusOK {
		t.Fatalf("снимок в альбом фото: %d", code)
	}
	if code, _ := upload(t, s, []byte("x"), "Трек.mp3", "target=into&id="+photoAlbum); code != http.StatusBadRequest {
		t.Fatalf("трек в альбом фото: %d", code)
	}
}

// Сборка альбома: новая папка, папка внутри неё, копирование и перенос треков.
func TestMkdirAndTransfer(t *testing.T) {
	s, _, music, _ := looksServer(t)
	src := filepath.Join(music, "Альбом")
	for _, f := range []string{"02.mp3", "03.mp3"} {
		_ = os.WriteFile(filepath.Join(src, f), []byte("x"), 0o644)
	}
	code, out := adminCall(t, s, "POST", "/api/admin/mkdir", testPin, map[string]string{"name": "Сборник"})
	if code != http.StatusOK {
		t.Fatalf("новый альбом: %d %v", code, out)
	}
	target := out["id"].(string)
	if !exists(filepath.Join(music, "Сборник")) {
		t.Fatal("папка не создалась в корне музыки")
	}
	if code, _ := adminCall(t, s, "POST", "/api/admin/mkdir", testPin, map[string]string{"name": "Сборник"}); code != http.StatusBadRequest {
		t.Fatal("занятое имя должно отвергаться")
	}
	// папка внутри альбома
	code, out = adminCall(t, s, "POST", "/api/admin/mkdir", testPin, map[string]string{"name": "CD1", "id": target})
	if code != http.StatusOK || !exists(filepath.Join(music, "Сборник", "CD1")) {
		t.Fatalf("папка внутри: %d %v", code, out)
	}
	inner := out["id"].(string)

	// копия: исходник остаётся
	code, out = adminCall(t, s, "POST", "/api/admin/transfer", testPin,
		map[string]any{"ids": []string{s.remember(filepath.Join(src, "01.mp3"))}, "to": inner})
	if code != http.StatusOK || out["done"].(float64) != 1 {
		t.Fatalf("копирование: %d %v", code, out)
	}
	if !exists(filepath.Join(src, "01.mp3")) || !exists(filepath.Join(music, "Сборник", "CD1", "01.mp3")) {
		t.Fatal("копия не там или исходник пропал")
	}
	// перенос: исходника больше нет
	code, out = adminCall(t, s, "POST", "/api/admin/transfer", testPin,
		map[string]any{"ids": []string{s.remember(filepath.Join(src, "02.mp3"))}, "to": inner, "move": true})
	if code != http.StatusOK || out["done"].(float64) != 1 {
		t.Fatalf("перенос: %d %v", code, out)
	}
	if exists(filepath.Join(src, "02.mp3")) || !exists(filepath.Join(music, "Сборник", "CD1", "02.mp3")) {
		t.Fatal("перенос не состоялся")
	}
	// повтор имени — рядом; свой же файл на месте — пропускается
	_ = os.WriteFile(filepath.Join(src, "02.mp3"), []byte("y"), 0o644)
	adminCall(t, s, "POST", "/api/admin/transfer", testPin,
		map[string]any{"ids": []string{s.remember(filepath.Join(src, "02.mp3"))}, "to": inner})
	if !exists(filepath.Join(music, "Сборник", "CD1", "02 (2).mp3")) {
		t.Fatal("занятое имя не обошлось")
	}
	code, out = adminCall(t, s, "POST", "/api/admin/transfer", testPin,
		map[string]any{"ids": []string{s.remember(filepath.Join(music, "Сборник", "CD1", "01.mp3"))}, "to": inner})
	if code != http.StatusOK || out["done"].(float64) != 0 || len(out["skipped"].([]any)) != 1 {
		t.Fatalf("файл уже в этой папке: %v", out)
	}
	// папку целиком так не перенести, и в корзину ход закрыт
	e, _ := s.trashPath(filepath.Join(src, "03.mp3"))
	code, out = adminCall(t, s, "POST", "/api/admin/transfer", testPin,
		map[string]any{"ids": []string{s.remember(src), s.remember(filepath.Join(e.Dir, "03.mp3"))}, "to": inner})
	if out["done"].(float64) != 0 || len(out["skipped"].([]any)) != 2 {
		t.Fatalf("папка и корзина: %v", out)
	}
	// в каталоге альбом виден с частями
	var album *catTile
	for i, tile := range s.buildCatalog().Music {
		if tile.Name == "Сборник" {
			album = &s.buildCatalog().Music[i]
		}
	}
	if album == nil || album.Count != 3 || len(album.Seasons) != 1 {
		t.Fatalf("собранный альбом: %+v", album)
	}
}

// Альбом со своими треками и подпапкой: одна плитка, подпапка — её часть.
func TestMusicAlbumMixed(t *testing.T) {
	s, _, music, _ := looksServer(t)
	album := filepath.Join(music, "Альбом")
	_ = os.MkdirAll(filepath.Join(album, "Бонусы"), 0o755)
	_ = os.WriteFile(filepath.Join(album, "Бонусы", "Живьём.mp3"), []byte("x"), 0o644)
	// треки прямо в корне библиотеки — своя плитка, но альбомы всё равно видны
	_ = os.WriteFile(filepath.Join(music, "Случайный.mp3"), []byte("x"), 0o644)
	names := map[string]catTile{}
	for _, tile := range s.buildCatalog().Music {
		names[tile.Name] = tile
	}
	if _, ok := names["Бонусы"]; ok {
		t.Fatalf("подпапка альбома не должна быть плиткой: %+v", names)
	}
	a := names["Альбом"]
	if a.Count != 2 || len(a.Seasons) != 1 || a.Seasons[0].Name != "Бонусы" {
		t.Fatalf("плитка альбома: %+v", a)
	}
	if lib, ok := names["Музыка"]; !ok || lib.Count != 1 {
		t.Fatalf("треки в корне библиотеки: %+v", names)
	}
}
