package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

type passwordsHandlers struct {
	store *store.Store
	bot   *notify.Bot
}

// GET /passwords/vault — зашифрованный блоб и его версия ({"vault":null} — пусто).
func (h *passwordsHandlers) getVault(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	v, err := h.store.GetPasswordVault(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"vault": v})
}

// PUT /passwords/vault {blob, base_version} — optimistic-версионирование:
// base_version должен совпасть с текущей версией (0 — блоба ещё нет),
// иначе 409 с актуальной версией.
func (h *passwordsHandlers) putVault(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Blob        string `json:"blob"`
		BaseVersion int64  `json:"base_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Blob == "" || len(req.Blob) > 2<<20 {
		badRequest(w, "blob must be non-empty and at most 2 MB")
		return
	}
	if req.BaseVersion < 0 {
		badRequest(w, "base_version must be >= 0")
		return
	}
	v, err := h.store.PutPasswordVault(r.Context(), user.ID, req.Blob, req.BaseVersion)
	switch {
	case errors.Is(err, store.ErrVersionConflict):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":           map[string]string{"code": "version_conflict", "message": "vault was updated from another device"},
			"current_version": v.Version,
		})
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"version": v.Version, "updated_at": v.UpdatedAt})
	}
}

// POST /passwords/shares — передать папку пользователю (пакет зашифрован
// на устройстве отправителя).
func (h *passwordsHandlers) createShare(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		To         string `json:"to"`
		FolderName string `json:"folder_name"`
		Payload    string `json:"payload"`
		Key        string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(req.FolderName)); n < 1 || n > 200 {
		badRequest(w, "folder_name must be 1-200 characters")
		return
	}
	if req.Payload == "" || len(req.Payload) > 1<<20 || req.Key == "" || len(req.Key) > 500 {
		badRequest(w, "payload (≤1 MB) and key are required")
		return
	}
	recipient, err := h.store.FindUserExact(r.Context(), strings.TrimSpace(req.To))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	if recipient.ID == user.ID {
		badRequest(w, "cannot send to yourself")
		return
	}
	if err := h.store.CreatePasswordShare(r.Context(), user.ID, recipient.ID,
		strings.TrimSpace(req.FolderName), req.Payload, req.Key); err != nil {
		internalError(w)
		return
	}
	from := user.FirstName
	if user.Username != "" {
		from += " @" + user.Username
	}
	go h.bot.SendMessage(context.Background(), recipient.ID,
		fmt.Sprintf("🔑 %s передал вам папку паролей «%s» — откройте вкладку Passwords в Habits, чтобы принять.",
			strings.TrimSpace(from), strings.TrimSpace(req.FolderName)))
	_ = h.store.TouchShareRecipient(r.Context(), user.ID, recipient.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"sent_to": recipient})
}

// GET /passwords/shares — входящие передачи.
func (h *passwordsHandlers) listShares(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	shares, err := h.store.ListPasswordShares(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if shares == nil {
		shares = []store.PasswordShare{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"shares": shares})
}

// DELETE /passwords/shares/{id} — принято или отклонено.
func (h *passwordsHandlers) deleteShare(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid share id")
		return
	}
	switch err := h.store.DeletePasswordShare(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "share not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// DELETE /passwords/vault — сброс хранилища (клиент делает это при
// «удалить хранилище» в Settings).
func (h *passwordsHandlers) deleteVault(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if err := h.store.DeletePasswordVault(r.Context(), user.ID); err != nil {
		internalError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
