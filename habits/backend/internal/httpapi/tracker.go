package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

const (
	dateLayout   = "2006-01-02"
	defaultColor = "#4caf50"
	defaultEmoji = "✅"
	maxRangeDays = 400
)

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type trackerHandlers struct {
	store *store.Store
	bot   *notify.Bot
}

func validKind(k string) bool  { return k == "marks" || k == "counter" }
func validStyle(s string) bool { return s == "square" || s == "circle" || s == "emoji" }
func validEmoji(e string) bool {
	n := utf8.RuneCountInString(e)
	return n >= 1 && n <= 8 && !strings.ContainsAny(e, "<>&\"'")
}

func (h *trackerHandlers) listCategories(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	categories, err := h.store.ListCategories(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if categories == nil {
		categories = []store.Category{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": categories})
}

func (h *trackerHandlers) createCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Name  string `json:"name"`
		Color string `json:"color"`
		Kind  string `json:"kind"`
		Style string `json:"style"`
		Multi bool   `json:"multi"`
		Emoji string `json:"emoji"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if !validName(req.Name) {
		badRequest(w, "name must be 1-100 characters")
		return
	}
	if req.Color == "" {
		req.Color = defaultColor
	}
	if req.Kind == "" {
		req.Kind = "marks"
	}
	if req.Style == "" {
		req.Style = "square"
	}
	if req.Emoji == "" {
		req.Emoji = defaultEmoji
	}
	if !colorRe.MatchString(req.Color) {
		badRequest(w, "color must be #rrggbb")
		return
	}
	if !validKind(req.Kind) || !validStyle(req.Style) || !validEmoji(req.Emoji) {
		badRequest(w, "invalid kind/style/emoji")
		return
	}

	category, err := h.store.CreateCategory(r.Context(), user.ID, req.Name, req.Color, req.Kind, req.Style, req.Emoji, req.Multi)
	switch {
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "category with this name already exists")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"category": category})
	}
}

func (h *trackerHandlers) updateCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid category id")
		return
	}
	var req struct {
		Name     *string `json:"name"`
		Color    *string `json:"color"`
		Position *int32  `json:"position"`
		Daily    *bool   `json:"daily"`
		Kind     *string `json:"kind"`
		Style    *string `json:"style"`
		Multi    *bool   `json:"multi"`
		Emoji    *string `json:"emoji"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Name == nil && req.Color == nil && req.Position == nil && req.Daily == nil &&
		req.Kind == nil && req.Style == nil && req.Multi == nil && req.Emoji == nil {
		badRequest(w, "nothing to update")
		return
	}
	if req.Name != nil && !validName(*req.Name) {
		badRequest(w, "name must be 1-100 characters")
		return
	}
	if req.Color != nil && !colorRe.MatchString(*req.Color) {
		badRequest(w, "color must be #rrggbb")
		return
	}
	if req.Kind != nil && !validKind(*req.Kind) {
		badRequest(w, "kind must be marks or counter")
		return
	}
	if req.Style != nil && !validStyle(*req.Style) {
		badRequest(w, "style must be square, circle or emoji")
		return
	}
	if req.Emoji != nil && !validEmoji(*req.Emoji) {
		badRequest(w, "invalid emoji")
		return
	}

	category, err := h.store.UpdateCategory(r.Context(), user.ID, id, store.CategoryPatch{
		Name:     req.Name,
		Color:    req.Color,
		Position: req.Position,
		Daily:    req.Daily,
		Kind:     req.Kind,
		Style:    req.Style,
		Multi:    req.Multi,
		Emoji:    req.Emoji,
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "category with this name already exists")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"category": category})
	}
}

func (h *trackerHandlers) deleteCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid category id")
		return
	}
	switch err := h.store.DeleteCategory(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *trackerHandlers) marks(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	q := r.URL.Query()

	from, err := time.Parse(dateLayout, q.Get("from"))
	if err != nil {
		badRequest(w, "from must be YYYY-MM-DD")
		return
	}
	to, err := time.Parse(dateLayout, q.Get("to"))
	if err != nil {
		badRequest(w, "to must be YYYY-MM-DD")
		return
	}
	if to.Before(from) || to.Sub(from) > maxRangeDays*24*time.Hour {
		badRequest(w, "invalid range: to must be >= from and span at most 400 days")
		return
	}
	var categoryID *int64
	if raw := q.Get("category_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			badRequest(w, "invalid category_id")
			return
		}
		categoryID = &id
	}

	marks, err := h.store.MarksInRange(r.Context(), user.ID, from, to, categoryID)
	if err != nil {
		internalError(w)
		return
	}
	if marks == nil {
		marks = []store.CategoryMarks{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"marks": marks})
}

// GET /tracker/categories/{id}/history — все отметки с самой старой.
func (h *trackerHandlers) history(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid category id")
		return
	}
	days, err := h.store.CategoryHistory(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
	case err != nil:
		internalError(w)
	default:
		if days == nil {
			days = []store.MarkDay{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"days": days})
	}
}

func (h *trackerHandlers) toggleMark(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		CategoryID int64   `json:"category_id"`
		Day        string  `json:"day"`
		Color      *string `json:"color"`
		Emoji      *string `json:"emoji"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	day, err := time.Parse(dateLayout, req.Day)
	if err != nil {
		badRequest(w, "day must be YYYY-MM-DD")
		return
	}
	if req.Color != nil && !colorRe.MatchString(*req.Color) {
		badRequest(w, "color must be #rrggbb")
		return
	}
	if req.Emoji != nil && !validEmoji(*req.Emoji) {
		badRequest(w, "invalid emoji")
		return
	}

	marked, err := h.store.ToggleMark(r.Context(), user.ID, req.CategoryID, day, req.Color, req.Emoji)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"marked": marked})
	}
}

// POST /tracker/marks/increment — счётчик: клик +1, долгое нажатие −1.
func (h *trackerHandlers) incrementMark(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		CategoryID int64  `json:"category_id"`
		Day        string `json:"day"`
		Delta      int32  `json:"delta"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	day, err := time.Parse(dateLayout, req.Day)
	if err != nil {
		badRequest(w, "day must be YYYY-MM-DD")
		return
	}
	if req.Delta != 1 && req.Delta != -1 {
		badRequest(w, "delta must be 1 or -1")
		return
	}

	count, err := h.store.IncrementMark(r.Context(), user.ID, req.CategoryID, day, req.Delta)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]int32{"count": count})
	}
}

// POST /tracker/categories/{id}/share — дать доступ пользователю приложения.
func (h *trackerHandlers) share(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid category id")
		return
	}
	var req struct {
		To string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.To) == "" {
		badRequest(w, "to (user id or @username) is required")
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
		badRequest(w, "cannot share with yourself")
		return
	}
	queued, _, err := deliverShare(r.Context(), h.store, h.bot, user, recipient.ID, "tracker", id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shared_with": recipient, "queued": queued})
}

// GET /tracker/categories/{id}/shares — участники трекера.
func (h *trackerHandlers) listShares(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid category id")
		return
	}
	users, err := h.store.ListTrackerShares(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
	case err != nil:
		internalError(w)
	default:
		if users == nil {
			users = []store.AccessUser{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": users})
	}
}

// DELETE /tracker/categories/{id}/shares/{userID} — отозвать доступ (владелец)
// или покинуть трекер (участник удаляет себя).
func (h *trackerHandlers) revokeShare(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid category id")
		return
	}
	target, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
	if err != nil {
		badRequest(w, "invalid user id")
		return
	}
	switch err := h.store.RevokeTrackerShare(r.Context(), user.ID, id, target); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "share not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func validName(name string) bool {
	n := utf8.RuneCountInString(name)
	return n >= 1 && n <= 100
}
