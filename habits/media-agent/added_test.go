package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func catalogTile(t *testing.T, s *server, section, name string) map[string]any {
	t.Helper()
	out := get(t, s, "/tv/api/catalog")
	list, _ := out[section].([]any)
	for _, x := range list {
		if m := x.(map[string]any); m["name"] == name {
			return m
		}
	}
	t.Fatalf("нет «%s» в %s: %v", name, section, out[section])
	return nil
}

// Для фильтров на главной у плитки есть год, жанры и когда она появилась.
// Год — из описания, а нет его — из имени «(2020)».
func TestCatalogFacts(t *testing.T) {
	s, _, films := plansServer(t)
	_ = writeInfo(filepath.Join(films, "Film (2020).mkv"), &itemInfo{Title: "Film", Year: 2019, Genres: []string{"драма", "триллер"}})
	_ = os.WriteFile(filepath.Join(films, "Other (1999).mkv"), []byte("x"), 0o644)
	film := catalogTile(t, s, "films", "Film (2020)")
	if film["year"] != 2019.0 || len(film["genres"].([]any)) != 2 {
		t.Fatalf("из описания: %v", film)
	}
	other := catalogTile(t, s, "films", "Other (1999)")
	if other["year"] != 1999.0 || other["genres"] != nil {
		t.Fatalf("из имени: %v", other)
	}
	if a, _ := other["added"].(float64); a < float64(time.Now().Add(-time.Hour).Unix()) {
		t.Fatalf("добавлен: %v", other["added"])
	}
}

// Время добавления запоминается при первой встрече и потом не меняется: ни
// новая серия внутри, ни переименование в админке не делают сериал «новым».
func TestAddedRemembered(t *testing.T) {
	s, _, films := plansServer(t)
	show := filepath.Join(films, "Show")
	_ = os.MkdirAll(filepath.Join(show, "S01"), 0o755)
	_ = os.WriteFile(filepath.Join(show, "S01", "E01.mkv"), []byte("x"), 0o644)
	get(t, s, "/tv/api/catalog")
	s.added.mu.Lock()
	s.added.items[show] = 1000
	s.added.mu.Unlock()
	_ = os.WriteFile(filepath.Join(show, "S01", "E02.mkv"), []byte("x"), 0o644)
	if a := catalogTile(t, s, "series", "Show")["added"]; a != 1000.0 {
		t.Fatalf("после новой серии: %v", a)
	}
	id := catalogTile(t, s, "series", "Show")["id"].(string)
	if code, out := adminCall(t, s, "POST", "/api/admin/rename", testPin, map[string]any{"id": id, "name": "Show (2001)"}); code != 200 {
		t.Fatalf("переименование: %d %v", code, out)
	}
	if a := catalogTile(t, s, "series", "Show (2001)")["added"]; a != 1000.0 {
		t.Fatalf("после переименования: %v", a)
	}
	// и переживает перезапуск
	if n := newAddedStore(s.cfg.cache); n.items[filepath.Join(films, "Show (2001)")] != 1000 {
		t.Fatalf("на диске: %v", n.items)
	}
}
