package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// templateBody — дерево шаблона в JSONB (без имени: имя — отдельная колонка).
type templateBody struct {
	Items     []ImportItem     `json:"items"`
	Subgroups []ImportSubgroup `json:"subgroups"`
}

// CheckTemplate — многоразовый список-шаблон («сборы в поездку») с подгруппами.
type CheckTemplate struct {
	ID         int64            `json:"id"`
	Name       string           `json:"name"`
	ShareToken *string          `json:"share_token,omitempty"`
	Items      []ImportItem     `json:"items"`
	Subgroups  []ImportSubgroup `json:"subgroups"`
}

func decodeTemplateBody(raw []byte) ([]ImportItem, []ImportSubgroup) {
	var b templateBody
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &b)
	}
	if b.Items == nil {
		b.Items = []ImportItem{}
	}
	if b.Subgroups == nil {
		b.Subgroups = []ImportSubgroup{}
	}
	return b.Items, b.Subgroups
}

func (s *Store) ListCheckTemplates(ctx context.Context, userID int64) ([]CheckTemplate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, share_token, body FROM checker_templates
		WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (CheckTemplate, error) {
		var t CheckTemplate
		var body []byte
		err := row.Scan(&t.ID, &t.Name, &t.ShareToken, &body)
		t.Items, t.Subgroups = decodeTemplateBody(body)
		return t, err
	})
}

// SaveCheckTemplate создаёт (id=0) или полностью заменяет шаблон (дерево в body).
func (s *Store) SaveCheckTemplate(ctx context.Context, userID, id int64, name string, items []ImportItem, subs []ImportSubgroup) (CheckTemplate, error) {
	if items == nil {
		items = []ImportItem{}
	}
	if subs == nil {
		subs = []ImportSubgroup{}
	}
	body, err := json.Marshal(templateBody{Items: items, Subgroups: subs})
	if err != nil {
		return CheckTemplate{}, err
	}
	t := CheckTemplate{Items: items, Subgroups: subs}
	if id == 0 {
		err = s.pool.QueryRow(ctx, `
			INSERT INTO checker_templates (user_id, name, body) VALUES ($1, $2, $3)
			RETURNING id, name, share_token`, userID, name, body).Scan(&t.ID, &t.Name, &t.ShareToken)
	} else {
		err = s.pool.QueryRow(ctx, `
			UPDATE checker_templates SET name = $3, body = $4 WHERE id = $1 AND user_id = $2
			RETURNING id, name, share_token`, id, userID, name, body).Scan(&t.ID, &t.Name, &t.ShareToken)
		if errors.Is(err, pgx.ErrNoRows) {
			return t, ErrNotFound
		}
	}
	return t, err
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

// getTemplateBody возвращает имя + дерево шаблона (без проверки владельца — для redeem).
func (s *Store) getTemplateBody(ctx context.Context, templateID int64) (string, []ImportItem, []ImportSubgroup, error) {
	var name string
	var body []byte
	if err := s.pool.QueryRow(ctx, `
		SELECT name, body FROM checker_templates WHERE id = $1`, templateID).Scan(&name, &body); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, nil, ErrNotFound
		}
		return "", nil, nil, err
	}
	items, subs := decodeTemplateBody(body)
	return name, items, subs, nil
}

// StartCheckTemplate разворачивает шаблон владельца в новую группу Checker (с подгруппами).
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
	name, items, subs, err := s.getTemplateBody(ctx, templateID)
	if err != nil {
		return CheckGroup{}, err
	}
	return s.ImportCheckGroup(ctx, userID, ImportGroup{Name: name, Items: items, Subgroups: subs})
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
	name, items, subs, err := s.getTemplateBody(ctx, templateID)
	if err != nil {
		return "", err
	}
	_, err = s.SaveCheckTemplate(ctx, recipientID, 0, name, items, subs)
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
	name, items, subs, err := s.getTemplateBody(ctx, templateID)
	if err != nil {
		return CheckTemplate{}, err
	}
	return s.SaveCheckTemplate(ctx, userID, 0, name, items, subs)
}
