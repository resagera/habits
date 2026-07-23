package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"
)

// MaxCheckerDepth — предельная глубина вложенности групп Checker (уровней,
// считая группу верхнего уровня за 1-й). Единый предел для ручного создания,
// импорта и раскрытия чек-листа в Projects.
const MaxCheckerDepth = 20

type CheckGroup struct {
	ID       int64       `json:"id"`
	ParentID *int64      `json:"parent_id"`
	Name     string      `json:"name"`
	Position int32       `json:"position"`
	HideDone bool        `json:"hide_done"`
	Items    []CheckItem `json:"items"`
}

type CheckItem struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Done     bool   `json:"done"`
	Position int32  `json:"position"`
}

type CheckItemPatch struct {
	Name     *string
	Done     *bool
	Position *int32
}

func (s *Store) ListCheckGroups(ctx context.Context, userID int64) ([]CheckGroup, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, parent_id, name, position, hide_done FROM checker_groups
		WHERE user_id = $1 ORDER BY position, id`, userID)
	if err != nil {
		return nil, err
	}
	groups, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (CheckGroup, error) {
		var g CheckGroup
		err := row.Scan(&g.ID, &g.ParentID, &g.Name, &g.Position, &g.HideDone)
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
	for i := range groups {
		byID[groups[i].ID] = &groups[i]
	}
	itemRows, err := s.pool.Query(ctx, `
		SELECT i.id, i.group_id, i.name, i.done, i.position
		FROM checker_items i
		JOIN checker_groups g ON g.id = i.group_id
		WHERE g.user_id = $1
		ORDER BY i.position, i.id`, userID)
	if err != nil {
		return nil, err
	}
	defer itemRows.Close()
	for itemRows.Next() {
		var it CheckItem
		var groupID int64
		if err := itemRows.Scan(&it.ID, &groupID, &it.Name, &it.Done, &it.Position); err != nil {
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
			    WHERE id = $1 AND user_id = $2
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
		RETURNING id, parent_id, name, position, hide_done`,
		userID, name, parentID).Scan(&g.ID, &g.ParentID, &g.Name, &g.Position, &g.HideDone)
	return g, err
}

// UpdateCheckGroup меняет имя и/или «скрывать выполненное» (nil — не трогать).
func (s *Store) UpdateCheckGroup(ctx context.Context, userID, id int64, name *string, hideDone *bool) (CheckGroup, error) {
	g := CheckGroup{Items: []CheckItem{}}
	err := s.pool.QueryRow(ctx, `
		UPDATE checker_groups
		SET name = COALESCE($3, name),
		    hide_done = COALESCE($4, hide_done)
		WHERE id = $1 AND user_id = $2
		RETURNING id, parent_id, name, position, hide_done`,
		id, userID, name, hideDone).Scan(&g.ID, &g.ParentID, &g.Name, &g.Position, &g.HideDone)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, err
}

func (s *Store) DeleteCheckGroup(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM checker_groups WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateCheckItem(ctx context.Context, userID, groupID int64, name string) (CheckItem, error) {
	var it CheckItem
	err := s.pool.QueryRow(ctx, `
		INSERT INTO checker_items (group_id, name, position)
		SELECT g.id, $3, (SELECT COALESCE(MAX(position) + 1, 0) FROM checker_items WHERE group_id = g.id)
		FROM checker_groups g
		WHERE g.id = $1 AND g.user_id = $2
		RETURNING id, name, done, position`,
		groupID, userID, name).Scan(&it.ID, &it.Name, &it.Done, &it.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return it, ErrNotFound
	}
	return it, err
}

func (s *Store) UpdateCheckItem(ctx context.Context, userID, id int64, p CheckItemPatch) (CheckItem, error) {
	var it CheckItem
	err := s.pool.QueryRow(ctx, `
		UPDATE checker_items i
		SET name = COALESCE($3, i.name),
		    done = COALESCE($4, i.done),
		    position = COALESCE($5, i.position)
		FROM checker_groups g
		WHERE i.id = $1 AND g.id = i.group_id AND g.user_id = $2
		RETURNING i.id, i.name, i.done, i.position`,
		id, userID, p.Name, p.Done, p.Position).Scan(&it.ID, &it.Name, &it.Done, &it.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return it, ErrNotFound
	}
	return it, err
}

// MoveCheckItem переносит пункт в другую группу того же пользователя (в конец
// целевой группы). Проверяет владение и исходной, и целевой группой.
func (s *Store) MoveCheckItem(ctx context.Context, userID, itemID, targetGroupID int64) (CheckItem, error) {
	var it CheckItem
	err := s.pool.QueryRow(ctx, `
		UPDATE checker_items i
		SET group_id = $3,
		    position = (SELECT COALESCE(MAX(position) + 1, 0)
		                FROM checker_items WHERE group_id = $3)
		FROM checker_groups src, checker_groups dst
		WHERE i.id = $1
		  AND src.id = i.group_id AND src.user_id = $2
		  AND dst.id = $3 AND dst.user_id = $2
		RETURNING i.id, i.name, i.done, i.position`,
		itemID, userID, targetGroupID).Scan(&it.ID, &it.Name, &it.Done, &it.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return it, ErrNotFound
	}
	return it, err
}

func (s *Store) DeleteCheckItem(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM checker_items i
		USING checker_groups g
		WHERE i.id = $1 AND g.id = i.group_id AND g.user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- шаринг живой группы (аналог шаблонов) ---

// EnsureGroupShareToken выдаёт (или возвращает существующий) токен-приглашение
// для группы. Делиться можно только группой верхнего уровня.
func (s *Store) EnsureGroupShareToken(ctx context.Context, userID, groupID int64) (string, error) {
	var token *string
	err := s.pool.QueryRow(ctx, `
		SELECT share_token FROM checker_groups
		WHERE id = $1 AND user_id = $2 AND parent_id IS NULL`,
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
		SELECT id FROM checker_groups WHERE parent_id = $1 ORDER BY position, id`, groupID)
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
		if _, err := s.CreateCheckItem(ctx, userID, g.ID, item); err != nil {
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
		               WHERE id = $1 AND user_id = $2 AND parent_id IS NULL)`,
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

func (s *Store) importItem(ctx context.Context, userID, groupID int64, item ImportItem) error {
	it, err := s.CreateCheckItem(ctx, userID, groupID, item.Name)
	if err != nil {
		return err
	}
	if item.Done {
		done := true
		_, err = s.UpdateCheckItem(ctx, userID, it.ID, CheckItemPatch{Done: &done})
	}
	return err
}

// RedeemGroupShareToken копирует группу по токену-приглашению новому владельцу.
func (s *Store) RedeemGroupShareToken(ctx context.Context, userID int64, token string) (CheckGroup, error) {
	var groupID int64
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM checker_groups WHERE share_token = $1`, token).Scan(&groupID)
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
