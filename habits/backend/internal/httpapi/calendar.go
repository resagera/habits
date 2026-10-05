package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/store"
)

// calendarHandlers — страница «Календарь»: агрегат по дням из трекеров,
// напоминаний, дневника, задач, чек-листов, еды и AI-расписаний.
type calendarHandlers struct {
	store *store.Store
}

// GET /calendar?from=YYYY-MM-DD&to=YYYY-MM-DD&tz_off=<минуты> — все слои
// диапазона (обычно месяц + хвосты недель). Фильтрация слоёв — на клиенте.
func (h *calendarHandlers) data(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	q := r.URL.Query()
	from, err1 := time.Parse("2006-01-02", q.Get("from"))
	to, err2 := time.Parse("2006-01-02", q.Get("to"))
	if err1 != nil || err2 != nil || to.Before(from) || to.Sub(from) > 62*24*time.Hour {
		badRequest(w, "from/to must be YYYY-MM-DD, range up to 62 days")
		return
	}
	tzOff, _ := strconv.Atoi(q.Get("tz_off"))
	if tzOff < -14*60 || tzOff > 14*60 {
		tzOff = 0
	}
	loc := time.FixedZone("tz", tzOff*60)
	// границы диапазона в моментах: [начало from .. конец to] локального времени
	fromT := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	toT := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, loc)
	fromStr, toStr := q.Get("from"), q.Get("to")

	categories, err := h.store.ListCategories(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	marks, err := h.store.MarksInRange(r.Context(), user.ID, from, to, nil)
	if err != nil {
		internalError(w)
		return
	}
	reminders, err := h.store.CalendarReminders(r.Context(), user.ID, fromT, toT, loc)
	if err != nil {
		internalError(w)
		return
	}
	diary, err := h.store.CalendarDiary(r.Context(), user.ID, fromT, toT, loc)
	if err != nil {
		internalError(w)
		return
	}
	tasks, err := h.store.CalendarTasks(r.Context(), user.ID, fromStr, toStr)
	if err != nil {
		internalError(w)
		return
	}
	checkerDays, err := h.store.CalendarCheckerDays(r.Context(), user.ID, fromStr, toStr)
	if err != nil {
		internalError(w)
		return
	}
	deadlines, err := h.store.CalendarDeadlines(r.Context(), user.ID, fromT, toT, loc)
	if err != nil {
		internalError(w)
		return
	}
	food, err := h.store.CalendarFood(r.Context(), user.ID, fromStr, toStr)
	if err != nil {
		internalError(w)
		return
	}
	ai, err := h.store.CalendarAIRuns(r.Context(), user.ID, fromT, toT, loc)
	if err != nil {
		internalError(w)
		return
	}
	if tasks == nil {
		tasks = []store.CalendarTask{}
	}
	if checkerDays == nil {
		checkerDays = []store.CalendarCheckerDay{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"categories":   categories,
		"marks":        marks,
		"reminders":    reminders,
		"diary":        diary,
		"tasks":        tasks,
		"checker_days": checkerDays,
		"deadlines":    deadlines,
		"food":         food,
		"ai":           ai,
	})
}

// GET /calendar/prefs — сохранённые слои (непрозрачный JSON фронтенда).
func (h *calendarHandlers) getPrefs(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	raw, err := h.store.GetCalendarPrefs(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"prefs":`))
	w.Write(raw)
	w.Write([]byte(`}`))
}

// PUT /calendar/prefs — полная замена настроек слоёв.
func (h *calendarHandlers) setPrefs(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16<<10))
	if err != nil || !json.Valid(raw) {
		badRequest(w, "body must be valid JSON (max 16KB)")
		return
	}
	if err := h.store.SetCalendarPrefs(r.Context(), user.ID, raw); err != nil {
		internalError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
