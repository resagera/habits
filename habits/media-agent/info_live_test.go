package main

import (
	"os"
	"testing"
)

// INFO_LIVE=1 — поиск «Инфо» в настоящем интернете (IMDb, Wikidata, Википедия, TVmaze).
func TestInfoLive(t *testing.T) {
	if os.Getenv("INFO_LIVE") == "" {
		t.Skip("INFO_LIVE не задан")
	}
	for _, c := range []struct {
		title string
		year  int
		kind  string
	}{{"Fauda", 0, "series"}, {"The Gentlemen", 2019, "film"}, {"Джентльмены", 2019, "film"}, {"Legion", 2017, "series"}, {"99 francs", 2007, "film"}} {
		inf, err := fetchInfo(c.title, c.year, c.kind)
		if err != nil {
			t.Errorf("%s: %v", c.title, err)
			continue
		}
		d := []rune(inf.Description)
		if len(d) > 90 {
			d = d[:90]
		}
		t.Logf("%s → %s (%d) %s | жанры %v | %v | актёры %d %v | %s…", c.title, inf.Title, inf.Year, inf.IMDb, inf.Genres, inf.Ratings, len(inf.Actors), inf.Actors[:min(3, len(inf.Actors))], string(d))
	}
}
