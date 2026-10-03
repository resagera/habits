package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
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

var timeOfDayRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

type remindersHandlers struct {
	store *store.Store
	bot   *notify.Bot
}

// reminderRequest — общее тело для create и update (полная замена).
type reminderRequest struct {
	Title           string     `json:"title"`
	Note            string     `json:"note"`
	Kind            string     `json:"kind"`
	At              *time.Time `json:"at"`
	TimeOfDay       *string    `json:"time_of_day"`
	DaysMask        int32      `json:"days_mask"`
	DayOfMonth      *int32     `json:"day_of_month"`
	Month           *int32     `json:"month"`
	IntervalMinutes *int32     `json:"interval_minutes"`
	CategoryID      *int64     `json:"category_id"`
	GroupID         *int64     `json:"group_id"`
	TzOffsetMinutes int32      `json:"tz_offset_minutes"`
	Enabled         *bool      `json:"enabled"`
}

// validate возвращает нормализованный store.Reminder или текст ошибки.
func (req *reminderRequest) validate() (store.Reminder, string) {
	var zero store.Reminder
	if n := utf8.RuneCountInString(req.Title); n < 1 || n > 200 {
		return zero, "title must be 1-200 characters"
	}
	if utf8.RuneCountInString(req.Note) > 1000 {
		return zero, "note must be at most 1000 characters"
	}
	if req.TzOffsetMinutes < -840 || req.TzOffsetMinutes > 840 {
		return zero, "tz_offset_minutes must be within [-840, 840]"
	}
	needTime := func() string {
		if req.TimeOfDay == nil || !timeOfDayRe.MatchString(*req.TimeOfDay) {
			return "time_of_day must be HH:MM"
		}
		return ""
	}
	r := store.Reminder{
		Title:           req.Title,
		Note:            req.Note,
		Kind:            req.Kind,
		GroupID:         req.GroupID,
		TzOffsetMinutes: req.TzOffsetMinutes,
		DaysMask:        127,
		Enabled:         req.Enabled == nil || *req.Enabled,
	}
	switch req.Kind {
	case "once":
		if req.At == nil {
			return zero, "at is required for kind=once"
		}
		r.At = req.At
	case "daily":
		if msg := needTime(); msg != "" {
			return zero, msg
		}
		r.TimeOfDay = req.TimeOfDay
	case "weekly":
		if msg := needTime(); msg != "" {
			return zero, msg
		}
		if req.DaysMask < 1 || req.DaysMask > 127 {
			return zero, "days_mask must be within [1, 127]"
		}
		r.TimeOfDay = req.TimeOfDay
		r.DaysMask = req.DaysMask
	case "monthly":
		if msg := needTime(); msg != "" {
			return zero, msg
		}
		if req.DayOfMonth == nil || *req.DayOfMonth < 1 || *req.DayOfMonth > 31 {
			return zero, "day_of_month must be within [1, 31]"
		}
		r.TimeOfDay = req.TimeOfDay
		r.DayOfMonth = req.DayOfMonth
	case "yearly":
		if msg := needTime(); msg != "" {
			return zero, msg
		}
		if req.DayOfMonth == nil || *req.DayOfMonth < 1 || *req.DayOfMonth > 31 {
			return zero, "day_of_month must be within [1, 31]"
		}
		if req.Month == nil || *req.Month < 1 || *req.Month > 12 {
			return zero, "month must be within [1, 12]"
		}
		r.TimeOfDay = req.TimeOfDay
		r.DayOfMonth = req.DayOfMonth
		r.Month = req.Month
	case "interval":
		if req.IntervalMinutes == nil || *req.IntervalMinutes < 5 || *req.IntervalMinutes > 60*24*365 {
			return zero, "interval_minutes must be within [5, 525600]"
		}
		r.IntervalMinutes = req.IntervalMinutes
	case "tracker":
		if msg := needTime(); msg != "" {
			return zero, msg
		}
		if req.CategoryID == nil {
			return zero, "category_id is required for kind=tracker"
		}
		if req.DaysMask >= 1 && req.DaysMask <= 127 {
			r.DaysMask = req.DaysMask
		}
		r.TimeOfDay = req.TimeOfDay
		r.CategoryID = req.CategoryID
	default:
		return zero, "kind must be one of: once, daily, weekly, monthly, yearly, interval, tracker"
	}
	return r, ""
}

// --- категории напоминаний ---

func (h *remindersHandlers) listGroups(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	groups, err := h.store.ListReminderCategories(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if groups == nil {
		groups = []store.ReminderCategory{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": groups})
}

func (h *remindersHandlers) createGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if n := utf8.RuneCountInString(req.Name); n < 1 || n > 100 {
		badRequest(w, "name must be 1-100 characters")
		return
	}
	c, err := h.store.CreateReminderCategory(r.Context(), user.ID, req.Name)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"category": c})
}

func (h *remindersHandlers) renameGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid category id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if n := utf8.RuneCountInString(req.Name); n < 1 || n > 100 {
		badRequest(w, "name must be 1-100 characters")
		return
	}
	c, err := h.store.RenameReminderCategory(r.Context(), user.ID, id, req.Name)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"category": c})
	}
}

func (h *remindersHandlers) deleteGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid category id")
		return
	}
	switch err := h.store.DeleteReminderCategory(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// POST /reminder-categories/{id}/share-token — токен и ссылка-приглашение.
func (h *remindersHandlers) shareGroupToken(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid category id")
		return
	}
	token, err := h.store.EnsureReminderCategoryShareToken(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	link := ""
	if username := h.bot.Username(r.Context()); username != "" {
		link = fmt.Sprintf("https://t.me/%s?startapp=rem_%s", username, token)
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token, "link": link})
}

// POST /reminder-categories/{id}/send — отправить копию категории пользователю.
func (h *remindersHandlers) sendGroup(w http.ResponseWriter, r *http.Request) {
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
		badRequest(w, "cannot send to yourself")
		return
	}
	queued, _, err := deliverShare(r.Context(), h.store, h.bot, user, recipient.ID, "reminder_category", id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent_to": recipient, "queued": queued})
}

// POST /reminder-categories/redeem — принять категорию по токену-приглашению.
func (h *remindersHandlers) redeemGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Token = strings.TrimPrefix(strings.TrimSpace(req.Token), "rem_")
	if !groupShareTokenRe.MatchString(req.Token) {
		badRequest(w, "invalid token")
		return
	}
	name, err := h.store.RedeemReminderCategoryShareToken(r.Context(), user.ID, req.Token, time.Now().UTC())
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "invite not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"category": map[string]string{"name": name}})
	}
}

// POST /reminder-categories/import — создать категорию из импорта (JSON).
func (h *remindersHandlers) importGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Name      string            `json:"name"`
		Reminders []reminderRequest `json:"reminders"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if n := utf8.RuneCountInString(req.Name); n < 1 || n > 100 {
		badRequest(w, "name must be 1-100 characters")
		return
	}
	if len(req.Reminders) > 500 {
		badRequest(w, "too many reminders")
		return
	}
	var rems []store.Reminder
	for _, rr := range req.Reminders {
		if rr.Kind == "tracker" {
			continue // завязаны на привычки Tracker получателя
		}
		rem, msg := rr.validate()
		if msg != "" {
			badRequest(w, "reminder: "+msg)
			return
		}
		rems = append(rems, rem)
	}
	cat, err := h.store.ImportReminderCategory(r.Context(), user.ID, req.Name, rems, time.Now().UTC())
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"category": cat})
}

func (h *remindersHandlers) list(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	reminders, err := h.store.ListReminders(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if reminders == nil {
		reminders = []store.Reminder{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reminders": reminders})
}

func (h *remindersHandlers) upcoming(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	limit := 3
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 20 {
			badRequest(w, "limit must be within [1, 20]")
			return
		}
		limit = n
	}
	reminders, err := h.store.UpcomingReminders(r.Context(), user.ID, limit)
	if err != nil {
		internalError(w)
		return
	}
	if reminders == nil {
		reminders = []store.Reminder{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reminders": reminders})
}

func (h *remindersHandlers) create(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req reminderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	rem, msg := req.validate()
	if msg != "" {
		badRequest(w, msg)
		return
	}
	created, err := h.store.CreateReminder(r.Context(), user.ID, rem, time.Now().UTC())
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "tracker category not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"reminder": created})
	}
}

func (h *remindersHandlers) update(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid reminder id")
		return
	}
	var req reminderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	rem, msg := req.validate()
	if msg != "" {
		badRequest(w, msg)
		return
	}
	updated, err := h.store.UpdateReminder(r.Context(), user.ID, id, rem, time.Now().UTC())
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "reminder not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"reminder": updated})
	}
}

// toggle — лёгкое включение/выключение без полной замены параметров.
func (h *remindersHandlers) toggle(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid reminder id")
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
		badRequest(w, "enabled (bool) is required")
		return
	}
	updated, err := h.store.SetReminderEnabled(r.Context(), user.ID, id, *req.Enabled, time.Now().UTC())
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "reminder not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"reminder": updated})
	}
}

func (h *remindersHandlers) delete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid reminder id")
		return
	}
	switch err := h.store.DeleteReminder(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "reminder not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
