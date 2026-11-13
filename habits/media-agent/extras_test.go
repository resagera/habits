package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// animeLibrary — сериал с допами в каждом сезоне (как «Аптекарша»: по коротышке
// на серию), сериал с допами рядом с сезонами («как снимался сериал») и обычный
// сериал без допов, в библиотеке вида «anime».
func animeLibrary(t *testing.T) (*server, string) {
	t.Helper()
	root := t.TempDir()
	for _, f := range []string{
		"Kusuriya/Season 01/ep 01.mkv", "Kusuriya/Season 01/ep 02.mkv",
		"Kusuriya/Season 01/SPs/sp 01.mkv", "Kusuriya/Season 01/SPs/sp 02.mkv",
		"Kusuriya/Season 02/ep 25.mkv",
		"Kusuriya/Season 02/SPs/sp 25.mkv",
		"Making/Season 01/S01E01.mkv", "Making/Season 01/S01E02.mkv",
		"Making/Доп материалы/how-it-was-made.mkv",
		"Plain/Season 01/S01E01.mkv",
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
		roots: []library{{ID: shortID(root), Path: root, Title: "Аниме", Kind: "anime"}}})
	return s, root
}

func seasonNames(t *catTile) []string {
	out := make([]string, 0, len(t.Seasons))
	for _, s := range t.Seasons {
		mark := ""
		if s.Extra {
			mark = "+"
		}
		out = append(out, mark+s.Name)
	}
	return out
}

func browseNames(t *testing.T, s *server, id string, withExtras bool) []string {
	t.Helper()
	url := "/tv/api/browse?id=" + id
	if withExtras {
		url += "&extras=1"
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("browse %s: %d %s", id, rec.Code, rec.Body.String())
	}
	var out struct{ Items []entry }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(out.Items))
	for _, it := range out.Items {
		if !it.IsDir {
			names = append(names, it.Name)
		}
	}
	return names
}

func setInline(t *testing.T, s *server, id string, on bool) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"id": id, "inline": on})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/tv/api/extras", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("переключатель %s=%v: %d %s", id, on, rec.Code, rec.Body.String())
	}
}

// Аниме едет в свой раздел, а не к сериалам и фильмам.
func TestAnimeSection(t *testing.T) {
	s, _ := animeLibrary(t)
	cat := catalogOf(t, s)
	if len(cat.Series) != 0 || len(cat.Films) != 0 {
		t.Fatalf("аниме попало в чужие разделы: сериалы %v, фильмы %v", names(cat.Series), names(cat.Films))
	}
	if got := names(cat.Anime); len(got) != 3 {
		t.Fatalf("в разделе «Аниме» ожидались три плитки, пришло %v", got)
	}
}

// Допы видно кнопкой рядом с сезонами, но в счёт серий они не идут.
func TestExtrasAsSeasons(t *testing.T) {
	s, _ := animeLibrary(t)
	cat := catalogOf(t, s)

	k := tileNamed(cat.Anime, "Kusuriya")
	if k == nil {
		t.Fatal("нет плитки Kusuriya")
	}
	want := []string{"Season 01", "+Season 01 · доп", "Season 02", "+Season 02 · доп"}
	if got := seasonNames(k); !equalStrings(got, want) {
		t.Fatalf("кнопки сезонов: %v, ожидали %v", got, want)
	}
	if k.Count != 3 || k.Extras != 3 {
		t.Fatalf("серий %d (ожидали 3), допов %d (ожидали 3)", k.Count, k.Extras)
	}

	// допы рядом с сезонами — последней кнопкой и с общим названием
	m := tileNamed(cat.Anime, "Making")
	if got, want := seasonNames(m), []string{"Season 01", "+Доп материалы"}; !equalStrings(got, want) {
		t.Fatalf("кнопки «Making»: %v, ожидали %v", got, want)
	}

	// сериал без допов остаётся как был
	p := tileNamed(cat.Anime, "Plain")
	if got, want := seasonNames(p), []string{"Season 01"}; !equalStrings(got, want) {
		t.Fatalf("кнопки «Plain»: %v, ожидали %v", got, want)
	}
	if p.Extras != 0 || p.ExtrasInline {
		t.Fatalf("у сериала без допов взялись допы: %+v", p)
	}
}

// Галочка включена, но режим «после каждой серии» выключен (так по умолчанию) —
// список сезона не меняется.
func TestExtrasSeparateByDefault(t *testing.T) {
	s, root := animeLibrary(t)
	season := s.remember(filepath.Join(root, "Kusuriya", "Season 01"))
	want := []string{"ep 01.mkv", "ep 02.mkv"}
	if got := browseNames(t, s, season, true); !equalStrings(got, want) {
		t.Fatalf("сезон с галочкой: %v, ожидали %v", got, want)
	}
}

// «После каждой серии»: коротышка встаёт сразу за своей серией. Пара ищется по
// порядку, когда серий и допов одинаково.
func TestExtrasInlineOrder(t *testing.T) {
	s, root := animeLibrary(t)
	series := s.remember(filepath.Join(root, "Kusuriya"))
	setInline(t, s, series, true)

	season := s.remember(filepath.Join(root, "Kusuriya", "Season 01"))
	want := []string{"ep 01.mkv", "sp 01.mkv", "ep 02.mkv", "sp 02.mkv"}
	if got := browseNames(t, s, season, true); !equalStrings(got, want) {
		t.Fatalf("вперемешку: %v, ожидали %v", got, want)
	}
	// галочка снята — те же серии без допов, даже если режим включён
	if got := browseNames(t, s, season, false); !equalStrings(got, []string{"ep 01.mkv", "ep 02.mkv"}) {
		t.Fatalf("без галочки допы всё равно подмешались: %v", got)
	}
	// настройка видна в каталоге и переживает перезапуск агента
	if !tileNamed(catalogOf(t, s).Anime, "Kusuriya").ExtrasInline {
		t.Fatal("настройка не доехала до каталога")
	}
	again := newServer(config{base: "/tv", cache: s.cfg.cache, roots: s.cfg.roots})
	if !again.extras.isInline(filepath.Join(root, "Kusuriya")) {
		t.Fatal("настройка не прочиталась с диска")
	}
}

// Пара по номеру в имени, когда допов меньше, чем серий; без номера — в конец.
func TestExtrasPairByNumber(t *testing.T) {
	eps := []entry{{Name: "Show.S01E01.mkv"}, {Name: "Show.S01E02.mkv"}, {Name: "Show.S01E03.mkv"}}
	extras := []entry{{Name: "Show.S01E02.behind.mkv"}, {Name: "bloopers.mkv"}}
	want := []string{"Show.S01E01.mkv", "Show.S01E02.mkv", "Show.S01E02.behind.mkv",
		"Show.S01E03.mkv", "bloopers.mkv"}
	got := make([]string, 0, 5)
	for _, e := range mixExtras(eps, extras) {
		got = append(got, e.Name)
	}
	if !equalStrings(got, want) {
		t.Fatalf("по номеру: %v, ожидали %v", got, want)
	}
	// «1080p» и «x265» за номер серии не считаются
	if n, ok := epNumber("[VCB-Studio] Hitorigoto 07 [BDRip 1080p x265 FLAC].mkv"); ok {
		t.Fatalf("в имени без явного номера нашёлся номер %d", n)
	}
}

// Какие папки считаются допами.
func TestExtrasDirNames(t *testing.T) {
	for _, name := range []string{"SPs", "sps", "SP", "Specials", "Extras", "BONUS",
		"Доп материалы", "доп_материалы", "Дополнения"} {
		if !isExtrasDir(name) {
			t.Errorf("папка %q не признана допами", name)
		}
	}
	for _, name := range []string{"Season 01", "OVA", "Фильмы", "Specials 2", "SPs2"} {
		if isExtrasDir(name) {
			t.Errorf("папка %q зря признана допами", name)
		}
	}
}

// Что играется после сезона: при выключенном режиме допы идут после ВСЕХ
// сезонов, при включённом — не идут вовсе (они уже внутри сезонов), а со снятой
// галочкой сериал кончается последним сезоном.
func TestNextSeasonWithExtras(t *testing.T) {
	s, root := animeLibrary(t)
	last := func(season, file string, withExtras bool) string {
		id := s.remember(filepath.Join(root, "Kusuriya", season, file))
		url := "/tv/api/next-season?id=" + id
		if withExtras {
			url += "&extras=1"
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		var out struct {
			Folder struct{ Name string }
			Items  []entry
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if len(out.Items) == 0 {
			return ""
		}
		return out.Folder.Name
	}
	if got := last("Season 01", "ep 02.mkv", true); got != "Season 02" {
		t.Fatalf("после первого сезона: %q, ожидали «Season 02»", got)
	}
	if got := last("Season 02", "ep 25.mkv", true); got != "Season 01 · доп" {
		t.Fatalf("после последнего сезона: %q, ожидали допы первого сезона", got)
	}
	if got := last("Season 02", "ep 25.mkv", false); got != "" {
		t.Fatalf("со снятой галочкой сериал должен кончиться, а пришло %q", got)
	}
	setInline(t, s, s.remember(filepath.Join(root, "Kusuriya")), true)
	if got := last("Season 02", "ep 25.mkv", true); got != "" {
		t.Fatalf("при «после каждой серии» допы уже в сезоне, а пришло %q", got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
