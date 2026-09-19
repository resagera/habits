package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

type helpHandlers struct {
	store    *store.Store
	bot      *notify.Bot
	adminIDs []int64
}

// POST /help/contact — обращение к админу: сохраняем и шлём уведомление
// каждому админу от бота.
func (h *helpHandlers) contact(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	text := strings.TrimSpace(req.Text)
	if n := utf8.RuneCountInString(text); n < 3 || n > 4000 {
		badRequest(w, "text must be 3-4000 characters")
		return
	}
	id, err := h.store.SaveHelpRequest(r.Context(), user.ID, text)
	if err != nil {
		internalError(w)
		return
	}

	from := fmt.Sprintf("id %d", user.ID)
	if user.Username != "" {
		from += " @" + user.Username
	}
	if user.FirstName != "" {
		from += " (" + user.FirstName + ")"
	}
	msg := fmt.Sprintf("🆘 Обращение #%d\nОт: %s\n\n%s", id, from, text)
	// уведомления не должны блокировать ответ; контекст запроса к этому
	// моменту уже закроется — берём фоновый
	go h.bot.Broadcast(context.Background(), h.adminIDs, msg)

	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}
