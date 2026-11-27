package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/connector"
	"streaks-backend/internal/store"
)

// Страница «Коннектор»: правила «источник → условия → результат».
//
// Реестр источников, полей, операций и результатов отдаётся страницей вместе с
// данными (connector.Meta): формы рисуются по нему, поэтому новое поле на
// бэкенде появляется в интерфейсе само.

type connectorHandlers struct {
	store  *store.Store
	engine *connector.Engine
}

type connectorRuleRequest struct {
	Name            string                  `json:"name"`
	Enabled         *bool                   `json:"enabled"`
	Sources         []string                `json:"sources"`
	Match           store.ConnectorNode     `json:"match"`
	Actions         []store.ConnectorAction `json:"actions"`
	TzOffsetMinutes int32                   `json:"tz_offset_minutes"`
}

func (req connectorRuleRequest) rule() store.ConnectorRule {
	r := store.ConnectorRule{
		Name: strings.TrimSpace(req.Name), Enabled: true,
		Sources: req.Sources, Match: req.Match, Actions: req.Actions,
		TzOffsetMinutes: req.TzOffsetMinutes,
	}
	if req.Enabled != nil {
		r.Enabled = *req.Enabled
	}
	return r
}

// GET /connector/overview — правила, журнал, реестр и всё, что нужно формам:
// трекеры (с их наборами цветов), адреса почты и последние письма для проверки.
func (h *connectorHandlers) overview(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	ctx := r.Context()

	rules, err := h.store.ListConnectorRules(ctx, user.ID)
	if err != nil {
		internalError(w)
		return
	}
	runs, err := h.store.ListConnectorRuns(ctx, user.ID, 50)
	if err != nil {
		internalError(w)
		return
	}
	// трекеры и письма берём здесь, а не запросами к чужим страницам: у
	// коннектора свой доступ, и он не должен зависеть от доступа к /tracker
	trackers, err := h.store.ListCategories(ctx, user.ID)
	if err != nil {
		internalError(w)
		return
	}
	addresses, err := h.store.ListMailAddresses(ctx, user.ID)
	if err != nil {
		internalError(w)
		return
	}
	messages, err := h.store.ConnectorMailMessages(ctx, user.ID, 20)
	if err != nil {
		internalError(w)
		return
	}
	if rules == nil {
		rules = []store.ConnectorRule{}
	}
	if runs == nil {
		runs = []store.ConnectorRun{}
	}
	if trackers == nil {
		trackers = []store.Category{}
	}
	if addresses == nil {
		addresses = []store.MailAddress{}
	}
	if messages == nil {
		messages = []store.MailMessage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rules": rules, "runs": runs, "meta": connector.Meta(),
		"trackers": trackers, "addresses": addresses, "messages": messages,
	})
}

func (h *connectorHandlers) create(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req connectorRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	rule := req.rule()
	if err := connector.Validate(rule); err != nil {
		badRequest(w, err.Error())
		return
	}
	out, err := h.store.CreateConnectorRule(r.Context(), user.ID, rule)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"rule": out})
}

func (h *connectorHandlers) update(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid rule id")
		return
	}
	var req connectorRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	rule := req.rule()
	rule.ID = id
	if err := connector.Validate(rule); err != nil {
		badRequest(w, err.Error())
		return
	}
	out, err := h.store.UpdateConnectorRule(r.Context(), user.ID, rule)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "rule not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"rule": out})
	}
}

func (h *connectorHandlers) setEnabled(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid rule id")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	err = h.store.SetConnectorRuleEnabled(r.Context(), user.ID, id, req.Enabled)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "rule not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"enabled": req.Enabled})
	}
}

func (h *connectorHandlers) delete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid rule id")
		return
	}
	err = h.store.DeleteConnectorRule(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "rule not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// POST /connector/test — проверка БЕЗ действий: сошлось или нет и почему.
// Правило приходит телом, а не берётся по id: проверять надо и то, что ещё не
// сохранено, — иначе правило не настроить, не насорив в трекере.
func (h *connectorHandlers) test(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Rule      connectorRuleRequest `json:"rule"`
		MessageID int64                `json:"message_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	rule := req.Rule.rule()
	// в проверке название и результаты не важны: интересно только дерево
	if rule.Name == "" {
		rule.Name = "проверка"
	}
	if len(rule.Actions) == 0 {
		rule.Actions = []store.ConnectorAction{{Type: connector.ActionNotify, Text: "проверка"}}
	}
	if err := connector.Validate(rule); err != nil {
		badRequest(w, err.Error())
		return
	}

	msgID := req.MessageID
	if msgID == 0 {
		list, err := h.store.ConnectorMailMessages(r.Context(), user.ID, 1)
		if err != nil {
			internalError(w)
			return
		}
		if len(list) == 0 {
			badRequest(w, "проверять не на чем: писем пока нет")
			return
		}
		msgID = list[0].ID
	}
	rule.UserID = user.ID
	ev, err := h.engine.MailEvent(r.Context(), user.ID, msgID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "message not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	matched, trace := connector.Eval(rule.Match, ev)
	writeJSON(w, http.StatusOK, map[string]any{
		"matched": matched, "trace": trace,
		"message_id": msgID, "event_label": ev.Label,
	})
}

// POST /connector/rules/{id}/apply — применить правило к письму по-настоящему.
// Нужно для писем, пришедших до создания правила. Повторное применение
// разрешено осознанно: человек просит сам (отметка от этого не удвоится —
// отметка дня идемпотентна, счётчик прибавится).
func (h *connectorHandlers) apply(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid rule id")
		return
	}
	var req struct {
		MessageID int64 `json:"message_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	rule, err := h.store.ConnectorRuleByID(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "rule not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	ev, err := h.engine.MailEvent(r.Context(), user.ID, req.MessageID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "message not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	res, err := h.engine.Run(r.Context(), rule, ev, true)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": res})
}

func (h *connectorHandlers) runs(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := h.store.ListConnectorRuns(r.Context(), user.ID, limit)
	if err != nil {
		internalError(w)
		return
	}
	if list == nil {
		list = []store.ConnectorRun{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": list})
}
