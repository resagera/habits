package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testLibrary — библиотека из пустых файлов: разбор здесь не нужен, правила
// «Продолжить просмотр» опираются только на раскладку папок.
func testLibrary(t *testing.T) (*server, string) {
	t.Helper()
	root := t.TempDir()
	for _, f := range []string{
		"Show/S01/E01.mp4", "Show/S01/E02.mp4", "Show/S02/E01.mp4",
		"Mini/p1.mp4", "Mini/p2.mp4",
		"Film.mp4",
		"FilmDir/movie.mp4",
	} {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := newServer(config{base: "/tv", cache: t.TempDir(),
		roots: []library{{ID: shortID(root), Path: root, Title: "Видео", Kind: "video"}}})
	return s, root
}

type saveResult struct {
	Entry   *progressEntry `json:"entry"`
	Removed bool           `json:"removed"`
	Skipped bool           `json:"skipped"`
}

func save(t *testing.T, s *server, path string, pos, dur float64) saveResult {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"file": s.remember(path), "position": pos, "duration": dur})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/tv/api/progress", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT %s: %d %s", path, rec.Code, rec.Body.String())
	}
	var out saveResult
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

func list(t *testing.T, s *server) []progressEntry {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tv/api/progress", nil))
	var out struct {
		Items []progressEntry `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Items
}

func TestProgressSeriesAdvancesAcrossSeasons(t *testing.T) {
	s, root := testLibrary(t)
	e01 := filepath.Join(root, "Show/S01/E01.mp4")
	e02 := filepath.Join(root, "Show/S01/E02.mp4")
	s2e1 := filepath.Join(root, "Show/S02/E01.mp4")

	if r := save(t, s, e01, 30, 2400); !r.Skipped {
		t.Fatalf("первая минута не должна создавать запись: %+v", r)
	}
	r := save(t, s, e01, 600, 2400)
	if r.Entry == nil || r.Entry.Key != s.remember(filepath.Join(root, "Show")) || r.Entry.Season != "S01" {
		t.Fatalf("запись должна принадлежать сериалу Show, сезон S01: %+v", r.Entry)
	}
	if r := save(t, s, e02, 30, 2400); !r.Skipped {
		t.Fatalf("случайно открытая чужая серия не должна затирать место: %+v", r)
	}
	if got := list(t, s); len(got) != 1 || got[0].Episode != "E01" || got[0].Position != 600 {
		t.Fatalf("после случайного включения должно остаться E01 на 600 с: %+v", got)
	}

	// досмотрели — запись переходит на следующую серию с нуля
	if r := save(t, s, e01, 2350, 2400); r.Entry == nil || r.Entry.Episode != "E02" || r.Entry.Position != 0 {
		t.Fatalf("после досмотренной E01 ждали E02 с нуля: %+v", r)
	}
	// свою запись (следующую серию) обновляем с любой секунды
	if r := save(t, s, e02, 20, 2400); r.Entry == nil || r.Entry.Position != 20 {
		t.Fatalf("E02 с 20 с должна обновиться: %+v", r)
	}
	// последняя серия сезона → первая следующего
	if r := save(t, s, e02, 2399, 2400); r.Entry == nil || r.Entry.Season != "S02" || r.Entry.Episode != "E01" {
		t.Fatalf("после последней серии S01 ждали S02E01: %+v", r)
	}
	// последняя серия сериала → записи нет
	if r := save(t, s, s2e1, 2390, 2400); !r.Removed {
		t.Fatalf("досмотренный сериал должен пропасть из «Продолжить»: %+v", r)
	}
	if got := list(t, s); len(got) != 0 {
		t.Fatalf("список должен быть пуст: %+v", got)
	}
}

func TestProgressFilmsAndMiniSeries(t *testing.T) {
	s, root := testLibrary(t)
	film := filepath.Join(root, "Film.mp4")
	inDir := filepath.Join(root, "FilmDir/movie.mp4")
	p1, p2 := filepath.Join(root, "Mini/p1.mp4"), filepath.Join(root, "Mini/p2.mp4")

	if r := save(t, s, film, 3000, 6000); r.Entry == nil || r.Entry.Kind != "film" || r.Entry.Key != s.remember(film) {
		t.Fatalf("фильм в корне: ключ — сам файл: %+v", r.Entry)
	}
	if r := save(t, s, inDir, 3000, 6000); r.Entry == nil || r.Entry.Kind != "film" ||
		r.Entry.Key != s.remember(filepath.Join(root, "FilmDir")) {
		t.Fatalf("фильм в папке: ключ — папка, как у плитки: %+v", r.Entry)
	}
	if r := save(t, s, film, 5950, 6000); !r.Removed {
		t.Fatalf("досмотренный фильм пропадает: %+v", r)
	}
	if r := save(t, s, p1, 400, 1200); r.Entry == nil || r.Entry.Kind != "series" ||
		r.Entry.Key != s.remember(filepath.Join(root, "Mini")) {
		t.Fatalf("мини-сериал: ключ — его папка: %+v", r.Entry)
	}
	if r := save(t, s, p1, 1199, 1200); r.Entry == nil || r.Entry.Episode != "p2" {
		t.Fatalf("после p1 ждали p2: %+v", r)
	}
	// после последней части мини-сериала не уходим в соседние папки библиотеки
	if r := save(t, s, p2, 1199, 1200); !r.Removed {
		t.Fatalf("досмотренный мини-сериал должен пропасть: %+v", r)
	}
}

func TestProgressSurvivesTranscoding(t *testing.T) {
	s, root := testLibrary(t)
	avi := filepath.Join(root, "Show/S02/E07.avi")
	if err := os.WriteFile(avi, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := save(t, s, avi, 900, 2400); r.Entry == nil {
		t.Fatalf("позиция на avi не записалась: %+v", r)
	}
	// ночью серию перекодировали: avi пропал, появился mp4 с другим id
	mp4 := strings.TrimSuffix(avi, ".avi") + ".mp4"
	if err := os.Rename(avi, mp4); err != nil {
		t.Fatal(err)
	}
	got := list(t, s)
	if len(got) != 1 || got[0].File != s.remember(mp4) || got[0].Position != 900 {
		t.Fatalf("после перекодирования позиция должна переехать на mp4: %+v", got)
	}
	// пути наружу не уходят
	raw, _ := json.Marshal(got[0])
	if strings.Contains(string(raw), root) {
		t.Fatalf("в ответе засветился путь: %s", raw)
	}
	// файл удалили совсем — запись тоже не нужна
	if err := os.Remove(mp4); err != nil {
		t.Fatal(err)
	}
	if got := list(t, s); len(got) != 0 {
		t.Fatalf("запись на удалённый файл должна исчезнуть: %+v", got)
	}
}
