package store

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

func itoa(n int) string { return strconv.Itoa(n) }

// Projects: страница-сборник. Проект — набор блоков (текст, картинки, файлы,
// гео и ссылки на живые сущности других страниц). Ref-блоки резолвятся при
// каждой загрузке проекта — данные всегда свежие.

type ProjectCategory struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Position int32  `json:"position"`
}

type Project struct {
	ID          int64      `json:"id"`
	CategoryID  *int64     `json:"category_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Icon        string     `json:"icon"`
	Color       string     `json:"color"`
	Cover       string     `json:"cover"`
	Ptype       string     `json:"ptype"`
	Status      string     `json:"status"`
	Tags        []string   `json:"tags"`
	StartDate   *string    `json:"start_date"`
	DueDate     *string    `json:"due_date"`
	Tz          string     `json:"tz"`
	Position    int32      `json:"position"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	OwnerID     int64      `json:"owner_id"`
	OwnerName   string     `json:"owner_name"`
	Mine        bool       `json:"mine"`
	Shared      bool       `json:"shared"`
	Changed     bool       `json:"changed"` // изменён другим после моего последнего просмотра
}

type ProjectBlock struct {
	ID        int64           `json:"id"`
	UserID    int64           `json:"user_id"`
	Kind      string          `json:"kind"`
	Position  int32           `json:"position"`
	Collapsed bool            `json:"collapsed"`
	Bg        string          `json:"bg"`
	Content   json.RawMessage `json:"content"`
	Data      any             `json:"data,omitempty"` // резолв ref-блока (живые данные)
}

type ProjectHistoryEntry struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	UserName  string    `json:"user_name"`
	Action    string    `json:"action"`
	At        time.Time `json:"at"`
}

// --- категории ---

func (s *Store) ListProjectCategories(ctx context.Context, userID int64) ([]ProjectCategory, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, position FROM project_categories
		WHERE user_id = $1 ORDER BY position, id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ProjectCategory])
}

func (s *Store) CreateProjectCategory(ctx context.Context, userID int64, name string) (ProjectCategory, error) {
	var c ProjectCategory
	err := s.pool.QueryRow(ctx, `
		INSERT INTO project_categories (user_id, name, position)
		VALUES ($1, $2, COALESCE((SELECT max(position)+1 FROM project_categories WHERE user_id = $1), 0))
		RETURNING id, name, position`, userID, name).Scan(&c.ID, &c.Name, &c.Position)
	return c, err
}

func (s *Store) UpdateProjectCategory(ctx context.Context, userID, id int64, name string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE project_categories SET name = $3 WHERE id = $2 AND user_id = $1`, userID, id, name)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) DeleteProjectCategory(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM project_categories WHERE id = $2 AND user_id = $1`, userID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// --- проекты ---

const projPageCols = `p.id, p.category_id, p.name, p.description, p.icon, p.color, p.cover,
	p.ptype, p.status, p.tags, to_char(p.start_date, 'YYYY-MM-DD'), to_char(p.due_date, 'YYYY-MM-DD'),
	p.tz, p.position, p.created_at, p.updated_at, p.user_id,
	COALESCE(NULLIF(u.first_name, ''), '@' || u.username, '#' || p.user_id::text)`

func scanProjectRow(row pgx.Row, userID int64) (Project, error) {
	var p Project
	var shared bool
	var changed bool
	err := row.Scan(&p.ID, &p.CategoryID, &p.Name, &p.Description, &p.Icon, &p.Color, &p.Cover,
		&p.Ptype, &p.Status, &p.Tags, &p.StartDate, &p.DueDate,
		&p.Tz, &p.Position, &p.CreatedAt, &p.UpdatedAt, &p.OwnerID, &p.OwnerName,
		&shared, &changed)
	p.Mine = p.OwnerID == userID
	p.Shared = shared
	p.Changed = changed
	if !p.Mine {
		p.CategoryID = nil // категории — личные; чужой проект идёт в общий список
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	return p, err
}

const projectSelect = `
	SELECT ` + projPageCols + `,
		EXISTS (SELECT 1 FROM project_shares ps2 WHERE ps2.project_id = p.id),
		(p.updated_by <> 0 AND p.updated_by <> $1 AND
		 p.updated_at > COALESCE((SELECT v.viewed_at FROM project_views v
			WHERE v.project_id = p.id AND v.user_id = $1), 'epoch'::timestamptz))
	FROM projects p JOIN users u ON u.id = p.user_id`

func (s *Store) ListProjects(ctx context.Context, userID int64) ([]Project, error) {
	rows, err := s.pool.Query(ctx, projectSelect+`
		WHERE p.user_id = $1 OR EXISTS (
			SELECT 1 FROM project_shares ps WHERE ps.project_id = p.id AND ps.user_id = $1)
		ORDER BY p.position, p.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		p, err := scanProjectRow(rows, userID)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProjectTypes — упомянутые пользователем типы проектов (для подсказок).
func (s *Store) ProjectTypes(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ptype FROM projects WHERE user_id = $1 AND ptype <> '' ORDER BY ptype`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// NewProject — параметры создания/обновления.
type NewProject struct {
	CategoryID  *int64
	Name        string
	Description string
	Icon        string
	Color       string
	Ptype       string
	Status      string
	Tags        []string
	StartDate   *string
	DueDate     *string
	Tz          string
}

func (s *Store) CreateProject(ctx context.Context, userID int64, n NewProject) (Project, error) {
	if n.Tags == nil {
		n.Tags = []string{}
	}
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO projects (user_id, category_id, name, description, icon, color, ptype,
			status, tags, start_date, due_date, tz, updated_by, position)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::date, $11::date, $12, $1,
			COALESCE((SELECT max(position)+1 FROM projects WHERE user_id = $1), 0))
		RETURNING id`,
		userID, n.CategoryID, n.Name, n.Description, n.Icon, n.Color, n.Ptype,
		n.Status, n.Tags, n.StartDate, n.DueDate, n.Tz).Scan(&id)
	if err != nil {
		return Project{}, err
	}
	_ = s.AddProjectHistory(ctx, id, userID, "создал проект")
	return s.getProjectMeta(ctx, userID, id)
}

func (s *Store) getProjectMeta(ctx context.Context, userID, id int64) (Project, error) {
	p, err := scanProjectRow(s.pool.QueryRow(ctx, projectSelect+` WHERE p.id = $2`, userID, id), userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return p, err
}

// ProjectPatch — nil-поля не меняются.
type ProjectPatch struct {
	CategoryID  *int64 // меняется только при SetCategory
	SetCategory bool
	Name        *string
	Description *string
	Icon        *string
	Color       *string
	Cover       *string
	Ptype       *string
	Status      *string
	Tags        []string
	StartDate   *string // "" → NULL
	SetStart    bool
	DueDate     *string
	SetDue      bool
	Tz          *string
	Position    *int32
}

// UpdateProject — только владелец. Возвращает прежнюю обложку (если заменена).
func (s *Store) UpdateProject(ctx context.Context, userID, id int64, patch ProjectPatch) (Project, string, error) {
	oldCover := ""
	set := "updated_at = now(), updated_by = $1"
	args := []any{userID, id}
	add := func(expr string, v any) {
		args = append(args, v)
		set += ", " + expr + " = $" + itoa(len(args))
	}
	if patch.SetCategory {
		add("category_id", patch.CategoryID)
	}
	if patch.Name != nil {
		add("name", *patch.Name)
	}
	if patch.Description != nil {
		add("description", *patch.Description)
	}
	if patch.Icon != nil {
		add("icon", *patch.Icon)
	}
	if patch.Color != nil {
		add("color", *patch.Color)
	}
	if patch.Cover != nil {
		if err := s.pool.QueryRow(ctx, `SELECT cover FROM projects WHERE id = $1 AND user_id = $2`,
			id, userID).Scan(&oldCover); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Project{}, "", ErrNotFound
			}
			return Project{}, "", err
		}
		add("cover", *patch.Cover)
	}
	if patch.Ptype != nil {
		add("ptype", *patch.Ptype)
	}
	if patch.Status != nil {
		add("status", *patch.Status)
	}
	if patch.Tags != nil {
		add("tags", patch.Tags)
	}
	if patch.SetStart {
		add("start_date", patch.StartDate)
	}
	if patch.SetDue {
		add("due_date", patch.DueDate)
	}
	if patch.Tz != nil {
		add("tz", *patch.Tz)
	}
	if patch.Position != nil {
		add("position", *patch.Position)
	}
	tag, err := s.pool.Exec(ctx, `UPDATE projects SET `+set+` WHERE id = $2 AND user_id = $1`, args...)
	if err != nil {
		return Project{}, "", err
	}
	if tag.RowsAffected() == 0 {
		return Project{}, "", ErrNotFound
	}
	if oldCover != "" && patch.Cover != nil && oldCover == *patch.Cover {
		oldCover = ""
	}
	p, err := s.getProjectMeta(ctx, userID, id)
	return p, oldCover, err
}

// DeleteProject — только владелец; возвращает пути файлов блоков и обложки.
func (s *Store) DeleteProject(ctx context.Context, userID, id int64) ([]string, error) {
	files, err := s.projectFiles(ctx, id)
	if err != nil {
		return nil, err
	}
	var cover string
	err = s.pool.QueryRow(ctx, `
		DELETE FROM projects WHERE id = $1 AND user_id = $2 RETURNING cover`, id, userID).Scan(&cover)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if cover != "" {
		files = append(files, cover)
	}
	return files, nil
}

// projectFiles — все файлы images/file-блоков проекта.
func (s *Store) projectFiles(ctx context.Context, projectID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT kind, content FROM project_blocks
		WHERE project_id = $1 AND kind IN ('images', 'file')`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var kind string
		var raw []byte
		if err := rows.Scan(&kind, &raw); err != nil {
			return nil, err
		}
		out = append(out, blockFiles(kind, raw)...)
	}
	return out, rows.Err()
}

func blockFiles(kind string, raw []byte) []string {
	switch kind {
	case "images":
		var c struct {
			Images []string `json:"images"`
		}
		_ = json.Unmarshal(raw, &c)
		return c.Images
	case "file":
		var c struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal(raw, &c)
		if c.URL != "" {
			return []string{c.URL}
		}
	}
	return nil
}

// HasProjectAccess — доступ к проекту (для HTTP-слоя).
func (s *Store) HasProjectAccess(ctx context.Context, userID, projectID int64) (bool, error) {
	return s.hasProjectAccess(ctx, userID, projectID)
}

// hasProjectAccess — владелец или участник.
func (s *Store) hasProjectAccess(ctx context.Context, userID, projectID int64) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM projects p WHERE p.id = $2 AND
			(p.user_id = $1 OR EXISTS (
				SELECT 1 FROM project_shares ps WHERE ps.project_id = p.id AND ps.user_id = $1)))`,
		userID, projectID).Scan(&ok)
	return ok, err
}

// GetProject — метаданные + блоки с резолвом ref-данных; отмечает просмотр.
func (s *Store) GetProject(ctx context.Context, userID, id int64) (Project, []ProjectBlock, error) {
	ok, err := s.hasProjectAccess(ctx, userID, id)
	if err != nil {
		return Project{}, nil, err
	}
	if !ok {
		return Project{}, nil, ErrNotFound
	}
	p, err := s.getProjectMeta(ctx, userID, id)
	if err != nil {
		return Project{}, nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, kind, position, collapsed, bg, content
		FROM project_blocks WHERE project_id = $1 ORDER BY position, id`, id)
	if err != nil {
		return Project{}, nil, err
	}
	blocks, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ProjectBlock, error) {
		var b ProjectBlock
		err := row.Scan(&b.ID, &b.UserID, &b.Kind, &b.Position, &b.Collapsed, &b.Bg, &b.Content)
		return b, err
	})
	if err != nil {
		return Project{}, nil, err
	}
	for i := range blocks {
		blocks[i].Data = s.resolveBlock(ctx, &blocks[i])
	}
	// отметка просмотра — звёздочка гаснет
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO project_views (project_id, user_id) VALUES ($1, $2)
		ON CONFLICT (project_id, user_id) DO UPDATE SET viewed_at = now()`, id, userID)
	return p, blocks, nil
}

// resolveBlock — живые данные ref-блока (nil для text/images/file/location).
// Сущность ищется у пользователя, добавившего блок; удалена → {"missing":true}.
func (s *Store) resolveBlock(ctx context.Context, b *ProjectBlock) any {
	var ref struct {
		RefID int64 `json:"ref_id"`
	}
	switch b.Kind {
	case "checker_group", "article", "task", "task_category":
		if err := json.Unmarshal(b.Content, &ref); err != nil || ref.RefID == 0 {
			return map[string]any{"missing": true}
		}
	default:
		return nil
	}
	missing := map[string]any{"missing": true}
	switch b.Kind {
	case "checker_group":
		data, err := s.resolveCheckerGroup(ctx, b.UserID, ref.RefID)
		if err != nil {
			return missing
		}
		return data
	case "article":
		var a struct {
			ID        int64     `json:"id"`
			Title     string    `json:"title"`
			Content   string    `json:"content"`
			UpdatedAt time.Time `json:"updated_at"`
		}
		err := s.pool.QueryRow(ctx, `
			SELECT id, title, content, updated_at FROM articles
			WHERE id = $1 AND user_id = $2`, ref.RefID, b.UserID).
			Scan(&a.ID, &a.Title, &a.Content, &a.UpdatedAt)
		if err != nil {
			return missing
		}
		return a
	case "task":
		t, err := s.resolveTask(ctx, b.UserID, ref.RefID)
		if err != nil {
			return missing
		}
		return t
	case "task_category":
		var out struct {
			ID    int64          `json:"id"`
			Name  string         `json:"name"`
			Color string         `json:"color"`
			Tasks []resolvedTask `json:"tasks"`
		}
		err := s.pool.QueryRow(ctx, `
			SELECT p.id, p.name, p.color FROM task_projects p
			WHERE p.id = $1 AND `+taskProjectAccessFor("$2"),
			ref.RefID, b.UserID).Scan(&out.ID, &out.Name, &out.Color)
		if err != nil {
			return missing
		}
		rows, err := s.pool.Query(ctx, taskResolveSelect+`
			WHERE t.project_id = $1 AND t.status_kind = 'open'
			ORDER BY t.priority DESC, t.position, t.id LIMIT 100`, out.ID)
		if err != nil {
			return missing
		}
		out.Tasks, err = pgx.CollectRows(rows, pgx.RowToStructByPos[resolvedTask])
		if err != nil {
			return missing
		}
		if out.Tasks == nil {
			out.Tasks = []resolvedTask{}
		}
		return out
	}
	return nil
}

// ResolveProjectRef проверяет, что сущность kind/refID доступна пользователю
// (перед созданием ref-блока), и возвращает её живые данные.
func (s *Store) ResolveProjectRef(ctx context.Context, userID int64, kind string, refID int64) (any, bool) {
	raw, _ := json.Marshal(map[string]int64{"ref_id": refID})
	b := ProjectBlock{UserID: userID, Kind: kind, Content: raw}
	data := s.resolveBlock(ctx, &b)
	if m, isMap := data.(map[string]any); isMap && m["missing"] == true {
		return nil, false
	}
	return data, data != nil
}

// taskProjectAccessFor — владелец или участник категории задач ($N — user id).
func taskProjectAccessFor(param string) string {
	return `(p.user_id = ` + param + ` OR EXISTS (
		SELECT 1 FROM task_project_shares s WHERE s.project_id = p.id AND s.user_id = ` + param + `))`
}

type resolvedTask struct {
	ID             int64   `json:"id"`
	Title          string  `json:"title"`
	Status         string  `json:"status"`
	StatusKind     string  `json:"status_kind"`
	Priority       int16   `json:"priority"`
	DueDate        *string `json:"due_date"`
	DueTime        *string `json:"due_time"`
	ChecklistDone  int64   `json:"checklist_done"`
	ChecklistTotal int64   `json:"checklist_total"`
}

const taskResolveSelect = `
	SELECT t.id, t.title, t.status, t.status_kind, t.priority,
		to_char(t.due_date, 'YYYY-MM-DD'), to_char(t.due_time, 'HH24:MI'),
		(SELECT count(*) FROM task_checklist c WHERE c.task_id = t.id AND c.done),
		(SELECT count(*) FROM task_checklist c WHERE c.task_id = t.id)
	FROM tasks t`

func (s *Store) resolveTask(ctx context.Context, ownerID, taskID int64) (resolvedTask, error) {
	row, err := s.pool.Query(ctx, taskResolveSelect+`
		WHERE t.id = $1 AND (t.user_id = $2 OR t.assignee_id = $2 OR EXISTS (
			SELECT 1 FROM task_projects p WHERE p.id = t.project_id AND `+taskProjectAccessFor("$2")+`))`,
		taskID, ownerID)
	if err != nil {
		return resolvedTask{}, err
	}
	return pgx.CollectOneRow(row, pgx.RowToStructByPos[resolvedTask])
}

type resolvedCheckItem struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Done bool   `json:"done"`
}

type resolvedCheckGroup struct {
	ID        int64                `json:"id"`
	Name      string               `json:"name"`
	Items     []resolvedCheckItem  `json:"items"`
	Subgroups []resolvedCheckGroup `json:"subgroups,omitempty"`
}

func (s *Store) resolveCheckerGroup(ctx context.Context, ownerID, groupID int64) (resolvedCheckGroup, error) {
	var g resolvedCheckGroup
	err := s.pool.QueryRow(ctx, `
		SELECT id, name FROM checker_groups WHERE id = $1 AND user_id = $2`,
		groupID, ownerID).Scan(&g.ID, &g.Name)
	if err != nil {
		return g, err
	}
	if err = s.fillCheckerSubtree(ctx, &g, 1); err != nil {
		return g, err
	}
	return g, nil
}

// fillCheckerSubtree загружает пункты и вложенные подгруппы. level — уровень
// узла (группа верхнего уровня = 1); глубже MaxCheckerDepth не раскрываем.
func (s *Store) fillCheckerSubtree(ctx context.Context, g *resolvedCheckGroup, level int) error {
	var err error
	if g.Items, err = s.checkerItemsOf(ctx, g.ID); err != nil {
		return err
	}
	if level >= MaxCheckerDepth {
		return nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, name FROM checker_groups WHERE parent_id = $1 ORDER BY position, id`, g.ID)
	if err != nil {
		return err
	}
	subs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (resolvedCheckGroup, error) {
		var sub resolvedCheckGroup
		err := row.Scan(&sub.ID, &sub.Name)
		return sub, err
	})
	if err != nil {
		return err
	}
	for i := range subs {
		if err = s.fillCheckerSubtree(ctx, &subs[i], level+1); err != nil {
			return err
		}
	}
	g.Subgroups = subs
	return nil
}

func (s *Store) checkerItemsOf(ctx context.Context, groupID int64) ([]resolvedCheckItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, done FROM checker_items WHERE group_id = $1 ORDER BY position, id`, groupID)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[resolvedCheckItem])
	if items == nil {
		items = []resolvedCheckItem{}
	}
	return items, err
}

// --- блоки ---

// CreateProjectBlock вставляет блок на позицию pos (nil — в конец), сдвигая
// последующие. Доступ — любой участник проекта.
func (s *Store) CreateProjectBlock(ctx context.Context, userID, projectID int64,
	kind string, content json.RawMessage, bg string, collapsed bool, pos *int32) (ProjectBlock, error) {
	ok, err := s.hasProjectAccess(ctx, userID, projectID)
	if err != nil {
		return ProjectBlock{}, err
	}
	if !ok {
		return ProjectBlock{}, ErrNotFound
	}
	// лимит блоков на проект — по типу владельца проекта
	var ownerID int64
	var blockCount int32
	if err := s.pool.QueryRow(ctx, `
		SELECT p.user_id, (SELECT count(*) FROM project_blocks b WHERE b.project_id = p.id)
		FROM projects p WHERE p.id = $1`, projectID).Scan(&ownerID, &blockCount); err != nil {
		return ProjectBlock{}, err
	}
	limits, err := s.LimitsForUser(ctx, ownerID)
	if err != nil {
		return ProjectBlock{}, err
	}
	if blockCount >= limits.MaxBlocks {
		return ProjectBlock{}, ErrLimit
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ProjectBlock{}, err
	}
	defer tx.Rollback(ctx)
	var position int32
	if pos != nil {
		position = *pos
		if _, err := tx.Exec(ctx, `
			UPDATE project_blocks SET position = position + 1
			WHERE project_id = $1 AND position >= $2`, projectID, position); err != nil {
			return ProjectBlock{}, err
		}
	} else {
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(max(position)+1, 0) FROM project_blocks WHERE project_id = $1`,
			projectID).Scan(&position); err != nil {
			return ProjectBlock{}, err
		}
	}
	b := ProjectBlock{UserID: userID, Kind: kind, Position: position, Collapsed: collapsed, Bg: bg, Content: content}
	if err := tx.QueryRow(ctx, `
		INSERT INTO project_blocks (project_id, user_id, kind, position, collapsed, bg, content)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		projectID, userID, kind, position, collapsed, bg, content).Scan(&b.ID); err != nil {
		return ProjectBlock{}, err
	}
	if err := touchProject(ctx, tx, projectID, userID); err != nil {
		return ProjectBlock{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectBlock{}, err
	}
	b.Data = s.resolveBlock(ctx, &b)
	return b, nil
}

type BlockPatch struct {
	Content   json.RawMessage
	Bg        *string
	Collapsed *bool
	Position  *int32
}

// UpdateProjectBlock — любой участник проекта. Возвращает блок и project_id.
func (s *Store) UpdateProjectBlock(ctx context.Context, userID, blockID int64, patch BlockPatch) (ProjectBlock, int64, error) {
	var projectID int64
	err := s.pool.QueryRow(ctx, `
		SELECT b.project_id FROM project_blocks b JOIN projects p ON p.id = b.project_id
		WHERE b.id = $2 AND `+accessExpr("$1"), userID, blockID).Scan(&projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectBlock{}, 0, ErrNotFound
	}
	if err != nil {
		return ProjectBlock{}, 0, err
	}
	set := "updated_at = now()"
	args := []any{blockID}
	add := func(expr string, v any) {
		args = append(args, v)
		set += ", " + expr + " = $" + itoa(len(args))
	}
	if patch.Content != nil {
		add("content", patch.Content)
	}
	if patch.Bg != nil {
		add("bg", *patch.Bg)
	}
	if patch.Collapsed != nil {
		add("collapsed", *patch.Collapsed)
	}
	if patch.Position != nil {
		add("position", *patch.Position)
	}
	var b ProjectBlock
	err = s.pool.QueryRow(ctx, `
		UPDATE project_blocks SET `+set+` WHERE id = $1
		RETURNING id, user_id, kind, position, collapsed, bg, content`, args...).
		Scan(&b.ID, &b.UserID, &b.Kind, &b.Position, &b.Collapsed, &b.Bg, &b.Content)
	if err != nil {
		return ProjectBlock{}, 0, err
	}
	// смена collapsed/позиции — не «изменение» для звёздочки других
	if patch.Content != nil || patch.Bg != nil {
		_, _ = s.pool.Exec(ctx, `
			UPDATE projects SET updated_at = now(), updated_by = $2 WHERE id = $1`, projectID, userID)
	}
	b.Data = s.resolveBlock(ctx, &b)
	return b, projectID, nil
}

// accessExpr — условие доступа для запросов с алиасом p (projects).
func accessExpr(param string) string {
	return `(p.user_id = ` + param + ` OR EXISTS (
		SELECT 1 FROM project_shares ps WHERE ps.project_id = p.id AND ps.user_id = ` + param + `))`
}

// DeleteProjectBlock — любой участник; возвращает project_id, kind и файлы блока.
func (s *Store) DeleteProjectBlock(ctx context.Context, userID, blockID int64) (int64, string, []string, error) {
	var projectID int64
	var kind string
	var raw []byte
	err := s.pool.QueryRow(ctx, `
		DELETE FROM project_blocks b USING projects p
		WHERE b.id = $2 AND p.id = b.project_id AND `+accessExpr("$1")+`
		RETURNING b.project_id, b.kind, b.content`, userID, blockID).Scan(&projectID, &kind, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", nil, ErrNotFound
	}
	if err != nil {
		return 0, "", nil, err
	}
	_, _ = s.pool.Exec(ctx, `
		UPDATE projects SET updated_at = now(), updated_by = $2 WHERE id = $1`, projectID, userID)
	return projectID, kind, blockFiles(kind, raw), nil
}

func touchProject(ctx context.Context, tx pgx.Tx, projectID, userID int64) error {
	_, err := tx.Exec(ctx, `
		UPDATE projects SET updated_at = now(), updated_by = $2 WHERE id = $1`, projectID, userID)
	return err
}

// --- шаринг ---

// ShareProject — доступ участнику (вызывается из ApplyShare).
func (s *Store) ShareProject(ctx context.Context, ownerID, projectID, recipientID int64) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `
		SELECT name FROM projects WHERE id = $1 AND user_id = $2`, projectID, ownerID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if _, err = s.pool.Exec(ctx, `
		INSERT INTO project_shares (project_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		projectID, recipientID); err != nil {
		return "", err
	}
	return name, nil
}

func (s *Store) ListProjectShares(ctx context.Context, userID, projectID int64) ([]AccessUser, error) {
	ok, err := s.hasProjectAccess(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, COALESCE(u.username, ''), COALESCE(u.first_name, '')
		FROM project_shares ps JOIN users u ON u.id = ps.user_id
		WHERE ps.project_id = $1 ORDER BY ps.created_at`, projectID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[AccessUser])
}

// RevokeProjectShare — владелец отзывает, либо участник убирает себя (покинуть).
func (s *Store) RevokeProjectShare(ctx context.Context, userID, projectID, targetID int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM project_shares ps USING projects p
		WHERE ps.project_id = $1 AND ps.user_id = $3 AND p.id = ps.project_id
		  AND (p.user_id = $2 OR ps.user_id = $2)`, projectID, userID, targetID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// --- история ---

func (s *Store) AddProjectHistory(ctx context.Context, projectID, userID int64, action string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO project_history (project_id, user_id, action) VALUES ($1, $2, $3)`,
		projectID, userID, action)
	return err
}

func (s *Store) ListProjectHistory(ctx context.Context, userID, projectID int64) ([]ProjectHistoryEntry, error) {
	ok, err := s.hasProjectAccess(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		SELECT h.id, h.user_id,
			COALESCE(NULLIF(u.first_name, ''), '@' || u.username, '#' || h.user_id::text),
			h.action, h.at
		FROM project_history h LEFT JOIN users u ON u.id = h.user_id
		WHERE h.project_id = $1 ORDER BY h.at DESC, h.id DESC LIMIT 200`, projectID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ProjectHistoryEntry])
}
