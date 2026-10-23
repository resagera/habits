// Package notifyqueue — досылка уведомлений, отложенных тихими часами.
//
// Тихие часы не глушат сообщение, а откладывают: напоминание, которое просто
// не пришло, — потерянное напоминание.
package notifyqueue

import (
	"context"
	"log/slog"
	"time"

	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

const (
	tickInterval = time.Minute
	batchLimit   = 100
)

type Worker struct {
	Store  *store.Store
	Bot    *notify.Bot
	Logger *slog.Logger
}

func (w *Worker) Run(ctx context.Context) {
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
	due, err := w.Store.DueNotifications(ctx, time.Now().UTC(), batchLimit)
	if err != nil {
		w.Logger.Error("notifyqueue: query due", "error", err)
		return
	}
	for _, n := range due {
		// SendMessage, а не SendKind: тихие часы уже отработали, второй раз
		// откладывать нечего — иначе сообщение ходило бы по кругу.
		if err := w.Bot.SendMessage(ctx, n.UserID, n.Text); err != nil {
			w.Logger.Error("notifyqueue: send", "id", n.ID, "user", n.UserID, "error", err)
		}
		if err := w.Store.MarkNotificationSent(ctx, n.ID); err != nil {
			w.Logger.Error("notifyqueue: mark sent", "id", n.ID, "error", err)
		}
	}
}
