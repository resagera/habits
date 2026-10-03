package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type DiaryEntry struct {
	ID   int64     `json:"id"`
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

type DiaryFilter struct {
	From  *time.Time
	To    *time.Time
	Query string
	Limit int32
}

type DiaryEntryPatch struct {
	At   *time.Time
	Text *string
}

func (s *Store) ListDiaryEntries(ctx context.Context, userID int64, f DiaryFilter) ([]DiaryEntry, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	var q *string
	if f.Query != "" {
		pattern := "%" + f.Query + "%"
		q = &pattern
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, at, text FROM diary_entries
		WHERE user_id = $1
		  AND ($2::timestamptz IS NULL OR at >= $2)
		  AND ($3::timestamptz IS NULL OR at < $3)
		  AND ($4::text IS NULL OR text ILIKE $4)
		ORDER BY at DESC
		LIMIT $5`,
		userID, f.From, f.To, q, f.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[DiaryEntry])
}

func (s *Store) CreateDiaryEntry(ctx context.Context, userID int64, at time.Time, text string) (DiaryEntry, error) {
	var e DiaryEntry
	err := s.pool.QueryRow(ctx, `
		INSERT INTO diary_entries (user_id, at, text)
		VALUES ($1, $2, $3)
		RETURNING id, at, text`,
		userID, at, text).Scan(&e.ID, &e.At, &e.Text)
	return e, err
}

func (s *Store) UpdateDiaryEntry(ctx context.Context, userID, id int64, p DiaryEntryPatch) (DiaryEntry, error) {
	var e DiaryEntry
	err := s.pool.QueryRow(ctx, `
		UPDATE diary_entries
		SET at = COALESCE($3, at),
		    text = COALESCE($4, text),
		    updated_at = now()
		WHERE id = $1 AND user_id = $2
		RETURNING id, at, text`,
		id, userID, p.At, p.Text).Scan(&e.ID, &e.At, &e.Text)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

func (s *Store) DeleteDiaryEntry(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM diary_entries WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
