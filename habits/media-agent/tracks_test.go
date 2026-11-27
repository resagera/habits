package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSideLabel(t *testing.T) {
	v := "/s/Show/S01/S01E01.mkv"
	cases := []struct {
		side, label, lang string
	}{
		{"/s/Show/S01/S01E01.mka", "", ""},
		{"/s/Show/S01/S01E01.rus.LostFilm.ac3", "LostFilm", "rus"},
		{"/s/Show/S01/Rus Sound/LostFilm/S01E01.mka", "LostFilm", "rus"},
		{"/s/Show/S01/Звук/Кубик в кубе/S01E01.mka", "Кубик в кубе", ""},
		{"/s/Show/S01/Subs/S01E01.eng.srt", "", "eng"},
		{"/s/Show/S01/S01E01.[AniLibria].mka", "AniLibria", ""},
	}
	for _, c := range cases {
		names, lang := sideLabel(v, c.side)
		if strings.Join(names, " · ") != c.label || lang != c.lang {
			t.Errorf("%s: %q %q", c.side, names, lang)
		}
	}
	if !sideMatches("S01E01.rus", "S01E01") || sideMatches("S01E010.rus", "S01E01") || sideMatches("S01E01x", "S01E01") {
		t.Fatal("sideMatches")
	}
}

// Дорожки в подпапках находятся по индексу: после обхода папки не читаются.
func TestTracksFromIndex(t *testing.T) {
	s, _, films := filesLibrary(t)
	show := filepath.Join(films, "Show", "S01")
	for _, p := range []string{
		filepath.Join(show, "S01E01.mkv"),
		filepath.Join(show, "S01E02.mkv"),
		filepath.Join(show, "Rus Sound", "LostFilm", "S01E01.mka"),
		filepath.Join(show, "Rus Sound", "LostFilm", "S01E02.mka"),
		filepath.Join(show, "S01E01.eng.mp3"),
		filepath.Join(show, "Subs", "S01E01.ru.srt"),
		filepath.Join(show, "S01E01.ru.ass"),
	} {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	walk(t, s)
	// после обхода дорожка, положенная мимо агента, не видна до следующего
	// обхода — значит, список действительно берётся из индекса
	_ = os.WriteFile(filepath.Join(show, "S01E02.late.mka"), []byte("x"), 0o644)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/tv/api/browse?id="+s.remember(show), nil))
	var out struct {
		Items []outItem `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	var e1, e2 outItem
	for _, it := range out.Items {
		switch it.Name {
		case "S01E01.mkv":
			e1 = it
		case "S01E02.mkv":
			e2 = it
		}
	}
	if len(e1.Audio) != 3 || !e1.Audio[0].Main {
		t.Fatalf("озвучки E01: %+v", e1.Audio)
	}
	byLabel := map[string]trackRef{}
	for _, a := range e1.Audio {
		byLabel[a.Label] = a
	}
	if lf := byLabel["LostFilm"]; !lf.Live || !lf.File || lf.Lang != "rus" || !strings.Contains(lf.URL, "/trackaudio/") {
		t.Fatalf("LostFilm: %+v", e1.Audio)
	}
	if en := byLabel["Английская"]; en.Live || !strings.Contains(en.URL, "/media/") {
		t.Fatalf("mp3 играется сам: %+v", e1.Audio)
	}
	if len(e1.Subs) != 2 {
		t.Fatalf("субтитры E01: %+v", e1.Subs)
	}
	if len(e2.Audio) != 2 || e2.Audio[1].Label != "LostFilm" {
		t.Fatalf("E02 (поздняя дорожка ещё не в индексе): %+v", e2.Audio)
	}
	// mp3-озвучка и папки «Rus Sound», «Subs» — не серии и не сезоны
	for _, it := range out.Items {
		if it.Name != "S01E01.mkv" && it.Name != "S01E02.mkv" {
			t.Fatalf("лишнее в списке серий: %s", it.Name)
		}
	}
}

func makeTwoTrackMKV(t *testing.T, path string) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("нет ffmpeg")
	}
	srt := filepath.Join(t.TempDir(), "s.srt")
	_ = os.WriteFile(srt, []byte("1\n00:00:00,500 --> 00:00:01,500\nПривет\n"), 0o644)
	cmd := exec.Command("ffmpeg", "-v", "error", "-y",
		"-f", "lavfi", "-i", "color=c=black:s=64x48:d=3",
		"-f", "lavfi", "-i", "sine=f=440:d=3",
		"-f", "lavfi", "-i", "sine=f=880:d=3",
		"-i", srt,
		"-map", "0:v", "-map", "1:a", "-map", "2:a", "-map", "3:s",
		"-c:v", "mpeg4", "-c:a", "aac", "-c:s", "srt",
		"-metadata:s:a:0", "language=eng", "-metadata:s:a:1", "language=rus",
		"-metadata:s:a:1", "title=Кубик в кубе", "-metadata:s:s:0", "language=rus",
		path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, out)
	}
}

// Встроенные дорожки: живой звук второй дорожки и выгрузка рядом файлами.
func TestTracksEmbeddedLiveAndExtract(t *testing.T) {
	s, _, films := filesLibrary(t)
	s.cfg.adminPin = testPin
	show := filepath.Join(films, "Show")
	_ = os.MkdirAll(show, 0o755)
	v := filepath.Join(show, "S01E01.mkv")
	makeTwoTrackMKV(t, v)
	walk(t, s)

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/tv/api/browse?id="+s.remember(show), nil))
	var out struct {
		Items []outItem `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Items) != 1 || len(out.Items[0].Audio) != 2 {
		t.Fatalf("встроенные дорожки: %+v", out.Items)
	}
	second := out.Items[0].Audio[1]
	if second.Label != "Кубик в кубе" || !second.Live {
		t.Fatalf("вторая дорожка: %+v", second)
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", second.URL+"&t=1", nil))
	body := rec.Body.Bytes()
	// ADTS начинается со слова синхронизации 0xFFF
	if rec.Code != 200 || len(body) < 100 || body[0] != 0xff || body[1]&0xf0 != 0xf0 {
		t.Fatalf("живой звук: %d, %d байт", rec.Code, len(body))
	}

	ids := []string{s.remember(show)}
	code, scan := adminCall(t, s, "POST", "/api/admin/tracks/scan", testPin, map[string]any{"ids": ids})
	if code != 200 {
		t.Fatalf("scan: %d %v", code, scan)
	}
	var keys []string
	for _, tr := range scan["tracks"].([]any) {
		m := tr.(map[string]any)
		if m["label"] == "Кубик в кубе" || m["type"] == "sub" {
			keys = append(keys, m["key"].(string))
		}
	}
	if len(keys) != 2 {
		t.Fatalf("scan: %v", scan)
	}
	if code, out := adminCall(t, s, "POST", "/api/admin/tracks/extract", testPin,
		map[string]any{"ids": ids, "keys": keys}); code != 200 {
		t.Fatalf("extract: %d %v", code, out)
	}
	waitIdle(t, s)
	mka := filepath.Join(show, "S01E01.rus.Кубик в кубе.mka")
	srt := filepath.Join(show, "S01E01.rus.srt")
	for _, p := range []string{mka, srt} {
		if st, err := os.Stat(p); err != nil || st.Size() == 0 {
			t.Fatalf("нет файла %s: %v", filepath.Base(p), err)
		}
	}
	// выгруженное сразу в индексе и в списке серии — без обхода
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/tv/api/browse?id="+s.remember(show), nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Items[0].Audio) != 3 || len(out.Items[0].Subs) != 1 || out.Items[0].Subs[0].Label != "Русские" {
		t.Fatalf("после выгрузки: %+v %+v", out.Items[0].Audio, out.Items[0].Subs)
	}
	// повторный прогон ничего не перезаписывает
	_, scan = adminCall(t, s, "POST", "/api/admin/tracks/scan", testPin, map[string]any{"ids": ids})
	for _, tr := range scan["tracks"].([]any) {
		m := tr.(map[string]any)
		if (m["label"] == "Кубик в кубе" || m["type"] == "sub") && m["done"].(float64) != 1 {
			t.Fatalf("done: %v", m)
		}
	}
}
