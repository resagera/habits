// Package tasksnotify — фоновый воркер страницы Tasks: шлёт через бота
// уведомления «скоро срок» (в момент срока или за N минут) и «просрочена»
// (один раз) для открытых задач с включённым «напомнить».
package tasksnotify

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

const (
	tickInterval = time.Minute
	batchLimit   = 500
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
	now := time.Now().UTC()
	notices, err := w.Store.TaskDueNotices(ctx, batchLimit)
	if err != nil {
		w.Logger.Error("tasks: query due notices", "error", err)
		return
	}
	for _, n := range notices {
		w.process(ctx, n, now)
	}
}

func (w *Worker) process(ctx context.Context, n store.TaskDueNotice, now time.Time) {
	recipient := n.UserID
	if n.AssigneeID != nil {
		recipient = *n.AssigneeID
	}
	inProject := ""
	if n.ProjectName != "" {
		inProject = fmt.Sprintf(" (категория «%s»)", n.ProjectName)
	}

	switch {
	case !n.OverdueNotified && now.After(n.OverdueMoment()):
		msg := fmt.Sprintf("🔴 Задача просрочена: «%s»%s — срок был %s.",
			n.Title, inProject, w.dueText(n))
		if err := w.Bot.SendMessage(ctx, recipient, msg); err != nil {
			w.Logger.Error("tasks: send overdue", "id", n.ID, "error", err)
		}
		// «скоро срок» после просрочки уже не имеет смысла
		_ = w.Store.MarkTaskReminded(ctx, n.ID, false)
		if err := w.Store.MarkTaskReminded(ctx, n.ID, true); err != nil {
			w.Logger.Error("tasks: mark overdue", "id", n.ID, "error", err)
		}
	case !n.Reminded && now.After(n.DueMoment().Add(-time.Duration(n.RemindBeforeMin)*time.Minute)):
		msg := fmt.Sprintf("⏳ Подходит срок задачи: «%s»%s — %s.", n.Title, inProject, w.dueText(n))
		if err := w.Bot.SendMessage(ctx, recipient, msg); err != nil {
			w.Logger.Error("tasks: send due-soon", "id", n.ID, "error", err)
		}
		if err := w.Store.MarkTaskReminded(ctx, n.ID, false); err != nil {
			w.Logger.Error("tasks: mark reminded", "id", n.ID, "error", err)
		}
	}
}

func (w *Worker) dueText(n store.TaskDueNotice) string {
	var y, m, d int
	fmt.Sscanf(n.DueDate, "%d-%d-%d", &y, &m, &d)
	s := fmt.Sprintf("%02d.%02d.%d", d, m, y)
	if n.DueTime != nil {
		s += " " + *n.DueTime
	}
	return s
}
