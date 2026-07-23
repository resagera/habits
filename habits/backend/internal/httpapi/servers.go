package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/servers"
	"streaks-backend/internal/store"
)

type serversHandlers struct {
	store *store.Store
	bot   *notify.Bot
}

type serverRequest struct {
	Kind   string       `json:"kind"` // pull (по умолчанию) | push
	Name   string       `json:"name"`
	URL    string       `json:"url"`
	Token  string       `json:"token"`
	Alerts *alertConfig `json:"alerts"`
}

type alertConfig struct {
	Enabled       bool             `json:"enabled"`
	DiskMinFreeMB int64            `json:"disk_min_free_mb"`
	DiskRules     []store.DiskRule `json:"disk_rules"`
	RAMPct        int16            `json:"ram_pct"`
	RAMMinutes    int16            `json:"ram_minutes"`
	CPUPct        int16            `json:"cpu_pct"`
	CPUMinutes    int16            `json:"cpu_minutes"`
}

// validate нормализует и проверяет пороги (диапазоны — защита от опечаток).
func (a *alertConfig) validate() string {
	if a.DiskMinFreeMB <= 0 {
		a.DiskMinFreeMB = 1024
	}
	if a.DiskMinFreeMB > 1024*1024 { // до 1 ТБ
		return "disk_min_free_mb too large"
	}
	if len(a.DiskRules) > 64 {
		return "too many disk rules"
	}
	clean := a.DiskRules[:0]
	for _, r := range a.DiskRules {
		if r.Mount == "" || utf8.RuneCountInString(r.Mount) > 256 {
			return "disk rule mount is empty or too long"
		}
		if r.MinFreeMB <= 0 {
			r.MinFreeMB = a.DiskMinFreeMB
		}
		if r.MinFreeMB > 1024*1024 {
			return "disk rule threshold too large"
		}
		clean = append(clean, r)
	}
	a.DiskRules = clean
	if a.RAMPct < 50 || a.RAMPct > 100 || a.CPUPct < 50 || a.CPUPct > 100 {
		return "ram_pct/cpu_pct must be within [50, 100]"
	}
	if a.RAMMinutes < 1 || a.RAMMinutes > 180 || a.CPUMinutes < 1 || a.CPUMinutes > 180 {
		return "ram_minutes/cpu_minutes must be within [1, 180]"
	}
	return ""
}

func (req *serverRequest) validate() string {
	if req.Kind == "" {
		req.Kind = "pull"
	}
	if req.Kind != "pull" && req.Kind != "push" {
		return "kind must be pull or push"
	}
	if n := utf8.RuneCountInString(req.Name); n < 1 || n > 100 {
		return "name must be 1-100 characters"
	}
	if req.Kind == "push" {
		// у push-машины нет адреса: агент сам приходит с токеном
		req.URL, req.Token = "", ""
		return ""
	}
	u, err := url.Parse(req.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "url must be http(s)://host[:port]/path"
	}
	if len(req.Token) > 200 {
		return "token is too long"
	}
	return ""
}

func newPushToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand не отказывает на живой системе
	}
	return hex.EncodeToString(b)
}

func (h *serversHandlers) list(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	list, err := h.store.ListServers(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if list == nil {
		list = []store.Server{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": list})
}

func (h *serversHandlers) create(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req serverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if msg := req.validate(); msg != "" {
		badRequest(w, msg)
		return
	}
	pushToken := ""
	if req.Kind == "push" {
		pushToken = newPushToken()
	}
	srv, err := h.store.CreateServer(r.Context(), user.ID, req.Kind, req.Name, req.URL, req.Token, pushToken)
	if err != nil {
		internalError(w)
		return
	}
	// pull: сразу пробуем опросить, чтобы карточка не была пустой до тика поллера
	if srv.Kind == "pull" {
		h.refreshServer(r, &srv, req.URL, req.Token)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"server": srv})
}

func (h *serversHandlers) update(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid server id")
		return
	}
	var req serverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if msg := req.validate(); msg != "" {
		badRequest(w, msg)
		return
	}
	srv, err := h.store.UpdateServer(r.Context(), user.ID, id, req.Name, req.URL, req.Token)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "server not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	if req.Alerts != nil {
		if msg := req.Alerts.validate(); msg != "" {
			badRequest(w, msg)
			return
		}
		srv, err = h.store.UpdateServerAlerts(r.Context(), user.ID, id, store.ServerAlerts{
			Enabled:       req.Alerts.Enabled,
			DiskMinFreeMB: req.Alerts.DiskMinFreeMB,
			DiskRules:     req.Alerts.DiskRules,
			RAMPct:        req.Alerts.RAMPct,
			RAMMinutes:    req.Alerts.RAMMinutes,
			CPUPct:        req.Alerts.CPUPct,
			CPUMinutes:    req.Alerts.CPUMinutes,
		})
		if err != nil {
			internalError(w)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"server": srv})
}

func (h *serversHandlers) delete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid server id")
		return
	}
	switch err := h.store.DeleteServer(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "server not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// GET /servers/{id}/history?hours=24 — точки CPU/RAM для графиков.
func (h *serversHandlers) history(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid server id")
		return
	}
	hours := 24
	if raw := r.URL.Query().Get("hours"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 48 {
			badRequest(w, "hours must be within [1, 48]")
			return
		}
		hours = n
	}
	samples, err := h.store.ServerSamples(r.Context(), user.ID, id, hours)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "server not found")
	case err != nil:
		internalError(w)
	default:
		if samples == nil {
			samples = []store.ServerSample{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"samples": samples})
	}
}

// POST /servers/{id}/refresh — немедленный опрос агента (только pull:
// до push-машины сервер достучаться не может, возвращаем как есть).
func (h *serversHandlers) refresh(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid server id")
		return
	}
	list, err := h.store.ListServers(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	for i := range list {
		if list[i].ID == id {
			if list[i].Kind == "pull" {
				h.refreshServer(r, &list[i], list[i].URL, list[i].Token)
			}
			writeJSON(w, http.StatusOK, map[string]any{"server": list[i]})
			return
		}
	}
	writeError(w, http.StatusNotFound, "not_found", "server not found")
}

// refreshServer опрашивает агента и обновляет srv свежими данными.
func (h *serversHandlers) refreshServer(r *http.Request, srv *store.Server, url, token string) {
	data, err := servers.Fetch(r.Context(), url, token)
	if err != nil {
		srv.LastError = err.Error()
		_, _ = h.store.SavePollResult(r.Context(), srv.ID, nil, 0, 0, 0, err.Error())
		return
	}
	var report servers.AgentReport
	_ = json.Unmarshal(data, &report)
	out, err := h.store.SavePollResult(r.Context(), srv.ID, data, report.CPUPct, report.RAM.Used, report.RAM.Total, "")
	if err == nil {
		srv.LastData = data
		srv.LastError = ""
		now := time.Now().UTC()
		srv.LastOkAt = &now
		servers.NotifyBackOnline(r.Context(), h.bot, out)
	}
}

// POST /api/v1/agent/push — приём отчёта от push-агента (домашняя машина).
// Вне tma-авторизации: агент представляется Bearer push-токеном.
func (h *serversHandlers) agentPush(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || len(token) > 200 {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
		return
	}
	id, err := h.store.ServerByPushToken(r.Context(), token)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unknown token")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 256*1024))
	if err != nil || !json.Valid(body) {
		badRequest(w, "body must be agent report JSON")
		return
	}
	var report servers.AgentReport
	_ = json.Unmarshal(body, &report)
	out, err := h.store.SavePollResult(r.Context(), id, body, report.CPUPct, report.RAM.Used, report.RAM.Total, "")
	if err != nil {
		internalError(w)
		return
	}
	servers.NotifyBackOnline(r.Context(), h.bot, out)
	w.WriteHeader(http.StatusNoContent)
}
