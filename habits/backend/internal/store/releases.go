package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Release — запись журнала релизов. PublicNotes видно всем, TechNotes и Comment —
// только админам (фильтрация в httpapi, обычным пользователям не отдаём).
type Release struct {
	ID          int64     `json:"id"`
	Version     string    `json:"version"`
	ReleasedOn  time.Time `json:"released_on"`
	Title       string    `json:"title"`
	PublicNotes string    `json:"public_notes"`
	TechNotes   string    `json:"tech_notes"`
	Status      string    `json:"status"`
	Comment     string    `json:"comment"`
}

const releaseCols = `id, version, released_on, title, public_notes, tech_notes, status, comment`

// ListReleases возвращает все релизы (новые сверху). Фильтрацию полей под роль
// делает обработчик.
func (s *Store) ListReleases(ctx context.Context) ([]Release, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+releaseCols+`
		FROM releases
		ORDER BY released_on DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Release])
}

// UpdateRelease меняет комментарий и/или статус (только для админов). Любой из
// указателей может быть nil — тогда поле не трогаем.
func (s *Store) UpdateRelease(ctx context.Context, id int64, comment, status *string) (Release, error) {
	var r Release
	err := s.pool.QueryRow(ctx, `
		UPDATE releases
		SET comment = COALESCE($2, comment),
		    status  = COALESCE($3, status)
		WHERE id = $1
		RETURNING `+releaseCols,
		id, comment, status).Scan(
		&r.ID, &r.Version, &r.ReleasedOn, &r.Title, &r.PublicNotes, &r.TechNotes, &r.Status, &r.Comment)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// PendingReleaseNotifications — выпущенные релизы, о которых ещё не уведомляли
// админа (notified_at IS NULL). Историю мы засеяли с проставленным notified_at,
// поэтому сюда попадают только новые релизы (добавленные миграцией).
func (s *Store) PendingReleaseNotifications(ctx context.Context) ([]Release, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+releaseCols+`
		FROM releases
		WHERE notified_at IS NULL AND status = 'released'
		ORDER BY released_on, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Release])
}

// MarkReleaseNotified помечает релиз как оповещённый.
func (s *Store) MarkReleaseNotified(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE releases SET notified_at = now() WHERE id = $1`, id)
	return err
}
