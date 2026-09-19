package httpapi

import (
	"testing"
	"time"
)

// Код набирают руками с экрана телевизора, поэтому нормализация должна
// прощать всё, что человек делает по дороге: регистр, пробелы, дефисы и
// русскую раскладку — «К9КН СТWХ» на телефоне набирается кириллицей и от
// латинского на вид неотличим.
func TestNormalizeTVCode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"как есть", "K9KHSTWX", "K9KHSTWX"},
		{"с пробелом", "K9KH STWX", "K9KHSTWX"},
		{"с дефисом и в нижнем регистре", "k9kh-stwx", "K9KHSTWX"},
		// у латинских S и W кириллических двойников нет — берём код из тех
		// букв, которые на телефоне действительно набирают кириллицей
		{"кириллица целиком", "КНТХ 2345", "KHTX2345"},
		{"смесь раскладок", "K9кН стWх", "K9KHCTWX"},
		{"короткий — не код", "K9KH", ""},
		{"длинный — не код", "K9KHSTWXY", ""},
		{"пусто", "   ", ""},
		{"ключ комнаты — не код", "res-i3-0dd65eae", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := normalizeTVCode(c.in); got != c.want {
				t.Fatalf("normalizeTVCode(%q) = %q, ожидалось %q", c.in, got, c.want)
			}
		})
	}
}

// Уведомления медиаагента: не больше tvNotifyPerHour в час на комнату, у
// каждой комнаты свой счёт, через час окно освобождается.
func TestTVNotifyLimiter(t *testing.T) {
	l := newTVNotifyLimiter()
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	for i := 0; i < tvNotifyPerHour; i++ {
		if !l.allow("res-i3-a", now.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("сообщение %d отклонено раньше предела", i+1)
		}
	}
	if l.allow("res-i3-a", now.Add(time.Minute)) {
		t.Fatal("сверх предела пропустило")
	}
	if !l.allow("other-room", now.Add(time.Minute)) {
		t.Fatal("чужая комната не должна страдать от чужого предела")
	}
	if !l.allow("res-i3-a", now.Add(time.Hour+time.Minute)) {
		t.Fatal("через час окно должно освободиться")
	}
}
