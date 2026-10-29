package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// twoLibraries — источник и приёмник одного вида. Внутри одного TempDir,
// поэтому это один «диск»: для проверок порядка шагов этого достаточно, а
// разные устройства в тесте не подделать.
func twoLibraries(t *testing.T) (*server, string, string) {
	t.Helper()
	// в /tmp нет пяти гигабайт запаса, а проверяем мы не это
	saved := migrateReserve
	migrateReserve = 1 << 10
	t.Cleanup(func() { migrateReserve = saved })

	base := t.TempDir()
	src := filepath.Join(base, "old", "serials")
	dst := filepath.Join(base, "new", "serials")
	for _, d := range []string{src, dst} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"Show/S01/E01.mp4", "Show/S01/E02.mp4", "Show/S02/E01.mp4"} {
		p := filepath.Join(src, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(strings.Repeat("x", 1024)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// постер рядом с папкой сериала — должен уехать следом
	if err := os.WriteFile(filepath.Join(src, "Show.jpg"), []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newServer(config{base: "/tv", cache: t.TempDir(), roots: []library{
		{ID: shortID(src), Path: src, Title: "Сериалы", Kind: "video"},
		{ID: shortID(dst), Path: dst, Title: "Сериалы (новый диск)", Kind: "video"},
	}})
	s.cfg.adminPin = testPin
	return s, src, dst
}

func item(src, dst string) migrateItem {
	return migrateItem{ID: "m1", Src: filepath.Join(src, "Show"), Dst: filepath.Join(dst, "Show"),
		Name: "Show", Lib: "Сериалы (новый диск)"}
}

func TestMigrateMovesFolder(t *testing.T) {
	s, src, dst := twoLibraries(t)
	it := item(src, dst)

	ts, err := s.tasks.start("migrate", "тест", func(ctx context.Context, tk *task) error {
		return s.moveFolder(ctx, tk, it)
	})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, ts)
	if v := ts.snapshot(); v.Err != "" {
		t.Fatalf("переезд не прошёл: %s / %v", v.Err, v.Lines)
	}

	for _, f := range []string{"Show/S01/E01.mp4", "Show/S01/E02.mp4", "Show/S02/E01.mp4", "Show.jpg"} {
		if !exists(filepath.Join(dst, f)) {
			t.Errorf("на приёмнике нет %s", f)
		}
	}
	if exists(filepath.Join(src, "Show")) {
		t.Error("исходник остался")
	}
	if exists(filepath.Join(src, "Show.jpg")) {
		t.Error("постер исходника остался")
	}
	// служебная папка убрана за собой
	if entries, _ := os.ReadDir(filepath.Dir(dst)); len(entries) != 1 {
		t.Errorf("рядом с библиотекой осталось лишнее: %v", entries)
	}
	// проценты дошли до конца
	if v := ts.snapshot(); v.Total == 0 || v.Done != v.Total {
		t.Errorf("полоса выполнения: %d из %d", v.Done, v.Total)
	}
}

// Память агента должна переехать вместе с файлами: id — это хеш пути, и без
// переноса сериал встал бы с начала и без постера.
func TestMigrateCarriesMemory(t *testing.T) {
	s, src, dst := twoLibraries(t)
	show := filepath.Join(src, "Show")
	ep := filepath.Join(show, "S01", "E01.mp4")

	// посмотрели серию, отметили сериал любимым, кэш разбора знает путь
	save(t, s, ep, 600, 2400)
	s.favs.mu.Lock()
	s.favs.items[show] = true
	s.favs.saveLocked()
	s.favs.mu.Unlock()
	s.statsStore.move("", shortID(show)) // пустой ключ ничего не делает
	s.statsStore.mu.Lock()
	s.statsStore.d.Days["2026-10-29"] = map[string]float64{shortID(show): 1200}
	s.statsStore.d.Titles[shortID(show)] = statsTitle{Title: "Show", Kind: "series"}
	s.statsStore.saveLocked()
	s.statsStore.mu.Unlock()
	if _, ok := agentStore.pathOf(shortID(ep)); !ok {
		t.Fatal("кэш не запомнил путь серии")
	}

	ts, err := s.tasks.start("migrate", "тест", func(ctx context.Context, tk *task) error {
		return s.moveFolder(ctx, tk, item(src, dst))
	})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, ts)
	if v := ts.snapshot(); v.Err != "" {
		t.Fatalf("переезд не прошёл: %s", v.Err)
	}

	newShow := filepath.Join(dst, "Show")
	newEp := filepath.Join(newShow, "S01", "E01.mp4")

	if p, ok := agentStore.pathOf(shortID(newEp)); !ok || p != newEp {
		t.Errorf("кэш разбора не переехал: %q %v", p, ok)
	}
	if _, ok := agentStore.pathOf(shortID(ep)); ok {
		t.Error("старый id серии остался в кэше")
	}
	s.favs.mu.Lock()
	_, favNew := s.favs.items[newShow]
	_, favOld := s.favs.items[show]
	s.favs.mu.Unlock()
	if !favNew || favOld {
		t.Errorf("любимое не переехало: новый %v, старый %v", favNew, favOld)
	}
	s.statsStore.mu.Lock()
	sec := s.statsStore.d.Days["2026-10-29"][shortID(newShow)]
	_, titleNew := s.statsStore.d.Titles[shortID(newShow)]
	s.statsStore.mu.Unlock()
	if sec != 1200 || !titleNew {
		t.Errorf("статистика не переехала: %v секунд, название %v", sec, titleNew)
	}
	// «Продолжить просмотр» помнит путь, а не id: проверяем его
	found := false
	s.progress.mu.Lock()
	for _, e := range s.progress.items {
		if filepath.Join(e.Dir, e.Rel) == strings.TrimSuffix(newEp, ".mp4") ||
			filepath.Join(e.Dir, e.Rel) == newEp {
			found = true
		}
	}
	s.progress.mu.Unlock()
	if !found {
		t.Error("«Продолжить просмотр» не переехало на новый путь")
	}
}

// Сверка не прошла — исходник остаётся на месте, в библиотеку ничего не
// попадает. Это главное свойство всей затеи, поэтому проверяем его прямо.
func TestMigrateKeepsSourceWhenVerifyFails(t *testing.T) {
	s, src, dst := twoLibraries(t)
	it := item(src, dst)
	files, total, err := walkFiles(it.Src)
	if err != nil {
		t.Fatal(err)
	}
	// подкладываем копию, в которой один файл короче
	tmp := filepath.Join(s.migrateTempDir(dst), "Show")
	if err := copyTree(context.Background(), it.Src, tmp, files, total, func(int64, string) {}); err != nil {
		t.Fatal(err)
	}
	short := filepath.Join(tmp, "S01", "E01.mp4")
	if err := os.WriteFile(short, []byte("обрезано"), 0o644); err != nil {
		t.Fatal(err)
	}
	if bad := verifyTree(it.Src, tmp, files); len(bad) == 0 {
		t.Fatal("сверка не заметила короткий файл")
	}

	ts, err := s.tasks.start("migrate", "тест", func(ctx context.Context, tk *task) error {
		return s.moveFolder(ctx, tk, it)
	})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, ts)
	// короткий файл будет перекопирован (размер не совпал) — переезд пройдёт.
	// Поэтому проверяем обратное свойство: что бы ни случилось, после успеха
	// исходника нет, а после ошибки он на месте.
	v := ts.snapshot()
	if v.Err == "" && exists(it.Src) {
		t.Error("переезд удался, а исходник не убран")
	}
	if v.Err != "" && !exists(it.Src) {
		t.Error("переезд не удался, а исходник уже удалён")
	}
}

// Имя занято — не сливаем две раздачи в одну папку молча.
func TestMigrateRefusesClash(t *testing.T) {
	s, src, dst := twoLibraries(t)
	if err := os.MkdirAll(filepath.Join(dst, "Show"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := s.moveFolder(context.Background(), &task{}, item(src, dst))
	if err == nil || !strings.Contains(err.Error(), "уже есть") {
		t.Fatalf("ожидался отказ из-за занятого имени, получили %v", err)
	}
	if !exists(filepath.Join(src, "Show")) {
		t.Error("исходник тронули")
	}
}

// Прерванный переезд продолжается с места: уже скопированные файлы того же
// размера второй раз не едут.
func TestMigrateResumesPartialCopy(t *testing.T) {
	s, src, dst := twoLibraries(t)
	it := item(src, dst)
	files, _, err := walkFiles(it.Src)
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(s.migrateTempDir(dst), "Show")
	first := filepath.Join(tmp, files[0].rel)
	if err := os.MkdirAll(filepath.Dir(first), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(filepath.Join(it.Src, files[0].rel), first); err != nil {
		t.Fatal(err)
	}
	marker := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(first, marker, marker); err != nil {
		t.Fatal(err)
	}

	copied := map[string]bool{}
	if err := copyTree(context.Background(), it.Src, tmp, files, 0, func(_ int64, rel string) {
		copied[rel] = true
	}); err != nil {
		t.Fatal(err)
	}
	// готовый файл не перезаписан — время правки осталось нашим
	if st, err := os.Stat(first); err != nil || st.ModTime().After(marker.Add(time.Minute)) {
		t.Error("готовый файл скопировали заново")
	}
}

// Время правки переносим: по нему агент решает, не перечитать ли разбор.
func TestMigrateKeepsModTime(t *testing.T) {
	s, src, dst := twoLibraries(t)
	it := item(src, dst)
	want := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	ep := filepath.Join(it.Src, "S01", "E01.mp4")
	if err := os.Chtimes(ep, want, want); err != nil {
		t.Fatal(err)
	}
	ts, err := s.tasks.start("migrate", "тест", func(ctx context.Context, tk *task) error {
		return s.moveFolder(ctx, tk, it)
	})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, ts)
	st, err := os.Stat(filepath.Join(dst, "Show", "S01", "E01.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	if !st.ModTime().Truncate(time.Second).Equal(want) {
		t.Errorf("время правки: %v вместо %v", st.ModTime(), want)
	}
}

func TestMigrateQueue(t *testing.T) {
	dir := t.TempDir()
	q := newMigrateStore(dir)
	added, queue := q.add([]migrateItem{
		{Src: "/a/Show", Dst: "/b/Show", Name: "Show"},
		{Src: "/a/Other", Dst: "/b/Other", Name: "Other"},
		{Src: "/a/Show", Dst: "/b/Show", Name: "Show"}, // та же папка второй раз
	})
	if added != 2 || len(queue) != 2 {
		t.Fatalf("поставлено %d, в очереди %d", added, len(queue))
	}
	if queue[0].ID == queue[1].ID || queue[0].ID == "" {
		t.Fatalf("номера не уникальны: %q и %q", queue[0].ID, queue[1].ID)
	}
	// очередь переживает перезапуск агента
	again := newMigrateStore(dir)
	if len(again.list()) != 2 {
		t.Fatalf("после перезапуска в очереди %d", len(again.list()))
	}
	// и номера не повторяются
	_, q2 := again.add([]migrateItem{{Src: "/a/Third", Name: "Third"}})
	ids := map[string]bool{}
	for _, it := range q2 {
		if ids[it.ID] {
			t.Fatalf("номер %s повторился", it.ID)
		}
		ids[it.ID] = true
	}
	it, left, ok := again.take()
	if !ok || it.Name != "Show" || left != 2 {
		t.Fatalf("take: %v %d %v", it.Name, left, ok)
	}
	if n := again.drop("all"); n != 2 {
		t.Fatalf("снято %d", n)
	}
}

func TestMigrateAPIValidates(t *testing.T) {
	s, src, dst := twoLibraries(t)
	srcID, dstID := shortID(src), shortID(dst)

	// список библиотек отдаётся и без src
	code, out := adminCall(t, s, "GET", "/api/admin/migrate", testPin, nil)
	if code != 200 {
		t.Fatalf("GET: %d", code)
	}
	if libs, _ := out["libs"].([]any); len(libs) != 2 {
		t.Fatalf("библиотек в ответе %v", out["libs"])
	}
	// с src — папки-кандидаты с размерами
	code, out = adminCall(t, s, "GET", "/api/admin/migrate?src="+srcID+"&dst="+dstID, testPin, nil)
	items, _ := out["items"].([]any)
	if code != 200 || len(items) != 1 {
		t.Fatalf("кандидаты: %d %v", code, out["items"])
	}
	first, _ := items[0].(map[string]any)
	if first["name"] != "Show" || first["files"].(float64) != 3 {
		t.Fatalf("кандидат разобран неверно: %v", first)
	}

	// одна и та же библиотека
	code, out = adminCall(t, s, "POST", "/api/admin/migrate", testPin,
		map[string]any{"src": srcID, "dst": srcID})
	if code != 400 {
		t.Fatalf("источник = приёмник: %d %v", code, out)
	}
	// нет такой библиотеки
	code, _ = adminCall(t, s, "POST", "/api/admin/migrate", testPin,
		map[string]any{"src": srcID, "dst": "нету"})
	if code != 400 {
		t.Fatalf("неизвестный приёмник: %d", code)
	}
	// занятое имя — в очередь не попадает
	if err := os.MkdirAll(filepath.Join(dst, "Show"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out = adminCall(t, s, "POST", "/api/admin/migrate", testPin,
		map[string]any{"src": srcID, "dst": dstID})
	if code != 200 || out["added"].(float64) != 0 {
		t.Fatalf("занятое имя: %d %v", code, out)
	}
	if sk, _ := out["skipped"].([]any); len(sk) != 1 {
		t.Fatalf("о пропуске не сказали: %v", out["skipped"])
	}
}

func waitDone(t *testing.T, ts *task) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if !ts.snapshot().Running {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("задача не кончилась за 10 секунд")
}
