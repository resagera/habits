package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// TouchUser обновляет профиль и last_seen_at, фиксирует связку IP+устройство
// (уникальную, с датой первого появления) и возвращает бан-статус.
func (s *Store) TouchUser(ctx context.Context, id int64, username, firstName, ip, device string) (banned bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `
		INSERT INTO users (id, username, first_name)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE
		SET username = EXCLUDED.username,
		    first_name = EXCLUDED.first_name,
		    last_seen_at = now()
		RETURNING banned`,
		id, username, firstName).Scan(&banned)
	if err != nil {
		return false, err
	}

	if ip != "" || device != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_devices (user_id, ip, device)
			VALUES ($1, $2, $3)
			ON CONFLICT (user_id, ip, device) DO NOTHING`,
			id, ip, device); err != nil {
			return false, err
		}
	}
	// человек появился в системе — привязываем «внешние» контакты, где его
	// добавили заранее по id или @логину (страница Contacts)
	if _, err := tx.Exec(ctx, `
		UPDATE contacts SET contact_id = $1, tg_id = COALESCE(tg_id, $1)
		WHERE contact_id IS NULL
		  AND (tg_id = $1 OR ($2 <> '' AND lower(ext_username) = lower($2)))`,
		id, username); err != nil {
		return false, err
	}
	return banned, tx.Commit(ctx)
}

// ---------- админ ----------

type AdminUser struct {
	ID         int64     `json:"id"`
	Username   string    `json:"username"`
	FirstName  string    `json:"first_name"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Banned     bool      `json:"banned"`
	UserType   string    `json:"user_type"`
	LastIP     string    `json:"last_ip"`
	LastDevice string    `json:"last_device"`
}

type UserDevice struct {
	IP        string    `json:"ip"`
	Device    string    `json:"device"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) ListUsers(ctx context.Context, limit, offset int32) ([]AdminUser, int64, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, COALESCE(u.username, ''), COALESCE(u.first_name, ''),
		       u.created_at, u.last_seen_at, u.banned, u.user_type,
		       COALESCE(d.ip, ''), COALESCE(d.device, '')
		FROM users u
		LEFT JOIN LATERAL (
			SELECT ip, device FROM user_devices
			WHERE user_id = u.id ORDER BY created_at DESC LIMIT 1
		) d ON true
		ORDER BY u.last_seen_at DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	users, err := pgx.CollectRows(rows, pgx.RowToStructByPos[AdminUser])
	return users, total, err
}

func (s *Store) GetAdminUser(ctx context.Context, id int64) (AdminUser, []UserDevice, map[string]int64, error) {
	var u AdminUser
	err := s.pool.QueryRow(ctx, `
		SELECT id, COALESCE(username, ''), COALESCE(first_name, ''),
		       created_at, last_seen_at, banned, user_type
		FROM users WHERE id = $1`, id).
		Scan(&u.ID, &u.Username, &u.FirstName, &u.CreatedAt, &u.LastSeenAt, &u.Banned, &u.UserType)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, nil, nil, ErrNotFound
	}
	if err != nil {
		return u, nil, nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT ip, device, created_at FROM user_devices
		WHERE user_id = $1 ORDER BY created_at DESC`, id)
	if err != nil {
		return u, nil, nil, err
	}
	devices, err := pgx.CollectRows(rows, pgx.RowToStructByPos[UserDevice])
	if err != nil {
		return u, nil, nil, err
	}
	if len(devices) > 0 {
		u.LastIP, u.LastDevice = devices[0].IP, devices[0].Device
	}

	var trackerCategories, trackerMarks, checkerGroups, checkerItems,
		diaryEntries, linksCount, linksFolders, backgrounds int64
	err = s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM tracker_categories WHERE user_id = $1),
			(SELECT count(*) FROM tracker_marks m JOIN tracker_categories c ON c.id = m.category_id WHERE c.user_id = $1),
			(SELECT count(*) FROM checker_groups WHERE user_id = $1),
			(SELECT count(*) FROM checker_items i JOIN checker_groups g ON g.id = i.group_id WHERE g.user_id = $1),
			(SELECT count(*) FROM diary_entries WHERE user_id = $1),
			(SELECT count(*) FROM links WHERE user_id = $1),
			(SELECT count(*) FROM links_folders WHERE user_id = $1),
			(SELECT count(*) FROM user_backgrounds WHERE user_id = $1)`, id).
		Scan(&trackerCategories, &trackerMarks, &checkerGroups, &checkerItems,
			&diaryEntries, &linksCount, &linksFolders, &backgrounds)
	data := map[string]int64{
		"tracker_categories": trackerCategories,
		"tracker_marks":      trackerMarks,
		"checker_groups":     checkerGroups,
		"checker_items":      checkerItems,
		"diary_entries":      diaryEntries,
		"links":              linksCount,
		"links_folders":      linksFolders,
		"backgrounds":        backgrounds,
	}
	return u, devices, data, err
}

func (s *Store) SetUserBanned(ctx context.Context, id int64, banned bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET banned = $2 WHERE id = $1`, id, banned)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UserExists — есть ли пользователь (для фонов из чата бота).
func (s *Store) UserExists(ctx context.Context, id int64) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, id).Scan(&exists)
	return exists, err
}
