package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func at(h, m int) time.Time { return time.Date(2026, 11, 13, h, m, 0, 0, time.Local) }

func TestQuietWindow(t *testing.T) {
	night := quietConfig{Enabled: true, From: "23:00", To: "06:30"}
	for _, c := range []struct {
		h, m int
		in   bool
	}{{22, 59, false}, {23, 0, true}, {2, 0, true}, {6, 29, true}, {6, 30, false}, {12, 0, false}} {
		if got := night.inWindow(at(c.h, c.m)); got != c.in {
			t.Errorf("23:00–06:30 в %02d:%02d: %v", c.h, c.m, got)
		}
	}
	day := quietConfig{Enabled: true, From: "01:00", To: "07:00"}
	if !day.inWindow(at(3, 0)) || day.inWindow(at(7, 0)) {
		t.Error("01:00–07:00")
	}
	if (quietConfig{From: "01:00", To: "07:00"}).inWindow(at(3, 0)) {
		t.Error("выключенные часы не должны срабатывать")
	}
	// ночь через полночь — одна, хотя дата сменилась
	if night.nightOf(at(23, 30)) != night.nightOf(at(23, 30).Add(4*time.Hour)) {
		t.Error("nightOf через полночь")
	}
}

func TestQuietJobPausesAfterWindow(t *testing.T) {
	s, _, films := filesLibrary(t)
	s.quiet.mu.Lock()
	// окно, которого сейчас точно нет: прошедшая минута
	past := time.Now().Add(-2 * time.Minute)
	s.quiet.Cfg = quietConfig{Enabled: true, From: past.Format("15:04"), To: past.Add(time.Minute).Format("15:04")}
	s.quiet.mu.Unlock()
	job := s.quiet.add(quietJob{Kind: "marks", Title: "проба", Items: []string{films, films}})
	tk := &task{v: taskView{Lines: []string{}}}
	if err := s.runQuietJob(context.Background(), tk, job.ID); !errors.Is(err, errQuietOver) {
		t.Fatalf("вне окна работа должна встать на паузу: %v", err)
	}
	if j := s.quiet.list()[0]; j.Done != 0 || j.Note == "" {
		t.Fatalf("состояние паузы: %+v", j)
	}

	// в окне — идёт до конца и уходит из очереди; сезон без двух серий
	// не роняет работу, а пишется в отчёт
	s.quiet.mu.Lock()
	s.quiet.Cfg = quietConfig{Enabled: true, From: "00:00", To: "23:59"}
	s.quiet.mu.Unlock()
	if time.Now().Format("15:04") == "23:59" {
		t.Skip("ровно в 23:59 окно 00:00–23:59 закрыто")
	}
	if err := s.runQuietJob(context.Background(), tk, job.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.quiet.list()) != 0 || len(tk.v.Lines) == 0 {
		t.Fatalf("работа не завершилась: %+v %v", s.quiet.list(), tk.v.Lines)
	}
}

func TestQuietAPI(t *testing.T) {
	s, _, _ := filesLibrary(t)
	s.cfg.adminPin = testPin
	code, out := adminCall(t, s, "PUT", "/api/admin/quiet", testPin,
		map[string]any{"enabled": true, "from": "25:00", "to": "07:00"})
	if code != 400 {
		t.Fatalf("кривые часы: %d %v", code, out)
	}
	code, out = adminCall(t, s, "PUT", "/api/admin/quiet", testPin,
		map[string]any{"enabled": true, "from": "02:00", "to": "06:00", "night_marks": true})
	if code != 200 || out["config"].(map[string]any)["from"] != "02:00" {
		t.Fatalf("сохранение: %d %v", code, out)
	}
	if code, _ = adminCall(t, s, "POST", "/api/admin/quiet/jobs", testPin, map[string]any{"kind": "nope"}); code != 400 {
		t.Fatalf("неизвестная работа: %d", code)
	}
}
