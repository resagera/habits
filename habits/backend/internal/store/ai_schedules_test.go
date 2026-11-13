package store

import (
	"testing"
	"time"
)

func TestNextAIScheduleRun(t *testing.T) {
	// среда 2026-07-29 10:00 UTC (в tz +240 это 14:00)
	from := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)

	t.Run("daily до времени", func(t *testing.T) {
		// 15:00 локального (+240) = 11:00 UTC — сегодня ещё впереди
		got, ok := NextAIScheduleRun("daily", 15*60, 0, 0, 240, from)
		want := time.Date(2026, 7, 29, 11, 0, 0, 0, time.UTC)
		if !ok || !got.Equal(want) {
			t.Fatalf("got %v ok=%v, want %v", got, ok, want)
		}
	})

	t.Run("daily после времени — завтра", func(t *testing.T) {
		// 09:00 локального (+240) = 05:00 UTC — уже прошло, завтра
		got, ok := NextAIScheduleRun("daily", 9*60, 0, 0, 240, from)
		want := time.Date(2026, 7, 30, 5, 0, 0, 0, time.UTC)
		if !ok || !got.Equal(want) {
			t.Fatalf("got %v ok=%v, want %v", got, ok, want)
		}
	})

	t.Run("weekly понедельник", func(t *testing.T) {
		// след. понедельник 08:30 локального (+240) → 2026-08-03 04:30 UTC
		got, ok := NextAIScheduleRun("weekly", 8*60+30, 1, 0, 240, from)
		want := time.Date(2026, 8, 3, 4, 30, 0, 0, time.UTC)
		if !ok || !got.Equal(want) || got.In(time.FixedZone("tz", 240*60)).Weekday() != time.Monday {
			t.Fatalf("got %v (%v) ok=%v, want %v", got, got.Weekday(), ok, want)
		}
	})

	t.Run("hours", func(t *testing.T) {
		got, ok := NextAIScheduleRun("hours", 0, 0, 6, 0, from)
		want := from.Add(6 * time.Hour)
		if !ok || !got.Equal(want) {
			t.Fatalf("got %v ok=%v, want %v", got, ok, want)
		}
	})

	t.Run("невалидный период", func(t *testing.T) {
		if _, ok := NextAIScheduleRun("monthly", 0, 0, 0, 0, from); ok {
			t.Fatal("monthly не поддержан, ok должен быть false")
		}
		if _, ok := NextAIScheduleRun("hours", 0, 0, 0, 0, from); ok {
			t.Fatal("hours с every=0 должен быть false")
		}
	})
}
