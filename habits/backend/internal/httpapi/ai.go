package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"streaks-backend/internal/aicoder"
	"streaks-backend/internal/auth"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"

	"github.com/gorilla/websocket"
)

// aiHandlers — страница AI: запуск coding-агентов (Claude Code, Codex)
// на домашних машинах через релей habits-ai-agent.
type aiHandlers struct {
	store  *store.Store
	hub    *aicoder.Hub
	bot    *notify.Bot
	logger *slog.Logger
}

// notifyRunFinished — сообщение владельцу о завершении прогона (вызывается
// из OnRunResult только при первой доставке результата).
func (h *aiHandlers) notifyRunFinished(runID int64, ok bool, output, errText string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	info, err := h.store.AIRunNotify(ctx, runID)
	if err != nil {
		h.logger.Error("ai: notify info", "run_id", runID, "error", err)
		return
	}
	icon, body := "✅", output
	if !ok {
		icon, body = "❌", errText
	}
	body = strings.Join(strings.Fields(strings.ReplaceAll(body, "\n", " ")), " ")
	if r := []rune(body); len(r) > 200 {
		body = string(r[:200]) + "…"
	}
	text := icon + " AI · " + info.Tool + "\n«" + info.TaskTitle + "»"
	if body != "" {
		text += "\n\n" + body
	}
	if err := h.bot.SendMessage(ctx, info.UserID, text); err != nil {
		h.logger.Error("ai: notify send", "run_id", runID, "error", err)
	}
}

// deliverQueued — доставка прогонов, скопившихся в очереди, при подключении
// агента (машина была офлайн или агент перезапускался). Дедуп повторной
// отправки на живом агенте — на стороне агента (accepted-map).
func (h *aiHandlers) deliverQueued(machineID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	queued, err := h.store.QueuedAIRunsForMachine(ctx, machineID)
	if err != nil {
		h.logger.Error("ai: queued query", "machine_id", machineID, "error", err)
		return
	}
	for _, q := range queued {
		err := h.hub.Dispatch(machineID, aicoder.Frame{
			Kind: "run", RunID: q.RunID, Tool: q.Tool, Workdir: q.Workdir,
			Model: q.Model, Params: q.Params, Prompt: q.Prompt, Mode: q.Mode, SessionID: q.SessionID,
		})
		if err != nil {
			return // соединение уже умерло — доставим на следующем подключении
		}
	}
	if len(queued) > 0 {
		h.logger.Info("ai: queued delivered", "machine_id", machineID, "count", len(queued))
	}
}

var aiUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// агент — не браузер, Origin не проверяем (авторизация по токену)
	CheckOrigin: func(*http.Request) bool { return true },
}

// поддерживаемые инструменты; codex — пока только проверка наличия (заглушка в UI)
var aiTools = map[string]bool{"claude": true, "codex": true}

// прогон, зависший дольше этого срока, помечается ошибкой при листинге
const aiStaleAfter = 2 * time.Hour

// --- машины ---

type aiMachineView struct {
	store.AIMachine
	Online bool  `json:"online"`
	Busy   int64 `json:"busy"` // активные прогоны (queued + running)
}

func (h *aiHandlers) listMachines(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	machines, err := h.store.ListAIMachines(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	busy, err := h.store.ActiveAIRunCounts(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	views := make([]aiMachineView, 0, len(machines))
	for _, m := range machines {
		views = append(views, aiMachineView{AIMachine: m, Online: h.hub.Online(m.ID), Busy: busy[m.ID]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"machines": views})
}

// POST /ai/runs/{id}/cancel — остановить прогон: queued отменяем в БД,
// running — команда агенту (kill процесса; результат «остановлено» придёт
// обычным путём run_result).
func (h *aiHandlers) cancelRun(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid run id")
		return
	}
	status, machineID, err := h.store.AIRunOwned(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "run not found")
		return
	} else if err != nil {
		internalError(w)
		return
	}
	switch status {
	case "queued":
		// сперва пробуем отменить в БД; если агент уже забрал (гонка) —
		// статус станет running, дошлём cancel агенту
		if ok, err := h.store.CancelQueuedAIRun(r.Context(), id); err != nil {
			internalError(w)
			return
		} else if ok {
			// агент мог уже получить кадр (ждёт семафор) — пусть тоже забудет
			_ = h.hub.Dispatch(machineID, aicoder.Frame{Kind: "cancel", RunID: id})
			w.WriteHeader(http.StatusNoContent)
			return
		}
		fallthrough
	case "running":
		if err := h.hub.Dispatch(machineID, aicoder.Frame{Kind: "cancel", RunID: id}); err != nil {
			writeError(w, http.StatusServiceUnavailable, "offline", "агент не в сети — прогон завершится по таймауту")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusConflict, "conflict", "прогон уже завершён")
	}
}

func (h *aiHandlers) createMachine(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if n := len([]rune(req.Name)); n < 1 || n > 100 {
		badRequest(w, "name must be 1-100 characters")
		return
	}
	m, err := h.store.CreateAIMachine(r.Context(), user.ID, req.Name, newMachineToken())
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"machine": aiMachineView{AIMachine: m}})
}

func (h *aiHandlers) renameMachine(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid machine id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if n := len([]rune(req.Name)); n < 1 || n > 100 {
		badRequest(w, "name must be 1-100 characters")
		return
	}
	m, err := h.store.RenameAIMachine(r.Context(), user.ID, id, req.Name)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "machine not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"machine": aiMachineView{AIMachine: m, Online: h.hub.Online(m.ID)}})
	}
}

func (h *aiHandlers) deleteMachine(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid machine id")
		return
	}
	switch err := h.store.DeleteAIMachine(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "machine not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *aiHandlers) ownedMachine(w http.ResponseWriter, r *http.Request) (store.AIMachine, bool) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid machine id")
		return store.AIMachine{}, false
	}
	m, err := h.store.AIMachineOwned(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "machine not found")
		return store.AIMachine{}, false
	}
	if err != nil {
		internalError(w)
		return store.AIMachine{}, false
	}
	return m, true
}

// toolRequest — тело запросов к агенту про инструмент (проверка, лимиты,
// сессии); SessionID нужен только для содержимого конкретной сессии.
type toolRequest struct {
	Tool      string `json:"tool"`
	SessionID string `json:"session_id"`
}

// agentCall — синхронный вызов агента: разбор {tool}, отправка кадра и ответ
// клиенту сырым JSON агента под ключом key. Возвращает результат (для
// обработчиков, которым он нужен ещё и для сохранения) или nil, если ответ
// клиенту уже отправлен как ошибка.
func (h *aiHandlers) agentCall(w http.ResponseWriter, r *http.Request, kind, key string, timeout time.Duration) (store.AIMachine, toolRequest, json.RawMessage) {
	m, ok := h.ownedMachine(w, r)
	var req toolRequest
	if !ok {
		return m, req, nil
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !aiTools[req.Tool] {
		badRequest(w, "tool must be 'claude' or 'codex'")
		return m, req, nil
	}
	if len(req.SessionID) > 200 {
		badRequest(w, "session_id too long")
		return m, req, nil
	}
	resp, err := h.hub.Call(r.Context(), m.ID,
		aicoder.Frame{Kind: kind, Tool: req.Tool, SessionID: req.SessionID}, timeout)
	switch {
	case errors.Is(err, aicoder.ErrOffline):
		writeError(w, http.StatusServiceUnavailable, "offline", "агент не в сети")
		return m, req, nil
	case errors.Is(err, aicoder.ErrTimeout):
		writeError(w, http.StatusGatewayTimeout, "timeout", "агент не ответил")
		return m, req, nil
	case err != nil:
		internalError(w)
		return m, req, nil
	}
	result := resp.Result
	if len(result) == 0 {
		result = json.RawMessage(`{}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"` + key + `":`))
	w.Write(result)
	w.Write([]byte(`}`))
	return m, req, result
}

// POST /ai/machines/{id}/check {tool} — агент проверяет наличие и авторизацию
// инструмента; результат кэшируется в ai_machines.tools.
func (h *aiHandlers) checkTool(w http.ResponseWriter, r *http.Request) {
	// проверка авторизации — реальный мини-запрос к API, даём время
	m, req, result := h.agentCall(w, r, "check", "tool", 90*time.Second)
	if result == nil {
		return
	}
	// ответ клиенту уже ушёл — статус кэшируем «вслед», ошибка только в лог
	if err := h.store.SetAIMachineTool(r.Context(), m.ID, req.Tool, result); err != nil {
		h.logger.Error("ai: save tool status", "machine_id", m.ID, "error", err)
	}
}

// POST /ai/machines/{id}/usage {tool} — оставшиеся лимиты инструмента
// (аналог /usage в Claude Code и /status в Codex). Живые данные, не кэшируем.
func (h *aiHandlers) toolUsage(w http.ResponseWriter, r *http.Request) {
	h.agentCall(w, r, "usage", "usage", 30*time.Second)
}

// POST /ai/machines/{id}/sessions {tool} — прошлые сессии CLI-инструмента на
// машине (метаданные из индекса агента: заголовок, папка, даты).
func (h *aiHandlers) listSessions(w http.ResponseWriter, r *http.Request) {
	h.agentCall(w, r, "sessions", "sessions", 60*time.Second)
}

// POST /ai/machines/{id}/session {tool, session_id} — содержимое одной сессии
// (грузится только по клику, файлы бывают большими).
func (h *aiHandlers) getSession(w http.ResponseWriter, r *http.Request) {
	h.agentCall(w, r, "session", "session", 60*time.Second)
}

// --- задачи ---

// workdirAllowed: папка задачи должна совпадать с разрешённой папкой агента
// или лежать внутри неё (агент проверяет ещё раз на своей стороне).
func workdirAllowed(workdir string, dirs []string) bool {
	wd := path.Clean(workdir)
	for _, d := range dirs {
		d = path.Clean(d)
		if wd == d || strings.HasPrefix(wd, d+"/") {
			return true
		}
	}
	return false
}

func aiTaskTitle(prompt string) string {
	t := strings.Join(strings.Fields(prompt), " ")
	r := []rune(t)
	if len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return t
}

func (h *aiHandlers) listTasks(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	machineID, _ := strconv.ParseInt(r.URL.Query().Get("machine_id"), 10, 64)
	// подвисшие прогоны (агент умер и не вернул результат) — в ошибку
	if err := h.store.FailStaleAIRuns(r.Context(), aiStaleAfter); err != nil {
		internalError(w)
		return
	}
	tasks, err := h.store.ListAITasks(r.Context(), user.ID, machineID)
	if err != nil {
		internalError(w)
		return
	}
	if tasks == nil {
		tasks = []store.AITask{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (h *aiHandlers) createTask(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		MachineID int64  `json:"machine_id"`
		Tool      string `json:"tool"`
		Workdir   string `json:"workdir"`
		Model     string `json:"model"`
		Params    string `json:"params"`
		Prompt    string `json:"prompt"`
		Mode      string `json:"mode"`
		// продолжение существующей сессии CLI на машине («Старые сессии»):
		// первый же прогон уйдёт агенту с --resume
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Mode != "" && req.Mode != "plan" {
		badRequest(w, "mode must be '' or 'plan'")
		return
	}
	if req.Tool == "" {
		req.Tool = "claude"
	}
	if !aiTools[req.Tool] {
		badRequest(w, "tool must be 'claude' or 'codex'")
		return
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" || len(req.Prompt) > 100_000 {
		badRequest(w, "prompt is required (max 100000 chars)")
		return
	}
	if len(req.Model) > 100 || len(req.Params) > 1000 || len(req.SessionID) > 200 {
		badRequest(w, "model/params/session_id too long")
		return
	}
	m, err := h.store.AIMachineOwned(r.Context(), user.ID, req.MachineID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "machine not found")
		return
	} else if err != nil {
		internalError(w)
		return
	}
	if !workdirAllowed(req.Workdir, m.Dirs) {
		badRequest(w, "workdir не входит в разрешённые папки агента")
		return
	}
	title := aiTaskTitle(req.Prompt)
	if req.SessionID != "" {
		title = "↩ " + title // продолжение старой сессии CLI
	}
	task, err := h.store.CreateAITask(r.Context(), user.ID, m.ID, req.Tool,
		path.Clean(req.Workdir), req.Model, req.Params, title, req.SessionID)
	if err != nil {
		internalError(w)
		return
	}
	run, err := h.store.CreateAIRun(r.Context(), task.ID, req.Prompt, req.Mode)
	if err != nil {
		internalError(w)
		return
	}
	h.dispatchRun(w, task, run)
}

// POST /ai/tasks/{id}/runs {prompt} — продолжение задачи с тем же контекстом
// (--resume session_id из предыдущего прогона).
func (h *aiHandlers) continueTask(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid task id")
		return
	}
	var req struct {
		Prompt string `json:"prompt"`
		Mode   string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Mode != "" && req.Mode != "plan" {
		badRequest(w, "mode must be '' or 'plan'")
		return
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" || len(req.Prompt) > 100_000 {
		badRequest(w, "prompt is required (max 100000 chars)")
		return
	}
	task, err := h.store.AITaskOwned(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	} else if err != nil {
		internalError(w)
		return
	}
	if active, err := h.store.HasActiveAIRun(r.Context(), task.ID); err != nil {
		internalError(w)
		return
	} else if active {
		writeError(w, http.StatusConflict, "conflict", "предыдущий прогон ещё выполняется")
		return
	}
	run, err := h.store.CreateAIRun(r.Context(), task.ID, req.Prompt, req.Mode)
	if err != nil {
		internalError(w)
		return
	}
	h.dispatchRun(w, task, run)
}

// dispatchRun отправляет прогон агенту (если он в сети) и отвечает клиенту.
// При офлайне прогон остаётся queued — доставится на подключении агента
// (deliverQueued); клиенту сообщаем queued: true.
func (h *aiHandlers) dispatchRun(w http.ResponseWriter, task store.AITask, run store.AIRun) {
	err := h.hub.Dispatch(task.MachineID, aicoder.Frame{
		Kind:      "run",
		RunID:     run.ID,
		Tool:      task.Tool,
		Workdir:   task.Workdir,
		Model:     task.Model,
		Params:    task.Params,
		Prompt:    run.Prompt,
		Mode:      run.Mode,
		SessionID: task.SessionID,
	})
	writeJSON(w, http.StatusCreated, map[string]any{"task": task, "run": run, "queued_offline": err != nil})
}

func (h *aiHandlers) getTask(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid task id")
		return
	}
	task, err := h.store.AITaskOwned(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	} else if err != nil {
		internalError(w)
		return
	}
	runs, err := h.store.ListAIRuns(r.Context(), task.ID)
	if err != nil {
		internalError(w)
		return
	}
	if runs == nil {
		runs = []store.AIRun{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": task, "runs": runs})
}

func (h *aiHandlers) deleteTask(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid task id")
		return
	}
	switch err := h.store.DeleteAITask(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "task not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- расписания ---

type aiScheduleRequest struct {
	MachineID  int64  `json:"machine_id"`
	Tool       string `json:"tool"`
	Workdir    string `json:"workdir"`
	Model      string `json:"model"`
	Params     string `json:"params"`
	Prompt     string `json:"prompt"`
	Period     string `json:"period"`
	AtMinute   int32  `json:"at_minute"`
	Dow        int32  `json:"dow"`
	EveryHours int32  `json:"every_hours"`
	TzOff      int32  `json:"tz_off"`
	Enabled    *bool  `json:"enabled"`
}

// validateSchedule проверяет поля и права на машину/папку. Возвращает
// заполненный store.AISchedule (без id).
func (h *aiHandlers) validateSchedule(w http.ResponseWriter, r *http.Request, req aiScheduleRequest) (store.AISchedule, bool) {
	user := auth.UserFromContext(r.Context())
	if req.Tool == "" {
		req.Tool = "claude"
	}
	if !aiTools[req.Tool] {
		badRequest(w, "tool must be 'claude' or 'codex'")
		return store.AISchedule{}, false
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" || len(req.Prompt) > 100_000 {
		badRequest(w, "prompt is required (max 100000 chars)")
		return store.AISchedule{}, false
	}
	if len(req.Model) > 100 || len(req.Params) > 1000 {
		badRequest(w, "model/params too long")
		return store.AISchedule{}, false
	}
	if req.Period != "daily" && req.Period != "weekly" && req.Period != "hours" {
		badRequest(w, "period must be daily|weekly|hours")
		return store.AISchedule{}, false
	}
	if req.AtMinute < 0 || req.AtMinute >= 24*60 || req.Dow < 0 || req.Dow > 6 ||
		req.EveryHours < 1 || req.EveryHours > 24*30 || req.TzOff < -14*60 || req.TzOff > 14*60 {
		badRequest(w, "invalid schedule fields")
		return store.AISchedule{}, false
	}
	m, err := h.store.AIMachineOwned(r.Context(), user.ID, req.MachineID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "machine not found")
		return store.AISchedule{}, false
	} else if err != nil {
		internalError(w)
		return store.AISchedule{}, false
	}
	if !workdirAllowed(req.Workdir, m.Dirs) {
		badRequest(w, "workdir не входит в разрешённые папки агента")
		return store.AISchedule{}, false
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return store.AISchedule{
		MachineID: m.ID, Tool: req.Tool, Workdir: path.Clean(req.Workdir),
		Model: req.Model, Params: req.Params, Prompt: req.Prompt,
		Period: req.Period, AtMinute: req.AtMinute, Dow: req.Dow,
		EveryHours: req.EveryHours, TzOff: req.TzOff, Enabled: enabled,
	}, true
}

func (h *aiHandlers) listSchedules(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	list, err := h.store.ListAISchedules(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if list == nil {
		list = []store.AISchedule{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": list})
}

func (h *aiHandlers) createSchedule(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req aiScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	sc, ok := h.validateSchedule(w, r, req)
	if !ok {
		return
	}
	out, err := h.store.CreateAISchedule(r.Context(), user.ID, sc)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"schedule": out})
}

func (h *aiHandlers) updateSchedule(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid schedule id")
		return
	}
	var req aiScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	sc, ok := h.validateSchedule(w, r, req)
	if !ok {
		return
	}
	out, err := h.store.UpdateAISchedule(r.Context(), user.ID, id, sc)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "schedule not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"schedule": out})
	}
}

func (h *aiHandlers) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid schedule id")
		return
	}
	switch err := h.store.DeleteAISchedule(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "schedule not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- WS агента (вне tma-авторизации, Bearer = токен машины) ---

func (h *aiHandlers) agentWS(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = r.URL.Query().Get("token") // фолбэк: заголовки WS ограничены
	}
	if token == "" || len(token) > 200 {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	m, err := h.store.AIMachineByToken(r.Context(), token)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "unknown token", http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	ws, err := aiUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	// первый кадр — hello со списком разрешённых папок
	ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, data, err := ws.ReadMessage()
	if err != nil {
		ws.Close()
		return
	}
	var hello aicoder.Hello
	if json.Unmarshal(data, &hello) == nil {
		_ = h.store.TouchAIMachine(r.Context(), m.ID, hello.Dirs)
		// прогоны, которых у подключившегося агента нет, — осиротели
		h.failOrphaned(m.ID, hello.Active)
	}
	ws.SetReadDeadline(time.Time{})
	h.logger.Info("ai agent connected", "machine_id", m.ID,
		"dirs", len(hello.Dirs), "version", hello.Version, "active", len(hello.Active))
	h.hub.Serve(m.ID, ws) // блокирует до обрыва
}

// failOrphaned закрывает прогоны, брошенные умершим процессом агента, и
// уведомляет владельца — иначе они висели бы в «выполняется» до реапера (2ч).
func (h *aiHandlers) failOrphaned(machineID int64, active []int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ids, err := h.store.FailOrphanedAIRuns(ctx, machineID, active)
	if err != nil {
		h.logger.Error("ai: orphaned runs", "machine_id", machineID, "error", err)
		return
	}
	for _, id := range ids {
		h.logger.Info("ai: run orphaned", "run_id", id, "machine_id", machineID)
		h.notifyRunFinished(id, false, "", "прогон прерван: агент был перезапущен или остановлен")
	}
}
