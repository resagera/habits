package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// MaxCheckerDepth — предельная глубина вложенности групп Checker (уровней,
// считая группу верхнего уровня за 1-й). Единый предел для ручного создания,
// импорта и раскрытия чек-листа в Projects.
const MaxCheckerDepth = 20

type CheckGroup struct {
	ID       int64  `json:"id"`
	ParentID *int64 `json:"parent_id"`
	Name     string `json:"name"`
	Position int32  `json:"position"`
	HideDone bool   `json:"hide_done"`
	// ProgressMode — у пунктов группы есть промежуточный статус «в работе»:
	// клик идёт пусто → в работе → сделано → пусто.
	ProgressMode bool   `json:"progress_mode"`
	Mine         bool   `json:"mine"`                 // группа принадлежит текущему пользователю
	Shared       bool   `json:"shared"`               // корень с участниками (у владельца) или полученный
	OwnerName    string `json:"owner_name,omitempty"` // имя владельца (для полученных)
	// повторяющийся список (только у групп верхнего уровня)
	ResetPeriod string      `json:"reset_period"` // none/daily/weekly/monthly
	ResetMinute int32       `json:"reset_minute"`
	ResetDow    int32       `json:"reset_dow"`
	ResetDom    int32       `json:"reset_dom"`
	ResetTzOff  int32       `json:"reset_tz_off"`
	RemindAt    *time.Time  `json:"remind_at"`
	Items       []CheckItem `json:"items"`
}

type CheckItem struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Done     bool   `json:"done"`
	Position int32  `json:"position"`
	Note     string `json:"note"`
	Label    string `json:"label"`
	// InProgress — пункт взят в работу (необязательная отметка «между»
	// несделанным и сделанным). С done = true несовместим.
	InProgress bool       `json:"in_progress"`
	RemindAt   *time.Time `json:"remind_at"`
}

type CheckItemPatch struct {
	Name       *string
	Done       *bool
	Position   *int32
	Note       *string
	Label      *string
	InProgress *bool
}

func (s *Store) ListCheckGroups(ctx context.Context, userID int64) ([]CheckGroup, error) {
	// корни: свои активные группы верхнего уровня + верхнеуровневые группы,
	// расшаренные мне; затем всё их активное поддерево
	rows, err := s.pool.Query(ctx, `
		WITH RECURSIVE roots AS (
			SELECT g.id FROM checker_groups g
			WHERE g.deleted_at IS NULL AND g.parent_id IS NULL
			  AND (g.user_id = $1 OR EXISTS (
			        SELECT 1 FROM checker_shares s WHERE s.group_id = g.id AND s.user_id = $1))
		),
		tree AS (
			SELECT g.id, g.parent_id, g.name, g.position, g.hide_done, g.progress_mode, g.user_id,
			       g.reset_period, g.reset_minute, g.reset_dow, g.reset_dom, g.reset_tz_off, g.remind_at
			FROM checker_groups g JOIN roots r ON g.id = r.id
			UNION ALL
			SELECT c.id, c.parent_id, c.name, c.position, c.hide_done, c.progress_mode, c.user_id,
			       c.reset_period, c.reset_minute, c.reset_dow, c.reset_dom, c.reset_tz_off, c.remind_at
			FROM checker_groups c JOIN tree t ON c.parent_id = t.id
			WHERE c.deleted_at IS NULL
		)
		SELECT t.id, t.parent_id, t.name, t.position, t.hide_done, t.progress_mode,
		       (t.user_id = $1) AS mine,
		       (t.parent_id IS NULL AND (t.user_id <> $1
		            OR EXISTS (SELECT 1 FROM checker_shares s WHERE s.group_id = t.id))) AS shared,
		       CASE WHEN t.user_id = $1 THEN ''
		            ELSE COALESCE(NULLIF(u.first_name, ''), '@' || u.username, '#' || t.user_id::text) END AS owner_name,
		       t.reset_period, t.reset_minute, t.reset_dow, t.reset_dom, t.reset_tz_off, t.remind_at
		FROM tree t JOIN users u ON u.id = t.user_id
		ORDER BY (t.user_id = $1) DESC, t.position, t.id`, userID)
	if err != nil {
		return nil, err
	}
	groups, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (CheckGroup, error) {
		var g CheckGroup
		err := row.Scan(&g.ID, &g.ParentID, &g.Name, &g.Position, &g.HideDone, &g.ProgressMode,
			&g.Mine, &g.Shared, &g.OwnerName,
			&g.ResetPeriod, &g.ResetMinute, &g.ResetDow, &g.ResetDom, &g.ResetTzOff, &g.RemindAt)
		g.Items = []CheckItem{}
		return g, err
	})
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return groups, nil
	}

	byID := make(map[int64]*CheckGroup, len(groups))
	ids := make([]int64, 0, len(groups))
	for i := range groups {
		byID[groups[i].ID] = &groups[i]
		ids = append(ids, groups[i].ID)
	}
	itemRows, err := s.pool.Query(ctx, `
		SELECT i.id, i.group_id, i.name, i.done, i.position, i.note, i.label, i.in_progress, i.remind_at
		FROM checker_items i
		WHERE i.group_id = ANY($1)
		ORDER BY i.position, i.id`, ids)
	if err != nil {
		return nil, err
	}
	defer itemRows.Close()
	for itemRows.Next() {
		var it CheckItem
		var groupID int64
		if err := itemRows.Scan(&it.ID, &groupID, &it.Name, &it.Done, &it.Position, &it.Note, &it.Label,
			&it.InProgress, &it.RemindAt); err != nil {
			return nil, err
		}
		if g, ok := byID[groupID]; ok {
			g.Items = append(g.Items, it)
		}
	}
	return groups, itemRows.Err()
}

// CreateCheckGroup создаёт группу или подгруппу (parentID != nil).
// Позиция считается среди соседей (в пределах одного родителя).
// Родитель должен принадлежать тому же пользователю (иначе ErrNotFound);
// глубина новой подгруппы не должна превышать MaxCheckerDepth (иначе ErrTooDeep).
func (s *Store) CreateCheckGroup(ctx context.Context, userID int64, name string, parentID *int64) (CheckGroup, error) {
	g := CheckGroup{Items: []CheckItem{}}
	if parentID != nil {
		// глубина родителя (группа верхнего уровня = 1) через подъём по parent_id
		var parentDepth *int
		if err := s.pool.QueryRow(ctx, `
			WITH RECURSIVE anc AS (
			    SELECT id, parent_id, 1 AS depth FROM checker_groups
			    WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
			    UNION ALL
			    SELECT g.id, g.parent_id, anc.depth + 1
			    FROM checker_groups g JOIN anc ON g.id = anc.parent_id
			)
			SELECT max(depth) FROM anc`,
			*parentID, userID).Scan(&parentDepth); err != nil {
			return g, err
		}
		if parentDepth == nil {
			return g, ErrNotFound
		}
		if *parentDepth >= MaxCheckerDepth {
			return g, ErrTooDeep
		}
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO checker_groups (user_id, parent_id, name, position)
		VALUES ($1, $3, $2,
		        (SELECT COALESCE(MAX(position) + 1, 0) FROM checker_groups
		         WHERE user_id = $1 AND parent_id IS NOT DISTINCT FROM $3))
		RETURNING id, parent_id, name, position, hide_done, progress_mode`,
		userID, name, parentID).
		Scan(&g.ID, &g.ParentID, &g.Name, &g.Position, &g.HideDone, &g.ProgressMode)
	return g, err
}

// UpdateCheckGroup меняет имя, «скрывать выполненное» и/или промежуточный
// статус (nil — не трогать).
func (s *Store) UpdateCheckGroup(ctx context.Context, userID, id int64, name *string, hideDone, progressMode *bool) (CheckGroup, error) {
	g := CheckGroup{Items: []CheckItem{}}
	err := s.pool.QueryRow(ctx, `
		UPDATE checker_groups
		SET name = COALESCE($3, name),
		    hide_done = COALESCE($4, hide_done),
		    progress_mode = COALESCE($5, progress_mode)
		WHERE id = $1 AND user_id = $2
		RETURNING id, parent_id, name, position, hide_done, progress_mode`,
		id, userID, name, hideDone, progressMode).
		Scan(&g.ID, &g.ParentID, &g.Name, &g.Position, &g.HideDone, &g.ProgressMode)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, ErrNotFound
	}
	// Режим выключили — гасим флаги пунктов: иначе у них осталось бы невидимое
	// состояние, которое всплыло бы при повторном включении.
	if err == nil && progressMode != nil && !*progressMode {
		_, err = s.pool.Exec(ctx,
			`UPDATE checker_items SET in_progress = false WHERE group_id = $1 AND in_progress`, id)
	}
	return g, err
}

// SoftDeleteCheckGroup помечает группу и всё её поддерево как удалённые (в корзину).
// Возвращает имя группы (для сообщения с «Отменить»).
func (s *Store) SoftDeleteCheckGroup(ctx context.Context, userID, id int64) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `
		SELECT name FROM checker_groups WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`,
		id, userID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	_, err = s.pool.Exec(ctx, `
		WITH RECURSIVE sub AS (
			SELECT id FROM checker_groups WHERE id = $1
			UNION ALL
			SELECT c.id FROM checker_groups c JOIN sub ON c.parent_id = sub.id
		)
		UPDATE checker_groups SET deleted_at = now()
		WHERE id IN (SELECT id FROM sub) AND user_id = $2`, id, userID)
	return name, err
}

// RestoreCheckGroup восстанавливает группу-корень корзины и всё её поддерево.
func (s *Store) RestoreCheckGroup(ctx context.Context, userID, id int64) error {
	var ok bool
	// корень корзины: удалён, принадлежит пользователю, родитель отсутствует или активен
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM checker_groups g
			WHERE g.id = $1 AND g.user_id = $2 AND g.deleted_at IS NOT NULL
			  AND (g.parent_id IS NULL OR NOT EXISTS (
			        SELECT 1 FROM checker_groups p WHERE p.id = g.parent_id AND p.deleted_at IS NOT NULL))
		)`, id, userID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	_, err := s.pool.Exec(ctx, `
		WITH RECURSIVE sub AS (
			SELECT id FROM checker_groups WHERE id = $1
			UNION ALL
			SELECT c.id FROM checker_groups c JOIN sub ON c.parent_id = sub.id
		)
		UPDATE checker_groups SET deleted_at = NULL
		WHERE id IN (SELECT id FROM sub) AND user_id = $2`, id, userID)
	return err
}

// PurgeCheckGroup физически удаляет группу из корзины (каскадом всё поддерево).
func (s *Store) PurgeCheckGroup(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM checker_groups WHERE id = $1 AND user_id = $2 AND deleted_at IS NOT NULL`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// EmptyCheckerTrash физически удаляет всё содержимое корзины пользователя.
func (s *Store) EmptyCheckerTrash(ctx context.Context, userID int64) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM checker_groups WHERE user_id = $1 AND deleted_at IS NOT NULL`, userID)
	return err
}

// PurgeExpiredCheckerTrash удаляет из корзины пользователя всё старше olderThan
// (ленивая очистка по сроку хранения).
func (s *Store) PurgeExpiredCheckerTrash(ctx context.Context, userID int64, olderThan time.Time) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM checker_groups WHERE user_id = $1 AND deleted_at IS NOT NULL AND deleted_at < $2`,
		userID, olderThan)
	return err
}

// TrashGroup — корень корзины с агрегатами по поддереву.
type TrashGroup struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	DeletedAt time.Time `json:"deleted_at"`
	Groups    int       `json:"groups"` // групп в поддереве (включая корень)
	Items     int       `json:"items"`  // пунктов в поддереве
}

// ListCheckerTrash возвращает корни корзины с числом групп/пунктов в поддереве.
func (s *Store) ListCheckerTrash(ctx context.Context, userID int64) ([]TrashGroup, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT g.id, g.parent_id, g.name, g.deleted_at,
		       (SELECT count(*) FROM checker_items i WHERE i.group_id = g.id)
		FROM checker_groups g
		WHERE g.user_id = $1 AND g.deleted_at IS NOT NULL`, userID)
	if err != nil {
		return nil, err
	}
	type row struct {
		id     int64
		parent *int64
		name   string
		del    time.Time
		items  int
	}
	var all []row
	trashed := map[int64]bool{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.parent, &r.name, &r.del, &r.items); err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, r)
		trashed[r.id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	children := map[int64][]int{}
	for i, r := range all {
		if r.parent != nil && trashed[*r.parent] {
			children[*r.parent] = append(children[*r.parent], i)
		}
	}
	var subtree func(i int) (int, int)
	subtree = func(i int) (int, int) {
		gc, ic := 1, all[i].items
		for _, ci := range children[all[i].id] {
			cg, cit := subtree(ci)
			gc += cg
			ic += cit
		}
		return gc, ic
	}
	out := []TrashGroup{}
	for i, r := range all {
		if r.parent != nil && trashed[*r.parent] {
			continue // не корень (родитель тоже в корзине)
		}
		gc, ic := subtree(i)
		out = append(out, TrashGroup{ID: r.id, Name: r.name, DeletedAt: r.del, Groups: gc, Items: ic})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].DeletedAt.After(out[b].DeletedAt) })
	return out, nil
}

// MoveCheckGroup меняет родителя группы (nil — в верхний уровень). Проверяет:
// владение группой и новым родителем, отсутствие цикла (новый родитель не сам
// узел и не его потомок) и предел глубины (глубина нового родителя + высота
// поддерева группы ≤ MaxCheckerDepth). Позиция — в конец новых соседей.
func (s *Store) MoveCheckGroup(ctx context.Context, userID, groupID int64, newParentID *int64) (CheckGroup, error) {
	g := CheckGroup{Items: []CheckItem{}}
	owned, err := s.checkerGroupOwned(ctx, userID, groupID)
	if err != nil {
		return g, err
	}
	if !owned {
		return g, ErrNotFound
	}

	// потомки группы (включая её саму) + высота поддерева
	rows, err := s.pool.Query(ctx, `
		WITH RECURSIVE d AS (
			SELECT id, 1 AS lvl FROM checker_groups WHERE id = $1
			UNION ALL
			SELECT c.id, d.lvl + 1 FROM checker_groups c JOIN d ON c.parent_id = d.id
			WHERE c.deleted_at IS NULL
		)
		SELECT id, lvl FROM d`, groupID)
	if err != nil {
		return g, err
	}
	descendants := map[int64]bool{}
	height := 0
	for rows.Next() {
		var id int64
		var lvl int
		if err := rows.Scan(&id, &lvl); err != nil {
			rows.Close()
			return g, err
		}
		descendants[id] = true
		if lvl > height {
			height = lvl
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return g, err
	}

	parentDepth := 0
	if newParentID != nil {
		if *newParentID == groupID || descendants[*newParentID] {
			return g, ErrConflict // перенос в собственное поддерево
		}
		var pd *int
		if err := s.pool.QueryRow(ctx, `
			WITH RECURSIVE anc AS (
				SELECT id, parent_id, 1 AS depth FROM checker_groups
				WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
				UNION ALL
				SELECT gg.id, gg.parent_id, anc.depth + 1
				FROM checker_groups gg JOIN anc ON gg.id = anc.parent_id
			)
			SELECT max(depth) FROM anc`, *newParentID, userID).Scan(&pd); err != nil {
			return g, err
		}
		if pd == nil {
			return g, ErrNotFound // родитель не найден/не принадлежит
		}
		parentDepth = *pd
	}
	if parentDepth+height > MaxCheckerDepth {
		return g, ErrTooDeep
	}

	err = s.pool.QueryRow(ctx, `
		UPDATE checker_groups
		SET parent_id = $3,
		    position = (SELECT COALESCE(MAX(position) + 1, 0) FROM checker_groups
		                WHERE user_id = $2 AND parent_id IS NOT DISTINCT FROM $3)
		WHERE id = $1 AND user_id = $2
		RETURNING id, parent_id, name, position, hide_done, progress_mode`,
		groupID, userID, newParentID).
		Scan(&g.ID, &g.ParentID, &g.Name, &g.Position, &g.HideDone, &g.ProgressMode)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, err
}

// ReorderCheckGroups задаёт порядок групп-соседей (в пределах одного родителя):
// position = индекс в orderedIDs. Все id должны принадлежать пользователю и иметь
// именно этого родителя.
func (s *Store) ReorderCheckGroups(ctx context.Context, userID int64, parentID *int64, orderedIDs []int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for i, id := range orderedIDs {
		tag, err := tx.Exec(ctx, `
			UPDATE checker_groups SET position = $3
			WHERE id = $1 AND user_id = $2 AND parent_id IS NOT DISTINCT FROM $4 AND deleted_at IS NULL`,
			id, userID, int32(i), parentID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
	}
	return tx.Commit(ctx)
}

// groupFullNode — снимок поддерева с полными пунктами (done + note) для дублирования.
type groupFullNode struct {
	name  string
	items []CheckItem
	subs  []groupFullNode
}

func (s *Store) groupFullSnapshot(ctx context.Context, groupID int64) (groupFullNode, error) {
	var node groupFullNode
	if err := s.pool.QueryRow(ctx,
		`SELECT name FROM checker_groups WHERE id = $1`, groupID).Scan(&node.name); err != nil {
		return node, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT name, done, note, label FROM checker_items WHERE group_id = $1 ORDER BY position, id`, groupID)
	if err != nil {
		return node, err
	}
	for rows.Next() {
		var it CheckItem
		if err := rows.Scan(&it.Name, &it.Done, &it.Note, &it.Label); err != nil {
			rows.Close()
			return node, err
		}
		node.items = append(node.items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return node, err
	}
	childRows, err := s.pool.Query(ctx, `
		SELECT id FROM checker_groups WHERE parent_id = $1 AND deleted_at IS NULL ORDER BY position, id`, groupID)
	if err != nil {
		return node, err
	}
	var childIDs []int64
	for childRows.Next() {
		var id int64
		if err := childRows.Scan(&id); err != nil {
			childRows.Close()
			return node, err
		}
		childIDs = append(childIDs, id)
	}
	childRows.Close()
	if err := childRows.Err(); err != nil {
		return node, err
	}
	for _, cid := range childIDs {
		sub, err := s.groupFullSnapshot(ctx, cid)
		if err != nil {
			return node, err
		}
		node.subs = append(node.subs, sub)
	}
	return node, nil
}

func (s *Store) createGroupNodeFull(ctx context.Context, userID int64, parentID *int64, node groupFullNode) (CheckGroup, error) {
	g, err := s.CreateCheckGroup(ctx, userID, node.name, parentID)
	if err != nil {
		return g, err
	}
	for _, it := range node.items {
		if _, err := s.createItemUnchecked(ctx, g.ID, it); err != nil {
			return g, err
		}
	}
	for _, sub := range node.subs {
		if _, err := s.createGroupNodeFull(ctx, userID, &g.ID, sub); err != nil {
			return g, err
		}
	}
	return g, nil
}

// DuplicateCheckGroup создаёт копию группы со всем поддеревом (сохраняя отметки и
// заметки) рядом с оригиналом, имя — «<name> (копия)».
func (s *Store) DuplicateCheckGroup(ctx context.Context, userID, groupID int64) (CheckGroup, error) {
	var parentID *int64
	err := s.pool.QueryRow(ctx,
		`SELECT parent_id FROM checker_groups WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`,
		groupID, userID).Scan(&parentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CheckGroup{}, ErrNotFound
	}
	if err != nil {
		return CheckGroup{}, err
	}
	node, err := s.groupFullSnapshot(ctx, groupID)
	if err != nil {
		return CheckGroup{}, err
	}
	node.name += " (копия)"
	return s.createGroupNodeFull(ctx, userID, parentID, node)
}

// createItemUnchecked вставляет пункт без проверки доступа (для внутренних
// операций — импорт/копия/дубликат, где владение уже подтверждено).
func (s *Store) createItemUnchecked(ctx context.Context, groupID int64, in CheckItem) (CheckItem, error) {
	var it CheckItem
	err := s.pool.QueryRow(ctx, `
		INSERT INTO checker_items (group_id, name, done, note, label, position)
		VALUES ($1, $2, $3, $4, $5, (SELECT COALESCE(MAX(position) + 1, 0) FROM checker_items WHERE group_id = $1))
		RETURNING id, name, done, position, note, label, in_progress`,
		groupID, in.Name, in.Done, in.Note, in.Label).
		Scan(&it.ID, &it.Name, &it.Done, &it.Position, &it.Note, &it.Label, &it.InProgress)
	return it, err
}

// CreateCheckItem добавляет пункт в группу (доступ: владелец корня или участник).
func (s *Store) CreateCheckItem(ctx context.Context, userID, groupID int64, name string) (CheckItem, error) {
	if ok, err := s.checkerAccess(ctx, userID, groupID); err != nil {
		return CheckItem{}, err
	} else if !ok {
		return CheckItem{}, ErrNotFound
	}
	it, err := s.createItemUnchecked(ctx, groupID, CheckItem{Name: name})
	if err == nil {
		s.logCheckerHistory(ctx, userID, groupID, "добавил «"+name+"»")
	}
	return it, err
}

func (s *Store) UpdateCheckItem(ctx context.Context, userID, id int64, p CheckItemPatch) (CheckItem, error) {
	var it CheckItem
	var groupID int64
	err := s.pool.QueryRow(ctx, `SELECT group_id FROM checker_items WHERE id = $1`, id).Scan(&groupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return it, ErrNotFound
	}
	if err != nil {
		return it, err
	}
	if ok, err := s.checkerAccess(ctx, userID, groupID); err != nil {
		return it, err
	} else if !ok {
		return it, ErrNotFound
	}
	err = s.pool.QueryRow(ctx, `
		UPDATE checker_items
		SET name = COALESCE($2, name),
		    done = COALESCE($3, done),
		    position = COALESCE($4, position),
		    note = COALESCE($5, note),
		    label = COALESCE($6, label),
		    -- отмеченный пункт «в работе» быть не может: галочка снимает флаг,
		    -- даже если клиент прислал его в том же запросе
		    -- «в работе» живёт только при включённом режиме группы: проверка
		    -- здесь, а не только на клиенте, иначе выключенный режим можно
		    -- обойти прямым запросом и получить состояние-призрак
		    in_progress = CASE WHEN COALESCE($3, done) THEN false
		                       ELSE COALESCE($7, in_progress)
		                            AND (SELECT g.progress_mode FROM checker_groups g
		                                 WHERE g.id = checker_items.group_id) END
		WHERE id = $1
		RETURNING id, name, done, position, note, label, in_progress`,
		id, p.Name, p.Done, p.Position, p.Note, p.Label, p.InProgress).
		Scan(&it.ID, &it.Name, &it.Done, &it.Position, &it.Note, &it.Label, &it.InProgress)
	if errors.Is(err, pgx.ErrNoRows) {
		return it, ErrNotFound
	}
	if err == nil {
		if p.Done != nil {
			if *p.Done {
				s.logCheckerHistory(ctx, userID, groupID, "отметил «"+it.Name+"»")
			} else {
				s.logCheckerHistory(ctx, userID, groupID, "снял «"+it.Name+"»")
			}
		} else if p.Name != nil {
			s.logCheckerHistory(ctx, userID, groupID, "переименовал пункт в «"+it.Name+"»")
		}
	}
	return it, err
}

// MoveCheckItem переносит пункт в другую группу (в конец). Доступ нужен и к
// исходной, и к целевой группе (владелец корня или участник).
func (s *Store) MoveCheckItem(ctx context.Context, userID, itemID, targetGroupID int64) (CheckItem, error) {
	var it CheckItem
	var srcGroup int64
	err := s.pool.QueryRow(ctx, `SELECT group_id FROM checker_items WHERE id = $1`, itemID).Scan(&srcGroup)
	if errors.Is(err, pgx.ErrNoRows) {
		return it, ErrNotFound
	}
	if err != nil {
		return it, err
	}
	for _, gid := range []int64{srcGroup, targetGroupID} {
		if ok, err := s.checkerAccess(ctx, userID, gid); err != nil {
			return it, err
		} else if !ok {
			return it, ErrNotFound
		}
	}
	err = s.pool.QueryRow(ctx, `
		UPDATE checker_items
		SET group_id = $2,
		    position = (SELECT COALESCE(MAX(position) + 1, 0) FROM checker_items WHERE group_id = $2)
		WHERE id = $1
		RETURNING id, name, done, position, note, label, in_progress`,
		itemID, targetGroupID).
		Scan(&it.ID, &it.Name, &it.Done, &it.Position, &it.Note, &it.Label, &it.InProgress)
	if errors.Is(err, pgx.ErrNoRows) {
		return it, ErrNotFound
	}
	if err == nil {
		s.logCheckerHistory(ctx, userID, targetGroupID, "перенёс «"+it.Name+"»")
	}
	return it, err
}

func (s *Store) DeleteCheckItem(ctx context.Context, userID, id int64) error {
	var groupID int64
	var name string
	err := s.pool.QueryRow(ctx, `SELECT group_id, name FROM checker_items WHERE id = $1`, id).Scan(&groupID, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if ok, err := s.checkerAccess(ctx, userID, groupID); err != nil {
		return err
	} else if !ok {
		return ErrNotFound
	}
	if _, err = s.pool.Exec(ctx, `DELETE FROM checker_items WHERE id = $1`, id); err != nil {
		return err
	}
	s.logCheckerHistory(ctx, userID, groupID, "удалил «"+name+"»")
	return nil
}

// checkerAccess — есть ли у пользователя доступ к группе: он владелец её корня
// (верхнеуровневого предка) или корень расшарен ему.
func (s *Store) checkerAccess(ctx context.Context, userID, groupID int64) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		WITH RECURSIVE up AS (
			SELECT id, parent_id, user_id FROM checker_groups WHERE id = $1 AND deleted_at IS NULL
			UNION ALL
			SELECT g.id, g.parent_id, g.user_id
			FROM checker_groups g JOIN up ON g.id = up.parent_id
			WHERE g.deleted_at IS NULL
		)
		SELECT EXISTS (
			SELECT 1 FROM up WHERE parent_id IS NULL AND (
				user_id = $2 OR EXISTS (SELECT 1 FROM checker_shares s WHERE s.group_id = up.id AND s.user_id = $2)))`,
		groupID, userID).Scan(&ok)
	return ok, err
}

// CheckerHistoryEntry — запись истории изменений списка.
type CheckerHistoryEntry struct {
	UserID   int64     `json:"user_id"`
	UserName string    `json:"user_name"`
	Action   string    `json:"action"`
	At       time.Time `json:"at"`
}

// logCheckerHistory пишет запись истории на корень (верхнеуровневый предок)
// группы. Best-effort: ошибки не влияют на основную операцию.
func (s *Store) logCheckerHistory(ctx context.Context, userID, groupID int64, action string) {
	_, _ = s.pool.Exec(ctx, `
		WITH RECURSIVE up AS (
			SELECT id, parent_id FROM checker_groups WHERE id = $1
			UNION ALL
			SELECT g.id, g.parent_id FROM checker_groups g JOIN up ON g.id = up.parent_id
		)
		INSERT INTO checker_history (root_id, user_id, action)
		SELECT id, $2, $3 FROM up WHERE parent_id IS NULL LIMIT 1`,
		groupID, userID, action)
}

// ListCheckerHistory — последние записи истории списка (для владельца/участника).
func (s *Store) ListCheckerHistory(ctx context.Context, userID, groupID int64) ([]CheckerHistoryEntry, error) {
	if ok, err := s.checkerAccess(ctx, userID, groupID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		WITH RECURSIVE up AS (
			SELECT id, parent_id FROM checker_groups WHERE id = $1
			UNION ALL
			SELECT g.id, g.parent_id FROM checker_groups g JOIN up ON g.id = up.parent_id
		), root AS (SELECT id FROM up WHERE parent_id IS NULL LIMIT 1)
		SELECT h.user_id,
		       COALESCE(NULLIF(u.first_name, ''), '@' || u.username, '#' || h.user_id::text),
		       h.action, h.at
		FROM checker_history h JOIN users u ON u.id = h.user_id
		WHERE h.root_id = (SELECT id FROM root)
		ORDER BY h.at DESC, h.id DESC LIMIT 100`, groupID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[CheckerHistoryEntry])
	if out == nil {
		out = []CheckerHistoryEntry{}
	}
	return out, err
}

// ShareCheckerGroupAccess даёт участнику совместный доступ к верхнеуровневой
// группе владельца (не копия — общий чек-лист). Возвращает имя группы.
func (s *Store) ShareCheckerGroupAccess(ctx context.Context, ownerID, groupID, recipientID int64) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `
		SELECT name FROM checker_groups
		WHERE id = $1 AND user_id = $2 AND parent_id IS NULL AND deleted_at IS NULL`,
		groupID, ownerID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO checker_shares (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		groupID, recipientID)
	return name, err
}

// ListCheckerShares — участники группы (только владелец видит список).
func (s *Store) ListCheckerShares(ctx context.Context, userID, groupID int64) ([]AccessUser, error) {
	var owned bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM checker_groups WHERE id = $1 AND user_id = $2)`,
		groupID, userID).Scan(&owned); err != nil {
		return nil, err
	}
	if !owned {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, COALESCE(u.username, ''), COALESCE(u.first_name, '')
		FROM checker_shares s JOIN users u ON u.id = s.user_id
		WHERE s.group_id = $1 ORDER BY u.first_name, u.id`, groupID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[AccessUser])
}

// RevokeCheckerShare — владелец отзывает доступ у участника, либо участник сам
// выходит (target == requester).
func (s *Store) RevokeCheckerShare(ctx context.Context, requesterID, groupID, targetID int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM checker_shares s
		USING checker_groups g
		WHERE s.group_id = $1 AND s.user_id = $3 AND g.id = s.group_id
		  AND (g.user_id = $2 OR $2 = $3)`, groupID, requesterID, targetID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// checkerGroupOwned проверяет владение группой.
func (s *Store) checkerGroupOwned(ctx context.Context, userID, groupID int64) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM checker_groups WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL)`,
		groupID, userID).Scan(&ok)
	return ok, err
}

// ListGroupItems возвращает прямые пункты группы (для ответа после массовых действий).
// ListGroupItems — прямые пункты группы (доступ проверяется у вызывающего).
func (s *Store) ListGroupItems(ctx context.Context, groupID int64) ([]CheckItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT i.id, i.name, i.done, i.position, i.note, i.label, i.in_progress, i.remind_at
		FROM checker_items i
		WHERE i.group_id = $1
		ORDER BY i.position, i.id`, groupID)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[CheckItem])
	if items == nil {
		items = []CheckItem{}
	}
	return items, err
}

// BulkSetItemsDone отмечает/снимает все прямые пункты группы (массовое действие).
func (s *Store) BulkSetItemsDone(ctx context.Context, userID, groupID int64, done bool) ([]CheckItem, error) {
	if ok, err := s.checkerAccess(ctx, userID, groupID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNotFound
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE checker_items SET done = $2,
		     in_progress = CASE WHEN $2 THEN false ELSE in_progress END
		 WHERE group_id = $1`, groupID, done); err != nil {
		return nil, err
	}
	if done {
		s.logCheckerHistory(ctx, userID, groupID, "отметил все пункты")
	} else {
		s.logCheckerHistory(ctx, userID, groupID, "снял все отметки")
	}
	return s.ListGroupItems(ctx, groupID)
}

// DeleteDoneItems удаляет выполненные прямые пункты группы (массовое действие).
func (s *Store) DeleteDoneItems(ctx context.Context, userID, groupID int64) ([]CheckItem, error) {
	if ok, err := s.checkerAccess(ctx, userID, groupID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNotFound
	}
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM checker_items WHERE group_id = $1 AND done`, groupID); err != nil {
		return nil, err
	}
	s.logCheckerHistory(ctx, userID, groupID, "удалил выполненные пункты")
	return s.ListGroupItems(ctx, groupID)
}

// --- шаринг живой группы (аналог шаблонов) ---

// EnsureGroupShareToken выдаёт (или возвращает существующий) токен-приглашение
// для группы. Делиться можно только группой верхнего уровня.
func (s *Store) EnsureGroupShareToken(ctx context.Context, userID, groupID int64) (string, error) {
	var token *string
	err := s.pool.QueryRow(ctx, `
		SELECT share_token FROM checker_groups
		WHERE id = $1 AND user_id = $2 AND parent_id IS NULL AND deleted_at IS NULL`,
		groupID, userID).Scan(&token)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if token != nil {
		return *token, nil
	}
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	fresh := hex.EncodeToString(buf)
	_, err = s.pool.Exec(ctx, `
		UPDATE checker_groups SET share_token = $3 WHERE id = $1 AND user_id = $2`,
		groupID, userID, fresh)
	return fresh, err
}

// groupNode — рекурсивный снимок группы (имя, пункты, вложенные подгруппы
// любой глубины) для копирования/redeem. Без проверки владельца.
type groupNode struct {
	name  string
	items []string
	subs  []groupNode
}

// groupSnapshotTree читает поддерево группы целиком (произвольная вложенность).
// Пункты берутся с текущим состоянием (done при копировании сбрасывается).
func (s *Store) groupSnapshotTree(ctx context.Context, groupID int64) (groupNode, error) {
	var node groupNode
	if err := s.pool.QueryRow(ctx, `
		SELECT name FROM checker_groups WHERE id = $1`, groupID).Scan(&node.name); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return node, ErrNotFound
		}
		return node, err
	}
	items, err := s.groupItemNames(ctx, groupID)
	if err != nil {
		return node, err
	}
	node.items = items

	rows, err := s.pool.Query(ctx, `
		SELECT id FROM checker_groups WHERE parent_id = $1 AND deleted_at IS NULL ORDER BY position, id`, groupID)
	if err != nil {
		return node, err
	}
	var childIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return node, err
		}
		childIDs = append(childIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return node, err
	}
	for _, cid := range childIDs {
		sub, err := s.groupSnapshotTree(ctx, cid)
		if err != nil {
			return node, err
		}
		node.subs = append(node.subs, sub)
	}
	return node, nil
}

func (s *Store) groupItemNames(ctx context.Context, groupID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT name FROM checker_items WHERE group_id = $1 ORDER BY position, id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		items = append(items, n)
	}
	return items, rows.Err()
}

// copyGroupTree создаёт у пользователя копию группы с пунктами и подгруппами
// любой глубины (отметки done сбрасываются — это свежий чек-лист). Возвращает имя.
func (s *Store) copyGroupTree(ctx context.Context, targetUserID, srcGroupID int64) (string, error) {
	tree, err := s.groupSnapshotTree(ctx, srcGroupID)
	if err != nil {
		return "", err
	}
	if err := s.createGroupNode(ctx, targetUserID, nil, tree); err != nil {
		return "", err
	}
	return tree.name, nil
}

// createGroupNode рекурсивно создаёт группу-узел с пунктами и поддеревом.
func (s *Store) createGroupNode(ctx context.Context, userID int64, parentID *int64, node groupNode) error {
	g, err := s.CreateCheckGroup(ctx, userID, node.name, parentID)
	if err != nil {
		return err
	}
	for _, item := range node.items {
		if _, err := s.createItemUnchecked(ctx, g.ID, CheckItem{Name: item}); err != nil {
			return err
		}
	}
	for _, sub := range node.subs {
		if err := s.createGroupNode(ctx, userID, &g.ID, sub); err != nil {
			return err
		}
	}
	return nil
}

// CopyGroupTo копирует группу владельца получателю (для «отправить пользователю»).
func (s *Store) CopyGroupTo(ctx context.Context, ownerID, groupID, recipientID int64) (string, error) {
	var owned bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM checker_groups
		               WHERE id = $1 AND user_id = $2 AND parent_id IS NULL AND deleted_at IS NULL)`,
		groupID, ownerID).Scan(&owned); err != nil {
		return "", err
	}
	if !owned {
		return "", ErrNotFound
	}
	return s.copyGroupTree(ctx, recipientID, groupID)
}

// --- импорт группы (текст/JSON) ---

// ImportItem — пункт при импорте (с состоянием выполнения).
type ImportItem struct {
	Name string `json:"name"`
	Done bool   `json:"done"`
}

// ImportSubgroup — подгруппа при импорте (может содержать вложенные подгруппы
// любой глубины).
type ImportSubgroup struct {
	Name      string           `json:"name"`
	Items     []ImportItem     `json:"items"`
	Subgroups []ImportSubgroup `json:"subgroups"`
}

// ImportGroup — дерево группы при импорте.
type ImportGroup struct {
	Name      string           `json:"name"`
	Items     []ImportItem     `json:"items"`
	Subgroups []ImportSubgroup `json:"subgroups"`
}

// ImportCheckGroup создаёт группу с пунктами и подгруппами (любой глубины) из импорта.
func (s *Store) ImportCheckGroup(ctx context.Context, userID int64, in ImportGroup) (CheckGroup, error) {
	group, err := s.CreateCheckGroup(ctx, userID, in.Name, nil)
	if err != nil {
		return CheckGroup{}, err
	}
	for _, item := range in.Items {
		if err := s.importItem(ctx, userID, group.ID, item); err != nil {
			return CheckGroup{}, err
		}
	}
	for _, sub := range in.Subgroups {
		if err := s.importSubgroup(ctx, userID, group.ID, sub); err != nil {
			return CheckGroup{}, err
		}
	}
	return group, nil
}

// importSubgroup рекурсивно создаёт подгруппу с пунктами и вложенными подгруппами.
func (s *Store) importSubgroup(ctx context.Context, userID, parentID int64, sub ImportSubgroup) error {
	g, err := s.CreateCheckGroup(ctx, userID, sub.Name, &parentID)
	if err != nil {
		return err
	}
	for _, item := range sub.Items {
		if err := s.importItem(ctx, userID, g.ID, item); err != nil {
			return err
		}
	}
	for _, child := range sub.Subgroups {
		if err := s.importSubgroup(ctx, userID, g.ID, child); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) importItem(ctx context.Context, _ int64, groupID int64, item ImportItem) error {
	_, err := s.createItemUnchecked(ctx, groupID, CheckItem{Name: item.Name, Done: item.Done})
	return err
}

// RedeemGroupShareToken копирует группу по токену-приглашению новому владельцу.
func (s *Store) RedeemGroupShareToken(ctx context.Context, userID int64, token string) (CheckGroup, error) {
	var groupID int64
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM checker_groups WHERE share_token = $1 AND deleted_at IS NULL`, token).Scan(&groupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CheckGroup{}, ErrNotFound
	}
	if err != nil {
		return CheckGroup{}, err
	}
	name, err := s.copyGroupTree(ctx, userID, groupID)
	if err != nil {
		return CheckGroup{}, err
	}
	return CheckGroup{Name: name}, nil
}
