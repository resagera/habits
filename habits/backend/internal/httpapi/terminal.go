package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/store"
	"streaks-backend/internal/terminal"

	"github.com/gorilla/websocket"
)

type terminalHandlers struct {
	store   *store.Store
	hub     *terminal.Hub
	tickets *terminal.Tickets
}

var terminalUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(*http.Request) bool { return true }, // авторизация — токен/пропуск, не Origin
}

func newTerminalToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

type terminalMachineView struct {
	store.TerminalMachine
	Online bool `json:"online"`
}

func (h *terminalHandlers) list(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	machines, err := h.store.ListTerminalMachines(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	views := make([]terminalMachineView, 0, len(machines))
	for _, m := range machines {
		views = append(views, terminalMachineView{TerminalMachine: m, Online: h.hub.Online(m.ID)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"machines": views})
}

func (h *terminalHandlers) create(w http.ResponseWriter, r *http.Request) {
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
	m, err := h.store.CreateTerminalMachine(r.Context(), user.ID, req.Name, newTerminalToken())
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"machine": terminalMachineView{TerminalMachine: m}})
}

func (h *terminalHandlers) rename(w http.ResponseWriter, r *http.Request) {
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
	m, err := h.store.RenameTerminalMachine(r.Context(), user.ID, id, req.Name)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "machine not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"machine": terminalMachineView{TerminalMachine: m, Online: h.hub.Online(m.ID)}})
	}
}

func (h *terminalHandlers) delete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid machine id")
		return
	}
	switch err := h.store.DeleteTerminalMachine(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "machine not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// POST /terminal/machines/{id}/session — пропуск на открытие консоли.
func (h *terminalHandlers) session(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid machine id")
		return
	}
	m, err := h.store.TerminalMachineOwned(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "machine not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	if !h.hub.Online(m.ID) {
		writeError(w, http.StatusServiceUnavailable, "offline", "machine is offline")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ticket": h.tickets.Issue(m.ID)})
}

// GET /api/v1/terminal/stream/{ticket} — WebSocket браузерной консоли.
// Вне tma-авторизации: пропуск сам является одноразовым секретом.
// Кадры браузера: бинарные — stdin; текстовые — JSON {"t":"resize",cols,rows}.
// Кадры к браузеру: бинарные — stdout PTY.
func (h *terminalHandlers) stream(w http.ResponseWriter, r *http.Request) {
	machineID, ok := h.tickets.Redeem(r.PathValue("ticket"))
	if !ok {
		http.Error(w, "ticket not found", http.StatusNotFound)
		return
	}
	sess, err := h.hub.OpenSession(machineID, 80, 24)
	if err != nil {
		http.Error(w, "machine offline", http.StatusServiceUnavailable)
		return
	}
	ws, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		sess.Close()
		return
	}
	defer ws.Close()
	defer sess.Close()

	// PTY stdout → браузер
	go func() {
		for data := range sess.Output {
			ws.SetWriteDeadline(time.Now().Add(15 * time.Second))
			if err := ws.WriteMessage(websocket.BinaryMessage, data); err != nil {
				sess.Close()
				return
			}
		}
		// канал закрыт (сессия/агент завершились) — закрываем сокет клиента
		ws.SetWriteDeadline(time.Now().Add(2 * time.Second))
		ws.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "session ended"))
		ws.Close()
	}()

	ws.SetReadLimit(1 << 20)
	// браузер → PTY
	for {
		typ, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		switch typ {
		case websocket.BinaryMessage:
			if err := sess.Write(data); err != nil {
				return
			}
		case websocket.TextMessage:
			var msg struct {
				T    string `json:"t"`
				Cols uint16 `json:"cols"`
				Rows uint16 `json:"rows"`
			}
			if json.Unmarshal(data, &msg) == nil && msg.T == "resize" {
				_ = sess.Resize(msg.Cols, msg.Rows)
			}
		}
	}
}

// GET /api/v1/terminal/agent — исходящее WS-соединение агента (Bearer токен).
func (h *terminalHandlers) agentWS(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if token == "" || len(token) > 200 {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	m, err := h.store.TerminalMachineByToken(r.Context(), token)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "unknown token", http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	ws, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	_ = h.store.TouchTerminalMachine(r.Context(), m.ID)
	h.hub.Serve(m.ID, ws) // блокирует до обрыва
}
