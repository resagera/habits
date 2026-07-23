package store

import (
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

// 2026-07-15 12:00 UTC — среда.
var wedNoon = time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)

func TestNextFireOnce(t *testing.T) {
	future := wedNoon.Add(2 * time.Hour)
	r := Reminder{Kind: "once", At: &future}
	if got := r.NextFire(wedNoon); got == nil || !got.Equal(future) {
		t.Fatalf("future once: got %v, want %v", got, future)
	}
	past := wedNoon.Add(-time.Hour)
	r = Reminder{Kind: "once", At: &past}
	if got := r.NextFire(wedNoon); got != nil {
		t.Fatalf("past once: got %v, want nil", got)
	}
}

func TestNextFireDailyWithTz(t *testing.T) {
	// Пользователь в UTC+3 (Москва): 12:00 UTC = 15:00 локально.
	// Напоминание на 15:30 локально должно быть сегодня в 12:30 UTC.
	r := Reminder{Kind: "daily", TimeOfDay: ptr("15:30"), DaysMask: 127, TzOffsetMinutes: 180}
	want := time.Date(2026, 7, 15, 12, 30, 0, 0, time.UTC)
	if got := r.NextFire(wedNoon); got == nil || !got.Equal(want) {
		t.Fatalf("daily today: got %v, want %v", got, want)
	}
	// А на 14:00 локально — уже прошло, значит завтра в 11:00 UTC.
	r.TimeOfDay = ptr("14:00")
	want = time.Date(2026, 7, 16, 11, 0, 0, 0, time.UTC)
	if got := r.NextFire(wedNoon); got == nil || !got.Equal(want) {
		t.Fatalf("daily tomorrow: got %v, want %v", got, want)
	}
}

func TestNextFireWeeklyMask(t *testing.T) {
	// Только Пн (бит 0) и Сб (бит 5): из среды ближайшая — суббота 18 июля.
	r := Reminder{Kind: "weekly", TimeOfDay: ptr("09:00"), DaysMask: 1 | 32}
	want := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	if got := r.NextFire(wedNoon); got == nil || !got.Equal(want) {
		t.Fatalf("weekly: got %v, want %v", got, want)
	}
}

func TestNextFireMonthlyClamp(t *testing.T) {
	// 31-е число: из 15 июля — 31 июля; из 31 июля 23:00 — 31 августа.
	r := Reminder{Kind: "monthly", TimeOfDay: ptr("10:00"), DayOfMonth: ptr(int32(31))}
	want := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
	if got := r.NextFire(wedNoon); got == nil || !got.Equal(want) {
		t.Fatalf("monthly jul: got %v, want %v", got, want)
	}
	// Из конца января — февраль без 31-го, «прижимается» к 28-му.
	after := time.Date(2027, 1, 31, 23, 0, 0, 0, time.UTC)
	want = time.Date(2027, 2, 28, 10, 0, 0, 0, time.UTC)
	if got := r.NextFire(after); got == nil || !got.Equal(want) {
		t.Fatalf("monthly feb clamp: got %v, want %v", got, want)
	}
}

func TestNextFireInterval(t *testing.T) {
	r := Reminder{Kind: "interval", IntervalMinutes: ptr(int32(90))}
	want := wedNoon.Add(90 * time.Minute)
	if got := r.NextFire(wedNoon); got == nil || !got.Equal(want) {
		t.Fatalf("interval: got %v, want %v", got, want)
	}
}

func TestNextFireTrackerUsesMask(t *testing.T) {
	// tracker ведёт себя как daily по маске.
	r := Reminder{Kind: "tracker", TimeOfDay: ptr("20:00"), DaysMask: 127, CategoryID: ptr(int64(1))}
	want := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	if got := r.NextFire(wedNoon); got == nil || !got.Equal(want) {
		t.Fatalf("tracker: got %v, want %v", got, want)
	}
}
