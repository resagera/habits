// Package financenotify — фоновый воркер страницы Finance: напоминает через
// бота о приближающихся платежах («уведомить за N дней») и спрашивает про
// автоплатежи, у которых дата уже прошла.
//
// Автоплатёж заранее не беспокоит — деньги уйдут сами. Но на следующий день
// после даты мы один раз спрашиваем «списалось?»: иначе в приложении
// накапливается неправда, а именно ради правды страница и заводилась.
package financenotify

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

const tickInterval = 15 * time.Minute

type Worker struct {
	Store  *store.Store
	Bot    *notify.Bot
	Logger *slog.Logger
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	w.tick(ctx) // первый проход сразу после старта
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
	if w.Bot == nil {
		return
	}
	w.notifyDue(ctx)
	w.confirmAutopays(ctx)
}

// isNotifyTime — наступил ли «утренний час» пользователя. Проверяем окно в
// один тик, чтобы сообщение ушло один раз, а не на каждом проходе.
func isNotifyTime(hour, tzOff int) bool {
	local := time.Now().UTC().Add(time.Duration(tzOff) * time.Minute)
	if local.Hour() != hour {
		return false
	}
	return local.Minute() < int(tickInterval/time.Minute)
}

func (w *Worker) notifyDue(ctx context.Context) {
	plans, err := w.Store.FinancePlansToNotify(ctx)
	if err != nil {
		w.Logger.Error("finance: plans to notify", "error", err)
		return
	}
	for _, p := range plans {
		if !isNotifyTime(p.NotifyHour, p.TzOff) {
			continue
		}
		today := time.Now().UTC().Add(time.Duration(p.TzOff) * time.Minute)
		days := int(p.NextDueDate.Sub(time.Date(today.Year(), today.Month(), today.Day(),
			0, 0, 0, 0, time.UTC)).Hours() / 24)

		var when string
		switch {
		case days < 0:
			when = fmt.Sprintf("просрочен на %d дн.", -days)
		case days == 0:
			when = "сегодня"
		case days == 1:
			when = "завтра"
		default:
			when = fmt.Sprintf("через %d дн.", days)
		}

		text := fmt.Sprintf("💸 Платёж %s: %s — %s%s\n%s",
			when, p.Name, amount(p.Amount, p.Currency), estimateMark(p.IsEstimate),
			p.NextDueDate.Format("02.01.2006"))
		if err := w.Bot.SendMessage(ctx, p.UserID, text); err != nil {
			w.Logger.Error("finance: send reminder", "plan_id", p.PlanID, "error", err)
			continue
		}
		if err := w.Store.MarkFinanceNotified(ctx, p.PlanID, p.NextDueDate, false); err != nil {
			w.Logger.Error("finance: mark notified", "plan_id", p.PlanID, "error", err)
		}
	}
}

func (w *Worker) confirmAutopays(ctx context.Context) {
	plans, err := w.Store.FinanceAutopaysToConfirm(ctx)
	if err != nil {
		w.Logger.Error("finance: autopays to confirm", "error", err)
		return
	}
	for _, p := range plans {
		if !isNotifyTime(p.NotifyHour, p.TzOff) {
			continue
		}
		text := fmt.Sprintf("🔄 Автоплатёж %s (%s%s) должен был списаться %s.\n"+
			"Отметьте «Оплатил» в Finance, если всё прошло.",
			p.Name, amount(p.Amount, p.Currency), estimateMark(p.IsEstimate),
			p.NextDueDate.Format("02.01.2006"))
		if err := w.Bot.SendMessage(ctx, p.UserID, text); err != nil {
			w.Logger.Error("finance: send autopay ask", "plan_id", p.PlanID, "error", err)
			continue
		}
		if err := w.Store.MarkFinanceNotified(ctx, p.PlanID, p.NextDueDate, true); err != nil {
			w.Logger.Error("finance: mark autopay asked", "plan_id", p.PlanID, "error", err)
		}
	}
}

func amount(v float64, cur string) string {
	return fmt.Sprintf("%s %s", trimZeros(v), strings.ToUpper(cur))
}

func trimZeros(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	s = strings.TrimSuffix(s, ".00")
	return s
}

func estimateMark(isEstimate bool) string {
	if isEstimate {
		return " (примерно)"
	}
	return ""
}
