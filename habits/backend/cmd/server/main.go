package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"streaks-backend/internal/aicoder"
	"streaks-backend/internal/aischeduler"
	"streaks-backend/internal/auth"
	"streaks-backend/internal/automation"
	"streaks-backend/internal/checkerreset"
	"streaks-backend/internal/config"
	"streaks-backend/internal/deadlinks"
	"streaks-backend/internal/egress"
	"streaks-backend/internal/extension"
	"streaks-backend/internal/financenotify"
	"streaks-backend/internal/httpapi"
	"streaks-backend/internal/migrations"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/rates"
	"streaks-backend/internal/receiptimport"
	"streaks-backend/internal/reminders"
	"streaks-backend/internal/servers"
	"streaks-backend/internal/smtpd"
	"streaks-backend/internal/store"
	"streaks-backend/internal/tasksnotify"
	"streaks-backend/internal/tgphotos"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
}

// announceReleases шлёт админам сообщение о выпущенных релизах, о которых ещё
// не уведомляли (notified_at IS NULL), и помечает их оповещёнными. Историю мы
// засеяли с проставленным notified_at, так что здесь всплывают только новые
// релизы, добавленные миграцией. В dev (пустой BOT_TOKEN) Bot лишь логирует.
func announceReleases(ctx context.Context, st *store.Store, bot *notify.Bot, adminIDs map[int64]bool, logger *slog.Logger) {
	pending, err := st.PendingReleaseNotifications(ctx)
	if err != nil {
		logger.Error("release notify: query", "error", err)
		return
	}
	for _, rel := range pending {
		text := fmt.Sprintf("🚀 Habits v%s\n%s", rel.Version, rel.Title)
		if rel.PublicNotes != "" {
			text += "\n\n" + rel.PublicNotes
		}
		for id := range adminIDs {
			if err := bot.SendMessage(ctx, id, text); err != nil {
				logger.Error("release notify: send", "chat_id", id, "version", rel.Version, "error", err)
			}
		}
		if err := st.MarkReleaseNotified(ctx, rel.ID); err != nil {
			logger.Error("release notify: mark", "version", rel.Version, "error", err)
		} else {
			logger.Info("release announced", "version", rel.Version, "admins", len(adminIDs))
		}
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := migrations.Up(ctx, cfg.DatabaseURL); err != nil {
		return err
	}
	logger.Info("migrations applied")

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return err
	}

	st := store.New(pool)
	store.SetAutomationKey(cfg.AutomationKey)
	authMW := &auth.Middleware{
		BotToken:  cfg.BotToken,
		DevBypass: cfg.DevAuthBypass,
		DevUserID: cfg.DevUserID,
		Users:     st,
		Tokens:    st,
		AdminIDs:  cfg.AdminIDs,
		Logger:    logger,
	}
	if cfg.DevAuthBypass {
		logger.Warn("DEV_AUTH_BYPASS is enabled — all requests run as dev user", "dev_user_id", cfg.DevUserID)
	}

	bot := &notify.Bot{Token: cfg.BotToken, Logger: logger}
	egressHub := egress.NewHub()
	// хаб AI-агентов создаётся здесь: нужен и роутеру, и воркеру расписаний
	aiHub := aicoder.NewHub()
	go (&aischeduler.Worker{Store: st, Hub: aiHub, Logger: logger}).Run(ctx)
	go (&reminders.Worker{Store: st, Bot: bot, Logger: logger}).Run(ctx)
	go (&tasksnotify.Worker{Store: st, Bot: bot, Logger: logger}).Run(ctx)
	go (&financenotify.Worker{Store: st, Bot: bot, Logger: logger}).Run(ctx)
	go (&servers.Poller{Store: st, Bot: bot, Logger: logger}).Run(ctx)
	go (&deadlinks.Worker{Store: st, Logger: logger}).Run(ctx)
	go (&tgphotos.Worker{Store: st, Bot: bot, DataDir: cfg.DataDir, Logger: logger}).Run(ctx)
	go (&automation.Worker{Store: st, Bot: bot, Egress: egressHub, Logger: logger}).Run(ctx)
	go (&checkerreset.Worker{Store: st, Bot: bot, Logger: logger}).Run(ctx)
	ratesCache := rates.New()
	// разбор писем магазинов в траты: нужен и приёмнику почты, и обработчику
	// кнопки «разобрать ещё раз»
	receiptsImporter := &receiptimport.Importer{
		Store: st, Rates: ratesCache, Bot: bot, AdminIDs: cfg.AdminIDs, Logger: logger,
	}
	// Приёмник почты слушает свой порт, к HTTP-серверу отношения не имеет, но
	// роутеру нужна ссылка: снимать блокировки надо и в его памяти. Зависимости
	// проставляются ДО запуска — иначе поле читается из чужой горутины.
	mailSrv := &smtpd.Server{
		Addr: cfg.SMTPAddr, Hostname: cfg.MailHostname, Domains: cfg.MailDomains,
		DataDir: cfg.DataDir, Store: st, Bot: bot, Logger: logger,
		Receipts: receiptsImporter,
	}
	go func() {
		if err := mailSrv.Run(ctx); err != nil {
			logger.Error("smtpd остановлен", "error", err)
		}
	}()
	// разовое оповещение админа о новых релизах (строки с notified_at IS NULL,
	// которые добавляет миграция каждого релиза) — срабатывает на старте после
	// деплоя на прод.
	go announceReleases(ctx, st, bot, cfg.AdminIDs, logger)

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: httpapi.New(st, authMW, bot, egressHub, aiHub, extension.New(cfg.PublicURL), ratesCache, logger, cfg.DevAuthBypass, cfg.StaticDir, cfg.DataDir,
			httpapi.MailInfo{Hostname: cfg.MailHostname, Domains: cfg.MailDomains,
				Unblock: mailSrv.Unblock, Receipts: receiptsImporter}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		logger.Info("server stopped")
		return nil
	}
}
