package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func timeClaim(t, precision string) wdClaim {
	var c wdClaim
	c.MainSnak.DataValue.Value = json.RawMessage(`{"time":"` + t + `","precision":` + precision + `}`)
	return c
}

// Дата выхода — с точностью до дня; год без дня даты не даёт.
func TestReleasedFromWikidata(t *testing.T) {
	e := &wdEntity{Claims: map[string][]wdClaim{"P577": {
		timeClaim("+2027-05-01T00:00:00Z", "11"), timeClaim("+2027-03-12T00:00:00Z", "11")}}}
	inf := &itemInfo{}
	e.fill(inf)
	if inf.Released != "2027-03-12" || inf.Year != 2027 {
		t.Fatalf("день: %+v", inf)
	}
	// отменённые даты (ранг deprecated) — не дата выхода: «Дюна 2» не вышла в 2023-м
	old := timeClaim("+2023-10-18T00:00:00Z", "11")
	old.Rank = "deprecated"
	e = &wdEntity{Claims: map[string][]wdClaim{"P577": {old, timeClaim("+2024-02-27T00:00:00Z", "11")}}}
	inf = &itemInfo{}
	e.fill(inf)
	if inf.Released != "2024-02-27" || inf.Year != 2024 {
		t.Fatalf("deprecated: %+v", inf)
	}
	e = &wdEntity{Claims: map[string][]wdClaim{"P577": {timeClaim("+2027-00-00T00:00:00Z", "9")}}}
	inf = &itemInfo{}
	e.fill(inf)
	if inf.Released != "" || inf.Year != 2027 {
		t.Fatalf("год: %+v", inf)
	}
}

func TestMergeRefetchKeepsEdited(t *testing.T) {
	old := &itemInfo{Title: "Моё", Description: "старое", EditedFields: []string{"title"}, Edited: true}
	got := mergeRefetch(old, &itemInfo{Title: "Their", Description: "новое"})
	if got.Title != "Моё" || got.Description != "новое" || !got.Edited || len(got.EditedFields) != 1 {
		t.Fatalf("по полям: %+v", got)
	}
	// старая правка без списка полей — правлено всё
	old = &itemInfo{Title: "Моё", Description: "старое", Edited: true}
	got = mergeRefetch(old, &itemInfo{Title: "Their", Description: "новое", Year: 2001})
	if got.Title != "Моё" || got.Description != "старое" || got.Year != 0 {
		t.Fatalf("всё правлено: %+v", got)
	}
	if got = mergeRefetch(nil, &itemInfo{Title: "Their"}); got.Title != "Their" {
		t.Fatalf("без старого: %+v", got)
	}
}

// «Найти в интернете» в медиатеке больше не затирает ручную правку.
func TestAdminInfoRefetchKeepsEdits(t *testing.T) {
	s, _, films := filesLibrary(t)
	s.cfg.adminPin = testPin
	film := filepath.Join(films, "Film (2020).mkv")
	id := s.remember(film)
	saved := fetchInfoFn
	defer func() { fetchInfoFn = saved }()
	fetchInfoFn = func(title string, year int, kind string) (*itemInfo, error) {
		if title == "" {
			return nil, errors.New("пусто")
		}
		return &itemInfo{Title: "Found", Description: "найдено", Year: 2020}, nil
	}
	code, out := adminCall(t, s, "POST", "/api/admin/info", testPin,
		map[string]any{"id": id, "info": map[string]any{"title": "Моё название", "year": 2020}})
	if code != 200 {
		t.Fatalf("правка: %d %v", code, out)
	}
	inf, _ := readInfo(film)
	if !contains(inf.EditedFields, "title") || contains(inf.EditedFields, "description") {
		t.Fatalf("поля правки: %v", inf.EditedFields)
	}
	if code, out = adminCall(t, s, "POST", "/api/admin/info", testPin, map[string]any{"id": id, "fetch": true}); code != 200 {
		t.Fatalf("поиск: %d %v", code, out)
	}
	inf, _ = readInfo(film)
	if inf.Title != "Моё название" || inf.Description != "найдено" {
		t.Fatalf("после поиска: %+v", inf)
	}
	adminCall(t, s, "POST", "/api/admin/info", testPin, map[string]any{"id": id, "fetch": true, "reset": true})
	inf, _ = readInfo(film)
	if inf.Title != "Found" || inf.Edited {
		t.Fatalf("со сбросом: %+v", inf)
	}
}
