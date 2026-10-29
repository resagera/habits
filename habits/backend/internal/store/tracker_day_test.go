package store

import (
	"testing"
	"time"
)

// Сутки трекера начинаются в 05:00 (day_start_minutes=300): отметка в час
// ночи 2 сентября относится к 1 сентября.
func TestTrackerDay(t *testing.T) {
	const tz = 4 * 60 // Ереван, UTC+4
	cases := []struct {
		name  string
		local string // местное время отметки
		start int32
		want  string
	}{
		{"день", "2026-09-01T13:00:00", 300, "2026-09-01"},
		{"ночь после полуночи", "2026-09-02T01:00:00", 300, "2026-09-01"},
		{"ровно начало суток", "2026-09-02T05:00:00", 300, "2026-09-02"},
		{"за минуту до", "2026-09-02T04:59:00", 300, "2026-09-01"},
		{"без сдвига", "2026-09-02T01:00:00", 0, "2026-09-02"},
	}
	loc := time.FixedZone("user", tz*60)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			local, err := time.ParseInLocation("2006-01-02T15:04:05", c.local, loc)
			if err != nil {
				t.Fatal(err)
			}
			got := TrackerDay(local.UTC(), tz, c.start).Format("2006-01-02")
			if got != c.want {
				t.Fatalf("TrackerDay(%s, start=%d) = %s, want %s", c.local, c.start, got, c.want)
			}
		})
	}
}
