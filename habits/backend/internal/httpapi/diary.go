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

const maxDiaryText = 100000

type diaryHandlers struct {
	store *store.Store
}

func (h *diaryHandlers) list(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	q := r.URL.Query()

	var filter store.DiaryFilter
	filter.Query = q.Get("q")
	if raw := q.Get("from"); raw != "" {
		t, err := time.Parse(dateLayout, raw)
		if err != nil {
			badRequest(w, "from must be YYYY-MM-DD")
			return
		}
		filter.From = &t
	}
	if raw := q.Get("to"); raw != "" {
		t, err := time.Parse(dateLayout, raw)
		if err != nil {
			badRequest(w, "to must be YYYY-MM-DD")
			return
		}
		// включительно: фильтр до конца дня
		end := t.Add(24 * time.Hour)
		filter.To = &end
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			badRequest(w, "invalid limit")
			return
		}
		filter.Limit = int32(n)
	}

	entries, err := h.store.ListDiaryEntries(r.Context(), user.ID, filter)
	if err != nil {
		internalError(w)
		return
	}
	if entries == nil {
		entries = []store.DiaryEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (h *diaryHandlers) create(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		At   time.Time `json:"at"`
		Text string    `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body (at must be RFC3339)")
		return
	}
	if req.At.IsZero() {
		req.At = time.Now()
	}
	if !validLen(req.Text, maxDiaryText) {
		badRequest(w, "text must be 1-100000 characters")
		return
	}
	entry, err := h.store.CreateDiaryEntry(r.Context(), user.ID, req.At, req.Text)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"entry": entry})
}

func (h *diaryHandlers) update(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid entry id")
		return
	}
	var req struct {
		At   *time.Time `json:"at"`
		Text *string    `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body (at must be RFC3339)")
		return
	}
	if req.At == nil && req.Text == nil {
		badRequest(w, "nothing to update")
		return
	}
	if req.Text != nil && !validLen(*req.Text, maxDiaryText) {
		badRequest(w, "text must be 1-100000 characters")
		return
	}
	entry, err := h.store.UpdateDiaryEntry(r.Context(), user.ID, id, store.DiaryEntryPatch{
		At:   req.At,
		Text: req.Text,
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "entry not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"entry": entry})
	}
}

func (h *diaryHandlers) delete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid entry id")
		return
	}
	switch err := h.store.DeleteDiaryEntry(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "entry not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
