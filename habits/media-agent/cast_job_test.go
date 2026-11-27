package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Задача «Актёры по всей медиатеке»: состав и фото у плиток и планов разом.
func TestCastJob(t *testing.T) {
	s, _, films := plansServer(t)
	film := filepath.Join(films, "Film (2020).mkv")
	show := filepath.Join(films, "Show")
	_ = os.MkdirAll(filepath.Join(show, "S01"), 0o755)
	_ = os.WriteFile(filepath.Join(show, "S01", "E01.mkv"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(show, "S01", "E02.mkv"), []byte("x"), 0o644)
	_ = writeInfo(film, &itemInfo{Title: "Film", QID: "Q100"})
	_ = writeInfo(show, &itemInfo{Title: "Show", QID: "Q200"})
	pl := s.plans.add(plan{Query: "Plan", Kind: "film", Type: "film", Info: &itemInfo{Title: "Plan", QID: "Q300"}})
	get(t, s, "/tv/api/catalog")

	se, sp := wikidataEntityFn, personFetchFn
	t.Cleanup(func() { wikidataEntityFn, personFetchFn = se, sp })
	wikidataEntityFn = func(qid string) (*wdEntity, error) {
		e := &wdEntity{Claims: map[string][]wdClaim{}, labels: map[string]string{"Q208026": "Том Харди"}}
		var c wdClaim
		c.MainSnak.DataValue.Value = json.RawMessage(`{"id":"Q208026"}`)
		e.Claims["P161"] = []wdClaim{c}
		return e, nil
	}
	photos := 0
	personFetchFn = func(s *server, qid, dst string) error {
		photos++
		_ = os.MkdirAll(filepath.Dir(dst), 0o755)
		return os.WriteFile(dst, pngBytes(t), 0o644)
	}

	if n := len(s.castTargets()); n != 3 {
		t.Fatalf("целей: %d", n)
	}
	if code, out := adminCall(t, s, "POST", "/api/admin/cast/run", testPin, map[string]any{}); code != 200 {
		t.Fatalf("запуск: %d %v", code, out)
	}
	waitIdle(t, s)
	for _, p := range []string{film, show} {
		inf, _ := readInfo(p)
		if inf.CastAt == 0 || len(inf.Cast) != 1 || inf.Cast[0].QID != "Q208026" {
			t.Fatalf("%s: %+v", filepath.Base(p), inf)
		}
	}
	if p, _ := s.plans.find(pl.ID); p.Info.CastAt == 0 || len(p.Info.Cast) != 1 {
		t.Fatalf("план: %+v", p.Info)
	}
	if photos != 1 || !fileReady(s.personPhotoPath("Q208026")) {
		t.Fatalf("фото: скачано %d", photos)
	}
	if n := len(s.castTargets()); n != 0 {
		t.Fatalf("после работы целей: %d", n)
	}
	if code, _ := adminCall(t, s, "POST", "/api/admin/cast/run", testPin, map[string]any{}); code != 400 {
		t.Fatalf("делать нечего: %d", code)
	}
}
