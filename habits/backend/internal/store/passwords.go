package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrVersionConflict — блоб на сервере новее, чем ожидает клиент
// (сохранение со старого устройства).
var ErrVersionConflict = errors.New("vault version conflict")

type PasswordVault struct {
	Version   int64     `json:"version"`
	Blob      string    `json:"blob"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GetPasswordVault возвращает nil, если блоба ещё нет.
func (s *Store) GetPasswordVault(ctx context.Context, userID int64) (*PasswordVault, error) {
	var v PasswordVault
	err := s.pool.QueryRow(ctx, `
		SELECT version, blob, updated_at FROM password_vaults WHERE user_id = $1`,
		userID).Scan(&v.Version, &v.Blob, &v.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// PutPasswordVault сохраняет блоб, если baseVersion совпадает с текущей
// (0 — блоба ещё нет). Возвращает новую версию; при несовпадении —
// ErrVersionConflict и актуальную версию.
func (s *Store) PutPasswordVault(ctx context.Context, userID int64, blob string, baseVersion int64) (PasswordVault, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PasswordVault{}, err
	}
	defer tx.Rollback(ctx)

	var current int64
	err = tx.QueryRow(ctx, `
		SELECT version FROM password_vaults WHERE user_id = $1 FOR UPDATE`, userID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		current = 0
	} else if err != nil {
		return PasswordVault{}, err
	}
	if current != baseVersion {
		return PasswordVault{Version: current}, ErrVersionConflict
	}

	var out PasswordVault
	err = tx.QueryRow(ctx, `
		INSERT INTO password_vaults (user_id, version, blob)
		VALUES ($1, 1, $2)
		ON CONFLICT (user_id) DO UPDATE
		SET version = password_vaults.version + 1, blob = EXCLUDED.blob, updated_at = now()
		RETURNING version, blob, updated_at`, userID, blob).
		Scan(&out.Version, &out.Blob, &out.UpdatedAt)
	if err != nil {
		return PasswordVault{}, err
	}
	return out, tx.Commit(ctx)
}

// --- шаринг папок ---

type PasswordShare struct {
	ID         int64      `json:"id"`
	FolderName string     `json:"folder_name"`
	Payload    string     `json:"payload"`
	Key        string     `json:"key"`
	From       AccessUser `json:"from"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (s *Store) CreatePasswordShare(ctx context.Context, fromUser, toUser int64, folderName, payload, key string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO password_shares (from_user, to_user, folder_name, payload, key)
		VALUES ($1, $2, $3, $4, $5)`, fromUser, toUser, folderName, payload, key)
	if isForeignKeyViolation(err) {
		return ErrNotFound
	}
	return err
}

// ListPasswordShares — входящие передачи папок (для получателя).
func (s *Store) ListPasswordShares(ctx context.Context, userID int64) ([]PasswordShare, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.folder_name, p.payload, p.key,
		       u.id, COALESCE(u.username, ''), COALESCE(u.first_name, ''), p.created_at
		FROM password_shares p
		JOIN users u ON u.id = p.from_user
		WHERE p.to_user = $1
		ORDER BY p.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PasswordShare
	for rows.Next() {
		var sh PasswordShare
		if err := rows.Scan(&sh.ID, &sh.FolderName, &sh.Payload, &sh.Key,
			&sh.From.ID, &sh.From.Username, &sh.From.FirstName, &sh.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, sh)
	}
	return result, rows.Err()
}

// DeletePasswordShare — получатель принял или отклонил передачу.
func (s *Store) DeletePasswordShare(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM password_shares WHERE id = $1 AND to_user = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeletePasswordVault — полный сброс хранилища на сервере.
func (s *Store) DeletePasswordVault(ctx context.Context, userID int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM password_vaults WHERE user_id = $1`, userID)
	return err
}
