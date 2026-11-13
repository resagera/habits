package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// filesLibrary — библиотека фото со снимком, текстом, архивом и подпапкой,
// плюс видеобиблиотека с фильмом, его постером и описанием рядом.
func filesLibrary(t *testing.T) (*server, string, string) {
	t.Helper()
	photos, films := t.TempDir(), t.TempDir()
	write := func(p string, data []byte) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var img bytes.Buffer
	_ = png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 64, 48)))
	write(filepath.Join(photos, "Trip", "IMG_1.png"), img.Bytes())
	write(filepath.Join(photos, "Trip", "notes.txt"), []byte("привет"))
	write(filepath.Join(photos, "Trip", "pack.zip"), []byte("PK"))
	write(filepath.Join(photos, "Trip", "Day 2", "a.txt"), []byte("12345"))
	write(filepath.Join(films, "Film (2020).mkv"), []byte("x"))
	write(filepath.Join(films, "Film (2020).jpg"), img.Bytes())
	write(filepath.Join(films, "Film (2020).info.json"), []byte("{}"))
	write(filepath.Join(films, "readme.nfo"), []byte{0xcf, 0xf0, 0xe8, 0xe2, 0xe5, 0xf2}) // «Привет» в cp1251
	s := newServer(config{base: "/tv", cache: t.TempDir(), roots: []library{
		{ID: shortID(photos), Path: photos, Title: "Фото", Kind: "photo"},
		{ID: shortID(films), Path: films, Title: "Фильмы", Kind: "video"},
	}})
	return s, photos, films
}

func walk(t *testing.T, s *server) *task {
	t.Helper()
	tk := &task{v: taskView{Lines: []string{}}}
	if err := s.walkIndex(context.Background(), tk); err != nil {
		t.Fatal(err)
	}
	return tk
}

type browseOut struct {
	Items []struct {
		entry
		URL  string `json:"url"`
		View string `json:"view"`
	}
}

func browseAllOf(t *testing.T, s *server, id string) browseOut {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tv/api/browse?all=1&id="+id, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("browse: %d %s", rec.Code, rec.Body.String())
	}
	var out browseOut
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestIndexWalkAndBrowseAll(t *testing.T) {
	s, photos, films := filesLibrary(t)
	walk(t, s)

	rec, ok := s.index.get(filepath.Join(photos, "Trip", "IMG_1.png"))
	if !ok || rec.Kind != "photo" || rec.W != 64 || rec.H != 48 {
		t.Fatalf("снимок в индексе: %+v %v", rec, ok)
	}
	if d, ok := s.index.dir(filepath.Join(photos, "Trip")); !ok || d.Files != 4 {
		t.Fatalf("сводка папки: %+v", d)
	}

	out := browseAllOf(t, s, s.remember(filepath.Join(photos, "Trip")))
	got := map[string]string{}
	for _, it := range out.Items {
		got[it.Name] = it.Kind
		if it.Name == "IMG_1.png" && (it.X == nil || it.X.W != 64 || it.View == "") {
			t.Fatalf("у снимка нет сведений или просмотра: %+v", it)
		}
		if it.Name == "Day 2" && (it.Dir == nil || it.Dir.Files != 1 || it.Dir.Bytes != 5) {
			t.Fatalf("у подпапки нет сводки: %+v", it.Dir)
		}
	}
	for name, kind := range map[string]string{"IMG_1.png": "photo", "notes.txt": "text", "pack.zip": "archive", "Day 2": ""} {
		if k, ok := got[name]; !ok || k != kind {
			t.Fatalf("%s: вид %q (есть: %v), ждали %q", name, k, ok, kind)
		}
	}

	// рядом с фильмом постер и описание — часть плитки, не файлы
	out = browseAllOf(t, s, shortID(films))
	names := []string{}
	for _, it := range out.Items {
		names = append(names, it.Name)
	}
	if !equalStrings(names, []string{"Film (2020).mkv", "readme.nfo"}) {
		t.Fatalf("фильмы: %v", names)
	}
	// обычный /api/browse (визуальный режим) по-прежнему только медиа
	if got := browseNames(t, s, shortID(films), false); !equalStrings(got, []string{"Film (2020).mkv"}) {
		t.Fatalf("визуальный режим: %v", got)
	}
}

func TestViewText(t *testing.T) {
	s, _, films := filesLibrary(t)
	id := s.remember(filepath.Join(films, "readme.nfo"))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tv/text/"+id+"?k="+s.sign(id), nil))
	var out struct{ Text, Enc string }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Text != "Привет" || out.Enc != "cp1251" {
		t.Fatalf("текст: %+v (%d)", out, rec.Code)
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tv/text/"+id+"?k=bad", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("без подписи: %d", rec.Code)
	}
}

func TestFileEditTitleAndRename(t *testing.T) {
	s, photos, _ := filesLibrary(t)
	walk(t, s)
	old := filepath.Join(photos, "Trip", "IMG_1.png")
	post := func(body any) map[string]any {
		b, _ := json.Marshal(body)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/tv/api/file/edit", bytes.NewReader(b)))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if rec.Code != http.StatusOK {
			t.Fatalf("правка: %d %v", rec.Code, out)
		}
		return out
	}
	out := post(map[string]any{"id": s.remember(old), "title": "  Дилижан,\n утро "})
	if out["title"] != "Дилижан, утро" {
		t.Fatalf("заголовок: %v", out)
	}
	// новое имя без расширения: расширение сохраняется, заголовок и индекс едут следом
	out = post(map[string]any{"id": s.remember(old), "name": "dilijan"})
	neu := filepath.Join(photos, "Trip", "dilijan.png")
	if out["name"] != "dilijan.png" || out["id"] != shortID(neu) || out["title"] != "Дилижан, утро" {
		t.Fatalf("переименование: %v", out)
	}
	if _, ok := s.index.get(neu); !ok {
		t.Fatal("запись индекса не переехала")
	}
	if _, ok := s.index.get(old); ok {
		t.Fatal("старая запись индекса осталась")
	}
	// пустой заголовок — снова имя файла
	if out = post(map[string]any{"id": shortID(neu), "title": ""}); out["title"] != "" {
		t.Fatalf("сброс заголовка: %v", out)
	}
}

func TestIndexAdoptsMoveOutsideAgent(t *testing.T) {
	s, photos, _ := filesLibrary(t)
	walk(t, s)
	old := filepath.Join(photos, "Trip", "notes.txt")
	s.meta.setTitle(old, "Заметки")
	neu := filepath.Join(photos, "Trip", "Day 2", "notes-moved.txt")
	if err := os.Rename(old, neu); err != nil {
		t.Fatal(err)
	}
	tk := walk(t, s)
	if s.meta.title(neu) != "Заметки" || s.meta.title(old) != "" {
		t.Fatalf("заголовок не переехал: %q / %q; отчёт %v", s.meta.title(neu), s.meta.title(old), tk.v.Lines)
	}
}

func TestIndexKeepsRecordsWhenDiskGone(t *testing.T) {
	s, photos, _ := filesLibrary(t)
	walk(t, s)
	before := len(s.index.under(photos))
	// диск отвалился: точка монтирования на месте, но пустая
	if err := os.RemoveAll(filepath.Join(photos, "Trip")); err != nil {
		t.Fatal(err)
	}
	walk(t, s)
	if got := len(s.index.under(photos)); got != before || before == 0 {
		t.Fatalf("записи пропали: было %d, стало %d", before, got)
	}
}

func TestWebpAndExif(t *testing.T) {
	// VP8X: ширина-1 и высота-1 в трёх байтах
	b := make([]byte, 30)
	copy(b, "RIFF")
	copy(b[8:], "WEBPVP8X")
	b[24], b[27] = 0x7f, 0x3f // 128×64
	if w, h := webpSize(b); w != 128 || h != 64 {
		t.Fatalf("webp: %d×%d", w, h)
	}

	// JPEG с EXIF: DateTimeOriginal через ExifIFD и поворот 6
	tiff := new(bytes.Buffer)
	le := binary.LittleEndian
	tiff.WriteString("II")
	_ = binary.Write(tiff, le, uint16(42))
	_ = binary.Write(tiff, le, uint32(8))
	// IFD0: 2 записи — Orientation и указатель на ExifIFD
	_ = binary.Write(tiff, le, uint16(2))
	entry := func(tag, typ uint16, count, val uint32) {
		_ = binary.Write(tiff, le, tag)
		_ = binary.Write(tiff, le, typ)
		_ = binary.Write(tiff, le, count)
		_ = binary.Write(tiff, le, val)
	}
	entry(0x0112, 3, 1, 6)
	exifOff := uint32(8 + 2 + 2*12 + 4)
	entry(0x8769, 4, 1, exifOff)
	_ = binary.Write(tiff, le, uint32(0))
	dateOff := exifOff + 2 + 12 + 4
	_ = binary.Write(tiff, le, uint16(1))
	entry(0x9003, 2, 20, dateOff)
	_ = binary.Write(tiff, le, uint32(0))
	tiff.WriteString("2024:08:12 10:15:30\x00")

	jpg := new(bytes.Buffer)
	jpg.Write([]byte{0xFF, 0xD8, 0xFF, 0xE1})
	_ = binary.Write(jpg, binary.BigEndian, uint16(2+6+tiff.Len()))
	jpg.WriteString("Exif\x00\x00")
	jpg.Write(tiff.Bytes())
	jpg.Write([]byte{0xFF, 0xDA, 0, 2})
	tm, orient := exifInfo(jpg.Bytes())
	want := time.Date(2024, 8, 12, 10, 15, 30, 0, time.Local)
	if !tm.Equal(want) || orient != 6 {
		t.Fatalf("exif: %v %d", tm, orient)
	}
}

func TestCatalogUsesFileTitleForFolderFilm(t *testing.T) {
	s, _, films := filesLibrary(t)
	dir := filepath.Join(films, "Other")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "other.mkv"), []byte("x"), 0o644)
	s.meta.setTitle(filepath.Join(dir, "other.mkv"), "Другой фильм")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tv/api/catalog", nil))
	var out struct{ Films []catTile }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	found := false
	for _, f := range out.Films {
		found = found || f.Name == "Другой фильм"
	}
	if !found {
		t.Fatalf("каталог: %+v", out.Films)
	}
}

func TestCatalogUsesTitle(t *testing.T) {
	s, _, films := filesLibrary(t)
	s.meta.setTitle(filepath.Join(films, "Film (2020).mkv"), "Мой фильм")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tv/api/catalog", nil))
	var out struct{ Films []catTile }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Films) != 1 || out.Films[0].Name != "Мой фильм" {
		t.Fatalf("каталог: %+v", out.Films)
	}
}
