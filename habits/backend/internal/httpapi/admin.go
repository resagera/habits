package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/store"
)

type adminHandlers struct {
	store  *store.Store
	authMW *auth.Middleware
}

// adminOnly пропускает только пользователей из ADMIN_IDS.
func (h *adminHandlers) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.authMW.IsAdmin(auth.UserFromContext(r.Context()).ID) {
			writeError(w, http.StatusForbidden, "forbidden", "admin access required")
			return
		}
		next(w, r)
	}
}

func (h *adminHandlers) listUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var limit, offset int64
	limit, _ = strconv.ParseInt(q.Get("limit"), 10, 32)
	offset, _ = strconv.ParseInt(q.Get("offset"), 10, 32)

	users, total, err := h.store.ListUsers(r.Context(), int32(limit), int32(offset))
	if err != nil {
		internalError(w)
		return
	}
	if users == nil {
		users = []store.AdminUser{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users, "total": total})
}

func (h *adminHandlers) getUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid user id")
		return
	}
	user, devices, data, err := h.store.GetAdminUser(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "user not found")
	case err != nil:
		internalError(w)
	default:
		if devices == nil {
			devices = []store.UserDevice{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"user":     user,
			"devices":  devices,
			"data":     data,
			"is_admin": h.authMW.IsAdmin(id),
		})
	}
}

func (h *adminHandlers) setBanned(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid user id")
		return
	}
	var req struct {
		Banned bool `json:"banned"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Banned && h.authMW.IsAdmin(id) {
		badRequest(w, "cannot ban an admin")
		return
	}
	switch err := h.store.SetUserBanned(r.Context(), id, req.Banned); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "user not found")
	case err != nil:
		internalError(w)
	default:
		// сбрасываем кэш auth-middleware, чтобы бан подействовал сразу
		h.authMW.InvalidateUser(id)
		writeJSON(w, http.StatusOK, map[string]bool{"banned": req.Banned})
	}
}

// --- типы пользователей и лимиты (Projects) ---

// POST /admin/users/{id}/type {type} — regular/vip/payed1/payed2.
func (h *adminHandlers) setUserType(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid user id")
		return
	}
	var req struct {
		Type string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !store.ValidUserType(req.Type) {
		badRequest(w, "type must be one of: regular, vip, payed1, payed2")
		return
	}
	switch err := h.store.SetUserType(r.Context(), id, req.Type); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "user not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]string{"type": req.Type})
	}
}

// GET /admin/limits — лимиты всех типов.
func (h *adminHandlers) listLimits(w http.ResponseWriter, r *http.Request) {
	limits, err := h.store.ListTypeLimits(r.Context())
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"limits": limits})
}

// PUT /admin/limits/{type} — изменить лимиты типа.
func (h *adminHandlers) updateLimits(w http.ResponseWriter, r *http.Request) {
	t := r.PathValue("type")
	if !store.ValidUserType(t) {
		badRequest(w, "unknown user type")
		return
	}
	var req store.TypeLimits
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Type = t
	if req.MaxBlocks < 1 || req.MaxBlocks > 100000 ||
		req.MaxImages < 0 || req.MaxImages > 1000000 ||
		req.MaxFiles < 0 || req.MaxFiles > 1000000 ||
		req.MaxImageMB < 1 || req.MaxImageMB > 1024 ||
		req.MaxFileMB < 1 || req.MaxFileMB > 4096 {
		badRequest(w, "limit values out of range")
		return
	}
	switch err := h.store.UpdateTypeLimits(r.Context(), req); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "type not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"limits": req})
	}
}
