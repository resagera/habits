package smtpd

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

// Приёмник почты (receive-only SMTP).
//
// Это НЕ почтовый сервер общего назначения: ящиков, IMAP и отправки нет.
// Письма принимаются только на адреса из белого списка и складываются в базу
// приложения — ради этого всё и затевалось (разбор чеков магазинов).
// Ретрансляции нет ни при каких условиях: адрес получателя обязан
// принадлежать нашему домену И быть заведённым.

// ReceiptImporter — разбор письма магазина в трату. Интерфейс, а не пакет:
// приёмник не должен знать ни про Finance, ни про курсы валют.
type ReceiptImporter interface {
	ImportFromMail(ctx context.Context, addr store.MailAddress, messageID int64, subject, body string) error
}

type Server struct {
	Addr     string   // ":2525" — снаружи порт 25 пробрасывает docker
	Hostname string   // mail.resager.ru — представляемся им в баннере
	Domains  []string // домены, для которых принимаем
	DataDir  string
	Store    *store.Store
	Bot      *notify.Bot
	Logger   *slog.Logger
	// Receipts разбирает письма магазинов в траты Finance; nil — не разбирать
	Receipts ReceiptImporter

	tlsConf  *tls.Config
	limiter  *limiter
	limOnce  sync.Once
	resolver *net.Resolver
}

// lim — лимитер создаётся лениво: Unblock из HTTP может прийти раньше Run.
func (s *Server) lim() *limiter {
	s.limOnce.Do(func() { s.limiter = newLimiter() })
	return s.limiter
}

// Unblock снимает блокировку и в памяти, и в базе. Иначе кнопка в интерфейсе
// работала бы только до перезапуска.
func (s *Server) Unblock(ctx context.Context, ip string) error {
	s.lim().Unblock(ip)
	return s.Store.UnblockMailIP(ctx, ip)
}

// Run поднимает слушателя и работает до отмены контекста.
func (s *Server) Run(ctx context.Context) error {
	if s.Addr == "" {
		s.Logger.Info("smtpd: выключен (SMTP_ADDR пуст)")
		return nil
	}
	if s.Hostname == "" {
		s.Hostname = "mail.local"
	}
	s.resolver = &net.Resolver{}

	if blocked, err := s.Store.BlockedMailIPs(ctx); err == nil {
		s.lim().Load(blocked)
		if len(blocked) > 0 {
			s.Logger.Info("smtpd: блокировки восстановлены", "count", len(blocked))
		}
	}
	if cfg, err := s.tls(); err != nil {
		s.Logger.Warn("smtpd: STARTTLS недоступен", "error", err)
	} else {
		s.tlsConf = cfg
	}

	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	s.Logger.Info("smtpd: приём почты", "addr", s.Addr, "hostname", s.Hostname,
		"domains", strings.Join(s.Domains, ","), "tls", s.tlsConf != nil)

	go s.housekeeping(ctx)
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			// временная ошибка приёма не повод ронять сервис
			s.Logger.Warn("smtpd: accept", "error", err)
			time.Sleep(time.Second)
			continue
		}
		go s.serve(ctx, conn)
	}
}

func (s *Server) housekeeping(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.lim().Cleanup(now)
		}
	}
}

func (s *Server) serve(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	ip := ""
	if h, _, err := net.SplitHostPort(conn.RemoteAddr().String()); err == nil {
		ip = h
	}
	now := time.Now()

	switch s.lim().Begin(ip, now) {
	case blocked:
		// заблокированному отвечаем сразу и коротко: разговаривать не о чем
		_, _ = conn.Write([]byte("421 4.7.0 Too many failed attempts, try later\r\n"))
		return
	case tooMany:
		until := s.lim().Block(ip, now)
		_ = s.Store.BlockMailIP(ctx, ip, until, "слишком много подключений")
		_, _ = conn.Write([]byte("421 4.7.0 Too many connections, try later\r\n"))
		s.Logger.Warn("smtpd: адрес заблокирован", "ip", ip, "until", until,
			"reason", "подключения")
		return
	case busy:
		_, _ = conn.Write([]byte("421 4.3.2 Too many concurrent sessions\r\n"))
		return
	}
	defer s.lim().End()

	ses := &session{srv: s, conn: conn, ip: ip, ctx: ctx}
	ses.run()
}

// tls готовит конфиг для STARTTLS. Для входящей почты сертификат может быть
// самоподписанным: отправители применяют оппортунистический TLS (RFC 7435) и
// его не проверяют — шифрование канала всё равно лучше открытого текста.
// Если задан свой сертификат (MAIL_TLS_CERT/KEY), берём его.
func (s *Server) tls() (*tls.Config, error) {
	certFile := os.Getenv("MAIL_TLS_CERT")
	keyFile := os.Getenv("MAIL_TLS_KEY")
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, err
		}
		return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
	}
	cert, err := s.selfSigned()
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{*cert}, MinVersion: tls.VersionTLS12}, nil
}

// selfSigned хранит пару в DATA_DIR: новый сертификат на каждый перезапуск
// выглядел бы для отправителей подозрительно.
func (s *Server) selfSigned() (*tls.Certificate, error) {
	dir := filepath.Join(s.DataDir, "mail", "tls")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	certPath := filepath.Join(dir, "smtp.crt")
	keyPath := filepath.Join(dir, "smtp.key")
	if c, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		return &c, nil
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: s.Hostname},
		DNSNames:     []string{s.Hostname},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return nil, err
	}
	c, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ownDomain — принимаем ли почту для этого домена.
func (s *Server) ownDomain(d string) bool {
	d = strings.ToLower(strings.TrimSuffix(d, "."))
	for _, own := range s.Domains {
		if d == strings.ToLower(own) {
			return true
		}
	}
	return false
}
