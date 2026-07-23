package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Category struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Position int32  `json:"position"`
	Daily    bool   `json:"daily"`
	Kind     string `json:"kind"`  // marks | counter
	Style    string `json:"style"` // square | circle | emoji
	Multi    bool   `json:"multi"` // мультицвет / мульти-эмодзи
	Emoji    string `json:"emoji"`
	OwnerID  int64  `json:"owner_id"`
	Mine     bool   `json:"mine"`
	Shared   bool   `json:"shared"` // есть участники (у владельца) или получен по шарингу
}

type CategoryPatch struct {
	Name     *string
	Color    *string
	Position *int32
	Daily    *bool
	Kind     *string
	Style    *string
	Multi    *bool
	Emoji    *string
}

type MarkDay struct {
	Day   string  `json:"day"`
	Color *string `json:"color,omitempty"`
	Emoji *string `json:"emoji,omitempty"`
	Count int32   `json:"count"`
}

type CategoryMarks struct {
	CategoryID int64     `json:"category_id"`
	Days       []MarkDay `json:"days"`
}

// trackerAccess — владелец или участник по шарингу.
const trackerAccess = `(c.user_id = $1 OR EXISTS (
	SELECT 1 FROM tracker_shares s WHERE s.category_id = c.id AND s.user_id = $1))`

const categoryCols = `c.id, c.name, c.color, c.position, c.daily, c.kind, c.style, c.multi, c.emoji,
	c.user_id, (c.user_id = $1),
	(EXISTS (SELECT 1 FROM tracker_shares s2 WHERE s2.category_id = c.id) OR c.user_id <> $1)`

func (s *Store) ListCategories(ctx context.Context, userID int64) ([]Category, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+categoryCols+`
		FROM tracker_categories c
		WHERE `+trackerAccess+`
		ORDER BY (c.user_id = $1) DESC, c.position, c.id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Category])
}

func (s *Store) CreateCategory(ctx context.Context, userID int64, name, color, kind, style, emoji string, multi bool) (Category, error) {
	var c Category
	err := s.pool.QueryRow(ctx, `
		WITH ins AS (
			INSERT INTO tracker_categories (user_id, name, color, kind, style, multi, emoji, position)
			VALUES ($1, $2, $3, $4, $5, $6, $7,
			        (SELECT COALESCE(MAX(position) + 1, 0) FROM tracker_categories WHERE user_id = $1))
			RETURNING *
		)
		SELECT `+categoryCols+` FROM ins c`,
		userID, name, color, kind, style, multi, emoji).Scan(
		&c.ID, &c.Name, &c.Color, &c.Position, &c.Daily, &c.Kind, &c.Style, &c.Multi, &c.Emoji,
		&c.OwnerID, &c.Mine, &c.Shared)
	if isUniqueViolation(err) {
		return c, ErrConflict
	}
	return c, err
}

// UpdateCategory — только владелец.
func (s *Store) UpdateCategory(ctx context.Context, userID, id int64, p CategoryPatch) (Category, error) {
	var c Category
	err := s.pool.QueryRow(ctx, `
		WITH upd AS (
			UPDATE tracker_categories
			SET name = COALESCE($3, name),
			    color = COALESCE($4, color),
			    position = COALESCE($5, position),
			    daily = COALESCE($6, daily),
			    kind = COALESCE($7, kind),
			    style = COALESCE($8, style),
			    multi = COALESCE($9, multi),
			    emoji = COALESCE($10, emoji),
			    updated_at = now()
			WHERE id = $2 AND user_id = $1
			RETURNING *
		)
		SELECT `+categoryCols+` FROM upd c`,
		userID, id, p.Name, p.Color, p.Position, p.Daily, p.Kind, p.Style, p.Multi, p.Emoji).Scan(
		&c.ID, &c.Name, &c.Color, &c.Position, &c.Daily, &c.Kind, &c.Style, &c.Multi, &c.Emoji,
		&c.OwnerID, &c.Mine, &c.Shared)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	if isUniqueViolation(err) {
		return c, ErrConflict
	}
	return c, err
}

func (s *Store) DeleteCategory(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM tracker_categories WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarksInRange returns marked days grouped by category for [from, to].
// If categoryID is non-nil, only that category is returned.
func (s *Store) MarksInRange(ctx context.Context, userID int64, from, to time.Time, categoryID *int64) ([]CategoryMarks, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.category_id, to_char(m.day, 'YYYY-MM-DD'), m.color, m.emoji, m.count
		FROM tracker_marks m
		JOIN tracker_categories c ON c.id = m.category_id
		WHERE `+trackerAccess+`
		  AND m.day BETWEEN $2 AND $3
		  AND ($4::bigint IS NULL OR m.category_id = $4)
		ORDER BY m.category_id, m.day`,
		userID, from, to, categoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectCategoryMarks(rows)
}

func collectCategoryMarks(rows pgx.Rows) ([]CategoryMarks, error) {
	var result []CategoryMarks
	for rows.Next() {
		var catID int64
		var d MarkDay
		if err := rows.Scan(&catID, &d.Day, &d.Color, &d.Emoji, &d.Count); err != nil {
			return nil, err
		}
		if n := len(result); n == 0 || result[n-1].CategoryID != catID {
			result = append(result, CategoryMarks{CategoryID: catID})
		}
		last := &result[len(result)-1]
		last.Days = append(last.Days, d)
	}
	return result, rows.Err()
}

// CategoryHistory — все отметки категории с самой старой (для полноэкранного вида).
func (s *Store) CategoryHistory(ctx context.Context, userID, categoryID int64) ([]MarkDay, error) {
	ok, err := s.canAccessCategory(ctx, userID, categoryID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		SELECT to_char(day, 'YYYY-MM-DD'), color, emoji, count
		FROM tracker_marks WHERE category_id = $1 ORDER BY day`, categoryID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[MarkDay])
}

func (s *Store) canAccessCategory(ctx context.Context, userID, categoryID int64) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM tracker_categories c WHERE c.id = $2 AND `+trackerAccess+`)`,
		userID, categoryID).Scan(&ok)
	return ok, err
}

// ToggleMark переключает отметку дня и возвращает состояние после (true = отмечено).
// Для мультицветных/мульти-эмодзи трекеров передаётся активный цвет/эмодзи:
// клик по отметке с другим цветом перекрашивает её, с тем же — снимает.
// ErrNotFound — нет доступа (не владелец и не участник).
func (s *Store) ToggleMark(ctx context.Context, userID, categoryID int64, day time.Time, color, emoji *string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var allowed bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM tracker_categories c WHERE c.id = $2 AND `+trackerAccess+`)`,
		userID, categoryID).Scan(&allowed)
	if err != nil {
		return false, err
	}
	if !allowed {
		return false, ErrNotFound
	}

	var curColor, curEmoji *string
	err = tx.QueryRow(ctx, `
		SELECT color, emoji FROM tracker_marks WHERE category_id = $1 AND day = $2`,
		categoryID, day).Scan(&curColor, &curEmoji)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if _, err := tx.Exec(ctx, `
			INSERT INTO tracker_marks (category_id, day, color, emoji) VALUES ($1, $2, $3, $4)`,
			categoryID, day, color, emoji); err != nil {
			return false, err
		}
		return true, tx.Commit(ctx)
	case err != nil:
		return false, err
	}

	// отметка есть: другой цвет/эмодзи — перекрасить, тот же — снять
	if (color != nil && !strEq(curColor, color)) || (emoji != nil && !strEq(curEmoji, emoji)) {
		_, err := tx.Exec(ctx, `
			UPDATE tracker_marks SET color = $3, emoji = $4 WHERE category_id = $1 AND day = $2`,
			categoryID, day, color, emoji)
		if err != nil {
			return false, err
		}
		return true, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM tracker_marks WHERE category_id = $1 AND day = $2`, categoryID, day); err != nil {
		return false, err
	}
	return false, tx.Commit(ctx)
}

func strEq(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// IncrementMark изменяет счётчик дня на delta (±1) и возвращает новое значение.
// Счётчик не уходит ниже нуля; ноль хранится отсутствием строки.
func (s *Store) IncrementMark(ctx context.Context, userID, categoryID int64, day time.Time, delta int32) (int32, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var allowed bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM tracker_categories c WHERE c.id = $2 AND `+trackerAccess+`)`,
		userID, categoryID).Scan(&allowed)
	if err != nil {
		return 0, err
	}
	if !allowed {
		return 0, ErrNotFound
	}

	var count int32
	err = tx.QueryRow(ctx, `
		SELECT count FROM tracker_marks WHERE category_id = $1 AND day = $2 FOR UPDATE`,
		categoryID, day).Scan(&count)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		count, err = 0, nil
	case err != nil:
		return 0, err
	}

	next := count + delta
	if next < 0 {
		next = 0
	}
	switch {
	case next == count:
		// ничего не менять (например, -1 при нуле)
	case next == 0:
		_, err = tx.Exec(ctx, `DELETE FROM tracker_marks WHERE category_id = $1 AND day = $2`, categoryID, day)
	case count == 0:
		_, err = tx.Exec(ctx, `INSERT INTO tracker_marks (category_id, day, count) VALUES ($1, $2, $3)`, categoryID, day, next)
	default:
		_, err = tx.Exec(ctx, `UPDATE tracker_marks SET count = $3 WHERE category_id = $1 AND day = $2`, categoryID, day, next)
	}
	if err != nil {
		return 0, err
	}
	return next, tx.Commit(ctx)
}

// ShareTracker выдаёт доступ к трекеру (только владелец). Возвращает имя трекера.
func (s *Store) ShareTracker(ctx context.Context, ownerID, categoryID, recipientID int64) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `
		SELECT name FROM tracker_categories WHERE id = $1 AND user_id = $2`,
		categoryID, ownerID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO tracker_shares (category_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		categoryID, recipientID)
	return name, err
}

// ListTrackerShares — участники трекера (владелец или участник могут смотреть).
func (s *Store) ListTrackerShares(ctx context.Context, userID, categoryID int64) ([]AccessUser, error) {
	ok, err := s.canAccessCategory(ctx, userID, categoryID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, COALESCE(u.username, ''), COALESCE(u.first_name, '')
		FROM tracker_shares s JOIN users u ON u.id = s.user_id
		WHERE s.category_id = $1 ORDER BY s.created_at`, categoryID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[AccessUser])
}

// RevokeTrackerShare снимает доступ: владелец — у любого участника,
// участник — только у себя («покинуть»).
func (s *Store) RevokeTrackerShare(ctx context.Context, requesterID, categoryID, targetID int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM tracker_shares s
		USING tracker_categories c
		WHERE s.category_id = $1 AND s.user_id = $2 AND c.id = s.category_id
		  AND (c.user_id = $3 OR $2 = $3)`,
		categoryID, targetID, requesterID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
