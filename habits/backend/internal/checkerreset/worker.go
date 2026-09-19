// Package checkerreset — фоновый воркер Checker: сбрасывает повторяющиеся списки
// по расписанию (снимок дня + снятие отметок) и шлёт напоминания (дедлайны
// пунктов/списков) через бота.
package checkerreset

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

type Worker struct {
	Store  *store.Store
	Bot    *notify.Bot
	Logger *slog.Logger
}

// Run блокируется до отмены ctx.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
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
	due, err := w.Store.DueCheckerResets(ctx, now, 100)
	if err != nil {
		w.Logger.Error("checker reset: query due", "error", err)
	} else {
		for _, d := range due {
			if err := w.Store.RunCheckerReset(ctx, d, now); err != nil {
				w.Logger.Error("checker reset: run", "root", d.RootID, "error", err)
			}
		}
	}
	w.sendReminders(ctx, now)
}

func (w *Worker) sendReminders(ctx context.Context, now time.Time) {
	rem, err := w.Store.DueCheckerReminders(ctx, now, 200)
	if err != nil {
		w.Logger.Error("checker reminders: query", "error", err)
		return
	}
	for _, r := range rem {
		targets, listName, err := w.Store.CheckerReminderTargets(ctx, r.GroupID)
		if err != nil {
			w.Logger.Error("checker reminders: targets", "id", r.ID, "error", err)
			continue
		}
		var text string
		if r.Kind == "item" {
			text = fmt.Sprintf("⏰ Напоминание: «%s» в списке «%s» (Checker)", r.Name, listName)
		} else {
			text = fmt.Sprintf("⏰ Напоминание про список «%s» (Checker)", r.Name)
		}
		for _, uid := range targets {
			if err := w.Bot.SendMessage(ctx, uid, text); err != nil {
				w.Logger.Error("checker reminders: send", "user", uid, "error", err)
			}
		}
		if err := w.Store.MarkCheckerReminded(ctx, r.Kind, r.ID, now); err != nil {
			w.Logger.Error("checker reminders: mark", "id", r.ID, "error", err)
		}
	}
}
