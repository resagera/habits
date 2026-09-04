// Package automation — фоновый воркер расписания автоматизаций и общий
// исполнитель (используется и расписанием, и ручным запуском/dry-run из API).
package automation

import (
	"context"
	"errors"
	"strings"
	"time"

	"streaks-backend/internal/egress"
	"streaks-backend/internal/jurwater"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

// Execute запускает автоматизацию: блокирует от параллельного запуска, пишет
// запись в историю, выполняет сценарий, шлёт пользователю результат в бот и
// обновляет состояние (при успехе по расписанию — планирует следующий запуск).
// trigger: schedule/manual/dry_run. Возвращает лог шагов и ошибку.
func Execute(ctx context.Context, st *store.Store, bot *notify.Bot, hub *egress.Hub,
	userID int64, a store.Automation, trigger string, dryRun bool) ([]jurwater.Step, error) {

	// сетевой выход к jur.am идёт через домашний агент (Cloudflare блокирует
	// датацентр-IP прода) — без активного агента запускать нельзя
	if hub == nil || !hub.Online(userID) {
		return nil, errors.New("домашний агент не в сети — запустите habits-automation-agent на домашней машине")
	}

	locked, err := st.TryLockAutomation(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, errors.New("автоматизация уже выполняется")
	}
	defer st.UnlockAutomation(context.Background(), a.ID)

	runID, err := st.StartRun(ctx, userID, a.ID, trigger, dryRun)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	bg := context.Background()

	login, password, err := a.Config.Creds()
	if err != nil {
		st.FinishRun(bg, runID, "failed", nil, "не удалось расшифровать учётные данные: "+err.Error())
		st.AfterRun(bg, a.ID, "failed", nil, now)
		return nil, err
	}
	if login == "" || password == "" {
		msg := "не заданы логин или пароль сайта"
		st.FinishRun(bg, runID, "failed", nil, msg)
		st.AfterRun(bg, a.ID, "failed", nil, now)
		return nil, errors.New(msg)
	}

	res := jurwater.Run(ctx, jurwater.Params{
		Login:     login,
		Password:  password,
		Quantity:  a.Config.Quantity,
		TareMode:  a.Config.TareMode,
		TareQty:   a.Config.TareQty,
		TimeSlot:  a.Config.TimeSlot,
		Payment:   a.Config.Payment,
		Comment:   a.Config.Comment,
		DryRun:    dryRun,
		Transport: hub.Transport(userID),
	})

	status := "success"
	errMsg := ""
	if res.Err != nil {
		status = "failed"
		errMsg = res.Err.Error()
	}
	st.FinishRun(bg, runID, status, res.Steps, errMsg)

	// планирование следующего запуска (только для автоматизаций с расписанием):
	// успех реального запуска → +interval_days; ошибка → повтор через час, чтобы
	// не крутиться каждый тик; dry-run расписание не трогает
	var next *time.Time
	if !dryRun && a.NextRunAt != nil {
		if status == "success" {
			n := now.AddDate(0, 0, a.IntervalDays)
			next = &n
		} else {
			n := now.Add(time.Hour)
			next = &n
		}
	}
	statusLabel := status
	if dryRun {
		statusLabel = "dry_run"
	}
	st.AfterRun(bg, a.ID, statusLabel, next, now)

	// уведомление пользователю
	if bot != nil {
		bot.SendMessage(bg, userID, resultMessage(a, res, dryRun))
	}
	return res.Steps, res.Err
}

func resultMessage(a store.Automation, res jurwater.Result, dryRun bool) string {
	var b strings.Builder
	head := "🤖 Автоматизация «" + a.Title + "»"
	if dryRun {
		head += " (пробный прогон)"
	}
	b.WriteString(head + "\n")
	if res.Err != nil {
		b.WriteString("❌ Ошибка: " + res.Err.Error() + "\n\n")
	} else if dryRun {
		b.WriteString("✅ Пробный прогон дошёл до оформления (заказ НЕ создан)\n\n")
	} else {
		b.WriteString("✅ Заказ оформлен")
		if res.OrderID != "" {
			b.WriteString(" №" + res.OrderID)
		}
		b.WriteString("\n\n")
	}
	for _, s := range res.Steps {
		mark := "✓"
		if !s.OK {
			mark = "✗"
		}
		b.WriteString(mark + " " + s.Name)
		if s.Detail != "" {
			b.WriteString(" — " + s.Detail)
		}
		b.WriteString("\n")
	}
	return b.String()
}
