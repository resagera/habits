package automation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"streaks-backend/internal/egress"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

const tickInterval = time.Minute

// Worker — фоновый планировщик: шлёт уведомления за сутки и за час до
// запуска и выполняет автоматизацию по наступлению срока.
type Worker struct {
	Store  *store.Store
	Bot    *notify.Bot
	Egress *egress.Hub
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
	// берём всё, что наступит в ближайшие 24 часа (для заблаговременных уведомлений)
	due, err := w.Store.DueAutomations(ctx, now.Add(24*time.Hour))
	if err != nil {
		w.Logger.Error("automation: query due", "error", err)
		return
	}
	for _, d := range due {
		w.process(ctx, d, now)
	}
}

func (w *Worker) process(ctx context.Context, d store.DueAutomation, now time.Time) {
	untilRun := d.NextRunAt.Sub(now)

	// пора запускать
	if untilRun <= 0 {
		// без активного агента откладываем на час, шлём предупреждение —
		// иначе на каждом тике будет «агент не в сети»
		if w.Egress == nil || !w.Egress.Online(d.UserID) {
			w.Store.AfterRun(ctx, d.ID, "failed", ptr(now.Add(time.Hour)), now)
			w.Bot.SendMessage(ctx, d.UserID, fmt.Sprintf(
				"⚠️ Автоматизация «%s»: домашний агент не в сети — запуск отложен на час. "+
					"Запустите habits-automation-agent на домашней машине.", d.Title))
			return
		}
		a, err := w.Store.GetAutomationInternal(ctx, d.ID)
		if err != nil {
			w.Logger.Error("automation: load", "id", d.ID, "error", err)
			return
		}
		if _, err := Execute(ctx, w.Store, w.Bot, w.Egress, d.UserID, *a, "schedule", false); err != nil {
			w.Logger.Error("automation: run", "id", d.ID, "error", err)
		}
		return
	}

	// уведомление за сутки (когда до запуска ≤ 24ч и ещё не слали)
	if untilRun <= 24*time.Hour && !d.NotifiedDay {
		w.notify(ctx, d, "завтра")
		_ = w.Store.MarkNotified(ctx, d.ID, true, d.NotifiedHour)
	}
	// уведомление за час
	if untilRun <= time.Hour && !d.NotifiedHour {
		w.notify(ctx, d, "через час")
		_ = w.Store.MarkNotified(ctx, d.ID, true, true)
	}
}

func ptr(t time.Time) *time.Time { return &t }

func (w *Worker) notify(ctx context.Context, d store.DueAutomation, when string) {
	at := d.NextRunAt.Format("02.01 15:04")
	msg := fmt.Sprintf("⏰ Автоматизация «%s» запустится %s (%s UTC) — %d бутылей.\n"+
		"Чтобы отменить или перенести, откройте страницу «Автоматизация» в Habits.",
		d.Title, when, at, d.Config.Quantity)
	if err := w.Bot.SendMessage(ctx, d.UserID, msg); err != nil {
		w.Logger.Error("automation: notify", "id", d.ID, "error", err)
	}
}
