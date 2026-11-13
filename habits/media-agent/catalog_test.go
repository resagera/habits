package main

import "testing"

// Название для поиска постера: релизные имена файлов и папок бывают какими
// угодно, а искать надо по голому названию и году.
func TestCleanTitle(t *testing.T) {
	cases := []struct {
		in     string
		isFile bool
		title  string
		year   int
	}{
		{"The.Gentlemen.2019.x264.BDRip.(1080p).OlLanDGroup.mkv", true, "The Gentlemen", 2019},
		{"Джентльмены (2019).mp4", true, "Джентльмены", 2019},
		{"The Big Bang Theory (Season 1-12)", false, "The Big Bang Theory", 0},
		{"Star Trek Discovery", false, "Star Trek Discovery", 0},
		{"Fauda", false, "Fauda", 0},
		{"Star.Trek.Discovery.s01e01.LostFilm.avi", true, "Star Trek Discovery", 0},
		{"The Big Bang Theory - S08E22_[Kyrazh-Bambej].mp4", true, "The Big Bang Theory", 0},
		{"Blade Runner 2049 (2017).mkv", true, "Blade Runner 2049", 2017},
		{"Blade.Runner.2049.2017.1080p.WEB-DL.mkv", true, "Blade Runner 2049", 2017},
	}
	for _, c := range cases {
		title, year := cleanTitle(c.in, c.isFile)
		if title != c.title || year != c.year {
			t.Errorf("cleanTitle(%q) = %q, %d; ожидалось %q, %d", c.in, title, year, c.title, c.year)
		}
	}
}
