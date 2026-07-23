package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"streaks-backend/internal/automation"
	"streaks-backend/internal/auth"
	"streaks-backend/internal/egress"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

// Automation: страница автоматизаций (persary — доступ не всем). Первый тип —
// заказ воды на jur.am. Учётные данные шифруются в БД, наружу не отдаются.
type automationHandlers struct {
	store  *store.Store
	bot    *notify.Bot
	egress *egress.Hub
}

var automationUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(*http.Request) bool { return true },
}

// вид для фронта: без секретов, с признаком что креды заданы.
type automationView struct {
	store.Automation
	Config   automationCfgView `json:"config"`
	HasCreds bool              `json:"has_creds"`
}

type automationCfgView struct {
	Login    string `json:"login"` // маскированный
	Quantity int    `json:"quantity"`
	TareMode string `json:"tare_mode"`
	TareQty  int    `json:"tare_qty"`
	TimeSlot string `json:"time_slot"`
	Payment  string `json:"payment"`
	Comment  string `json:"comment"`
}

func toView(a store.Automation) automationView {
	login, _, _ := a.Config.Creds()
	return automationView{
		Automation: a,
		Config: automationCfgView{
			Login:    maskLogin(login),
			Quantity: a.Config.Quantity,
			TareMode: a.Config.TareMode,
			TareQty:  a.Config.TareQty,
			TimeSlot: a.Config.TimeSlot,
			Payment:  a.Config.Payment,
			Comment:  a.Config.Comment,
		},
		HasCreds: a.Config.LoginEnc != "" && a.Config.PasswordEnc != "",
	}
}

func maskLogin(login string) string {
	if login == "" {
		return ""
	}
	if at := strings.IndexByte(login, '@'); at > 1 {
		return login[:1] + "***" + login[at:]
	}
	if len(login) <= 2 {
		return "***"
	}
	return login[:1] + "***" + login[len(login)-1:]
}

// тело запроса создания/обновления
type automationReq struct {
	Title        *string  `json:"title"`
	Enabled      *bool    `json:"enabled"`
	IntervalDays *int     `json:"interval_days"`
	NextRunAt    *string  `json:"next_run_at"` // RFC3339 или "" (только вручную)
	Login        *string  `json:"login"`
	Password     *string  `json:"password"`
	Quantity     *int     `json:"quantity"`
	TareMode     *string  `json:"tare_mode"`
	TareQty      *int     `json:"tare_qty"`
	TimeSlot     *string  `json:"time_slot"`
	Payment      *string  `json:"payment"`
	Comment      *string  `json:"comment"`
}

func applyAutomationReq(a *store.Automation, req automationReq) string {
	if req.Title != nil {
		a.Title = strings.TrimSpace(*req.Title)
	}
	if len(a.Title) == 0 || len(a.Title) > 200 {
		return "название обязательно (до 200 символов)"
	}
	if req.Enabled != nil {
		a.Enabled = *req.Enabled
	}
	if req.IntervalDays != nil {
		if *req.IntervalDays < 1 || *req.IntervalDays > 365 {
			return "интервал должен быть от 1 до 365 дней"
		}
		a.IntervalDays = *req.IntervalDays
	}
	if req.NextRunAt != nil {
		if strings.TrimSpace(*req.NextRunAt) == "" {
			a.NextRunAt = nil
		} else {
			t, err := time.Parse(time.RFC3339, *req.NextRunAt)
			if err != nil {
				return "next_run_at: ожидается формат RFC3339"
			}
			tu := t.UTC()
			a.NextRunAt = &tu
		}
	}
	if req.Quantity != nil {
		if *req.Quantity < 1 || *req.Quantity > 100 {
			return "количество должно быть от 1 до 100"
		}
		a.Config.Quantity = *req.Quantity
	}
	if req.TareMode != nil {
		if *req.TareMode != "auto" && *req.TareMode != "fixed" {
			return "tare_mode: auto или fixed"
		}
		a.Config.TareMode = *req.TareMode
	}
	if req.TareQty != nil {
		if *req.TareQty < 0 || *req.TareQty > 100 {
			return "количество тары от 0 до 100"
		}
		a.Config.TareQty = *req.TareQty
	}
	if req.TimeSlot != nil {
		if len(*req.TimeSlot) > 40 {
			return "time_slot слишком длинный"
		}
		a.Config.TimeSlot = *req.TimeSlot
	}
	if req.Payment != nil {
		a.Config.Payment = *req.Payment
	}
	if a.Config.Payment == "" {
		a.Config.Payment = "checkmo"
	}
	if req.Comment != nil {
		if len(*req.Comment) > 500 {
			return "комментарий до 500 символов"
		}
		a.Config.Comment = *req.Comment
	}
	// учётные данные — только если присланы (иначе не трогаем зашифрованные)
	if err := a.Config.SetCredentials(deref(req.Login), deref(req.Password)); err != nil {
		return "не удалось зашифровать учётные данные"
	}
	return ""
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

func (h *automationHandlers) list(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	items, err := h.store.ListAutomations(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	views := make([]automationView, 0, len(items))
	for _, a := range items {
		views = append(views, toView(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"automations": views})
}

func (h *automationHandlers) create(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req automationReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid json")
		return
	}
	a := store.Automation{
		Kind:         "jur_water",
		Enabled:      true,
		IntervalDays: 10,
		Config:       store.AutomationConfig{Quantity: 14, TareMode: "auto", TimeSlot: "first", Payment: "checkmo"},
	}
	if req.Enabled == nil {
		a.Enabled = true
	}
	if msg := applyAutomationReq(&a, req); msg != "" {
		badRequest(w, msg)
		return
	}
	created, err := h.store.CreateAutomation(r.Context(), user.ID, a)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"automation": toView(*created)})
}

func (h *automationHandlers) update(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	existing, err := h.store.GetAutomation(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "automation not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	var req automationReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid json")
		return
	}
	if msg := applyAutomationReq(existing, req); msg != "" {
		badRequest(w, msg)
		return
	}
	updated, err := h.store.UpdateAutomation(r.Context(), user.ID, id, *existing)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"automation": toView(*updated)})
}

func (h *automationHandlers) delete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	switch err := h.store.DeleteAutomation(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "automation not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *automationHandlers) runs(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	if _, err := h.store.GetAutomation(r.Context(), user.ID, id); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "automation not found")
		return
	}
	runs, err := h.store.ListAutomationRuns(r.Context(), user.ID, id, 30)
	if err != nil {
		internalError(w)
		return
	}
	if runs == nil {
		runs = []store.AutomationRun{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

// POST /automation/{id}/run{?dry=true} — ручной запуск или пробный прогон.
// Выполняется синхронно (сценарий занимает считанные секунды) — фронт сразу
// получает лог шагов.
func (h *automationHandlers) run(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	a, err := h.store.GetAutomation(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "automation not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	dry := r.URL.Query().Get("dry") == "true"
	trigger := "manual"
	if dry {
		trigger = "dry_run"
	}
	steps, runErr := automation.Execute(r.Context(), h.store, h.bot, h.egress, user.ID, *a, trigger, dry)
	resp := map[string]any{"steps": steps, "ok": runErr == nil}
	if runErr != nil {
		resp["error"] = runErr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

// GET /automation/agent-info — токен агента (создаётся при первом обращении),
// команда установки и статус подключения. Токен — секрет, показывается владельцу.
func (h *automationHandlers) agentInfo(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	token, err := h.store.GetOrCreateAgentToken(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":   token,
		"online":  h.egress.Online(user.ID),
		"install": "curl -fsSL https://raw.githubusercontent.com/resagera/habits-agent/main/install-automation.sh | sudo bash -s -- " + token,
	})
}

// POST /automation/agent-token/regenerate — новый токен (старый агент отвалится).
func (h *automationHandlers) regenAgentToken(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	token, err := h.store.RegenAgentToken(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token})
}

// GET /api/v1/automation/agent — WS домашнего агента (вне tma-auth,
// Bearer-токен). Держит исходящее соединение для туннелирования запросов к jur.am.
func (h *automationHandlers) agentWS(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if token == "" || len(token) > 200 {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	uid, err := h.store.AutomationAgentUser(r.Context(), token)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "unknown token", http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	ws, err := automationUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	h.store.TouchAutomationAgent(r.Context(), uid)
	h.egress.Serve(uid, ws) // блокирует до обрыва
}
