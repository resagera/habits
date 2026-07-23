package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Типы пользователей и лимиты (страница Projects). Тип назначает админ,
// лимиты на тип редактируются в админке.

var UserTypes = []string{"regular", "vip", "payed1", "payed2"}

func ValidUserType(t string) bool {
	for _, x := range UserTypes {
		if x == t {
			return true
		}
	}
	return false
}

type TypeLimits struct {
	Type       string `json:"type"`
	MaxBlocks  int32  `json:"max_blocks"`
	MaxImages  int32  `json:"max_images"`
	MaxFiles   int32  `json:"max_files"`
	MaxImageMB int32  `json:"max_image_mb"`
	MaxFileMB  int32  `json:"max_file_mb"`
}

func (s *Store) ListTypeLimits(ctx context.Context) ([]TypeLimits, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT type, max_blocks, max_images, max_files, max_image_mb, max_file_mb
		FROM user_type_limits
		ORDER BY array_position(ARRAY['regular','vip','payed1','payed2'], type)`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[TypeLimits])
}

func (s *Store) UpdateTypeLimits(ctx context.Context, l TypeLimits) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE user_type_limits
		SET max_blocks = $2, max_images = $3, max_files = $4, max_image_mb = $5, max_file_mb = $6
		WHERE type = $1`,
		l.Type, l.MaxBlocks, l.MaxImages, l.MaxFiles, l.MaxImageMB, l.MaxFileMB)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// LimitsForUser — лимиты по типу пользователя (fallback на regular).
func (s *Store) LimitsForUser(ctx context.Context, userID int64) (TypeLimits, error) {
	var l TypeLimits
	err := s.pool.QueryRow(ctx, `
		SELECT l.type, l.max_blocks, l.max_images, l.max_files, l.max_image_mb, l.max_file_mb
		FROM user_type_limits l
		WHERE l.type = COALESCE((SELECT user_type FROM users WHERE id = $1), 'regular')`,
		userID).Scan(&l.Type, &l.MaxBlocks, &l.MaxImages, &l.MaxFiles, &l.MaxImageMB, &l.MaxFileMB)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.pool.QueryRow(ctx, `
			SELECT type, max_blocks, max_images, max_files, max_image_mb, max_file_mb
			FROM user_type_limits WHERE type = 'regular'`).
			Scan(&l.Type, &l.MaxBlocks, &l.MaxImages, &l.MaxFiles, &l.MaxImageMB, &l.MaxFileMB)
	}
	return l, err
}

func (s *Store) SetUserType(ctx context.Context, userID int64, t string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET user_type = $2 WHERE id = $1`, userID, t)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// --- учёт загрузок Projects ---

func (s *Store) RegisterProjectUpload(ctx context.Context, url string, userID int64, image bool, size int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO project_uploads (url, user_id, image, size) VALUES ($1, $2, $3, $4)
		ON CONFLICT (url) DO NOTHING`, url, userID, image, size)
	return err
}

func (s *Store) DeleteProjectUpload(ctx context.Context, url string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM project_uploads WHERE url = $1`, url)
	return err
}

// CountProjectUploads — сколько картинок и файлов уже загрузил пользователь.
func (s *Store) CountProjectUploads(ctx context.Context, userID int64) (images, files int32, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE image), count(*) FILTER (WHERE NOT image)
		FROM project_uploads WHERE user_id = $1`, userID).Scan(&images, &files)
	return
}
