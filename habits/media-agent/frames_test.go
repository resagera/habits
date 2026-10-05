package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// makeClip — короткий видеофайл нужной длины; «кадр» в нём меняется со временем.
func makeClip(t *testing.T, path string, seconds int) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("нет ffmpeg")
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	cmd := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi",
		"-i", fmt.Sprintf("testsrc=size=320x180:rate=5:duration=%d", seconds),
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, out)
	}
	return path
}

// У фильма кадры берутся из середины: ни заставки, ни титров.
func TestFramePlanFilm(t *testing.T) {
	s, root := adminServer(t)
	film := makeClip(t, filepath.Join(root, "Кино.mp4"), 120)
	plan := s.framePlan(film, false)
	if len(plan) != frameCount {
		t.Fatalf("кадров в плане: %d", len(plan))
	}
	for _, shot := range plan {
		if shot.file != film {
			t.Fatalf("чужой файл: %s", shot.file)
		}
		if shot.at < 120*frameFrom || shot.at > 120*frameTo {
			t.Fatalf("кадр вне середины: %.1f", shot.at)
		}
	}
	// короткий ролик разбирать нечего
	if got := s.framePlan(makeClip(t, filepath.Join(root, "Ролик.mp4"), 20), false); len(got) != 0 {
		t.Fatalf("у ролика короче минуты кадров быть не должно: %v", got)
	}
}

// У сериала — по кадру из разных серий, а не сто кадров с каждой.
func TestFramePlanSeries(t *testing.T) {
	s, root := adminServer(t)
	dir := filepath.Join(root, "Сериал")
	for i := 1; i <= 3; i++ {
		makeClip(t, filepath.Join(dir, fmt.Sprintf("S01/E%02d.mp4", i)), 90)
	}
	plan := s.framePlan(dir, true)
	if len(plan) != frameCount {
		t.Fatalf("кадров в плане: %d", len(plan))
	}
	seen := map[string]int{}
	for _, shot := range plan {
		seen[shot.file]++
	}
	if len(seen) != 3 {
		t.Fatalf("серии должны чередоваться: %v", seen)
	}
	for f, n := range seen {
		if n > 4 {
			t.Fatalf("слишком много кадров из одной серии %s: %d", filepath.Base(f), n)
		}
	}
}

// Сборка кадров целиком: файлы появились, свои идут первыми, переименование их
// не теряет.
func TestMakeFramesAndOrder(t *testing.T) {
	s, root := adminServer(t)
	film := makeClip(t, filepath.Join(root, "Кино.mp4"), 90)
	n, err := s.makeFrames(context.Background(), film, false, nil)
	if err != nil || n == 0 {
		t.Fatalf("кадры не собрались: %d %v", n, err)
	}
	id := shortID(film)
	names := s.frameNames(id)
	if len(names) != n {
		t.Fatalf("на диске %d кадров, собрали %d", len(names), n)
	}
	// свой кадр встаёт первым
	own := filepath.Join(s.framesDir(id), "own-1.jpg")
	if err := os.WriteFile(own, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := s.frameNames(id); got[0] != "own-1.jpg" {
		t.Fatalf("свои кадры должны быть первыми: %v", got[:2])
	}
	if !s.hasAutoFrames(id) {
		t.Fatal("автоматические кадры не видны")
	}
	// переименовали фильм — кадры переехали
	neu := filepath.Join(root, "Другое кино.mp4")
	if err := os.Rename(film, neu); err != nil {
		t.Fatal(err)
	}
	s.moveFrames(film, neu)
	if got := s.frameNames(shortID(neu)); len(got) != len(names)+1 {
		t.Fatalf("после переименования кадров: %d", len(got))
	}
	if got := s.frameNames(id); len(got) != 0 {
		t.Fatalf("на старом месте осталось: %v", got)
	}
}

// Чёрный кадр не берём: ffmpeg попробует чуть дальше.
func TestGrabFrameSkipsBlack(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("нет ffmpeg")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "dark.mp4")
	// первые пять секунд — чернота, дальше картинка
	cmd := exec.Command("ffmpeg", "-v", "error", "-y",
		"-f", "lavfi", "-i", "color=black:size=320x180:rate=5:duration=5",
		"-f", "lavfi", "-i", "testsrc=size=320x180:rate=5:duration=10",
		"-filter_complex", "[0:v][1:v]concat=n=2:v=1[v]", "-map", "[v]",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, out)
	}
	dst := filepath.Join(dir, "shot.jpg")
	if err := grabFrame(context.Background(), src, dst, 1, 4); err != nil {
		t.Fatalf("кадр не вышел: %v", err)
	}
	if b := brightness(dst); b < frameDarkMin {
		t.Fatalf("взяли чёрный кадр: яркость %.1f", b)
	}
}

// Задачи идут по одной, и остановка доходит до самой работы.
func TestTasksOneAtATime(t *testing.T) {
	ts := newTaskStore()
	started, stop := make(chan struct{}), make(chan struct{})
	first, err := ts.start("frames", "первая", func(ctx context.Context, tk *task) error {
		close(started)
		<-ctx.Done()
		close(stop)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := ts.start("frames", "вторая", func(context.Context, *task) error { return nil }); err == nil {
		t.Fatal("вторую задачу брать нельзя")
	} else if !strings.Contains(err.Error(), "первая") {
		t.Fatalf("в отказе должно быть, чем заняты: %v", err)
	}
	if !ts.stop(first.snapshot().ID) {
		t.Fatal("задача не остановилась")
	}
	<-stop
	if v := ts.list(); len(v) != 1 || v[0].Title != "первая" {
		t.Fatalf("список задач: %+v", v)
	}
}
