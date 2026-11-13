// Package aischeduler — воркер расписаний страницы AI: раз в минуту запускает
// назревшие расписания (создаёт задачу с прогоном и отправляет агенту; при
// офлайне прогон остаётся в очереди и доставится на подключении агента).
package aischeduler

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"streaks-backend/internal/aicoder"
	"streaks-backend/internal/store"
)

type Worker struct {
	Store  *store.Store
	Hub    *aicoder.Hub
	Logger *slog.Logger
}

func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	due, err := w.Store.DueAISchedules(ctx, time.Now().UTC())
	if err != nil {
		w.Logger.Error("aischeduler: due query", "error", err)
		return
	}
	for _, d := range due {
		w.fire(ctx, d)
	}
}

func (w *Worker) fire(ctx context.Context, d store.DueAISchedule) {
	title := "🕒 " + scheduleTitle(d.Prompt)
	task, err := w.Store.CreateAITask(ctx, d.UserID, d.MachineID, d.Tool, d.Workdir, d.Model, d.Params, title, "")
	if err != nil {
		w.Logger.Error("aischeduler: create task", "schedule_id", d.ID, "error", err)
		return
	}
	run, err := w.Store.CreateAIRun(ctx, task.ID, d.Prompt, "")
	if err != nil {
		w.Logger.Error("aischeduler: create run", "schedule_id", d.ID, "error", err)
		return
	}
	// сдвигаем ДО отправки: сбой доставки не должен зациклить расписание
	if err := w.Store.AdvanceAISchedule(ctx, d.ID, task.ID, time.Now().UTC()); err != nil {
		w.Logger.Error("aischeduler: advance", "schedule_id", d.ID, "error", err)
	}
	err = w.Hub.Dispatch(d.MachineID, aicoder.Frame{
		Kind: "run", RunID: run.ID, Tool: d.Tool, Workdir: d.Workdir,
		Model: d.Model, Params: d.Params, Prompt: d.Prompt,
	})
	if err != nil {
		w.Logger.Info("aischeduler: agent offline, run queued", "schedule_id", d.ID, "run_id", run.ID)
	} else {
		w.Logger.Info("aischeduler: fired", "schedule_id", d.ID, "task_id", task.ID)
	}
}

func scheduleTitle(prompt string) string {
	t := strings.Join(strings.Fields(prompt), " ")
	r := []rune(t)
	if len(r) > 76 {
		return string(r[:76]) + "…"
	}
	return t
}
