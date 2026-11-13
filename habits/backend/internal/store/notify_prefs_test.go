package store

import (
	"testing"
	"time"
)

// Тихие часы почти всегда переходят через полночь, а считаются по местному
// времени пользователя — на этой паре условий проще всего ошибиться.
func TestQuietUntil(t *testing.T) {
	// 23:00–08:00 по местному, устройство в UTC+4
	p := NotifyPrefs{QuietEnabled: true, QuietFrom: 23 * 60, QuietTo: 8 * 60, TzOff: 4 * 60}

	cases := []struct {
		name  string
		utc   string
		quiet bool
		until string // ожидаемый конец тишины в UTC
	}{
		{"день", "2026-09-23T10:00:00Z", false, ""},                            // 14:00 местного
		{"поздний вечер", "2026-09-23T19:30:00Z", true, "2026-09-24T04:00:00Z"}, // 23:30 местного
		{"ночь после полуночи", "2026-09-23T22:00:00Z", true, "2026-09-24T04:00:00Z"},
		{"утро до конца тишины", "2026-09-23T03:30:00Z", true, "2026-09-23T04:00:00Z"}, // 07:30
		{"ровно конец тишины", "2026-09-23T04:00:00Z", false, ""},                      // 08:00
	}
	for _, c := range cases {
		now, err := time.Parse(time.RFC3339, c.utc)
		if err != nil {
			t.Fatal(err)
		}
		until, quiet := p.QuietUntil(now)
		if quiet != c.quiet {
			t.Errorf("%s: тишина = %v, ожидалось %v", c.name, quiet, c.quiet)
			continue
		}
		if !quiet {
			continue
		}
		if got := until.UTC().Format(time.RFC3339); got != c.until {
			t.Errorf("%s: конец тишины %s, ожидался %s", c.name, got, c.until)
		}
	}

	off := NotifyPrefs{QuietFrom: 23 * 60, QuietTo: 8 * 60}
	if _, quiet := off.QuietUntil(time.Now()); quiet {
		t.Error("выключенные тихие часы не должны глушить")
	}
}

func TestMuted(t *testing.T) {
	p := NotifyPrefs{Off: []string{"servers", "mail"}}
	if !p.Muted("mail") {
		t.Error("выключенный вид должен глушиться")
	}
	if p.Muted("reminder") {
		t.Error("не выключенный вид глушиться не должен")
	}
}
