package smtpd

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"streaks-backend/internal/store"
)

// session — один разговор по SMTP.
type session struct {
	srv  *Server
	conn net.Conn
	ctx  context.Context
	ip   string

	tp       *textproto.Conn
	helo     string
	from     string
	rcpts    []store.MailAddress
	tls      bool
	spf      string // результат проверки, считается один раз за письмо
	errors   int
	commands int
	accepted int
	rejected int
	reason   string
	ptr      string
}

func (s *session) run() {
	deadline := time.Now().Add(sessionMax)
	_ = s.conn.SetDeadline(deadline)

	// Заминка перед приветствием и проверка, не заговорил ли клиент первым.
	// Спам-боты часто вываливают команды не дожидаясь баннера — это дешёвый и
	// на удивление точный признак, законный сервер так не делает.
	if s.talkedEarly() {
		s.reason = "заговорил до приветствия"
		s.punish()
		_, _ = s.conn.Write([]byte("554 5.7.1 Not talking to you\r\n"))
		s.finish()
		return
	}

	s.ptr = s.lookupPTR()
	s.tp = textproto.NewConn(s.conn)
	s.reply("220 %s ESMTP Habits", s.srv.Hostname)

	for {
		_ = s.conn.SetReadDeadline(time.Now().Add(idleTimeout))
		line, err := s.tp.ReadLine()
		if err != nil {
			break
		}
		s.commands++
		if s.commands > maxCommands {
			s.reason = "слишком много команд"
			s.punish()
			s.reply("421 4.7.0 Too many commands")
			break
		}
		if quit := s.command(line); quit {
			break
		}
		if s.errors >= maxErrors {
			s.reason = "поток ошибочных команд"
			s.punish()
			s.reply("421 4.7.0 Too many errors")
			break
		}
	}
	s.finish()
}

// talkedEarly — правда, если клиент прислал данные до баннера.
func (s *session) talkedEarly() bool {
	_ = s.conn.SetReadDeadline(time.Now().Add(greetDelay))
	buf := make([]byte, 1)
	n, err := s.conn.Read(buf)
	// нас интересует именно таймаут: он означает, что клиент вежливо ждёт
	if n > 0 && err == nil {
		return true
	}
	return false
}

// finish записывает итог сессии по IP. Пишем на каждом подключении, включая
// пустые: сканы порта — тоже полезное знание, а стоит это один UPSERT.
func (s *session) finish() {
	_ = s.srv.Store.TouchMailIP(s.ctx, s.ip, s.ptr, s.accepted, s.rejected, s.reason)
}

// punish закрывает адрес на растущий срок и запоминает это в базе.
func (s *session) punish() {
	until := s.srv.lim().Block(s.ip, time.Now())
	_ = s.srv.Store.BlockMailIP(s.ctx, s.ip, until, s.reason)
	s.srv.Logger.Warn("smtpd: адрес заблокирован", "ip", s.ip, "until", until,
		"reason", s.reason)
}

func (s *session) reply(format string, args ...any) {
	_ = s.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	_ = s.tp.PrintfLine(format, args...)
}

func (s *session) bad(format string, args ...any) {
	s.errors++
	s.rejected++
	s.reply(format, args...)
}

// command возвращает true, если разговор пора заканчивать.
func (s *session) command(line string) bool {
	verb, arg, _ := strings.Cut(line, " ")
	verb = strings.ToUpper(strings.TrimSpace(verb))
	arg = strings.TrimSpace(arg)

	switch verb {
	case "EHLO", "HELO":
		if arg == "" {
			s.bad("501 5.5.4 HELO requires a domain")
			return false
		}
		s.helo = arg
		s.from, s.rcpts = "", nil
		if verb == "HELO" {
			s.reply("250 %s", s.srv.Hostname)
			return false
		}
		ext := []string{
			fmt.Sprintf("250-%s", s.srv.Hostname),
			fmt.Sprintf("250-SIZE %d", maxSize),
			"250-8BITMIME",
			"250-PIPELINING",
		}
		if s.srv.tlsConf != nil && !s.tls {
			ext = append(ext, "250-STARTTLS")
		}
		ext = append(ext, "250 ENHANCEDSTATUSCODES")
		s.reply("%s", strings.Join(ext, "\r\n"))
		return false

	case "STARTTLS":
		if s.srv.tlsConf == nil || s.tls {
			s.bad("454 4.7.0 TLS not available")
			return false
		}
		s.reply("220 2.0.0 Ready to start TLS")
		tc := tls.Server(s.conn, s.srv.tlsConf)
		if err := tc.HandshakeContext(s.ctx); err != nil {
			return true
		}
		// после TLS протокол начинается заново — состояние обнуляется (RFC 3207)
		s.conn = tc
		s.tp = textproto.NewConn(tc)
		s.tls, s.helo, s.from, s.rcpts = true, "", "", nil
		return false

	case "MAIL":
		return s.mailFrom(arg)

	case "RCPT":
		return s.rcptTo(arg)

	case "DATA":
		return s.data()

	case "RSET":
		s.from, s.rcpts = "", nil
		s.reply("250 2.0.0 OK")
		return false

	case "NOOP":
		s.reply("250 2.0.0 OK")
		return false

	case "QUIT":
		s.reply("221 2.0.0 Bye")
		return true

	case "VRFY", "EXPN":
		// подтверждать существование адресов — значит помогать перебору
		s.reply("252 2.5.2 Cannot VRFY user")
		return false

	case "AUTH":
		// аутентификации нет намеренно: отправлять через нас нельзя вообще
		s.bad("502 5.5.1 Authentication not supported")
		return false

	default:
		s.bad("500 5.5.2 Unrecognized command")
		return false
	}
}

func (s *session) mailFrom(arg string) bool {
	if s.helo == "" {
		s.bad("503 5.5.1 Send HELO/EHLO first")
		return false
	}
	addr, params, ok := cutAddress(arg, "FROM:")
	if !ok {
		s.bad("501 5.5.4 Syntax: MAIL FROM:<address>")
		return false
	}
	// SIZE из ESMTP: отказываем до передачи, а не после 15 МБ трафика
	if n := sizeParam(params); n > maxSize {
		s.bad("552 5.3.4 Message too big")
		return false
	}
	// пустой адрес (<>) законен — так приходят отчёты о недоставке
	if addr != "" {
		domain := domainOf(addr)
		if domain == "" {
			s.bad("501 5.1.7 Bad sender address")
			return false
		}
		if !s.resolvable(domain) {
			// домен без MX и без A писем не отправляет: это выдумка спамера
			s.reason = "несуществующий домен отправителя"
			s.bad("550 5.1.8 Sender domain does not resolve")
			return false
		}
	}
	s.from = addr
	s.rcpts = nil
	s.reply("250 2.1.0 OK")
	return false
}

func (s *session) rcptTo(arg string) bool {
	if s.from == "" && len(s.rcpts) == 0 && s.helo == "" {
		s.bad("503 5.5.1 Send MAIL first")
		return false
	}
	addr, _, ok := cutAddress(arg, "TO:")
	if !ok || addr == "" {
		s.bad("501 5.5.4 Syntax: RCPT TO:<address>")
		return false
	}
	if len(s.rcpts) >= maxRcpt {
		s.bad("452 4.5.3 Too many recipients")
		return false
	}
	// ретрансляция запрещена: чужой домен не обсуждается
	if !s.srv.ownDomain(domainOf(addr)) {
		s.reason = "попытка ретрансляции"
		s.bad("550 5.7.1 Relay access denied")
		return false
	}
	// главный барьер: адрес обязан быть заведён. Словарный перебор ботов
	// заканчивается здесь и в базу писем не попадает вообще
	rec, err := s.srv.Store.MailAddressByAddress(s.ctx, addr)
	if err != nil || !rec.Enabled {
		s.reason = "неизвестный получатель"
		s.bad("550 5.1.1 No such user here")
		return false
	}
	// у алиаса магазина можно потребовать конкретный домен отправителя —
	// самый сильный фильтр из возможных
	if rec.OnlyFrom != "" && !strings.EqualFold(domainOf(s.from), rec.OnlyFrom) {
		_ = s.srv.Store.BumpMailAddress(s.ctx, rec.ID, false)
		s.reason = "отправитель не разрешён для адреса"
		s.bad("550 5.7.1 Sender not allowed for this address")
		return false
	}
	s.rcpts = append(s.rcpts, rec)
	s.reply("250 2.1.5 OK")
	return false
}

func (s *session) data() bool {
	if len(s.rcpts) == 0 {
		s.bad("503 5.5.1 Need RCPT first")
		return false
	}
	if !s.srv.lim().Message(s.ip, time.Now()) {
		s.reason = "часовой лимит писем"
		s.rejected++
		s.reply("451 4.7.1 Too many messages, try later")
		return false
	}
	s.reply("354 End data with <CR><LF>.<CR><LF>")

	_ = s.conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
	// DotReader сам снимает точечное экранирование и ловит конец письма
	raw, err := io.ReadAll(io.LimitReader(s.tp.DotReader(), maxSize+1))
	if err != nil {
		s.rejected++
		return true
	}
	if int64(len(raw)) > maxSize {
		// дочитывать остаток смысла нет — рвём соединение, это законно
		s.reason = "письмо больше лимита"
		s.rejected++
		s.reply("552 5.3.4 Message too big")
		return true
	}

	for _, rec := range s.rcpts {
		if err := s.deliver(rec, raw); err != nil {
			s.srv.Logger.Error("smtpd: сохранение письма", "error", err, "rcpt", rec.Address)
			s.reply("451 4.3.0 Temporary failure, try later")
			return false
		}
		s.accepted++
		_ = s.srv.Store.BumpMailAddress(s.ctx, rec.ID, true)
	}
	s.from, s.rcpts = "", nil
	s.reply("250 2.0.0 Accepted")
	return false
}

// deliver разбирает письмо, кладёт исходник на диск и пишет запись в базу.
func (s *session) deliver(rec store.MailAddress, raw []byte) error {
	p := Parse(raw)
	rawPath, err := s.saveRaw(raw)
	if err != nil {
		return err
	}

	score, reasons := s.score(p)
	msg := store.MailMessage{
		AddressID: &rec.ID,
		Rcpt:      rec.Address,
		MailFrom:  s.from,
		FromName:  clip(p.FromName, 200),
		FromAddr:  clip(p.FromAddr, 320),
		Subject:   clip(p.Subject, 500),
		MessageID: clip(p.MessageID, 300),
		SentAt:    p.Date,
		SizeBytes: len(raw),
		// тела обрезаем: рассылки бывают на мегабайты, а для чтения и поиска
		// столько не нужно — исходник всегда лежит на диске
		TextBody:    clip(p.Text, 200_000),
		HTMLBody:    clip(p.HTML, 400_000),
		RemoteIP:    s.ip,
		Helo:        clip(s.helo, 200),
		PTR:         clip(s.ptr, 200),
		TLS:         s.tls,
		SPF:         s.spf,
		SpamScore:   score,
		SpamReasons: strings.Join(reasons, "; "),
		IsSpam:      score >= spamThreshold,
		RawPath:     rawPath,
	}

	atts := make([]store.MailAttachment, 0, len(p.Attachments))
	for _, a := range p.Attachments {
		path, err := s.saveAttachment(a)
		if err != nil {
			s.srv.Logger.Warn("smtpd: вложение не сохранено", "error", err)
			continue
		}
		atts = append(atts, store.MailAttachment{
			Filename: clip(a.Filename, 300), ContentType: clip(a.ContentType, 200),
			SizeBytes: len(a.Data), Path: path,
		})
	}

	id, err := s.srv.Store.SaveMailMessage(s.ctx, rec.UserID, msg, atts)
	if err != nil {
		return err
	}
	s.srv.Logger.Info("smtpd: письмо принято", "id", id, "rcpt", rec.Address,
		"from", msg.FromAddr, "ip", s.ip, "score", score, "spam", msg.IsSpam)

	if !msg.IsSpam {
		s.notify(rec.UserID, msg)
	}
	if rec.Parser != "" && !msg.IsSpam {
		s.importReceipt(rec, id, p)
	}
	return nil
}

// importReceipt разбирает письмо магазина в чек и записывает трату в Finance.
//
// Разбирается текстовая часть: вёрстка рассылок меняется от кампании к
// кампании, а текстовая версия у магазина годами одна и та же. Ошибки здесь
// письмо не теряют — оно уже сохранено, разбор всегда можно повторить руками.
func (s *session) importReceipt(rec store.MailAddress, messageID int64, p Parsed) {
	if s.srv.Receipts == nil {
		return
	}
	body := p.Text
	if strings.TrimSpace(body) == "" {
		body = htmlToText(p.HTML)
	}
	if err := s.srv.Receipts.ImportFromMail(s.ctx, rec, messageID, p.Subject, body); err != nil {
		s.srv.Logger.Info("smtpd: чек не разобран", "rcpt", rec.Address,
			"message_id", messageID, "error", err)
	}
}

func (s *session) notify(userID int64, msg store.MailMessage) {
	on, err := s.srv.Store.MailNotifyEnabled(s.ctx, userID)
	if err != nil || !on {
		return
	}
	text := fmt.Sprintf("📬 Письмо на %s\nОт: %s\n%s",
		msg.Rcpt, firstNonEmpty(msg.FromAddr, msg.MailFrom, "неизвестно"),
		firstNonEmpty(msg.Subject, "(без темы)"))
	if err := s.srv.Bot.SendMessage(s.ctx, userID, text); err != nil {
		s.srv.Logger.Warn("smtpd: уведомление", "error", err)
	}
}

// spamThreshold — с какой оценки письмо уезжает в «Спам». Не удаляем ничего:
// ложное срабатывание не должно терять чек из магазина.
const spamThreshold = 6

// score считает подозрительность по дешёвым признакам. Ни один из них не
// повод отказать: законные отправители тоже бывают настроены криво.
func (s *session) score(p Parsed) (int, []string) {
	score := 0
	var reasons []string
	add := func(n int, why string) {
		score += n
		reasons = append(reasons, why)
	}

	if s.ptr == "" {
		add(2, "у адреса нет обратной записи (PTR)")
	} else if !s.forwardConfirmed() {
		add(2, "PTR не подтверждается прямой записью")
	}
	if !strings.Contains(s.helo, ".") {
		add(2, "HELO без домена: "+clip(s.helo, 60))
	}
	if s.srv.ownDomain(s.helo) || strings.EqualFold(s.helo, s.srv.Hostname) {
		add(4, "HELO выдаёт себя за наш сервер")
	}
	if !p.HasMessageID {
		add(2, "нет Message-ID")
	}
	if !p.HasDate {
		add(1, "нет даты")
	}
	if s.from == "" {
		add(1, "пустой отправитель (отчёт о недоставке)")
	}
	if !s.tls {
		add(1, "без шифрования")
	}
	if n, why := spfScore(s.spfResult()); n > 0 {
		add(n, why)
	}
	return score, reasons
}

// spf вычисляется один раз за письмо: DNS-запросы недёшевы.
func (s *session) spfResult() spfResult {
	if s.spf != "" {
		return spfResult(s.spf)
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	r := checkSPF(ctx, s.srv.resolver, domainOf(s.from), net.ParseIP(s.ip))
	s.spf = string(r)
	return r
}

// forwardConfirmed — сходится ли PTR с прямой записью (FCrDNS).
func (s *session) forwardConfirmed() bool {
	ctx, cancel := context.WithTimeout(s.ctx, 4*time.Second)
	defer cancel()
	addrs, err := s.srv.resolver.LookupIPAddr(ctx, s.ptr)
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if a.IP.String() == s.ip {
			return true
		}
	}
	return false
}

func (s *session) lookupPTR() string {
	ctx, cancel := context.WithTimeout(s.ctx, 4*time.Second)
	defer cancel()
	names, err := s.srv.resolver.LookupAddr(ctx, s.ip)
	if err != nil || len(names) == 0 {
		return ""
	}
	return strings.TrimSuffix(names[0], ".")
}

// resolvable — есть ли у домена отправителя MX или хотя бы A.
func (s *session) resolvable(domain string) bool {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	if mxs, err := s.srv.resolver.LookupMX(ctx, domain); err == nil && len(mxs) > 0 {
		return true
	}
	if ips, err := s.srv.resolver.LookupIPAddr(ctx, domain); err == nil && len(ips) > 0 {
		return true
	}
	return false
}

func (s *session) saveRaw(raw []byte) (string, error) {
	now := time.Now()
	dir := filepath.Join(s.srv.DataDir, "mail", now.Format("2006"), now.Format("01"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, now.Format("020405")+"-"+randHex(6)+".eml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func (s *session) saveAttachment(a ParsedAttachment) (string, error) {
	dir := filepath.Join(s.srv.DataDir, "mail", "attachments")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	// имя файла из письма на диск не попадает: там бывает и «../», и что угодно
	ext := strings.ToLower(filepath.Ext(a.Filename))
	if len(ext) > 10 || strings.ContainsAny(ext, `/\`) {
		ext = ""
	}
	path := filepath.Join(dir, randHex(12)+ext)
	if err := os.WriteFile(path, a.Data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// cutAddress разбирает "FROM:<a@b> SIZE=123" → адрес и остаток параметров.
func cutAddress(arg, prefix string) (addr, params string, ok bool) {
	up := strings.ToUpper(arg)
	if !strings.HasPrefix(up, prefix) {
		return "", "", false
	}
	rest := strings.TrimSpace(arg[len(prefix):])
	if !strings.HasPrefix(rest, "<") {
		return "", "", false
	}
	end := strings.Index(rest, ">")
	if end < 0 {
		return "", "", false
	}
	addr = strings.TrimSpace(rest[1:end])
	params = strings.TrimSpace(rest[end+1:])
	if len(addr) > 320 {
		return "", "", false
	}
	return strings.ToLower(addr), params, true
}

func sizeParam(params string) int64 {
	for _, f := range strings.Fields(params) {
		if k, v, ok := strings.Cut(f, "="); ok && strings.EqualFold(k, "SIZE") {
			n, _ := strconv.ParseInt(v, 10, 64)
			return n
		}
	}
	return 0
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
