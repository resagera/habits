package main

import (
	"archive/zip"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// В архив попадает разметка и не попадает ни кэш, ни ключ подписи.
func TestBackupContents(t *testing.T) {
	s, _ := adminServer(t)
	cache := s.cfg.cache
	write := func(rel, body string) {
		p := filepath.Join(cache, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("marks.json", `{"m":1}`)
	write("collections.json", `[]`)
	write("collections/cl1.jpg", "обложка папки")
	write("frames/abc/auto-01.jpg", "кадр")
	write("app/background.jpg", "фон")
	write("secret", "ключ-подписи-ссылок")
	write("probe.json", "кэш разбора")
	write("photos/x.jpg", "копия снимка")
	write("transcoded/y.mp4", "перекодированное")
	write("uploads/z.part", "недокачанное")

	path, size, err := s.makeBackup()
	if err != nil {
		t.Fatal(err)
	}
	if size == 0 {
		t.Fatal("пустой архив")
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	inside := map[string]bool{}
	for _, f := range zr.File {
		inside[f.Name] = true
	}
	for _, want := range []string{"marks.json", "collections.json", "collections/cl1.jpg",
		"frames/abc/auto-01.jpg", "app/background.jpg", "ЧТО-ЭТО.txt"} {
		if !inside[want] {
			t.Fatalf("в архиве нет %s: %v", want, inside)
		}
	}
	// секрет наружу не уезжает, а кэш не раздувает архив
	for _, no := range []string{"secret", "probe.json", "photos/x.jpg",
		"transcoded/y.mp4", "uploads/z.part"} {
		if inside[no] {
			t.Fatalf("в архив попало лишнее: %s", no)
		}
	}
}

// Старые архивы не копятся.
func TestBackupKeepsLast(t *testing.T) {
	s, _ := adminServer(t)
	dir := s.backupDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < backupKeep+4; i++ {
		name := filepath.Join(dir, "habits-media-2026010"+string(rune('0'+i%10))+"-0000.zip")
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// чужие файлы в папке не трогаем
	if err := os.WriteFile(filepath.Join(dir, "заметка.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.dropOldBackups(dir)
	entries, _ := os.ReadDir(dir)
	zips := 0
	other := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".zip") {
			zips++
		} else {
			other = true
		}
	}
	if zips > backupKeep {
		t.Fatalf("архивов осталось %d, держим %d", zips, backupKeep)
	}
	if !other {
		t.Fatal("чужой файл из папки пропал")
	}
}

// Ручка админки собирает копию и рассказывает о ней.
func TestAdminBackup(t *testing.T) {
	s, _ := adminServer(t)
	code, out := adminCall(t, s, "POST", "/api/admin/backup", testPin, nil)
	if code != http.StatusOK {
		t.Fatalf("копия: %d %v", code, out)
	}
	b, _ := out["backup"].(map[string]any)
	if b == nil || b["last_size"].(float64) == 0 || b["last_name"] == "" {
		t.Fatalf("о копии ничего не сказано: %v", out)
	}
	// шины в тесте нет — в Telegram уйти не могло, и это должно быть видно
	if b["last_sent"] != false || b["last_error"] == "" {
		t.Fatalf("неудачная отправка не отмечена: %v", b)
	}
	if code, _ := adminCall(t, s, "POST", "/api/admin/backup", "0000", nil); code != http.StatusUnauthorized {
		t.Fatalf("копия собралась без PIN: %d", code)
	}
}

// Места мало — предупреждаем; стало много — говорим и об этом, и только раз.
func TestSpaceWarning(t *testing.T) {
	s, _ := adminServer(t)
	s.cfg.lowSpaceGB = 1 << 20 // «мало» при любом реальном остатке
	for _, sp := range s.spaceSpots() {
		if sp.total == 0 {
			t.Fatalf("раздел без размера: %+v", sp)
		}
	}
	// подменять отправку нечем, поэтому проверяем через память: после первого
	// прогона про раздел записано предупреждение
	s.checkSpace()
	if len(s.maint.get().Space) == 0 {
		t.Fatal("предупреждение не записалось")
	}
	before := len(s.maint.get().Space)
	s.checkSpace()
	if got := len(s.maint.get().Space); got != before {
		t.Fatalf("повтор завёл лишние записи: было %d, стало %d", before, got)
	}
	// места снова много — запись снимается
	s.cfg.lowSpaceGB = 0
	s.checkSpace()
	if len(s.maint.get().Space) != before {
		t.Fatal("при выключенном присмотре записи трогать нельзя")
	}
	s.cfg.lowSpaceGB = 1
	s.checkSpace()
	if len(s.maint.get().Space) != 0 {
		t.Fatalf("после возврата места запись осталась: %v", s.maint.get().Space)
	}
}

// Один раздел — одна строка, но со всеми именами библиотек, которые на нём
// живут: предупреждение «Сериалы — мало места» про диск с фильмами путает.
func TestSpaceSpotsMergeTitles(t *testing.T) {
	s, root := adminServer(t)
	// вторая библиотека на том же разделе, что и первая
	second := filepath.Join(root, "Show")
	s.cfg.roots = append(s.cfg.roots,
		library{ID: shortID(second), Path: second, Title: "Фильмы", Kind: "video"})
	spots := s.spaceSpots()
	if len(spots) != 1 {
		t.Fatalf("раздел один, строк %d: %+v", len(spots), spots)
	}
	for _, want := range []string{"Фильмы", "кэш агента"} {
		if !strings.Contains(spots[0].title, want) {
			t.Fatalf("в подписи нет «%s»: %q", want, spots[0].title)
		}
	}
	if spots[0].total == 0 {
		t.Fatal("размер раздела не определился")
	}
}
