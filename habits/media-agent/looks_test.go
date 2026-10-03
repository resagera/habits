package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestJPEG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, nil)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Библиотеки видео, музыки и фото — для обложек и фона.
func looksServer(t *testing.T) (*server, string, string, string) {
	t.Helper()
	s, video := adminServer(t)
	music, photo := t.TempDir(), t.TempDir()
	_ = os.MkdirAll(filepath.Join(music, "Альбом"), 0o755)
	_ = os.WriteFile(filepath.Join(music, "Альбом", "01.mp3"), []byte("x"), 0o644)
	writeTestJPEG(t, filepath.Join(photo, "Свадьба", "IMG_1.jpg"), 900, 600)
	writeTestJPEG(t, filepath.Join(photo, "Свадьба", "IMG_2.jpg"), 900, 600)
	s.cfg.roots = append(s.cfg.roots,
		library{ID: shortID(music), Path: music, Title: "Музыка", Kind: "music"},
		library{ID: shortID(photo), Path: photo, Title: "Фото", Kind: "photo"})
	return s, video, music, photo
}

// Отметка заставки в серии первого сезона видна из серии второго: запись одна
// на сериал.
func TestMarksPerSeries(t *testing.T) {
	s, root := adminServer(t)
	put := func(body map[string]any) map[string]any {
		data, _ := json.Marshal(body)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/tv/api/marks", bytes.NewReader(data)))
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT marks: %d %s", rec.Code, rec.Body.String())
		}
		out := map[string]any{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out["marks"].(map[string]any)
	}
	e1 := s.remember(filepath.Join(root, "Show", "S01", "E01.mp4"))
	e2 := s.remember(filepath.Join(root, "Show", "S02", "E01.mp4"))
	put(map[string]any{"file": e1, "intro_start": 30.0, "intro_end": 95.0, "credits_from_end": 60.0})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tv/api/marks?file="+e2, nil))
	var got struct {
		Kind  string      `json:"kind"`
		Marks seriesMarks `json:"marks"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Kind != "series" || got.Marks.IntroStart != 30 || got.Marks.IntroEnd != 95 || got.Marks.CreditsFromEnd != 60 {
		t.Fatalf("отметки из другого сезона: %+v", got)
	}
	// конец заставки раньше начала — начало сбрасывается в ноль
	if m := put(map[string]any{"file": e2, "intro_end": 20.0}); m["intro_start"].(float64) != 0 {
		t.Fatalf("начало после конца не сброшено: %v", m)
	}
	if m := put(map[string]any{"file": e2, "reset": true}); m["intro_end"].(float64) != 0 || m["credits_from_end"].(float64) != 0 {
		t.Fatalf("сброс: %v", m)
	}
	if _, ok := s.marks.items[s.remember(filepath.Join(root, "Show"))]; ok {
		t.Fatal("пустая запись после сброса осталась")
	}
}

// Отметки трёх уровней: общая на сериал, уточнение на сезон и на серию.
func TestMarksScopes(t *testing.T) {
	s, root := adminServer(t)
	e1 := s.remember(filepath.Join(root, "Show", "S01", "E01.mp4"))
	e2 := s.remember(filepath.Join(root, "Show", "S01", "E02.mp4"))
	s2 := s.remember(filepath.Join(root, "Show", "S02", "E01.mp4"))
	put := func(file string, body map[string]any) map[string]any {
		body["file"] = file
		data, _ := json.Marshal(body)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/tv/api/marks", bytes.NewReader(data)))
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT marks: %d %s", rec.Code, rec.Body.String())
		}
		out := map[string]any{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	get := func(file string) map[string]any {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tv/api/marks?file="+file, nil))
		out := map[string]any{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	num := func(m map[string]any, path ...string) float64 {
		cur := any(m)
		for _, k := range path {
			cur = cur.(map[string]any)[k]
		}
		if cur == nil {
			return -1
		}
		return cur.(float64)
	}
	// общая отметка сериала — видна из любой серии
	put(e1, map[string]any{"intro_start": 10.0, "intro_end": 40.0, "credits_from_end": 60.0})
	if num(get(s2), "marks", "intro_end") != 40 || num(get(s2), "marks", "credits_from_end") != 60 {
		t.Fatalf("общая отметка не дошла до другого сезона: %v", get(s2)["marks"])
	}
	// у второго сезона заставка длиннее — уточняем на сезон
	put(s2, map[string]any{"scope": "season", "intro_start": 5.0, "intro_end": 90.0})
	if num(get(s2), "marks", "intro_end") != 90 || num(get(s2), "marks", "credits_from_end") != 60 {
		t.Fatalf("сезонная отметка: %v", get(s2)["marks"])
	}
	if num(get(e1), "marks", "intro_end") != 40 {
		t.Fatalf("первый сезон не должен меняться: %v", get(e1)["marks"])
	}
	// у одной серии заставки нет вовсе — отмечаем только её
	put(e2, map[string]any{"scope": "episode", "intro_start": 0.0, "intro_end": 3.0})
	if num(get(e2), "marks", "intro_end") != 3 || num(get(e1), "marks", "intro_end") != 40 {
		t.Fatalf("отметка серии: %v и %v", get(e2)["marks"], get(e1)["marks"])
	}
	// после титров есть сцена: «титры кончаются» ближе к концу, чем начинаются
	put(e1, map[string]any{"credits_from_end": 90.0, "credits_end_from_end": 20.0})
	if num(get(e1), "marks", "credits_end_from_end") != 20 {
		t.Fatalf("конец титров: %v", get(e1)["marks"])
	}
	// конец «позже» начала — значит, до конца серии
	put(e1, map[string]any{"credits_from_end": 60.0, "credits_end_from_end": 80.0})
	if num(get(e1), "marks", "credits_end_from_end") != 0 {
		t.Fatalf("неверный конец титров не сброшен: %v", get(e1)["marks"])
	}
	// у фильма сезона нет — season отвергается
	film := s.remember(filepath.Join(root, "Film.mp4"))
	data, _ := json.Marshal(map[string]any{"file": film, "scope": "season", "intro_end": 5.0})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/tv/api/marks", bytes.NewReader(data)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("сезон у фильма: %d", rec.Code)
	}
	// переименовали сериал — отметки едут следом
	if code, out := adminCall(t, s, "POST", "/api/admin/rename", testPin,
		map[string]string{"id": s.remember(filepath.Join(root, "Show")), "name": "Fauda"}); code != http.StatusOK {
		t.Fatalf("переименование: %d %v", code, out)
	}
	moved := s.remember(filepath.Join(root, "Fauda", "S02", "E01.mp4"))
	if num(get(moved), "marks", "intro_end") != 90 {
		t.Fatalf("после переименования сезонная отметка потерялась: %v", get(moved))
	}
	// сброс на уровне сезона возвращает общую
	put(moved, map[string]any{"scope": "season", "reset": true})
	if num(get(moved), "marks", "intro_end") != 40 {
		t.Fatalf("после сброса сезона должна остаться общая: %v", get(moved)["marks"])
	}
	// в одной серии после титров есть сцена: отмечаем только её конец, начало
	// титров записано на сериал — оно должно донестись в запись серии
	e3 := s.remember(filepath.Join(root, "Fauda", "S02", "E01.mp4"))
	put(e3, map[string]any{"scope": "episode", "credits_end_from_end": 25.0})
	if num(get(e3), "episode", "credits_from_end") != 60 || num(get(e3), "marks", "credits_end_from_end") != 25 {
		t.Fatalf("конец титров у серии без своего начала: %v", get(e3)["episode"])
	}
	// фильм прямо в корне: отметки едут за переименованием самого файла
	put(film, map[string]any{"intro_start": 1.0, "intro_end": 20.0})
	if code, out := adminCall(t, s, "POST", "/api/admin/rename", testPin,
		map[string]string{"id": film, "name": "Kino.mp4"}); code != http.StatusOK {
		t.Fatalf("переименование фильма: %d %v", code, out)
	}
	if kino := s.remember(filepath.Join(root, "Kino.mp4")); num(get(kino), "marks", "intro_end") != 20 {
		t.Fatalf("отметки фильма не переехали: %v", get(kino))
	}
}

// Главы файла: «Opening» в начале и «Ending» в конце становятся отметками, а
// главы с обычными названиями — нет.
func TestMarksFromChapters(t *testing.T) {
	chs := []chapter{
		{Start: 0, End: 30, Title: "Recap"},
		{Start: 30, End: 120, Title: "Opening Credits"},
		{Start: 120, End: 1300, Title: "Chapter 3"},
		{Start: 1300, End: 1380, Title: "Ending Credits"},
		{Start: 1380, End: 1400, Title: "Post Credits Scene"},
	}
	m := marksFromChapters(chs, 1400)
	if m.IntroStart != 30 || m.IntroEnd != 120 {
		t.Fatalf("заставка из глав: %+v", m)
	}
	if m.CreditsFromEnd != 100 || m.CreditsEndFromEnd != 20 {
		t.Fatalf("титры из глав: %+v", m)
	}
	// заставка в середине серии — это не заставка
	late := marksFromChapters([]chapter{{Start: 800, End: 900, Title: "Intro to the war"}}, 1400)
	if !late.empty() {
		t.Fatalf("глава из середины не должна становиться заставкой: %+v", late)
	}
	// титры до самого конца — «после них ничего нет»
	tail := marksFromChapters([]chapter{{Start: 1340, End: 1400, Title: "Титры"}}, 1400)
	if tail.CreditsFromEnd != 60 || tail.CreditsEndFromEnd != 0 {
		t.Fatalf("титры до конца: %+v", tail)
	}
}

// Обложка из снимка библиотеки: музыкальной папке — скрытый .cover.jpg внутрь,
// плитка получает постер; альбому фото — вместо первого снимка.
func TestCoverFromPhoto(t *testing.T) {
	s, _, music, photo := looksServer(t)
	cat := s.buildCatalog()
	if len(cat.Music) != 1 || cat.Music[0].Poster != "" {
		t.Fatalf("у музыки без обложки не должно быть постера: %+v", cat.Music)
	}
	firstThumb := cat.Photos[0].Poster
	photoID := s.remember(filepath.Join(photo, "Свадьба", "IMG_2.jpg"))
	for _, target := range []string{filepath.Join(music, "Альбом"), filepath.Join(photo, "Свадьба")} {
		code, out := adminCall(t, s, "POST", "/api/admin/image?target=cover&id="+s.remember(target), testPin,
			map[string]string{"photo_id": photoID})
		if code != http.StatusOK {
			t.Fatalf("обложка для %s: %d %v", target, code, out)
		}
		if !fileReady(filepath.Join(target, coverFile)) {
			t.Fatalf("нет %s в %s", coverFile, target)
		}
	}
	cat = s.buildCatalog()
	if cat.Music[0].Poster == "" || !strings.Contains(cat.Music[0].Poster, "&v=") {
		t.Fatalf("плитка музыки без постера (или без версии в ссылке): %q", cat.Music[0].Poster)
	}
	if cat.Photos[0].Poster == firstThumb || !strings.Contains(cat.Photos[0].Poster, "/poster/") {
		t.Fatalf("альбом фото по-прежнему с первым снимком: %q", cat.Photos[0].Poster)
	}
	// обложка скрытая — в альбоме лишним снимком не стала
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tv/api/photos?id="+s.remember(filepath.Join(photo, "Свадьба")), nil))
	var album struct {
		Photos []photoOut `json:"photos"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &album)
	if len(album.Photos) != 2 {
		t.Fatalf("в альбоме %d элементов, ожидалось 2", len(album.Photos))
	}
	// постер отдаётся без поиска в интернете
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, cat.Music[0].Poster, nil)
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("постер музыки: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	// убрать обложку
	if code, _ := adminCall(t, s, "DELETE", "/api/admin/image?target=cover&id="+s.remember(filepath.Join(music, "Альбом")), testPin, nil); code != http.StatusOK {
		t.Fatalf("удаление обложки: %d", code)
	}
	if s.buildCatalog().Music[0].Poster != "" {
		t.Fatal("после удаления обложка осталась")
	}
}

// Фон — файл из браузера (PNG), уменьшенный под экран; страница получает
// ссылку с версией, настройки размытия меняются отдельно.
func TestBackgroundUpload(t *testing.T) {
	s, _, _, _ := looksServer(t)
	img := image.NewRGBA(image.Rect(0, 0, 3000, 2000))
	for i := range img.Pix {
		img.Pix[i] = 90
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	req := httptest.NewRequest(http.MethodPost, "/tv/api/admin/image?target=background", bytes.NewReader(buf.Bytes()))
	req.Header.Set("X-Admin-Pin", testPin)
	req.Header.Set("Content-Type", "image/png")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("загрузка фона: %d %s", rec.Code, rec.Body.String())
	}
	f, _ := os.Open(s.backgroundPath())
	cfg, err := jpeg.DecodeConfig(f)
	f.Close()
	if err != nil || cfg.Width != 1620 || cfg.Height != 1080 {
		t.Fatalf("фон %d×%d (%v), ожидалось 1620×1080", cfg.Width, cfg.Height, err)
	}
	if code, _ := adminCall(t, s, "POST", "/api/admin/settings", testPin, map[string]any{"bg_blur": 99, "arrows": "volume"}); code != http.StatusOK {
		t.Fatal("настройки не сохранились")
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tv/api/settings", nil))
	var st struct {
		Background string `json:"background"`
		BgBlur     int    `json:"bg_blur"`
		Arrows     string `json:"arrows"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if !strings.HasPrefix(st.Background, "/tv/background?v=") || st.BgBlur != 40 || st.Arrows != "volume" {
		t.Fatalf("настройки для страницы: %+v", st)
	}
	// мусор вместо картинки — внятный отказ
	req = httptest.NewRequest(http.MethodPost, "/tv/api/admin/image?target=background", strings.NewReader("not an image"))
	req.Header.Set("X-Admin-Pin", testPin)
	req.Header.Set("Content-Type", "image/png")
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("мусор вместо картинки: %d", rec.Code)
	}
	if code, _ := adminCall(t, s, "DELETE", "/api/admin/image?target=background", testPin, nil); code != http.StatusOK || fileReady(s.backgroundPath()) {
		t.Fatal("фон не убрался")
	}
}

// Ролик с телефона в альбоме: элемент полосы с кадром вместо превью и ссылкой
// на сам файл.
func TestAlbumVideo(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("нет ffmpeg")
	}
	s, _, _, photo := looksServer(t)
	clip := filepath.Join(photo, "Свадьба", "VID_1.mp4")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=10",
		"-t", "2", "-pix_fmt", "yuv420p", clip).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg не собрал ролик: %s", out)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tv/api/photos?id="+s.remember(filepath.Join(photo, "Свадьба")), nil))
	var album struct {
		Photos []photoOut `json:"photos"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &album)
	var vid *photoOut
	for i := range album.Photos {
		if album.Photos[i].Video {
			vid = &album.Photos[i]
		}
	}
	if len(album.Photos) != 3 || vid == nil || !strings.Contains(vid.URL, "/media/") {
		t.Fatalf("ролик в альбоме: %+v", album.Photos)
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, vid.Thumb, nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("кадр ролика: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if cfg, err := jpeg.DecodeConfig(bytes.NewReader(rec.Body.Bytes())); err != nil || cfg.Width != 320 {
		t.Fatalf("кадр ролика %v %v", cfg, err)
	}
}
