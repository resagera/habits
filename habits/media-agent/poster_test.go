package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Название для сравнения: знаки препинания и регистр не в счёт, «ё» — это «е».
func TestNormTitle(t *testing.T) {
	same := [][2]string{
		{"Фантастические твари: Преступления Грин-де-Вальда", "Фантастические твари - Преступления Грин-де-Вальда"},
		{"Элизиум — рай не на Земле", "Элизиум - Рай не на Земле"},
		{"Ёлки", "Елки"},
		{"The Gentlemen", "the gentlemen"},
	}
	for _, p := range same {
		if normTitle(p[0]) != normTitle(p[1]) {
			t.Errorf("должны совпасть: %q и %q → %q / %q", p[0], p[1], normTitle(p[0]), normTitle(p[1]))
		}
	}
	if normTitle("Мама") == normTitle("Мама Мия") {
		t.Error("разные названия совпали")
	}
}

// Варианты поиска: само название, его начало до тире и название из файла.
func TestTitleTries(t *testing.T) {
	tries := titleTries("", true, "Элизиум - Рай не на Земле", 2013)
	if len(tries) != 2 || tries[0].title != "Элизиум - Рай не на Земле" || tries[1].title != "Элизиум" {
		t.Fatalf("варианты: %+v", tries)
	}
	if tries[1].year != 2013 {
		t.Fatalf("год должен переехать в короткий вариант: %+v", tries[1])
	}
	// двоеточие работает так же, а повторы не плодятся
	if got := titleTries("", true, "Фантастические твари: Тайны Дамблдора", 2022); len(got) != 2 ||
		got[1].title != "Фантастические твари" {
		t.Fatalf("двоеточие: %+v", got)
	}
	if got := titleTries("", true, "Мама", 2013); len(got) != 1 {
		t.Fatalf("делить нечего: %+v", got)
	}
}

// Название внутри файла: у русских раздач имя файла бывает транслитом, а в
// метаданных лежит нормальное название — или сразу два через косую черту.
func TestTitleTriesFromFile(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("нет ffmpeg")
	}
	make := func(name, meta string) string {
		p := filepath.Join(t.TempDir(), name)
		cmd := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=size=64x64:rate=5:duration=1",
			"-metadata", "title="+meta, p)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("ffmpeg: %v %s", err, out)
		}
		return p
	}
	names := func(tries []titleTry) string {
		var out []string
		for _, x := range tries {
			out = append(out, fmt.Sprintf("%s (%d)", x.title, x.year))
		}
		return strings.Join(out, " | ")
	}
	// имя файла транслитом, в метаданных — английское название с мусором рипа
	tries := titleTries(make("PoCHti.17.2016.mkv", "The Edge of Seventeen 2016 1080p BluRay x264 DTS-HDChina"), false, "PoCHti 17", 2016)
	if len(tries) != 2 || tries[1].title != "The Edge of Seventeen" || tries[1].year != 2016 {
		t.Fatalf("название из файла не подхватилось: %s", names(tries))
	}
	// «Мама / Mama (2013)» — два названия сразу; то, что уже есть, не повторяем
	tries = titleTries(make("Mama.2013.mkv", "Мама / Mama (2013)"), false, "Mama", 2013)
	if len(tries) != 2 || tries[1].title != "Мама" {
		t.Fatalf("две части не разобрались: %s", names(tries))
	}
	// кракозябры из CP1251 в запрос не идут вовсе
	tries = titleTries(make("Film.2019.mkv", "\uFFFD\uFFFD\uFFFD\uFFFD"), false, "Film", 2019)
	if len(tries) != 1 {
		t.Fatalf("кракозябры попали в варианты: %s", names(tries))
	}
	// подпись рипера попадёт, но помеченной: такому верим только по году
	tries = titleTries(make("Film.2019.mkv", "-=LEONARDO=-"), false, "Film", 2019)
	if len(tries) != 2 || !tries[1].strict {
		t.Fatalf("название из метаданных должно быть строгим: %s", names(tries))
	}
}

// POSTER_LIVE=1 — поиск постеров в настоящем интернете.
func TestPosterLive(t *testing.T) {
	if os.Getenv("POSTER_LIVE") == "" {
		t.Skip("POSTER_LIVE не задан")
	}
	// имя файла транслитом, название — только в метаданных: так лежит «Почти 17»
	p := filepath.Join(t.TempDir(), "PoCHti.17.2016.D.BDRip.1O8OP.mkv")
	cmd := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=size=64x64:rate=5:duration=1",
		"-metadata", "title=The Edge of Seventeen 2016 1080p BluRay x264 DTS-HDChina", p)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, out)
	}
	if data, _, err := findPosterFor(titleTries(p, false, "PoCHti 17", 2016), "film"); err != nil {
		t.Errorf("Почти 17 по метаданным файла: %v", err)
	} else {
		t.Logf("Почти 17 (по метаданным файла): %d КБ", len(data)/1024)
	}
	for _, c := range []struct {
		path, title string
		year        int
	}{
		{"", "Элизиум - Рай не на Земле", 2013},
		{"", "Фантастические твари - Преступления Грин-де-Вальда", 2018},
		{"", "Фантастические твари - Тайны Дамблдора", 2022},
		{"", "The Edge of Seventeen", 2016},
		{"", "Мама", 2013},
		{"", "Я худею", 2018},
		{"", "Love", 2015},
	} {
		tries := titleTries(c.path, c.path == "", c.title, c.year)
		data, ext, err := findPosterFor(tries, "film")
		if err != nil {
			t.Errorf("%s (%d): не нашлось — %v", c.title, c.year, err)
			continue
		}
		t.Logf("%s (%d): %d КБ %s", c.title, c.year, len(data)/1024, ext)
	}
}
