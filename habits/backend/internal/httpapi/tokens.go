package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/store"
)

// tokensHandlers — персональные токены доступа: вход в приложение вне
// Telegram (веб-версия, расширение браузера). Выпуск доступен ТОЛЬКО из
// Telegram — токен-сессия сюда не проходит (гард в auth.Middleware).
type tokensHandlers struct {
	store *store.Store
}

// tokenPrefix делает токен узнаваемым в чужих логах и утечках.
const tokenPrefix = "hbt_"

// newAccessToken генерирует токен и возвращает его вместе с sha256-хэшем
// (в БД ложится только хэш) и коротким префиксом для списка.
func newAccessToken() (token, hash, prefix string) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	token = tokenPrefix + hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:]), token[:len(tokenPrefix)+8]
}

func (h *tokensHandlers) list(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	tokens, err := h.store.ListAccessTokens(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if tokens == nil {
		tokens = []store.AccessToken{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

// POST /settings/tokens {name, expires_days?} — единственный момент, когда
// сам токен виден: он возвращается в ответе и больше нигде не хранится.
func (h *tokensHandlers) create(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Name        string `json:"name"`
		ExpiresDays int32  `json:"expires_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = "Браузер"
	}
	if len([]rune(req.Name)) > 100 {
		badRequest(w, "name must be at most 100 characters")
		return
	}
	if req.ExpiresDays < 0 || req.ExpiresDays > 3650 {
		badRequest(w, "expires_days must be 0 (бессрочно) или до 3650")
		return
	}
	var expiresAt *time.Time
	if req.ExpiresDays > 0 {
		t := time.Now().UTC().AddDate(0, 0, int(req.ExpiresDays))
		expiresAt = &t
	}

	token, hash, prefix := newAccessToken()
	meta, err := h.store.CreateAccessToken(r.Context(), user.ID, req.Name, hash, prefix, expiresAt)
	switch {
	case errors.Is(err, store.ErrTooManyTokens):
		writeError(w, http.StatusConflict, "conflict", "слишком много токенов — отзовите ненужные")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"token": token, "meta": meta})
	}
}

func (h *tokensHandlers) revoke(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid token id")
		return
	}
	switch err := h.store.RevokeAccessToken(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "token not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
