package store

import (
	"testing"
	"time"
)

func TestNextResetAt(t *testing.T) {
	// tzOff +240 (Ереван). from = 2026-07-25 03:00 UTC = 07:00 local.
	from := time.Date(2026, 7, 25, 3, 0, 0, 0, time.UTC)
	tz := 240

	// daily at 06:00 local — уже прошло сегодня → завтра 06:00 local = 02:00 UTC 26-го
	got, ok := nextResetAt("daily", 6*60, 0, 0, tz, from)
	if !ok || !got.Equal(time.Date(2026, 7, 26, 2, 0, 0, 0, time.UTC)) {
		t.Fatalf("daily: got %v ok=%v", got, ok)
	}

	// daily at 09:00 local — ещё не наступило сегодня → сегодня 09:00 local = 05:00 UTC
	got, _ = nextResetAt("daily", 9*60, 0, 0, tz, from)
	if !got.Equal(time.Date(2026, 7, 25, 5, 0, 0, 0, time.UTC)) {
		t.Fatalf("daily future: got %v", got)
	}

	// none → false
	if _, ok := nextResetAt("none", 0, 0, 0, 0, from); ok {
		t.Fatal("none must be false")
	}

	// weekly: 2026-07-25 — суббота (Weekday=6). dow=1 (пн) → ближайший пн 27-го 06:00 local
	got, _ = nextResetAt("weekly", 6*60, 1, 0, tz, from)
	if got.Weekday() != time.Monday || got.Day() != 27-0 { // 27-е июля 2026 = пн; в UTC 06:00 local=02:00 UTC того же дня
		// 06:00 local monday 27 = 02:00 UTC monday 27
	}
	want := time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("weekly: got %v want %v", got, want)
	}

	// monthly: dom=1, from 25-е → 1-е след. месяца 06:00 local = 02:00 UTC 1 авг
	got, _ = nextResetAt("monthly", 6*60, 0, 1, tz, from)
	if !got.Equal(time.Date(2026, 8, 1, 2, 0, 0, 0, time.UTC)) {
		t.Fatalf("monthly: got %v", got)
	}

	// clamp: dom=31 в феврале → 28 (2026 не високосный)
	feb := time.Date(2026, 2, 10, 3, 0, 0, 0, time.UTC)
	got, _ = nextResetAt("monthly", 6*60, 0, 31, tz, feb)
	if got.In(time.FixedZone("x", tz*60)).Day() != 28 {
		t.Fatalf("monthly clamp: got local day %d", got.In(time.FixedZone("x", tz*60)).Day())
	}
}
