package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeYtdlp — скрипт вместо yt-dlp: печатает тот же прогресс и кладёт в «-P»
// названные файлы. Так проверяется весь путь задачи, кроме самой сети.
func fakeYtdlp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "yt-dlp_linux")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func fetchTask(t *testing.T, s *server, bin, url, dir string, playlist bool) *task {
	t.Helper()
	tk := &task{v: taskView{Lines: []string{}}}
	err := s.fetchAudio(context.Background(), tk, bin, url, dir, playlist, 1, 1)
	if err != nil {
		t.Fatalf("скачивание: %v", err)
	}
	return tk
}

// Главное: звук из временной папки доезжает до выбранного альбома, и имя
// получается читаемым — именем дорожки в плеере служит имя файла.
func TestFetchAudioMovesTrack(t *testing.T) {
	s, _, music, _ := looksServer(t)
	dir := filepath.Join(music, "Альбом")
	bin := fakeYtdlp(t, `
while [ "$1" != "--" ]; do
  if [ "$1" = "-P" ]; then shift; out="$1"; fi
  shift
done
echo "[download]  50.0% of 3.00MiB"
echo "[download] 100.0% of 3.00MiB"
echo "[ExtractAudio] Destination: $out/Песня.opus"
printf 'opus' > "$out/Песня.opus"
`)
	tk := fetchTask(t, s, bin, "https://example.com/watch?v=1", dir, false)
	if _, err := os.Stat(filepath.Join(dir, "Песня.opus")); err != nil {
		t.Fatalf("трека нет в альбоме: %v", err)
	}
	v := tk.snapshot()
	if !strings.Contains(v.Stage, "файлов: 1") {
		t.Fatalf("итог задачи: %q", v.Stage)
	}
	if len(v.Lines) != 1 || v.Lines[0] != "Песня.opus" {
		t.Fatalf("в отчёте должно быть имя файла, а там %v", v.Lines)
	}
	// временная папка убрана за собой
	left, _ := filepath.Glob(filepath.Join(s.cfg.cache, "fetch-*"))
	if len(left) != 0 {
		t.Fatalf("после задачи остался мусор: %v", left)
	}
}

// Занятое имя не затирается: второй трек с тем же названием становится «(2)».
func TestFetchAudioKeepsExisting(t *testing.T) {
	s, _, music, _ := looksServer(t)
	dir := filepath.Join(music, "Альбом")
	if err := os.WriteFile(filepath.Join(dir, "Песня.opus"), []byte("старый"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := fakeYtdlp(t, `
while [ "$1" != "--" ]; do
  if [ "$1" = "-P" ]; then shift; out="$1"; fi
  shift
done
printf 'новый' > "$out/Песня.opus"
`)
	fetchTask(t, s, bin, "https://example.com/watch?v=1", dir, false)
	old, err := os.ReadFile(filepath.Join(dir, "Песня.opus"))
	if err != nil || string(old) != "старый" {
		t.Fatalf("прежний файл подменён: %q, %v", old, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Песня (2).opus")); err != nil {
		t.Fatalf("второго трека нет: %v", err)
	}
}

// Из временной папки берём только звук: обложки и прочий хвост yt-dlp в
// библиотеку не едут, а недокачанное «.part» — тем более.
func TestFetchAudioTakesOnlyAudio(t *testing.T) {
	s, _, music, _ := looksServer(t)
	dir := filepath.Join(music, "Альбом")
	bin := fakeYtdlp(t, `
while [ "$1" != "--" ]; do
  if [ "$1" = "-P" ]; then shift; out="$1"; fi
  shift
done
printf 'x' > "$out/Трек.m4a"
printf 'x' > "$out/Трек.jpg"
printf 'x' > "$out/Трек.m4a.part"
printf 'x' > "$out/Трек.info.json"
`)
	fetchTask(t, s, bin, "https://example.com/x", dir, false)
	got, _ := os.ReadDir(dir)
	names := []string{}
	for _, e := range got {
		names = append(names, e.Name())
	}
	for _, bad := range []string{"Трек.jpg", "Трек.m4a.part", "Трек.info.json"} {
		if _, err := os.Stat(filepath.Join(dir, bad)); err == nil {
			t.Fatalf("в альбом попало лишнее (%s): %v", bad, names)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "Трек.m4a")); err != nil {
		t.Fatalf("звука нет: %v (в папке %v)", err, names)
	}
}

// Ошибка без единого файла — это ошибка задачи, и в отчёте видно, какая:
// по молчанию «не получилось» ничего не починить.
func TestFetchAudioReportsError(t *testing.T) {
	s, _, music, _ := looksServer(t)
	bin := fakeYtdlp(t, `
echo "[youtube] Extracting URL: https://example.com/x"
echo "[youtube] x: Downloading webpage"
echo "ERROR: Video unavailable" >&2
exit 1
`)
	tk := &task{v: taskView{Lines: []string{}}}
	err := s.fetchAudio(context.Background(), tk, bin, "https://example.com/x",
		filepath.Join(music, "Альбом"), false, 1, 1)
	if err == nil {
		t.Fatal("ждали ошибку")
	}
	// В отказе должна быть причина, а не хвост вывода: «Extracting URL» и
	// «Downloading webpage» ничего не объясняют.
	if err.Error() != "Video unavailable" {
		t.Fatalf("ошибка должна быть одной причиной, а это %q", err)
	}
	if lines := tk.snapshot().Lines; len(lines) == 0 || !strings.Contains(lines[0], "ERROR") {
		t.Fatalf("строка ERROR не попала в отчёт: %v", lines)
	}
}

// Файл доехал, а на следующей позиции подборки сорвалось — трек всё равно
// остаётся в альбоме, а причина уходит в отчёт.
func TestFetchAudioKeepsWhatArrived(t *testing.T) {
	s, _, music, _ := looksServer(t)
	dir := filepath.Join(music, "Альбом")
	bin := fakeYtdlp(t, `
while [ "$1" != "--" ]; do
  if [ "$1" = "-P" ]; then shift; out="$1"; fi
  shift
done
echo "[download] Downloading item 1 of 2"
printf 'x' > "$out/Первый.opus"
echo "ERROR: Private video" >&2
exit 1
`)
	tk := &task{v: taskView{Lines: []string{}}}
	if err := s.fetchAudio(context.Background(), tk, bin, "https://example.com/x", dir, true, 1, 1); err != nil {
		t.Fatalf("задача не должна падать, если что-то доехало: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Первый.opus")); err != nil {
		t.Fatalf("доехавший трек потерян: %v", err)
	}
	if !strings.Contains(strings.Join(tk.snapshot().Lines, "\n"), "Private video") {
		t.Fatalf("причина обрыва не видна: %v", tk.snapshot().Lines)
	}
}

// Один трек или вся подборка — это разные ключи, и перепутать их нельзя:
// ссылка из плеера подборки иначе притащила бы сотню чужих песен.
func TestFetchAudioPlaylistFlag(t *testing.T) {
	s, _, music, _ := looksServer(t)
	dir := filepath.Join(music, "Альбом")
	bin := fakeYtdlp(t, `
echo "$@" > "$HOME/args.txt"
while [ "$1" != "--" ]; do
  if [ "$1" = "-P" ]; then shift; out="$1"; fi
  shift
done
printf 'x' > "$out/Т.opus"
`)
	home := t.TempDir()
	t.Setenv("HOME", home)
	fetchTask(t, s, bin, "https://example.com/x", dir, false)
	args, _ := os.ReadFile(filepath.Join(home, "args.txt"))
	if !strings.Contains(string(args), "--no-playlist") {
		t.Fatalf("у одной ссылки должен быть --no-playlist: %s", args)
	}
	_ = os.Remove(filepath.Join(dir, "Т.opus"))
	fetchTask(t, s, bin, "https://example.com/x", dir, true)
	args, _ = os.ReadFile(filepath.Join(home, "args.txt"))
	if !strings.Contains(string(args), "--yes-playlist") || !strings.Contains(string(args), "--playlist-end") {
		t.Fatalf("у подборки должны быть --yes-playlist и предел: %s", args)
	}
}

// Остановка задачи убивает процесс, а не ждёт его: «✖ Остановить» иначе
// ничего бы не меняло.
func TestFetchAudioCancels(t *testing.T) {
	s, _, music, _ := looksServer(t)
	bin := fakeYtdlp(t, "sleep 60\n")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.fetchAudio(ctx, &task{v: taskView{Lines: []string{}}}, bin,
			"https://example.com/x", filepath.Join(music, "Альбом"), false, 1, 1)
	}()
	cancel()
	if err := <-done; err == nil {
		t.Fatal("после отмены задача должна вернуть ошибку")
	}
}

func TestFetchFileName(t *testing.T) {
	cases := map[string]string{
		"Песня.opus":           "Песня.opus",
		" Песня.m4a":           "Песня.m4a",
		"../../etc/passwd.mp3": "passwd.mp3",
		`C:\music\Song.mp3`:    "Song.mp3",
	}
	for in, want := range cases {
		if got := fetchFileName(in); got != want {
			t.Errorf("fetchFileName(%q) = %q, ждали %q", in, got, want)
		}
	}
	// скрытым файлом трек быть не может: каталог такое не показывает
	if got := fetchFileName(".opus"); strings.HasPrefix(got, ".") {
		t.Errorf("скрытое имя: %q", got)
	}
}

// Путь к yt-dlp: указанный в настройках главнее найденного рядом, а
// отсутствие файла — внятная ошибка, а не «не найден».
func TestYtdlpBin(t *testing.T) {
	s, _, _, _ := looksServer(t)
	bin := fakeYtdlp(t, "exit 0\n")
	s.cfg.ytdlp = bin
	if got, err := s.ytdlpBin(); err != nil || got != bin {
		t.Fatalf("ytdlpBin = %q, %v", got, err)
	}
	s.cfg.ytdlp = filepath.Join(t.TempDir(), "нет-такого")
	_, err := s.ytdlpBin()
	if err == nil || !strings.Contains(err.Error(), "нет-такого") {
		t.Fatalf("ошибка должна называть путь: %v", err)
	}
}

func TestFetchProgressRegexps(t *testing.T) {
	if m := reFetchPercent.FindStringSubmatch("[download]  45.2% of 4.00MiB at 1.00MiB/s"); m == nil || m[1] != "45.2" {
		t.Fatalf("проценты не разобрались: %v", m)
	}
	// «item» в свежем yt-dlp, «video» в прежних — читаем оба
	for _, line := range []string{"[download] Downloading item 3 of 12", "[download] Downloading video 3 of 12"} {
		m := reFetchItem.FindStringSubmatch(line)
		if m == nil || m[1] != "3" || m[2] != "12" {
			t.Fatalf("позиция подборки не разобралась в %q: %v", line, m)
		}
	}
	if m := reFetchDest.FindStringSubmatch("[ExtractAudio] Destination: /tmp/x/Песня.opus"); m == nil {
		t.Fatalf("имя файла не разобралось: %v", m)
	}
	if parseFloatSafe("45.2") != 45.2 || parseFloatSafe("ерунда") != 0 || parseFloatSafe("900") != 0 {
		t.Fatal("parseFloatSafe")
	}
	if atoiSafe("12") != 12 || atoiSafe("1x") != 0 {
		t.Fatal("atoiSafe")
	}
}

// --- ручки ---

// Состояние для страницы: нет yt-dlp — не «ошибка 500», а понятный ответ с
// подсказкой, откуда его взять.
func TestAdminFetchStateMissing(t *testing.T) {
	s, _, _, _ := looksServer(t)
	s.cfg.ytdlp = filepath.Join(t.TempDir(), "нет")
	code, out := adminCall(t, s, "GET", "/api/admin/fetch", testPin, nil)
	if code != 200 || out["ready"] != false {
		t.Fatalf("ответ %d %v", code, out)
	}
	if hint, _ := out["hint"].(string); !strings.Contains(hint, "yt-dlp_linux") {
		t.Fatalf("подсказки нет: %v", out)
	}
}

func TestAdminFetchStateReady(t *testing.T) {
	s, _, _, _ := looksServer(t)
	s.cfg.ytdlp = fakeYtdlp(t, "echo 2026.10.01\n")
	code, out := adminCall(t, s, "GET", "/api/admin/fetch", testPin, nil)
	if code != 200 || out["ready"] != true || out["version"] != "2026.10.01" {
		t.Fatalf("ответ %d %v", code, out)
	}
}

// Отказы: не ссылка, не папка, нет yt-dlp. Задача при этом не ставится —
// иначе список задач заполнялся бы пустыми попытками.
func TestAdminFetchRefuses(t *testing.T) {
	s, _, music, _ := looksServer(t)
	s.cfg.ytdlp = fakeYtdlp(t, "exit 0\n")
	dirID := s.remember(filepath.Join(music, "Альбом"))
	fileID := s.remember(filepath.Join(music, "Альбом", "01.mp3"))

	if code, out := adminCall(t, s, "POST", "/api/admin/fetch", testPin,
		map[string]any{"url": "ерунда", "id": dirID}); code != 400 || out["error"] == nil {
		t.Fatalf("не ссылка: %d %v", code, out)
	}
	if code, _ := adminCall(t, s, "POST", "/api/admin/fetch", testPin,
		map[string]any{"url": "https://example.com/x", "id": fileID}); code != 400 {
		t.Fatalf("в файл качать нельзя, а ответ %d", code)
	}
	s.cfg.ytdlp = filepath.Join(t.TempDir(), "нет")
	if code, out := adminCall(t, s, "POST", "/api/admin/fetch", testPin,
		map[string]any{"url": "https://example.com/x", "id": dirID}); code != 400 || out["hint"] == nil {
		t.Fatalf("без yt-dlp: %d %v", code, out)
	}
	if n := len(s.tasks.list()); n != 0 {
		t.Fatalf("отказы не должны ставить задачу, а их %d", n)
	}
}

// Версия спрашивается один раз: сам yt-dlp разворачивается из однофайловой
// сборки полторы секунды, а «Обслуживание» дёргает это каждое открытие.
// Перезапись бинарника («-U») кэш сбрасывает.
func TestYtdlpVersionCached(t *testing.T) {
	bin := fakeYtdlp(t, `
n=$(cat "$0.calls" 2>/dev/null || echo 0)
echo $((n+1)) > "$0.calls"
echo 2026.01.01
`)
	calls := func() string {
		data, _ := os.ReadFile(bin + ".calls")
		return strings.TrimSpace(string(data))
	}
	for i := 0; i < 3; i++ {
		if v, err := ytdlpVersion(context.Background(), bin); err != nil || v != "2026.01.01" {
			t.Fatalf("версия: %q, %v", v, err)
		}
	}
	if calls() != "1" {
		t.Fatalf("yt-dlp должен был запуститься один раз, а запусков %s", calls())
	}
	// обновление перезаписывает файл — версию надо спросить заново
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 2026.02.02\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if v, _ := ytdlpVersion(context.Background(), bin); v != "2026.02.02" {
		t.Fatalf("после обновления версия должна смениться, а это %q", v)
	}
}

// Имя файла — как у всей остальной музыки в медиатеке: с «[идентификатором]»
// в конце. Она собрана тем же yt-dlp, и новое не должно выделяться.
func TestFetchAudioNamesLikeLibrary(t *testing.T) {
	s, _, music, _ := looksServer(t)
	dir := filepath.Join(music, "Альбом")
	// подставляем шаблон «-o» сами: так проверяется имя на диске, а не ключ
	bin := fakeYtdlp(t, `
while [ "$1" != "--" ]; do
  if [ "$1" = "-P" ]; then shift; out="$1"; fi
  if [ "$1" = "-o" ]; then shift; tmpl="$1"; fi
  shift
done
name=$(printf '%s' "$tmpl" | sed -e 's/%(title)\.150B/Песня/' -e 's/%(id)s/aqz-KE-bpKQ/' -e 's/%(ext)s/opus/')
printf 'x' > "$out/$name"
`)
	fetchTask(t, s, bin, "https://example.com/watch?v=aqz-KE-bpKQ", dir, false)
	want := "Песня [aqz-KE-bpKQ].opus"
	if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
		got, _ := os.ReadDir(dir)
		names := []string{}
		for _, e := range got {
			names = append(names, e.Name())
		}
		t.Fatalf("ждали %q, а в альбоме %v", want, names)
	}
}

// --- очередь ---

func qItem(dir, url string, n int) fetchItem {
	return fetchItem{ID: fmt.Sprintf("q%d", n), URL: url, Dir: dir, Where: filepath.Base(dir), Added: 1}
}

// Каждая ссылка пишет в $HOME/order.txt свой адрес и кладёт файл по имени из
// него: так видно и порядок, и что доехало.
func queueYtdlp(t *testing.T) string {
	t.Helper()
	return fakeYtdlp(t, `
while [ "$1" != "--" ]; do
  if [ "$1" = "-P" ]; then shift; out="$1"; fi
  shift
done
shift
echo "$1" >> "$HOME/order.txt"
printf 'x' > "$out/$(basename "$1").opus"
`)
}

// Несколько ссылок за раз качаются по одной и в том порядке, в котором их
// набрали: очередь на то и очередь.
func TestFetchQueueDrainsInOrder(t *testing.T) {
	s, _, music, _ := looksServer(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	s.cfg.ytdlp = queueYtdlp(t)
	dir := filepath.Join(music, "Альбом")

	added, queue := s.fetchq.add([]fetchItem{
		qItem(dir, "https://example.com/a", 1),
		qItem(dir, "https://example.com/b", 2),
		qItem(dir, "https://example.com/c", 3),
	})
	if added != 3 || len(queue) != 3 {
		t.Fatalf("в очередь встало %d, в ней %d", added, len(queue))
	}
	tk := &task{v: taskView{Lines: []string{}}}
	if err := s.fetchDrain(context.Background(), tk); err != nil {
		t.Fatalf("очередь: %v", err)
	}
	order, _ := os.ReadFile(filepath.Join(home, "order.txt"))
	want := "https://example.com/a\nhttps://example.com/b\nhttps://example.com/c\n"
	if string(order) != want {
		t.Fatalf("порядок закачек: %q", order)
	}
	for _, name := range []string{"a.opus", "b.opus", "c.opus"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s не доехал: %v", name, err)
		}
	}
	if left := s.fetchq.list(); len(left) != 0 {
		t.Fatalf("после работы в очереди осталось %d", len(left))
	}
	// полоска у очереди считает ссылки, а не проценты одной закачки
	if v := tk.snapshot(); !strings.Contains(v.Stage, "3 из 3") || v.Total != 3 {
		t.Fatalf("состояние задачи: %q, %d из %d", v.Stage, v.Done, v.Total)
	}
}

// Тот же адрес в ту же папку второй раз не берём: «Скачать» легко нажать
// дважды, а приедет он тогда дважды, второй раз с «(2)».
func TestFetchQueueSkipsSame(t *testing.T) {
	s, _, music, _ := looksServer(t)
	dir := filepath.Join(music, "Альбом")
	if added, _ := s.fetchq.add([]fetchItem{qItem(dir, "https://example.com/a", 1)}); added != 1 {
		t.Fatalf("первая ссылка не встала")
	}
	added, queue := s.fetchq.add([]fetchItem{qItem(dir, "https://example.com/a", 2)})
	if added != 0 || len(queue) != 1 {
		t.Fatalf("повтор встал в очередь: %d, всего %d", added, len(queue))
	}
	// в другую папку — это другая работа, её берём
	if added, _ := s.fetchq.add([]fetchItem{qItem(filepath.Join(music, "Второй"), "https://example.com/a", 3)}); added != 1 {
		t.Fatalf("та же ссылка в другую папку должна встать")
	}
}

// Одна ссылка сорвалась — остальные всё равно качаются: иначе первая же
// опечатка в середине списка останавливала бы всю работу.
func TestFetchQueueSurvivesOneBad(t *testing.T) {
	s, _, music, _ := looksServer(t)
	t.Setenv("HOME", t.TempDir())
	s.cfg.ytdlp = queueYtdlp(t)
	dir := filepath.Join(music, "Альбом")

	s.fetchq.add([]fetchItem{
		qItem(dir, "https://example.com/a", 1),
		qItem(filepath.Join(music, "Такой папки нет"), "https://example.com/b", 2),
		qItem(dir, "https://example.com/c", 3),
	})
	tk := &task{v: taskView{Lines: []string{}}}
	if err := s.fetchDrain(context.Background(), tk); err != nil {
		t.Fatalf("из-за одной сорвавшейся упала вся очередь: %v", err)
	}
	for _, name := range []string{"a.opus", "c.opus"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s не доехал: %v", name, err)
		}
	}
	v := tk.snapshot()
	if !strings.Contains(strings.Join(v.Lines, "\n"), "больше нет") {
		t.Fatalf("причина не видна: %v", v.Lines)
	}
	// Счёт неудач — строкой отчёта: состояние задачи после возврата всё равно
	// станет «готово», и «не вышло» в нём не доживёт до страницы.
	if !strings.Contains(strings.Join(v.Lines, "\n"), "не вышло: 1 из 3") {
		t.Fatalf("итог должен считать неудачи: %v", v.Lines)
	}
}

// «Остановить» снимает всю очередь: иначе fetchWorker поднял бы её заново
// через полминуты, и кнопка выглядела бы сломанной.
func TestFetchQueueStopClears(t *testing.T) {
	s, _, music, _ := looksServer(t)
	s.cfg.ytdlp = fakeYtdlp(t, "sleep 60\n")
	dir := filepath.Join(music, "Альбом")
	s.fetchq.add([]fetchItem{
		qItem(dir, "https://example.com/a", 1),
		qItem(dir, "https://example.com/b", 2),
	})
	ctx, cancel := context.WithCancel(context.Background())
	tk := &task{v: taskView{Lines: []string{}}}
	done := make(chan error, 1)
	go func() { done <- s.fetchDrain(ctx, tk) }()
	cancel()
	if err := <-done; err == nil {
		t.Fatal("после отмены очередь должна вернуть ошибку")
	}
	if left := s.fetchq.list(); len(left) != 0 {
		t.Fatalf("после «Остановить» в очереди осталось %d", len(left))
	}
}

// Очередь лежит на диске: деплой перезапускает агента, а набранные руками
// ссылки должны его пережить.
func TestFetchQueueSurvivesRestart(t *testing.T) {
	s, _, music, _ := looksServer(t)
	dir := filepath.Join(music, "Альбом")
	s.fetchq.add([]fetchItem{qItem(dir, "https://example.com/a", 1)})
	again := newFetchStore(s.cfg.cache)
	if items := again.list(); len(items) != 1 || items[0].URL != "https://example.com/a" {
		t.Fatalf("очередь не пережила перезапуск: %v", items)
	}
}

func TestFetchQueueDrop(t *testing.T) {
	s, _, music, _ := looksServer(t)
	dir := filepath.Join(music, "Альбом")
	s.fetchq.add([]fetchItem{
		qItem(dir, "https://example.com/a", 1),
		qItem(dir, "https://example.com/b", 2),
	})
	if gone := s.fetchq.drop("q1"); gone != 1 {
		t.Fatalf("снято %d", gone)
	}
	if items := s.fetchq.list(); len(items) != 1 || items[0].ID != "q2" {
		t.Fatalf("осталось не то: %v", items)
	}
	if gone := s.fetchq.drop(""); gone != 1 || len(s.fetchq.list()) != 0 {
		t.Fatalf("пустой id должен снять всё, а снято %d", gone)
	}
}

// Поле на несколько строк: разделителем может быть и перевод строки, и пробел.
func TestFetchURLs(t *testing.T) {
	got, err := fetchURLs([]string{"https://a.ru/1\nhttps://b.ru/2\n  \nhttps://c.ru/3", ""})
	if err != nil || len(got) != 3 || got[2] != "https://c.ru/3" {
		t.Fatalf("разбор строк: %v, %v", got, err)
	}
	if got, err := fetchURLs([]string{"a.ru/1 b.ru/2"}); err != nil || len(got) != 2 ||
		got[0] != "https://a.ru/1" {
		t.Fatalf("схему надо дописать: %v, %v", got, err)
	}
	// набранное наугад слово — не ссылка: задача, которой нечего делать, не нужна
	if _, err := fetchURLs([]string{"https://a.ru/1\nерунда"}); err == nil ||
		!strings.Contains(err.Error(), "ерунда") {
		t.Fatalf("негодная строка должна быть названа: %v", err)
	}
	if _, err := fetchURLs([]string{"", "   "}); err == nil {
		t.Fatal("пустое поле — отказ")
	}
	if _, err := fetchURLs([]string{strings.Repeat("https://a.ru/x ", fetchQueueMax+1)}); err == nil {
		t.Fatal("предел на число ссылок за раз")
	}
}

func waitIdle(t *testing.T, s *server) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if !s.tasks.busy() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("задача не закончилась")
}

// Несколько ссылок одним запросом: ответ говорит, сколько встало, и агент
// сразу берётся за работу.
func TestAdminFetchQueuesMany(t *testing.T) {
	s, _, music, _ := looksServer(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	s.cfg.ytdlp = queueYtdlp(t)
	dir := filepath.Join(music, "Альбом")
	dirID := s.remember(dir)

	code, out := adminCall(t, s, "POST", "/api/admin/fetch", testPin, map[string]any{
		"id":   dirID,
		"urls": []string{"https://example.com/a", "https://example.com/b"},
	})
	if code != 200 || out["added"] != 2.0 || out["task"] == nil {
		t.Fatalf("ответ %d %v", code, out)
	}
	waitIdle(t, s)
	for _, name := range []string{"a.opus", "b.opus"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s не доехал: %v", name, err)
		}
	}
	// одна задача на всю очередь: список задач не должен ею заполняться
	if n := len(s.tasks.list()); n != 1 {
		t.Fatalf("задач %d, а должна быть одна на всю очередь", n)
	}
}

// Пока агент занят другой работой, ссылки просто ждут в очереди — это не
// отказ: иначе набрать их во время перекодирования было бы нельзя.
func TestAdminFetchQueuesWhileBusy(t *testing.T) {
	s, _, music, _ := looksServer(t)
	s.cfg.ytdlp = fakeYtdlp(t, "exit 0\n")
	dirID := s.remember(filepath.Join(music, "Альбом"))
	hold := make(chan struct{})
	if _, err := s.tasks.start("test", "занято", func(ctx context.Context, t *task) error {
		<-hold
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	defer close(hold)

	code, out := adminCall(t, s, "POST", "/api/admin/fetch", testPin,
		map[string]any{"id": dirID, "url": "https://example.com/a"})
	if code != 200 || out["added"] != 1.0 || out["busy"] == nil || out["task"] != nil {
		t.Fatalf("ответ %d %v", code, out)
	}
	if left := s.fetchq.list(); len(left) != 1 {
		t.Fatalf("ссылка должна ждать в очереди, а там %d", len(left))
	}
	// очередь видна странице и снимается по одной
	code, out = adminCall(t, s, "GET", "/api/admin/fetch", testPin, nil)
	if q, _ := out["queue"].([]any); code != 200 || len(q) != 1 {
		t.Fatalf("очередь в состоянии: %d %v", code, out)
	}
	code, out = adminCall(t, s, "POST", "/api/admin/fetch/drop", testPin, map[string]any{"all": true})
	if code != 200 || out["dropped"] != 1.0 || len(s.fetchq.list()) != 0 {
		t.Fatalf("снятие очереди: %d %v", code, out)
	}
	if code, _ := adminCall(t, s, "POST", "/api/admin/fetch/drop", testPin, map[string]any{}); code != 400 {
		t.Fatalf("без id и без «all» снимать нечего, а ответ %d", code)
	}
}

// Перезапуск на середине закачки оставляет временную папку: «defer» в
// fetchAudio не успевает сработать. Своих папок у свежего агента нет, значит
// всё, что лежит в кэше, — мусор прошлого запуска.
func TestSweepFetchTemp(t *testing.T) {
	s, _, _, _ := looksServer(t)
	junk := filepath.Join(s.cfg.cache, "fetch-123456")
	if err := os.MkdirAll(junk, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(junk, "Трек.opus.part"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(s.cfg.cache, "fetch-queue.json")
	if err := os.WriteFile(keep, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	sweepFetchTemp(s.cfg.cache)
	if _, err := os.Stat(junk); err == nil {
		t.Fatal("временная папка осталась")
	}
	// сама очередь — не мусор: она лежит рядом и начинается так же
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("очередь снесли вместе с мусором: %v", err)
	}
}
