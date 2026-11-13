package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/store"
)

type releasesHandlers struct {
	store  *store.Store
	authMW *auth.Middleware
}

// допустимые статусы релиза (released по умолчанию). Набор можно расширить.
var releaseStatuses = map[string]bool{
	"released":    true,
	"planned":     true,
	"in_progress": true,
	"rolled_back": true,
	"deprecated":  true,
}

// publicRelease — то, что видит обычный пользователь: без технических
// подробностей и админского комментария.
type publicRelease struct {
	ID          int64     `json:"id"`
	Version     string    `json:"version"`
	ReleasedOn  time.Time `json:"released_on"`
	Title       string    `json:"title"`
	PublicNotes string    `json:"public_notes"`
	Status      string    `json:"status"`
}

// GET /releases — список релизов. Админу отдаём все поля, обычному пользователю
// только публичные (tech_notes и comment не возвращаем принципиально).
func (h *releasesHandlers) list(w http.ResponseWriter, r *http.Request) {
	rels, err := h.store.ListReleases(r.Context())
	if err != nil {
		internalError(w)
		return
	}
	if h.authMW.IsAdminSession(r.Context()) {
		if rels == nil {
			rels = []store.Release{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"releases": rels})
		return
	}
	pub := make([]publicRelease, 0, len(rels))
	for _, rel := range rels {
		pub = append(pub, publicRelease{
			ID: rel.ID, Version: rel.Version, ReleasedOn: rel.ReleasedOn,
			Title: rel.Title, PublicNotes: rel.PublicNotes, Status: rel.Status,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"releases": pub})
}

// PATCH /releases/{id} — админ меняет комментарий и/или статус.
func (h *releasesHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid release id")
		return
	}
	var req struct {
		Comment *string `json:"comment"`
		Status  *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Status != nil && !releaseStatuses[*req.Status] {
		badRequest(w, "status must be released|planned|in_progress|rolled_back|deprecated")
		return
	}
	rel, err := h.store.UpdateRelease(r.Context(), id, req.Comment, req.Status)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "release not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"release": rel})
	}
}
