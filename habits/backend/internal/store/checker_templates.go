package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"
)

// CheckTemplate — многоразовый список-шаблон («сборы в поездку»).
type CheckTemplate struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	ShareToken *string  `json:"share_token,omitempty"`
	Items      []string `json:"items"`
}

func (s *Store) ListCheckTemplates(ctx context.Context, userID int64) ([]CheckTemplate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, share_token FROM checker_templates
		WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	templates, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (CheckTemplate, error) {
		var t CheckTemplate
		err := row.Scan(&t.ID, &t.Name, &t.ShareToken)
		t.Items = []string{}
		return t, err
	})
	if err != nil || len(templates) == 0 {
		return templates, err
	}
	byID := make(map[int64]*CheckTemplate, len(templates))
	for i := range templates {
		byID[templates[i].ID] = &templates[i]
	}
	itemRows, err := s.pool.Query(ctx, `
		SELECT i.template_id, i.name
		FROM checker_template_items i
		JOIN checker_templates t ON t.id = i.template_id
		WHERE t.user_id = $1
		ORDER BY i.position, i.id`, userID)
	if err != nil {
		return nil, err
	}
	defer itemRows.Close()
	for itemRows.Next() {
		var tid int64
		var name string
		if err := itemRows.Scan(&tid, &name); err != nil {
			return nil, err
		}
		if t, ok := byID[tid]; ok {
			t.Items = append(t.Items, name)
		}
	}
	return templates, itemRows.Err()
}

// SaveCheckTemplate создаёт (id=0) или полностью заменяет шаблон с пунктами.
func (s *Store) SaveCheckTemplate(ctx context.Context, userID, id int64, name string, items []string) (CheckTemplate, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CheckTemplate{}, err
	}
	defer tx.Rollback(ctx)

	t := CheckTemplate{Items: items}
	if id == 0 {
		err = tx.QueryRow(ctx, `
			INSERT INTO checker_templates (user_id, name) VALUES ($1, $2)
			RETURNING id, name, share_token`, userID, name).Scan(&t.ID, &t.Name, &t.ShareToken)
	} else {
		err = tx.QueryRow(ctx, `
			UPDATE checker_templates SET name = $3 WHERE id = $1 AND user_id = $2
			RETURNING id, name, share_token`, id, userID, name).Scan(&t.ID, &t.Name, &t.ShareToken)
		if errors.Is(err, pgx.ErrNoRows) {
			return t, ErrNotFound
		}
	}
	if err != nil {
		return t, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM checker_template_items WHERE template_id = $1`, t.ID); err != nil {
		return t, err
	}
	for i, item := range items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO checker_template_items (template_id, name, position)
			VALUES ($1, $2, $3)`, t.ID, item, i); err != nil {
			return t, err
		}
	}
	return t, tx.Commit(ctx)
}

func (s *Store) DeleteCheckTemplate(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM checker_templates WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// getTemplate возвращает шаблон с пунктами (без проверки владельца — для redeem).
func (s *Store) getTemplateItems(ctx context.Context, templateID int64) (string, []string, error) {
	var name string
	if err := s.pool.QueryRow(ctx, `
		SELECT name FROM checker_templates WHERE id = $1`, templateID).Scan(&name); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, ErrNotFound
		}
		return "", nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT name FROM checker_template_items WHERE template_id = $1 ORDER BY position, id`, templateID)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	var items []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return "", nil, err
		}
		items = append(items, n)
	}
	return name, items, rows.Err()
}

// StartCheckTemplate разворачивает шаблон владельца в новую группу Checker.
func (s *Store) StartCheckTemplate(ctx context.Context, userID, templateID int64) (CheckGroup, error) {
	var owned bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM checker_templates WHERE id = $1 AND user_id = $2)`,
		templateID, userID).Scan(&owned); err != nil {
		return CheckGroup{}, err
	}
	if !owned {
		return CheckGroup{}, ErrNotFound
	}
	return s.instantiateTemplate(ctx, userID, templateID)
}

func (s *Store) instantiateTemplate(ctx context.Context, userID, templateID int64) (CheckGroup, error) {
	name, items, err := s.getTemplateItems(ctx, templateID)
	if err != nil {
		return CheckGroup{}, err
	}
	group, err := s.CreateCheckGroup(ctx, userID, name, nil)
	if err != nil {
		return group, err
	}
	for _, item := range items {
		it, err := s.CreateCheckItem(ctx, userID, group.ID, item)
		if err != nil {
			return group, err
		}
		group.Items = append(group.Items, it)
	}
	return group, nil
}

// EnsureShareToken выдаёт (или возвращает существующий) токен шаринга.
func (s *Store) EnsureShareToken(ctx context.Context, userID, templateID int64) (string, error) {
	var token *string
	err := s.pool.QueryRow(ctx, `
		SELECT share_token FROM checker_templates WHERE id = $1 AND user_id = $2`,
		templateID, userID).Scan(&token)
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
		UPDATE checker_templates SET share_token = $3 WHERE id = $1 AND user_id = $2`,
		templateID, userID, fresh)
	return fresh, err
}

// CopyTemplateTo копирует шаблон получателю (для «отправить пользователю»).
// Возвращает имя шаблона для уведомления.
func (s *Store) CopyTemplateTo(ctx context.Context, ownerID, templateID, recipientID int64) (string, error) {
	var owned bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM checker_templates WHERE id = $1 AND user_id = $2)`,
		templateID, ownerID).Scan(&owned); err != nil {
		return "", err
	}
	if !owned {
		return "", ErrNotFound
	}
	name, items, err := s.getTemplateItems(ctx, templateID)
	if err != nil {
		return "", err
	}
	_, err = s.SaveCheckTemplate(ctx, recipientID, 0, name, items)
	return name, err
}

// RedeemShareToken копирует шаблон по токену-приглашению.
func (s *Store) RedeemShareToken(ctx context.Context, userID int64, token string) (CheckTemplate, error) {
	var templateID int64
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM checker_templates WHERE share_token = $1`, token).Scan(&templateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CheckTemplate{}, ErrNotFound
	}
	if err != nil {
		return CheckTemplate{}, err
	}
	name, items, err := s.getTemplateItems(ctx, templateID)
	if err != nil {
		return CheckTemplate{}, err
	}
	return s.SaveCheckTemplate(ctx, userID, 0, name, items)
}
