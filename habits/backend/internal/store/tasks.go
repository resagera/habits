package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ProjectStatus — статус в переопределяемом списке статусов проекта.
// kind задаёт поведение: open — в работе, done — лог выполненных
// (перечёркивается), archived — архив.
type ProjectStatus struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// DefaultTaskStatuses — статусы «Входящих» и проектов без переопределения.
var DefaultTaskStatuses = []ProjectStatus{
	{Name: "Открыта", Kind: "open"},
	{Name: "Выполнена", Kind: "done"},
	{Name: "Архив", Kind: "archived"},
}

// TaskDefaults — параметры, проставляемые новым задачам проекта.
type TaskDefaults struct {
	Priority        *int16  `json:"priority,omitempty"`
	Remind          *bool   `json:"remind,omitempty"`
	RemindBeforeMin *int32  `json:"remind_before_min,omitempty"`
	RepeatKind      *string `json:"repeat_kind,omitempty"`
	RepeatParam     *int32  `json:"repeat_param,omitempty"`
}

type TaskProject struct {
	ID       int64           `json:"id"`
	Name     string          `json:"name"`
	Color    string          `json:"color"`
	Position int32           `json:"position"`
	Statuses []ProjectStatus `json:"statuses"` // nil → DefaultTaskStatuses
	Defaults *TaskDefaults   `json:"defaults"`
	OwnerID  int64           `json:"owner_id"`
	Mine     bool            `json:"mine"`
	Shared   bool            `json:"shared"`
}

type Task struct {
	ID              int64      `json:"id"`
	UserID          int64      `json:"user_id"`
	ProjectID       *int64     `json:"project_id"`
	Title           string     `json:"title"`
	Note            string     `json:"note"`
	Status          string     `json:"status"`
	StatusKind      string     `json:"status_kind"`
	Priority        int16      `json:"priority"`
	DueDate         *string    `json:"due_date"`
	DueTime         *string    `json:"due_time"`
	Remind          bool       `json:"remind"`
	RemindBeforeMin int32      `json:"remind_before_min"`
	RepeatKind      *string    `json:"repeat_kind"`
	RepeatParam     *int32     `json:"repeat_param"`
	AssigneeID      *int64     `json:"assignee_id"`
	TzOffsetMinutes int32      `json:"tz_offset_minutes"`
	Position        int32      `json:"position"`
	CompletedAt     *time.Time `json:"completed_at"`
	ChecklistTotal  int32      `json:"checklist_total"`
	ChecklistDone   int32      `json:"checklist_done"`
	Mine            bool       `json:"mine"`
}

type TaskChecklistItem struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Done     bool   `json:"done"`
	Position int32  `json:"position"`
}

// taskAccess — автор задачи, владелец или участник её проекта.
const taskAccess = `(t.user_id = $1 OR EXISTS (
	SELECT 1 FROM task_projects ap WHERE ap.id = t.project_id
	  AND (ap.user_id = $1 OR EXISTS (
	      SELECT 1 FROM task_project_shares aps WHERE aps.project_id = ap.id AND aps.user_id = $1))))`

// projectAccess — владелец или участник проекта p.
const projectAccess = `(p.user_id = $1 OR EXISTS (
	SELECT 1 FROM task_project_shares ps WHERE ps.project_id = p.id AND ps.user_id = $1))`

const taskCols = `t.id, t.user_id, t.project_id, t.title, t.note, t.status, t.status_kind,
	t.priority, to_char(t.due_date, 'YYYY-MM-DD'), to_char(t.due_time, 'HH24:MI'),
	t.remind, t.remind_before_min, t.repeat_kind, t.repeat_param, t.assignee_id,
	t.tz_offset_minutes, t.position, t.completed_at,
	(SELECT count(*) FROM task_checklist c WHERE c.task_id = t.id)::int,
	(SELECT count(*) FROM task_checklist c WHERE c.task_id = t.id AND c.done)::int,
	(t.user_id = $1)`

// ---------- проекты ----------

func scanProject(row pgx.Row, p *TaskProject) error {
	var statuses, defaults []byte
	if err := row.Scan(&p.ID, &p.Name, &p.Color, &p.Position, &statuses, &defaults,
		&p.OwnerID, &p.Mine, &p.Shared); err != nil {
		return err
	}
	if len(statuses) > 0 {
		_ = json.Unmarshal(statuses, &p.Statuses)
	}
	if len(defaults) > 0 {
		_ = json.Unmarshal(defaults, &p.Defaults)
	}
	return nil
}

const projectCols = `p.id, p.name, p.color, p.position, p.statuses, p.defaults, p.user_id,
	(p.user_id = $1),
	(EXISTS (SELECT 1 FROM task_project_shares s2 WHERE s2.project_id = p.id) OR p.user_id <> $1)`

func (s *Store) ListTaskProjects(ctx context.Context, userID int64) ([]TaskProject, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+projectCols+`
		FROM task_projects p
		WHERE `+projectAccess+`
		ORDER BY (p.user_id = $1) DESC, p.position, p.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []TaskProject
	for rows.Next() {
		var p TaskProject
		if err := scanProject(rows, &p); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *Store) CreateTaskProject(ctx context.Context, userID int64, name, color string) (TaskProject, error) {
	var p TaskProject
	err := scanProject(s.pool.QueryRow(ctx, `
		WITH ins AS (
			INSERT INTO task_projects (user_id, name, color, position)
			VALUES ($1, $2, $3,
			        (SELECT COALESCE(MAX(position) + 1, 0) FROM task_projects WHERE user_id = $1))
			RETURNING *
		)
		SELECT `+projectCols+` FROM ins p`, userID, name, color), &p)
	return p, err
}

type TaskProjectPatch struct {
	Name     *string
	Color    *string
	Position *int32
	Statuses []byte // валидный JSON-массив; "null" — сбросить к стандартным
	Defaults []byte
}

func (s *Store) UpdateTaskProject(ctx context.Context, userID, id int64, patch TaskProjectPatch) (TaskProject, error) {
	var p TaskProject
	err := scanProject(s.pool.QueryRow(ctx, `
		WITH upd AS (
			UPDATE task_projects
			SET name = COALESCE($3, name),
			    color = COALESCE($4, color),
			    position = COALESCE($5, position),
			    statuses = CASE WHEN $6::jsonb IS NULL THEN statuses
			                    WHEN $6::jsonb = 'null'::jsonb THEN NULL ELSE $6::jsonb END,
			    defaults = CASE WHEN $7::jsonb IS NULL THEN defaults
			                    WHEN $7::jsonb = 'null'::jsonb THEN NULL ELSE $7::jsonb END,
			    updated_at = now()
			WHERE id = $2 AND user_id = $1
			RETURNING *
		)
		SELECT `+projectCols+` FROM upd p`,
		userID, id, patch.Name, patch.Color, patch.Position, patch.Statuses, patch.Defaults), &p)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *Store) DeleteTaskProject(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM task_projects WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetTaskProject — проект по id с проверкой доступа (владелец или участник).
func (s *Store) GetTaskProject(ctx context.Context, userID, id int64) (TaskProject, error) {
	var p TaskProject
	err := scanProject(s.pool.QueryRow(ctx, `
		SELECT `+projectCols+`
		FROM task_projects p WHERE p.id = $2 AND `+projectAccess, userID, id), &p)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// StatusList — действующие статусы проекта (или стандартные).
func (p *TaskProject) StatusList() []ProjectStatus {
	if p == nil || len(p.Statuses) == 0 {
		return DefaultTaskStatuses
	}
	return p.Statuses
}

// FirstStatusOfKind возвращает первый статус заданного kind (fallback — стандартные).
func FirstStatusOfKind(list []ProjectStatus, kind string) ProjectStatus {
	for _, st := range list {
		if st.Kind == kind {
			return st
		}
	}
	for _, st := range DefaultTaskStatuses {
		if st.Kind == kind {
			return st
		}
	}
	return ProjectStatus{Name: "Открыта", Kind: "open"}
}

// StatusKindOf ищет kind статуса по имени в списке; "" если не найден.
func StatusKindOf(list []ProjectStatus, name string) string {
	for _, st := range list {
		if st.Name == name {
			return st.Kind
		}
	}
	return ""
}

// ---------- шаринг проектов ----------

func (s *Store) ShareTaskProject(ctx context.Context, ownerID, projectID, recipientID int64) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `
		SELECT name FROM task_projects WHERE id = $1 AND user_id = $2`,
		projectID, ownerID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO task_project_shares (project_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		projectID, recipientID)
	return name, err
}

func (s *Store) ListTaskProjectShares(ctx context.Context, userID, projectID int64) ([]AccessUser, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM task_projects p WHERE p.id = $2 AND `+projectAccess+`)`,
		userID, projectID).Scan(&ok)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, COALESCE(u.username, ''), COALESCE(u.first_name, '')
		FROM task_project_shares s JOIN users u ON u.id = s.user_id
		WHERE s.project_id = $1 ORDER BY s.created_at`, projectID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[AccessUser])
}

func (s *Store) RevokeTaskProjectShare(ctx context.Context, requesterID, projectID, targetID int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM task_project_shares s
		USING task_projects p
		WHERE s.project_id = $1 AND s.user_id = $2 AND p.id = s.project_id
		  AND (p.user_id = $3 OR $2 = $3)`,
		projectID, targetID, requesterID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ProjectMemberIDs — владелец + участники (для валидации исполнителя).
func (s *Store) ProjectMemberIDs(ctx context.Context, projectID int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_id FROM task_projects WHERE id = $1
		UNION
		SELECT user_id FROM task_project_shares WHERE project_id = $1`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ---------- задачи ----------

func collectTasks(rows pgx.Rows) ([]Task, error) {
	defer rows.Close()
	var result []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.UserID, &t.ProjectID, &t.Title, &t.Note, &t.Status,
			&t.StatusKind, &t.Priority, &t.DueDate, &t.DueTime, &t.Remind, &t.RemindBeforeMin,
			&t.RepeatKind, &t.RepeatParam, &t.AssigneeID, &t.TzOffsetMinutes, &t.Position,
			&t.CompletedAt, &t.ChecklistTotal, &t.ChecklistDone, &t.Mine); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

// ListOpenTasks — все доступные пользователю открытые задачи.
func (s *Store) ListOpenTasks(ctx context.Context, userID int64) ([]Task, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+taskCols+`
		FROM tasks t
		WHERE t.status_kind = 'open' AND `+taskAccess+`
		ORDER BY t.project_id NULLS FIRST, t.position, t.id`, userID)
	if err != nil {
		return nil, err
	}
	return collectTasks(rows)
}

// ListDoneTasks — лог выполненных и архива, свежие сверху.
func (s *Store) ListDoneTasks(ctx context.Context, userID int64, limit int) ([]Task, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+taskCols+`
		FROM tasks t
		WHERE t.status_kind IN ('done', 'archived') AND `+taskAccess+`
		ORDER BY t.completed_at DESC NULLS LAST, t.updated_at DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	return collectTasks(rows)
}

func (s *Store) GetTask(ctx context.Context, userID, id int64) (Task, []TaskChecklistItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+taskCols+`
		FROM tasks t WHERE t.id = $2 AND `+taskAccess, userID, id)
	if err != nil {
		return Task{}, nil, err
	}
	tasks, err := collectTasks(rows)
	if err != nil {
		return Task{}, nil, err
	}
	if len(tasks) == 0 {
		return Task{}, nil, ErrNotFound
	}
	items, err := s.listChecklist(ctx, id)
	return tasks[0], items, err
}

func (s *Store) listChecklist(ctx context.Context, taskID int64) ([]TaskChecklistItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, done, position FROM task_checklist
		WHERE task_id = $1 ORDER BY position, id`, taskID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[TaskChecklistItem])
}

// NewTask — параметры создания (после применения дефолтов проекта в handler).
type NewTask struct {
	ProjectID       *int64
	Title           string
	Note            string
	Status          string
	StatusKind      string
	Priority        int16
	DueDate         *string
	DueTime         *string
	Remind          bool
	RemindBeforeMin int32
	RepeatKind      *string
	RepeatParam     *int32
	AssigneeID      *int64
	TzOffsetMinutes int32
}

func (s *Store) CreateTask(ctx context.Context, userID int64, n NewTask) (Task, error) {
	rows, err := s.pool.Query(ctx, `
		WITH ins AS (
			INSERT INTO tasks (user_id, project_id, title, note, status, status_kind, priority,
			                   due_date, due_time, remind, remind_before_min, repeat_kind,
			                   repeat_param, assignee_id, tz_offset_minutes, position)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9::time, $10, $11, $12, $13, $14, $15,
			        (SELECT COALESCE(MAX(position) + 1, 0) FROM tasks
			         WHERE ($2::bigint IS NOT NULL AND project_id = $2)
			            OR ($2::bigint IS NULL AND project_id IS NULL AND user_id = $1)))
			RETURNING *
		)
		SELECT `+taskCols+` FROM ins t`,
		userID, n.ProjectID, n.Title, n.Note, n.Status, n.StatusKind, n.Priority,
		n.DueDate, n.DueTime, n.Remind, n.RemindBeforeMin, n.RepeatKind, n.RepeatParam,
		n.AssigneeID, n.TzOffsetMinutes)
	if err != nil {
		return Task{}, err
	}
	tasks, err := collectTasks(rows)
	if err != nil {
		return Task{}, err
	}
	return tasks[0], nil
}

// TaskPatch: обычный указатель — «не менять», двойной — «установить/сбросить».
type TaskPatch struct {
	Title           *string
	Note            *string
	Status          *string // вместе с StatusKind
	StatusKind      *string
	Priority        *int16
	ProjectID       **int64
	DueDate         **string
	DueTime         **string
	Remind          *bool
	RemindBeforeMin *int32
	RepeatKind      **string
	RepeatParam     **int32
	AssigneeID      **int64
	Position        *int32
	TzOffsetMinutes *int32
}

func (s *Store) UpdateTask(ctx context.Context, userID, id int64, p TaskPatch) (Task, error) {
	set := []string{"updated_at = now()"}
	args := []any{userID, id}
	add := func(expr string, v any) {
		args = append(args, v)
		set = append(set, fmt.Sprintf(expr, len(args)))
	}
	if p.Title != nil {
		add("title = $%d", *p.Title)
	}
	if p.Note != nil {
		add("note = $%d", *p.Note)
	}
	if p.Status != nil && p.StatusKind != nil {
		add("status = $%d", *p.Status)
		add("status_kind = $%d", *p.StatusKind)
		switch *p.StatusKind {
		case "done":
			set = append(set, "completed_at = COALESCE(completed_at, now())")
		case "open":
			set = append(set, "completed_at = NULL")
		}
	}
	if p.Priority != nil {
		add("priority = $%d", *p.Priority)
	}
	if p.ProjectID != nil {
		add("project_id = $%d", *p.ProjectID)
	}
	if p.DueDate != nil {
		add("due_date = $%d::date", *p.DueDate)
		// срок изменился — уведомления отправляются заново
		set = append(set, "reminded_at = NULL", "overdue_notified_at = NULL")
	}
	if p.DueTime != nil {
		add("due_time = $%d::time", *p.DueTime)
	}
	if p.Remind != nil {
		add("remind = $%d", *p.Remind)
	}
	if p.RemindBeforeMin != nil {
		add("remind_before_min = $%d", *p.RemindBeforeMin)
	}
	if p.RepeatKind != nil {
		add("repeat_kind = $%d", *p.RepeatKind)
	}
	if p.RepeatParam != nil {
		add("repeat_param = $%d", *p.RepeatParam)
	}
	if p.AssigneeID != nil {
		add("assignee_id = $%d", *p.AssigneeID)
	}
	if p.Position != nil {
		add("position = $%d", *p.Position)
	}
	if p.TzOffsetMinutes != nil {
		add("tz_offset_minutes = $%d", *p.TzOffsetMinutes)
	}

	rows, err := s.pool.Query(ctx, `
		WITH upd AS (
			UPDATE tasks SET `+strings.Join(set, ", ")+`
			WHERE id = $2 AND (user_id = $1 OR EXISTS (
				SELECT 1 FROM task_projects ap WHERE ap.id = tasks.project_id
				  AND (ap.user_id = $1 OR EXISTS (
				      SELECT 1 FROM task_project_shares aps
				      WHERE aps.project_id = ap.id AND aps.user_id = $1))))
			RETURNING *
		)
		SELECT `+taskCols+` FROM upd t`, args...)
	if err != nil {
		return Task{}, err
	}
	tasks, err := collectTasks(rows)
	if err != nil {
		return Task{}, err
	}
	if len(tasks) == 0 {
		return Task{}, ErrNotFound
	}
	return tasks[0], nil
}

func (s *Store) DeleteTask(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM tasks t WHERE t.id = $2 AND `+taskAccess, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- чек-лист ----------

func (s *Store) taskAccessible(ctx context.Context, userID, taskID int64) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM tasks t WHERE t.id = $2 AND `+taskAccess+`)`,
		userID, taskID).Scan(&ok)
	return ok, err
}

func (s *Store) AddTaskChecklistItem(ctx context.Context, userID, taskID int64, name string) (TaskChecklistItem, error) {
	ok, err := s.taskAccessible(ctx, userID, taskID)
	if err != nil {
		return TaskChecklistItem{}, err
	}
	if !ok {
		return TaskChecklistItem{}, ErrNotFound
	}
	var item TaskChecklistItem
	err = s.pool.QueryRow(ctx, `
		INSERT INTO task_checklist (task_id, name, position)
		VALUES ($1, $2, (SELECT COALESCE(MAX(position) + 1, 0) FROM task_checklist WHERE task_id = $1))
		RETURNING id, name, done, position`, taskID, name).Scan(&item.ID, &item.Name, &item.Done, &item.Position)
	return item, err
}

func (s *Store) UpdateTaskChecklistItem(ctx context.Context, userID, itemID int64, name *string, done *bool) (TaskChecklistItem, error) {
	var item TaskChecklistItem
	err := s.pool.QueryRow(ctx, `
		UPDATE task_checklist c
		SET name = COALESCE($3, c.name), done = COALESCE($4, c.done)
		FROM tasks t
		WHERE c.id = $2 AND t.id = c.task_id AND `+taskAccess+`
		RETURNING c.id, c.name, c.done, c.position`,
		userID, itemID, name, done).Scan(&item.ID, &item.Name, &item.Done, &item.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, ErrNotFound
	}
	return item, err
}

func (s *Store) DeleteTaskChecklistItem(ctx context.Context, userID, itemID int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM task_checklist c
		USING tasks t
		WHERE c.id = $2 AND t.id = c.task_id AND `+taskAccess, userID, itemID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- уведомления о сроках ----------

// TaskDueNotice — кандидат на уведомление (remind включён, срок задан).
type TaskDueNotice struct {
	ID              int64
	UserID          int64
	AssigneeID      *int64
	Title           string
	ProjectName     string
	DueDate         string
	DueTime         *string
	RemindBeforeMin int32
	TzOffsetMinutes int32
	Reminded        bool
	OverdueNotified bool
}

func (s *Store) TaskDueNotices(ctx context.Context, limit int) ([]TaskDueNotice, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.user_id, t.assignee_id, t.title, COALESCE(p.name, ''),
		       to_char(t.due_date, 'YYYY-MM-DD'), to_char(t.due_time, 'HH24:MI'),
		       t.remind_before_min, t.tz_offset_minutes,
		       t.reminded_at IS NOT NULL, t.overdue_notified_at IS NOT NULL
		FROM tasks t
		LEFT JOIN task_projects p ON p.id = t.project_id
		WHERE t.status_kind = 'open' AND t.remind AND t.due_date IS NOT NULL
		  AND (t.reminded_at IS NULL OR t.overdue_notified_at IS NULL)
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[TaskDueNotice])
}

// DueMoment — момент срока: дата+время или 09:00 пользователя, UTC.
func (n *TaskDueNotice) DueMoment() time.Time {
	return taskMoment(n.DueDate, n.DueTime, n.TzOffsetMinutes, 9, 0)
}

// OverdueMoment — когда задача считается просроченной: момент времени
// или конец дня (00:00 следующего дня), UTC.
func (n *TaskDueNotice) OverdueMoment() time.Time {
	if n.DueTime != nil {
		return n.DueMoment()
	}
	return taskMoment(n.DueDate, nil, n.TzOffsetMinutes, 0, 0).AddDate(0, 0, 1)
}

func taskMoment(date string, tod *string, tzOffset int32, defHH, defMM int) time.Time {
	loc := time.FixedZone("user", int(tzOffset)*60)
	var y, m, d int
	fmt.Sscanf(date, "%d-%d-%d", &y, &m, &d)
	hh, mm := defHH, defMM
	if tod != nil {
		fmt.Sscanf(*tod, "%d:%d", &hh, &mm)
	}
	return time.Date(y, time.Month(m), d, hh, mm, 0, 0, loc).UTC()
}

func (s *Store) MarkTaskReminded(ctx context.Context, id int64, overdue bool) error {
	col := "reminded_at"
	if overdue {
		col = "overdue_notified_at"
	}
	_, err := s.pool.Exec(ctx, `UPDATE tasks SET `+col+` = now() WHERE id = $1`, id)
	return err
}

// ---------- сводка для главной ----------

// TasksSummary считает открытые задачи со сроком «сегодня» и просроченные
// в часовом поясе пользователя.
func (s *Store) TasksSummary(ctx context.Context, userID int64, tzOffset int32, now time.Time) (today, overdue int, err error) {
	rows, err := s.pool.Query(ctx, `
		SELECT to_char(t.due_date, 'YYYY-MM-DD'), to_char(t.due_time, 'HH24:MI')
		FROM tasks t
		WHERE t.status_kind = 'open' AND t.due_date IS NOT NULL AND `+taskAccess, userID)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	loc := time.FixedZone("user", int(tzOffset)*60)
	todayStr := now.In(loc).Format("2006-01-02")
	for rows.Next() {
		var date string
		var tod *string
		if err := rows.Scan(&date, &tod); err != nil {
			return 0, 0, err
		}
		n := TaskDueNotice{DueDate: date, DueTime: tod, TzOffsetMinutes: tzOffset}
		switch {
		case now.After(n.OverdueMoment()):
			overdue++
		case date == todayStr:
			today++
		}
	}
	return today, overdue, rows.Err()
}
