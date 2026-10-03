package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr          string
	DatabaseURL   string
	BotToken      string
	DevAuthBypass bool
	DevUserID     int64
	StaticDir     string // если задан — раздаём SPA-фронтенд с этого каталога
	DataDir       string // каталог пользовательских файлов (фоны)
	AdminIDs      map[int64]bool
	AutomationKey string // ключ шифрования учётных данных автоматизаций (AUTOMATION_KEY или производный от BOT_TOKEN)
	PublicURL     string // публичный адрес приложения — попадает в popup.html расширения
	// приём почты: пустой SMTPAddr выключает приёмник целиком
	SMTPAddr     string
	MailHostname string
	MailDomains  []string
}

func Load() (Config, error) {
	cfg := Config{
		Addr:        getenv("ADDR", ":8081"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		BotToken:    os.Getenv("BOT_TOKEN"),
		DevUserID:   1,
		StaticDir:   os.Getenv("STATIC_DIR"),
		DataDir:     getenv("DATA_DIR", "./data"),
		PublicURL:   getenv("PUBLIC_URL", "https://telegram.resager.ru/app/habits/"),
		// внутри контейнера слушаем непривилегированный порт: приложение
		// работает не от root, наружу порт 25 пробрасывает docker
		SMTPAddr:     os.Getenv("SMTP_ADDR"),
		MailHostname: getenv("MAIL_HOSTNAME", "mail.resager.ru"),
	}
	for _, d := range strings.Split(getenv("MAIL_DOMAINS", "resager.ru"), ",") {
		if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
			cfg.MailDomains = append(cfg.MailDomains, d)
		}
	}
	if cfg.DatabaseURL == "" {
		return cfg, fmt.Errorf("DATABASE_URL is required")
	}
	cfg.DevAuthBypass = os.Getenv("DEV_AUTH_BYPASS") == "true"
	if v := os.Getenv("DEV_USER_ID"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return cfg, fmt.Errorf("DEV_USER_ID: %w", err)
		}
		cfg.DevUserID = id
	}
	if !cfg.DevAuthBypass && cfg.BotToken == "" {
		return cfg, fmt.Errorf("BOT_TOKEN is required unless DEV_AUTH_BYPASS=true")
	}

	cfg.AdminIDs = map[int64]bool{}
	for _, raw := range strings.Split(getenv("ADMIN_IDS", "180564250"), ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return cfg, fmt.Errorf("ADMIN_IDS: %w", err)
		}
		cfg.AdminIDs[id] = true
	}

	// Ключ шифрования учётных данных автоматизаций. Явный AUTOMATION_KEY
	// (любая строка) или, если не задан, — производный от BOT_TOKEN, чтобы
	// не заводить новую переменную на проде. В dev с bypass допускаем пустой.
	cfg.AutomationKey = os.Getenv("AUTOMATION_KEY")
	if cfg.AutomationKey == "" {
		cfg.AutomationKey = cfg.BotToken
	}
	return cfg, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
