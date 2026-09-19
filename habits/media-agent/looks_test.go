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
