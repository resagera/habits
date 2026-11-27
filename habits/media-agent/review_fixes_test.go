package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Сбор состава плана, пока админка читает планы и актёров, — без гонок
// (go test -race).
func TestPlanCastNoRace(t *testing.T) {
	s, _ := peopleSetup(t)
	pl := s.plans.add(plan{Query: "X", Kind: "film", Type: "film", Info: &itemInfo{Title: "X", QID: "Q300"}})
	saved := wikidataEntityFn
	t.Cleanup(func() { wikidataEntityFn = saved })
	wikidataEntityFn = func(string) (*wdEntity, error) {
		var c wdClaim
		c.MainSnak.DataValue.Value = json.RawMessage(`{"id":"Q208026"}`)
		return &wdEntity{Claims: map[string][]wdClaim{"P161": {c}}, labels: map[string]string{"Q208026": "Том Харди"}}, nil
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			s.plans.update(pl.ID, func(p *plan) { p.Info = &itemInfo{Title: "X", QID: "Q300"} })
			s.ensurePlanCast(pl.ID)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			s.peopleIndex()
			for _, p := range s.plans.all() {
				if p.Info != nil {
					_ = len(p.Info.Cast)
				}
			}
		}
	}()
	wg.Wait()
}

// Правки человека без Q-id не пропадают, когда у него находится Q-id.
func TestNameEditsSurviveQID(t *testing.T) {
	s, film := peopleSetup(t)
	if code := uploadPersonPhoto(t, s, "name:Кто-то"); code != 200 {
		t.Fatalf("фото: %d", code)
	}
	adminCall(t, s, "PUT", "/api/admin/people?key=name:Кто-то", testPin, map[string]any{"about": "свой текст"})
	// ночной сбор нашёл ему Q-id
	_ = writeInfo(film, &itemInfo{Title: "Film", CastAt: 1,
		Cast: []castMember{{"Том Харди", "Q208026"}, {"Кто-то", "Q777"}}})
	row := peopleRows(t, s)["Q777"]
	if row == nil || row["about"] != "свой текст" || !strings.Contains(row["photo"].(string), "/person/n-") {
		t.Fatalf("строка Q777: %v", row)
	}
	out := get(t, s, "/tv/api/person?qid=Q777&name=Кто-то")
	if out["about"] != "свой текст" || !strings.Contains(out["photo"].(string), "/person/n-") {
		t.Fatalf("/api/person: %v", out)
	}
	inf, _ := readInfo(film)
	if ph := s.castPhotos(inf); !strings.Contains(ph["Кто-то"], "/person/n-") {
		t.Fatalf("people_photos: %v", ph)
	}
}

// «Искать как…» в медиатеке берёт найденное целиком: старое описание —
// про другой фильм. Старые правки без списка полей сбрасываются reset.
func TestLibrarySearchAsResets(t *testing.T) {
	s, _, films := filesLibrary(t)
	s.cfg.adminPin = testPin
	film := filepath.Join(films, "Film (2020).mkv")
	id := s.remember(film)
	saved := fetchInfoFn
	t.Cleanup(func() { fetchInfoFn = saved })
	fetchInfoFn = func(title string, year int, kind string) (*itemInfo, error) {
		if title == "" {
			return nil, errors.New("пусто")
		}
		return &itemInfo{Title: title, Description: "про " + title}, nil
	}
	_ = writeInfo(film, &itemInfo{Title: "Не тот", Description: "не то", Edited: true})
	adminCall(t, s, "POST", "/api/admin/info", testPin, map[string]any{"id": id, "fetch": true, "title": "Right One"})
	if inf, _ := readInfo(film); inf.Title != "Right One" || inf.Description != "про Right One" || inf.Edited {
		t.Fatalf("искать как: %+v", inf)
	}
}
