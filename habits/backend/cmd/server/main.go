package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/automation"
	"streaks-backend/internal/config"
	"streaks-backend/internal/deadlinks"
	"streaks-backend/internal/egress"
	"streaks-backend/internal/httpapi"
	"streaks-backend/internal/migrations"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/reminders"
	"streaks-backend/internal/servers"
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
		AdminIDs:  cfg.AdminIDs,
	}
	if cfg.DevAuthBypass {
		logger.Warn("DEV_AUTH_BYPASS is enabled — all requests run as dev user", "dev_user_id", cfg.DevUserID)
	}

	bot := &notify.Bot{Token: cfg.BotToken, Logger: logger}
	egressHub := egress.NewHub()
	go (&reminders.Worker{Store: st, Bot: bot, Logger: logger}).Run(ctx)
	go (&tasksnotify.Worker{Store: st, Bot: bot, Logger: logger}).Run(ctx)
	go (&servers.Poller{Store: st, Bot: bot, Logger: logger}).Run(ctx)
	go (&deadlinks.Worker{Store: st, Logger: logger}).Run(ctx)
	go (&tgphotos.Worker{Store: st, Bot: bot, DataDir: cfg.DataDir, Logger: logger}).Run(ctx)
	go (&automation.Worker{Store: st, Bot: bot, Egress: egressHub, Logger: logger}).Run(ctx)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.New(st, authMW, bot, egressHub, logger, cfg.DevAuthBypass, cfg.StaticDir, cfg.DataDir),
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
