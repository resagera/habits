// Package reminders — фоновый воркер: находит наступившие напоминания,
// шлёт сообщения через Telegram Bot API и планирует следующее срабатывание.
package reminders

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

const (
	tickInterval = 30 * time.Second
	batchLimit   = 100
)

type Worker struct {
	Store  *store.Store
	Bot    *notify.Bot
	Logger *slog.Logger
}

// Run блокируется до отмены ctx.
func (w *Worker) Run(ctx context.Context) {
	if w.Bot.Token == "" {
		w.Logger.Warn("reminders worker: BOT_TOKEN is empty, messages will be logged only")
	}
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	now := time.Now().UTC()
	due, err := w.Store.DueReminders(ctx, now, batchLimit)
	if err != nil {
		w.Logger.Error("reminders: query due", "error", err)
		return
	}
	for _, d := range due {
		w.process(ctx, d, now)
	}
}

func (w *Worker) process(ctx context.Context, d store.DueReminder, now time.Time) {
	send := true
	if d.Kind == "tracker" && d.CategoryID != nil {
		marked, err := w.Store.MarkedToday(ctx, *d.CategoryID, d.TzOffsetMinutes, now)
		if err != nil {
			w.Logger.Error("reminders: check mark", "id", d.ID, "error", err)
			return // не двигаем — попробуем на следующем тике
		}
		send = !marked // уже отмечено — молчим, но переносим на следующий день
	}

	if send {
		if err := w.Bot.SendMessage(ctx, d.UserID, w.messageText(d)); err != nil {
			// пользователь мог не запускать бота или заблокировать его —
			// напоминание всё равно переносим, чтобы не спамить ошибками
			w.Logger.Error("reminders: send", "id", d.ID, "user", d.UserID, "error", err)
		}
	}

	next := d.Reminder.NextFire(now)
	if err := w.Store.AdvanceReminder(ctx, d.ID, now, next); err != nil {
		w.Logger.Error("reminders: advance", "id", d.ID, "error", err)
	}
}

func (w *Worker) messageText(d store.DueReminder) string {
	if d.Kind == "tracker" {
		return fmt.Sprintf("📊 Не забудь отметить «%s» в Tracker за сегодня!", d.CategoryName)
	}
	text := "⏰ " + d.Title
	if d.Note != "" {
		text += "\n" + d.Note
	}
	return text
}

