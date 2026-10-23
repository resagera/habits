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
	if d.Kind == "tracker" && d.CategoryID != nil {
		w.processTracker(ctx, d, now)
		return
	}
	if err := w.Bot.SendKind(ctx, d.UserID, "reminder", w.messageText(d)); err != nil {
		// пользователь мог не запускать бота или заблокировать его —
		// напоминание всё равно переносим, чтобы не спамить ошибками
		w.Logger.Error("reminders: send", "id", d.ID, "user", d.UserID, "error", err)
	}
	next := d.Reminder.NextFire(now)
	if err := w.Store.AdvanceReminder(ctx, d.ID, now, next); err != nil {
		w.Logger.Error("reminders: advance", "id", d.ID, "error", err)
	}
}

// processTracker — напоминание про привычку. Отличий от обычного три:
// отметки за «день трекера» (сутки могут начинаться не в полночь), порог
// «не меньше чем» (в 13:00 ждём одну отметку, в 01:00 — обе) и цикл
// повторов, пока не отметят.
//
// Живой цикл повторов помечен непустым spam_day: пока он стоит, очередное
// срабатывание — это повтор, а не приход по расписанию.
func (w *Worker) processTracker(ctx context.Context, d store.DueReminder, now time.Time) {
	day := store.TrackerDay(now, d.TzOffsetMinutes, d.DayStartMinutes)
	repeat := d.SpamDay != nil

	// день трекера сменился на повторе (например, всю тишину молчали) —
	// бросаем цикл: напоминать про прошедшие сутки уже поздно
	if repeat && !d.SpamDay.Equal(day) {
		w.finishTracker(ctx, d, now)
		return
	}

	marks, err := w.Store.MarksOnDay(ctx, *d.CategoryID, day)
	if err != nil {
		w.Logger.Error("reminders: count marks", "id", d.ID, "error", err)
		return // не двигаем — попробуем на следующем тике
	}
	hushed, err := w.Store.TrackerHushed(ctx, d.UserID, *d.CategoryID, day)
	if err != nil {
		w.Logger.Error("reminders: check hush", "id", d.ID, "error", err)
		return
	}
	if marks >= d.ExpectMarks || hushed {
		w.finishTracker(ctx, d, now) // отметили (или попросили не трогать) — молчим до следующего раза
		return
	}

	// в тихие часы повторы пропускаем, но цикл держим: проверим через X минут
	if repeat || d.SpamMinutes != nil {
		prefs, err := w.Store.GetNotifyPrefs(ctx, d.UserID)
		if err != nil {
			w.Logger.Error("reminders: prefs", "id", d.ID, "error", err)
		} else if _, quiet := prefs.QuietUntil(now); quiet {
			if repeat {
				w.repeatTracker(ctx, d, now, day, d.SpamSent)
			} else {
				// по расписанию в тишину: отдаём боту (он положит в очередь
				// до конца тишины) и цикл не заводим
				w.sendTracker(ctx, d, marks, 0)
				w.finishTracker(ctx, d, now)
			}
			return
		}
	}

	sent := int32(0)
	if repeat {
		sent = d.SpamSent
	}
	sent++
	w.sendTracker(ctx, d, marks, sent)

	if d.SpamMinutes != nil && sent < d.SpamMax {
		w.repeatTracker(ctx, d, now, day, sent)
		return
	}
	w.finishTracker(ctx, d, now) // повторы выключены или кончились
}

// finishTracker закрывает цикл повторов и планирует следующее срабатывание
// по расписанию.
func (w *Worker) finishTracker(ctx context.Context, d store.DueReminder, now time.Time) {
	next := d.Reminder.NextFire(now)
	if err := w.Store.AdvanceReminderSpam(ctx, d.ID, now, next, 0, nil); err != nil {
		w.Logger.Error("reminders: advance", "id", d.ID, "error", err)
	}
}

// repeatTracker переносит срабатывание на X минут вперёд, сохраняя счётчик.
func (w *Worker) repeatTracker(ctx context.Context, d store.DueReminder, now, day time.Time, sent int32) {
	minutes := int32(10)
	if d.SpamMinutes != nil {
		minutes = *d.SpamMinutes
	}
	next := now.Add(time.Duration(minutes) * time.Minute)
	if err := w.Store.AdvanceReminderSpam(ctx, d.ID, now, &next, sent, &day); err != nil {
		w.Logger.Error("reminders: advance repeat", "id", d.ID, "error", err)
	}
}

func (w *Worker) sendTracker(ctx context.Context, d store.DueReminder, marks, repeat int32) {
	text := fmt.Sprintf("📊 Не забудь отметить «%s» в Tracker за сегодня!", d.CategoryName)
	if d.ExpectMarks > 1 {
		text = fmt.Sprintf("📊 «%s» в Tracker: отмечено %d из %d за сегодня.",
			d.CategoryName, marks, d.ExpectMarks)
	}
	if repeat > 1 {
		text += fmt.Sprintf("\nНапоминание %d из %d.", repeat, d.SpamMax)
	}
	if err := w.Bot.SendKind(ctx, d.UserID, "reminder", text); err != nil {
		w.Logger.Error("reminders: send", "id", d.ID, "user", d.UserID, "error", err)
	}
}

func (w *Worker) messageText(d store.DueReminder) string {
	text := "⏰ " + d.Title
	if d.Note != "" {
		text += "\n" + d.Note
	}
	return text
}
