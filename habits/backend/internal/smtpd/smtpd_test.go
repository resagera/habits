package smtpd

import (
	"net"
	"strings"
	"testing"
	"time"
)

func TestCutAddress(t *testing.T) {
	cases := []struct {
		in       string
		prefix   string
		addr     string
		params   string
		ok       bool
		whatFail string
	}{
		{"FROM:<a@b.ru>", "FROM:", "a@b.ru", "", true, ""},
		{"FROM:<A@B.RU> SIZE=100", "FROM:", "a@b.ru", "SIZE=100", true, ""},
		{"FROM: <a@b.ru>", "FROM:", "a@b.ru", "", true, "пробел после двоеточия законен"},
		{"FROM:<>", "FROM:", "", "", true, "пустой отправитель — это отчёт о недоставке"},
		{"TO:<x@y.z>", "TO:", "x@y.z", "", true, ""},
		{"FROM:a@b.ru", "FROM:", "", "", false, "без угловых скобок"},
		{"FROM:<a@b.ru", "FROM:", "", "", false, "незакрытая скобка"},
		{"TO:<a@b.ru>", "FROM:", "", "", false, "чужой префикс"},
		{"FROM:<" + strings.Repeat("x", 400) + ">", "FROM:", "", "", false, "адрес длиннее лимита"},
	}
	for _, c := range cases {
		addr, params, ok := cutAddress(c.in, c.prefix)
		if ok != c.ok || addr != c.addr || params != c.params {
			t.Errorf("cutAddress(%q) = %q,%q,%v; ожидалось %q,%q,%v (%s)",
				c.in, addr, params, ok, c.addr, c.params, c.ok, c.whatFail)
		}
	}
}

func TestSizeParam(t *testing.T) {
	if got := sizeParam("SIZE=1234 BODY=8BITMIME"); got != 1234 {
		t.Errorf("SIZE = %d, ожидалось 1234", got)
	}
	if got := sizeParam("size=99"); got != 99 {
		t.Errorf("регистр параметра не должен мешать: %d", got)
	}
	if got := sizeParam("BODY=8BITMIME"); got != 0 {
		t.Errorf("без SIZE ожидался 0, получено %d", got)
	}
}

func TestDomainOf(t *testing.T) {
	for in, want := range map[string]string{
		"a@b.ru": "b.ru", "A@B.RU": "b.ru", "noatsign": "", "trailing@": "", "": "",
	} {
		if got := domainOf(in); got != want {
			t.Errorf("domainOf(%q) = %q, ожидалось %q", in, got, want)
		}
	}
}

func TestMatchNet(t *testing.T) {
	ip := net.ParseIP("45.129.196.26")
	if !matchNet("45.129.196.0/24", ip) {
		t.Error("адрес обязан попадать в свою подсеть")
	}
	if matchNet("45.129.197.0/24", ip) {
		t.Error("чужая подсеть совпасть не должна")
	}
	if !matchNet("45.129.196.26", ip) {
		t.Error("точное совпадение без маски")
	}
	if matchNet("не-адрес", ip) {
		t.Error("мусор не должен совпадать")
	}
}

func TestLimiterBlocksAfterBurst(t *testing.T) {
	l := newLimiter()
	now := time.Now()
	for i := 0; i < maxConnPerHour; i++ {
		if v := l.Begin("1.2.3.4", now); v != allow {
			t.Fatalf("подключение %d отклонено раньше лимита: %v", i, v)
		}
		l.End()
	}
	if v := l.Begin("1.2.3.4", now); v != tooMany {
		t.Fatalf("после лимита ожидалось tooMany, получено %v", v)
	}
	// другому адресу перебор соседа мешать не должен
	if v := l.Begin("5.6.7.8", now); v != allow {
		t.Fatalf("чужой адрес заблокирован: %v", v)
	}
	l.End()
}

func TestLimiterBlockEscalates(t *testing.T) {
	l := newLimiter()
	now := time.Now()
	first := l.Block("9.9.9.9", now)
	second := l.Block("9.9.9.9", now)
	third := l.Block("9.9.9.9", now)
	if !second.After(first) || !third.After(second) {
		t.Errorf("срок блокировки обязан расти: %v, %v, %v", first, second, third)
	}
	if v := l.Begin("9.9.9.9", now); v != blocked {
		t.Errorf("заблокированный адрес пропущен: %v", v)
	}
}

func TestLimiterMessageQuota(t *testing.T) {
	l := newLimiter()
	now := time.Now()
	for i := 0; i < maxMsgPerHour; i++ {
		if !l.Message("2.2.2.2", now) {
			t.Fatalf("письмо %d отклонено раньше лимита", i)
		}
	}
	if l.Message("2.2.2.2", now) {
		t.Error("после часового лимита письмо должно отклоняться")
	}
	// час прошёл — счётчик обязан очиститься
	if !l.Message("2.2.2.2", now.Add(61*time.Minute)) {
		t.Error("через час лимит должен обнуляться")
	}
}

func TestLimiterCleanupKeepsBlocks(t *testing.T) {
	l := newLimiter()
	now := time.Now()
	l.Block("3.3.3.3", now)
	l.Cleanup(now.Add(2 * time.Hour))
	// блокировка на час уже истекла — запись можно забыть
	if v := l.Begin("3.3.3.3", now.Add(2*time.Hour)); v != allow {
		t.Errorf("истёкшая блокировка должна сниматься: %v", v)
	}
	l.End()
	l.Block("4.4.4.4", now)
	l.Cleanup(now.Add(10 * time.Minute))
	if v := l.Begin("4.4.4.4", now.Add(10*time.Minute)); v != blocked {
		t.Errorf("действующая блокировка не должна теряться при уборке: %v", v)
	}
}

func TestParsePlainMessage(t *testing.T) {
	raw := "From: \"Магазин\" <shop@example.com>\r\n" +
		"To: habits@resager.ru\r\n" +
		"Subject: =?utf-8?B?0KfQtdC6INC30LAg0LfQsNC60LDQtw==?=\r\n" +
		"Message-ID: <abc123@example.com>\r\n" +
		"Date: Tue, 05 Aug 2026 10:00:00 +0400\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		"Спасибо за покупку. Сумма 5000 AMD.\r\n"
	p := Parse([]byte(raw))
	if p.Subject != "Чек за заказ" {
		t.Errorf("тема декодирована неверно: %q", p.Subject)
	}
	if p.FromAddr != "shop@example.com" || p.FromName != "Магазин" {
		t.Errorf("отправитель: %q / %q", p.FromName, p.FromAddr)
	}
	if !p.HasMessageID || p.MessageID != "abc123@example.com" {
		t.Errorf("Message-ID: %q", p.MessageID)
	}
	if !p.HasDate || p.Date == nil {
		t.Error("дата не разобрана")
	}
	if !strings.Contains(p.Text, "5000 AMD") {
		t.Errorf("тело: %q", p.Text)
	}
}

func TestParseCP1251Subject(t *testing.T) {
	// русские магазины до сих пор шлют windows-1251
	raw := "From: shop@example.com\r\n" +
		"Subject: =?windows-1251?Q?=D7=E5=EA?=\r\n" +
		"Content-Type: text/plain; charset=windows-1251\r\n\r\n" +
		"\xd7\xe5\xea\r\n"
	p := Parse([]byte(raw))
	if p.Subject != "Чек" {
		t.Errorf("тема из cp1251: %q", p.Subject)
	}
	if !strings.Contains(p.Text, "Чек") {
		t.Errorf("тело из cp1251: %q", p.Text)
	}
}

func TestParseMultipartWithAttachment(t *testing.T) {
	raw := "From: shop@example.com\r\n" +
		"Subject: Order\r\n" +
		"Content-Type: multipart/mixed; boundary=BOUND\r\n\r\n" +
		"--BOUND\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		"текстовая часть\r\n" +
		"--BOUND\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n\r\n" +
		"<b>итого 100</b>\r\n" +
		"--BOUND\r\n" +
		"Content-Type: application/pdf\r\n" +
		"Content-Disposition: attachment; filename=\"receipt.pdf\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		"cGRmLWJvZHk=\r\n" +
		"--BOUND--\r\n"
	p := Parse([]byte(raw))
	if !strings.Contains(p.Text, "текстовая часть") {
		t.Errorf("text/plain потерян: %q", p.Text)
	}
	if !strings.Contains(p.HTML, "итого 100") {
		t.Errorf("text/html потерян: %q", p.HTML)
	}
	if len(p.Attachments) != 1 {
		t.Fatalf("вложений %d, ожидалось 1", len(p.Attachments))
	}
	a := p.Attachments[0]
	if a.Filename != "receipt.pdf" || string(a.Data) != "pdf-body" {
		t.Errorf("вложение: %q / %q", a.Filename, string(a.Data))
	}
}

func TestParseHTMLOnlyFallsBackToText(t *testing.T) {
	// у писем магазинов часто есть только HTML — поиск и разбор чеков должны
	// работать и по нему
	raw := "From: shop@example.com\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n\r\n" +
		"<html><style>p{color:red}</style><body><p>Итого:&nbsp;5&nbsp;000&nbsp;AMD</p>" +
		"<script>alert(1)</script></body></html>"
	p := Parse([]byte(raw))
	if strings.Contains(p.Text, "alert") || strings.Contains(p.Text, "color:red") {
		t.Errorf("скрипты и стили не должны попадать в текст: %q", p.Text)
	}
	if !strings.Contains(p.Text, "Итого") || !strings.Contains(p.Text, "5 000 AMD") {
		t.Errorf("текст из HTML: %q", p.Text)
	}
}

func TestParseBrokenMessageKeepsData(t *testing.T) {
	// разбор не должен терять письмо: исходник всё равно лежит на диске
	p := Parse([]byte("это вообще не письмо"))
	if p.Text == "" {
		t.Error("при сбое разбора тело должно остаться")
	}
}

func TestOwnDomain(t *testing.T) {
	s := &Server{Domains: []string{"resager.ru"}}
	for _, d := range []string{"resager.ru", "RESAGER.RU", "resager.ru."} {
		if !s.ownDomain(d) {
			t.Errorf("%q должен считаться своим", d)
		}
	}
	for _, d := range []string{"evil.com", "resager.ru.evil.com", ""} {
		if s.ownDomain(d) {
			t.Errorf("%q своим быть не должен", d)
		}
	}
}

func TestClip(t *testing.T) {
	if got := clip("абвгд", 3); got != "абв…" {
		t.Errorf("обрезка по рунам: %q", got)
	}
	if got := clip("абв", 10); got != "абв" {
		t.Errorf("короткая строка не должна меняться: %q", got)
	}
}

func TestSpfScoreOnlyPenalises(t *testing.T) {
	// pass и neutral не должны добавлять очков — иначе честные письма уедут в спам
	for _, r := range []spfResult{spfPass, spfNeutral, spfTempError, spfPermError} {
		if n, _ := spfScore(r); n != 0 {
			t.Errorf("%s не должен штрафоваться, получено %d", r, n)
		}
	}
	if n, _ := spfScore(spfFail); n == 0 {
		t.Error("fail обязан штрафоваться")
	}
}
