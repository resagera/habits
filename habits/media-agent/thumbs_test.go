package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Превью альбомов собираются заранее — ночью или кнопкой в админке: вид
// «плиткой» показывает их все сразу, и ждать сборки на первом открытии незачем.
func TestThumbsJob(t *testing.T) {
	s, photos, _ := plansServer(t)
	_ = os.WriteFile(filepath.Join(photos, "Root.png"), pngBytes(t), 0o644)
	_ = os.WriteFile(filepath.Join(photos, "Trip", "Day 2", "IMG_2.png"), pngBytes(t), 0o644)
	if got := s.thumbTargets(); len(got) != 3 {
		t.Fatalf("папок без превью: %v", got)
	}
	s.quietPlanNight(s.quiet.config(), time.Date(2026, 11, 28, 2, 0, 0, 0, time.Local))
	if !s.quiet.hasKind("thumbs") {
		t.Fatal("ночью работа «превью» не встала")
	}
	if code, out := adminCall(t, s, "POST", "/api/admin/thumbs/run", testPin, map[string]any{}); code != 200 {
		t.Fatalf("запуск: %d %v", code, out)
	}
	waitIdle(t, s)
	for _, p := range []string{filepath.Join(photos, "Root.png"), filepath.Join(photos, "Trip", "IMG_1.png"),
		filepath.Join(photos, "Trip", "Day 2", "IMG_2.png")} {
		st, _ := os.Stat(p)
		if thumb, _ := s.photoCopyPaths(p, st); !fileReady(thumb) {
			t.Fatalf("нет превью у %s", p)
		}
	}
	if got := s.thumbTargets(); len(got) != 0 {
		t.Fatalf("после работы: %v", got)
	}
	if code, _ := adminCall(t, s, "POST", "/api/admin/thumbs/run", testPin, map[string]any{}); code != 400 {
		t.Fatalf("делать нечего — отказ, а не %d", code)
	}
}
