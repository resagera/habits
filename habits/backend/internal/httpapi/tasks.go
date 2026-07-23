package httpapi

import (
	"context"
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

type tasksHandlers struct {
	store *store.Store
	bot   *notify.Bot
}

var (
	taskDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	taskTimeRe = regexp.MustCompile(`^\d{2}:\d{2}$`)
)

func validTaskTitle(t string) bool {
	n := utf8.RuneCountInString(t)
	return n >= 1 && n <= 300
}

func validRepeatKind(k string) bool {
	return k == "daily" || k == "weekly" || k == "monthly" || k == "interval"
}

// validStatuses проверяет переопределение статусов проекта.
func validStatuses(list []store.ProjectStatus) string {
	if len(list) == 0 || len(list) > 20 {
		return "statuses must have 1-20 items"
	}
	hasOpen, hasDone := false, false
	seen := map[string]bool{}
	for _, st := range list {
		name := strings.TrimSpace(st.Name)
		if n := utf8.RuneCountInString(name); n < 1 || n > 60 {
			return "status name must be 1-60 characters"
		}
		if st.Kind != "open" && st.Kind != "done" && st.Kind != "archived" {
			return "status kind must be open, done or archived"
		}
		if seen[name] {
			return "duplicate status name"
		}
		seen[name] = true
		hasOpen = hasOpen || st.Kind == "open"
		hasDone = hasDone || st.Kind == "done"
	}
	if !hasOpen || !hasDone {
		return "statuses must include at least one open and one done status"
	}
	return ""
}

// ---------- список / сводка ----------

func (h *tasksHandlers) list(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	projects, err := h.store.ListTaskProjects(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	tasks, err := h.store.ListOpenTasks(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if projects == nil {
		projects = []store.TaskProject{}
	}
	if tasks == nil {
		tasks = []store.Task{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects, "tasks": tasks})
}

func (h *tasksHandlers) listDone(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	limit := 200
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	tasks, err := h.store.ListDoneTasks(r.Context(), user.ID, limit)
	if err != nil {
		internalError(w)
		return
	}
	if tasks == nil {
		tasks = []store.Task{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (h *tasksHandlers) summary(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	tz, _ := strconv.Atoi(r.URL.Query().Get("tz"))
	today, overdue, err := h.store.TasksSummary(r.Context(), user.ID, int32(tz), time.Now().UTC())
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"today": today, "overdue": overdue})
}

// ---------- создание / карточка ----------

type taskFields struct {
	Title           *string `json:"title"`
	Note            *string `json:"note"`
	Status          *string `json:"status"`
	Priority        *int16  `json:"priority"`
	ProjectID       *int64  `json:"project_id"`
	DueDate         *string `json:"due_date"`
	DueTime         *string `json:"due_time"`
	Remind          *bool   `json:"remind"`
	RemindBeforeMin *int32  `json:"remind_before_min"`
	RepeatKind      *string `json:"repeat_kind"`
	RepeatParam     *int32  `json:"repeat_param"`
	AssigneeID      *int64  `json:"assignee_id"`
	Position        *int32  `json:"position"`
	TzOffsetMinutes *int32  `json:"tz_offset_minutes"`
}

func (f *taskFields) validate() string {
	if f.Title != nil && !validTaskTitle(strings.TrimSpace(*f.Title)) {
		return "title must be 1-300 characters"
	}
	if f.Note != nil && len(*f.Note) > 64*1024 {
		return "note is too long"
	}
	if f.Priority != nil && (*f.Priority < 0 || *f.Priority > 3) {
		return "priority must be 0-3"
	}
	if f.DueDate != nil && *f.DueDate != "" && !taskDateRe.MatchString(*f.DueDate) {
		return "due_date must be YYYY-MM-DD"
	}
	if f.DueTime != nil && *f.DueTime != "" && !taskTimeRe.MatchString(*f.DueTime) {
		return "due_time must be HH:MM"
	}
	if f.RemindBeforeMin != nil && (*f.RemindBeforeMin < 0 || *f.RemindBeforeMin > 10080) {
		return "remind_before_min must be 0-10080"
	}
	if f.RepeatKind != nil && *f.RepeatKind != "" && !validRepeatKind(*f.RepeatKind) {
		return "repeat_kind must be daily, weekly, monthly or interval"
	}
	if f.RepeatKind != nil && *f.RepeatKind == "interval" &&
		(f.RepeatParam == nil || *f.RepeatParam < 1 || *f.RepeatParam > 365) {
		return "repeat_param (days, 1-365) is required for interval"
	}
	if f.Status != nil && utf8.RuneCountInString(strings.TrimSpace(*f.Status)) > 60 {
		return "status is too long"
	}
	return ""
}

// projectFor загружает проект (nil для «Входящих») с проверкой доступа.
func (h *tasksHandlers) projectFor(ctx context.Context, userID int64, projectID *int64) (*store.TaskProject, error) {
	if projectID == nil {
		return nil, nil
	}
	p, err := h.store.GetTaskProject(ctx, userID, *projectID)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (h *tasksHandlers) create(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req taskFields
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Title == nil {
		badRequest(w, "title is required")
		return
	}
	if msg := req.validate(); msg != "" {
		badRequest(w, msg)
		return
	}

	project, err := h.projectFor(r.Context(), user.ID, req.ProjectID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}

	// дефолты проекта — для не заданных явно полей
	if project != nil && project.Defaults != nil {
		d := project.Defaults
		if req.Priority == nil {
			req.Priority = d.Priority
		}
		if req.Remind == nil {
			req.Remind = d.Remind
		}
		if req.RemindBeforeMin == nil {
			req.RemindBeforeMin = d.RemindBeforeMin
		}
		if req.RepeatKind == nil {
			req.RepeatKind = d.RepeatKind
			req.RepeatParam = d.RepeatParam
		}
	}

	statuses := project.StatusList()
	first := store.FirstStatusOfKind(statuses, "open")

	n := store.NewTask{
		ProjectID: req.ProjectID,
		Title:     strings.TrimSpace(*req.Title),
		Status:    first.Name,
		StatusKind: first.Kind,
	}
	if req.Note != nil {
		n.Note = *req.Note
	}
	if req.Priority != nil {
		n.Priority = *req.Priority
	}
	if req.DueDate != nil && *req.DueDate != "" {
		n.DueDate = req.DueDate
	}
	if req.DueTime != nil && *req.DueTime != "" && n.DueDate != nil {
		n.DueTime = req.DueTime
	}
	if req.Remind != nil {
		n.Remind = *req.Remind
	}
	if req.RemindBeforeMin != nil {
		n.RemindBeforeMin = *req.RemindBeforeMin
	}
	if req.RepeatKind != nil && *req.RepeatKind != "" {
		n.RepeatKind = req.RepeatKind
		n.RepeatParam = req.RepeatParam
	}
	if req.TzOffsetMinutes != nil {
		n.TzOffsetMinutes = *req.TzOffsetMinutes
	}
	if req.AssigneeID != nil {
		if msg := h.checkAssignee(r.Context(), req.ProjectID, *req.AssigneeID); msg != "" {
			badRequest(w, msg)
			return
		}
		n.AssigneeID = req.AssigneeID
	}

	task, err := h.store.CreateTask(r.Context(), user.ID, n)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"task": task})
}

func (h *tasksHandlers) checkAssignee(ctx context.Context, projectID *int64, assignee int64) string {
	if projectID == nil {
		return "assignee is allowed only in a project"
	}
	ids, err := h.store.ProjectMemberIDs(ctx, *projectID)
	if err != nil {
		return "cannot verify assignee"
	}
	for _, id := range ids {
		if id == assignee {
			return ""
		}
	}
	return "assignee must be the project owner or a member"
}

func (h *tasksHandlers) get(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid task id")
		return
	}
	task, checklist, err := h.store.GetTask(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "task not found")
	case err != nil:
		internalError(w)
	default:
		if checklist == nil {
			checklist = []store.TaskChecklistItem{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"task": task, "checklist": checklist})
	}
}

func (h *tasksHandlers) update(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid task id")
		return
	}
	// сырое тело — чтобы отличать «поле не прислали» от «прислали null»
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil || len(raw) == 0 {
		badRequest(w, "invalid JSON body")
		return
	}
	body, _ := json.Marshal(raw)
	var req taskFields
	if err := json.Unmarshal(body, &req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if msg := req.validate(); msg != "" {
		badRequest(w, msg)
		return
	}

	current, _, err := h.store.GetTask(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}

	has := func(k string) bool { _, ok := raw[k]; return ok }
	patch := store.TaskPatch{
		Title:           nonEmptyTrim(req.Title),
		Note:            req.Note,
		Priority:        req.Priority,
		Remind:          req.Remind,
		RemindBeforeMin: req.RemindBeforeMin,
		Position:        req.Position,
		TzOffsetMinutes: req.TzOffsetMinutes,
	}

	targetProject := current.ProjectID
	if has("project_id") {
		targetProject = req.ProjectID
		if _, err := h.projectFor(r.Context(), user.ID, req.ProjectID); err != nil {
			writeError(w, http.StatusNotFound, "not_found", "project not found")
			return
		}
		patch.ProjectID = &req.ProjectID
	}
	if has("due_date") {
		d := req.DueDate
		if d != nil && *d == "" {
			d = nil
		}
		patch.DueDate = &d
		if d == nil {
			var noTime *string
			patch.DueTime = &noTime
		}
	}
	if has("due_time") && patch.DueTime == nil {
		t := req.DueTime
		if t != nil && *t == "" {
			t = nil
		}
		patch.DueTime = &t
	}
	if has("repeat_kind") {
		k := req.RepeatKind
		if k != nil && *k == "" {
			k = nil
		}
		patch.RepeatKind = &k
		p := req.RepeatParam
		if k == nil {
			p = nil
		}
		patch.RepeatParam = &p
	}
	if has("assignee_id") {
		a := req.AssigneeID
		if a != nil {
			if msg := h.checkAssignee(r.Context(), targetProject, *a); msg != "" {
				badRequest(w, msg)
				return
			}
		}
		patch.AssigneeID = &a
	}
	if has("status") && req.Status != nil {
		name := strings.TrimSpace(*req.Status)
		project, err := h.projectFor(r.Context(), user.ID, targetProject)
		if err != nil {
			internalError(w)
			return
		}
		kind := store.StatusKindOf(project.StatusList(), name)
		if kind == "" {
			badRequest(w, "unknown status for this project")
			return
		}
		patch.Status = &name
		patch.StatusKind = &kind
	}

	task, err := h.store.UpdateTask(r.Context(), user.ID, id, patch)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "task not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"task": task})
	}
}

func nonEmptyTrim(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	return &t
}

func (h *tasksHandlers) delete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid task id")
		return
	}
	switch err := h.store.DeleteTask(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "task not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// ---------- выполнение + повтор ----------

func (h *tasksHandlers) complete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid task id")
		return
	}
	task, _, err := h.store.GetTask(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	project, err := h.projectFor(r.Context(), user.ID, task.ProjectID)
	if err != nil {
		internalError(w)
		return
	}
	done := store.FirstStatusOfKind(project.StatusList(), "done")
	updated, err := h.store.UpdateTask(r.Context(), user.ID, id, store.TaskPatch{
		Status: &done.Name, StatusKind: &done.Kind,
	})
	if err != nil {
		internalError(w)
		return
	}

	resp := map[string]any{"task": updated}
	// повторяющаяся задача: создать следующий экземпляр
	if task.RepeatKind != nil {
		open := store.FirstStatusOfKind(project.StatusList(), "open")
		nextDue := nextTaskDue(task, time.Now().UTC())
		next, err := h.store.CreateTask(r.Context(), user.ID, store.NewTask{
			ProjectID:       task.ProjectID,
			Title:           task.Title,
			Note:            task.Note,
			Status:          open.Name,
			StatusKind:      open.Kind,
			Priority:        task.Priority,
			DueDate:         &nextDue,
			DueTime:         task.DueTime,
			Remind:          task.Remind,
			RemindBeforeMin: task.RemindBeforeMin,
			RepeatKind:      task.RepeatKind,
			RepeatParam:     task.RepeatParam,
			AssigneeID:      task.AssigneeID,
			TzOffsetMinutes: task.TzOffsetMinutes,
		})
		if err == nil {
			resp["next"] = next
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// nextTaskDue — следующая дата повторяющейся задачи: шагаем от срока
// (или сегодня), пока не окажемся строго в будущем (в поясе пользователя).
func nextTaskDue(t store.Task, now time.Time) string {
	loc := time.FixedZone("user", int(t.TzOffsetMinutes)*60)
	today := now.In(loc).Format("2006-01-02")
	base := today
	if t.DueDate != nil {
		base = *t.DueDate
	}
	d, err := time.ParseInLocation("2006-01-02", base, loc)
	if err != nil {
		d, _ = time.ParseInLocation("2006-01-02", today, loc)
	}
	step := func(d time.Time) time.Time {
		switch *t.RepeatKind {
		case "daily":
			return d.AddDate(0, 0, 1)
		case "weekly":
			return d.AddDate(0, 0, 7)
		case "monthly":
			return addMonthClamped(d)
		default: // interval
			days := 1
			if t.RepeatParam != nil && *t.RepeatParam > 0 {
				days = int(*t.RepeatParam)
			}
			return d.AddDate(0, 0, days)
		}
	}
	for i := 0; i < 1000; i++ {
		d = step(d)
		if d.Format("2006-01-02") > today {
			break
		}
	}
	return d.Format("2006-01-02")
}

// addMonthClamped: 31 января → 28/29 февраля, а не 2/3 марта.
func addMonthClamped(d time.Time) time.Time {
	y, m, day := d.Date()
	first := time.Date(y, m, 1, 0, 0, 0, 0, d.Location()).AddDate(0, 1, 0)
	last := first.AddDate(0, 1, -1).Day()
	if day > last {
		day = last
	}
	return time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, d.Location())
}

// ---------- чек-лист ----------

func (h *tasksHandlers) addChecklistItem(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid task id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validTaskTitle(strings.TrimSpace(req.Name)) {
		badRequest(w, "name must be 1-300 characters")
		return
	}
	item, err := h.store.AddTaskChecklistItem(r.Context(), user.ID, id, strings.TrimSpace(req.Name))
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "task not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"item": item})
	}
}

func (h *tasksHandlers) updateChecklistItem(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid item id")
		return
	}
	var req struct {
		Name *string `json:"name"`
		Done *bool   `json:"done"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Name == nil && req.Done == nil) {
		badRequest(w, "nothing to update")
		return
	}
	if req.Name != nil && !validTaskTitle(strings.TrimSpace(*req.Name)) {
		badRequest(w, "name must be 1-300 characters")
		return
	}
	item, err := h.store.UpdateTaskChecklistItem(r.Context(), user.ID, id, nonEmptyTrim(req.Name), req.Done)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "item not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"item": item})
	}
}

func (h *tasksHandlers) deleteChecklistItem(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid item id")
		return
	}
	switch err := h.store.DeleteTaskChecklistItem(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "item not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// ---------- проекты ----------

func (h *tasksHandlers) createProject(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Name  string `json:"name"`
		Color string `json:"color"`
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
	if req.Color == "" {
		req.Color = "#607d8b"
	}
	if !colorRe.MatchString(req.Color) {
		badRequest(w, "color must be #rrggbb")
		return
	}
	p, err := h.store.CreateTaskProject(r.Context(), user.ID, req.Name, req.Color)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"project": p})
}

func (h *tasksHandlers) updateProject(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid project id")
		return
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil || len(raw) == 0 {
		badRequest(w, "invalid JSON body")
		return
	}
	var req struct {
		Name     *string               `json:"name"`
		Color    *string               `json:"color"`
		Position *int32                `json:"position"`
		Statuses []store.ProjectStatus `json:"statuses"`
		Defaults *store.TaskDefaults   `json:"defaults"`
	}
	body, _ := json.Marshal(raw)
	if err := json.Unmarshal(body, &req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Name != nil {
		t := strings.TrimSpace(*req.Name)
		if n := utf8.RuneCountInString(t); n < 1 || n > 100 {
			badRequest(w, "name must be 1-100 characters")
			return
		}
		req.Name = &t
	}
	if req.Color != nil && !colorRe.MatchString(*req.Color) {
		badRequest(w, "color must be #rrggbb")
		return
	}

	patch := store.TaskProjectPatch{Name: req.Name, Color: req.Color, Position: req.Position}
	if _, ok := raw["statuses"]; ok {
		if req.Statuses == nil {
			patch.Statuses = []byte("null") // сброс к стандартным
		} else {
			if msg := validStatuses(req.Statuses); msg != "" {
				badRequest(w, msg)
				return
			}
			patch.Statuses, _ = json.Marshal(req.Statuses)
		}
	}
	if _, ok := raw["defaults"]; ok {
		if req.Defaults == nil {
			patch.Defaults = []byte("null")
		} else {
			if req.Defaults.Priority != nil && (*req.Defaults.Priority < 0 || *req.Defaults.Priority > 3) {
				badRequest(w, "defaults.priority must be 0-3")
				return
			}
			if req.Defaults.RepeatKind != nil && !validRepeatKind(*req.Defaults.RepeatKind) {
				badRequest(w, "defaults.repeat_kind is invalid")
				return
			}
			patch.Defaults, _ = json.Marshal(req.Defaults)
		}
	}

	p, err := h.store.UpdateTaskProject(r.Context(), user.ID, id, patch)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "project not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"project": p})
	}
}

func (h *tasksHandlers) deleteProject(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid project id")
		return
	}
	switch err := h.store.DeleteTaskProject(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "project not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// ---------- шаринг проектов ----------

func (h *tasksHandlers) shareProject(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid project id")
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
	queued, _, err := deliverShare(r.Context(), h.store, h.bot, user, recipient.ID, "task_project", id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shared_with": recipient, "queued": queued})
}

func (h *tasksHandlers) listProjectShares(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid project id")
		return
	}
	users, err := h.store.ListTaskProjectShares(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "project not found")
	case err != nil:
		internalError(w)
	default:
		if users == nil {
			users = []store.AccessUser{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": users})
	}
}

func (h *tasksHandlers) revokeProjectShare(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid project id")
		return
	}
	target, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
	if err != nil {
		badRequest(w, "invalid user id")
		return
	}
	switch err := h.store.RevokeTaskProjectShare(r.Context(), user.ID, id, target); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "share not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
