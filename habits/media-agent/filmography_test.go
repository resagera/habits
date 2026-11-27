package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// в тестах Wikidata SPARQL не зовём: кому нужно — подменяет sparqlFn сам
func init() {
	sparqlFn = func(string) ([]byte, error) { return nil, errors.New("сеть в тестах выключена") }
}

func TestParseFilmography(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "sparql_person.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows := parseFilmography(body)
	if len(rows) != 4 {
		t.Fatalf("строк: %d %+v", len(rows), rows)
	}
	// по году вниз, без года — в конце; сериал и фильм сразу — сериал
	if rows[0].QID != "Q189330" || rows[1].Year != 2010 || rows[2].QID != "Q16290" || rows[2].Kind != "series" ||
		rows[3].Title != "Untitled Project" || rows[3].Year != 0 {
		t.Fatalf("порядок и типы: %+v", rows)
	}
}

func TestPersonFilms(t *testing.T) {
	s, film := peopleSetup(t)
	_ = writeInfo(film, &itemInfo{Title: "Film", QID: "Q25188", Cast: []castMember{{"Том Харди", "Q208026"}}, CastAt: 1})
	pl := s.plans.add(plan{Query: "TDKR", Kind: "film", Type: "film", Info: &itemInfo{Title: "TDKR", QID: "Q189330"}})
	body, _ := os.ReadFile(filepath.Join("testdata", "sparql_person.json"))
	calls := 0
	saved := sparqlFn
	t.Cleanup(func() { sparqlFn = saved })
	sparqlFn = func(q string) ([]byte, error) { calls++; return body, nil }

	out := get(t, s, "/tv/api/person?qid=Q208026")
	films, _ := out["films"].([]any)
	if len(films) != 4 {
		t.Fatalf("фильмография: %v", out)
	}
	by := map[string]map[string]any{}
	for _, f := range films {
		m := f.(map[string]any)
		by[m["qid"].(string)] = m
	}
	if by["Q25188"]["have"] != s.remember(film) || by["Q189330"]["plan"] != pl.ID || by["Q16290"]["have"] != nil {
		t.Fatalf("отметки: %v", by)
	}
	get(t, s, "/tv/api/person?qid=Q208026")
	if calls != 1 {
		t.Fatalf("кэш: запросов %d", calls)
	}
	// сеть упала — пустой список, не ошибка
	sparqlFn = func(string) ([]byte, error) { return nil, errors.New("нет сети") }
	if out = get(t, s, "/tv/api/person?qid=Q1"); out["films"] == nil || len(out["films"].([]any)) != 0 {
		t.Fatalf("без сети: %v", out)
	}
}
