package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func (s *Store) ListCurrencies(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT code FROM user_currencies
		WHERE user_id = $1 ORDER BY position, created_at`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (s *Store) AddCurrency(ctx context.Context, userID int64, code string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_currencies (user_id, code, position)
		VALUES ($1, $2,
		        (SELECT COALESCE(MAX(position) + 1, 0) FROM user_currencies WHERE user_id = $1))
		ON CONFLICT (user_id, code) DO NOTHING`, userID, code)
	return err
}

func (s *Store) RemoveCurrency(ctx context.Context, userID int64, code string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM user_currencies WHERE user_id = $1 AND code = $2`, userID, code)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
