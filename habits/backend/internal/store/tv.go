package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Комнаты ТВ-плеера. Комната — это строка, которую объявляет агент (по
// умолчанию его адрес в локальной сети); приставка и пульт встречаются по ней.

type TVRoom struct {
	Key        string    `json:"key"`
	Label      string    `json:"label"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// NormalizeTVRoom приводит ключ к одному виду. Адрес агента пишут по-разному
// («192.168.0.226», «http://192.168.0.226/tv/», с пробелом на конце), а
// приставка и пульт обязаны попасть в одну комнату.
func NormalizeTVRoom(key string) string {
	key = strings.TrimSpace(strings.ToLower(key))
	key = strings.TrimPrefix(strings.TrimPrefix(key, "https://"), "http://")
	key = strings.TrimSuffix(strings.TrimSuffix(key, "/"), "/tv")
	key = strings.TrimSuffix(key, "/")
	if len(key) > 120 {
		key = key[:120]
	}
	return key
}

// ClaimTVRoom закрепляет комнату за пользователем при первом подключении
// пульта и возвращает ErrConflict, если она уже чужая.
//
// Без этого ключ вроде «192.168.0.226» подобрал бы кто угодно: приставка сидит
// в локальной сети, а шина — на общедоступном сервере.
func (s *Store) ClaimTVRoom(ctx context.Context, userID int64, key, label string) (TVRoom, error) {
	key = NormalizeTVRoom(key)
	rows, err := s.pool.Query(ctx, `
		INSERT INTO tv_rooms (key, user_id, label) VALUES ($1,$2,$3)
		ON CONFLICT (key) DO UPDATE
			SET last_seen_at = now(),
			    label = CASE WHEN $3 <> '' THEN $3 ELSE tv_rooms.label END
			WHERE tv_rooms.user_id = $2
		RETURNING key, label, created_at, last_seen_at`,
		key, userID, strings.TrimSpace(label))
	if err != nil {
		return TVRoom{}, err
	}
	r, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[TVRoom])
	if errors.Is(err, pgx.ErrNoRows) {
		// строка есть, но условие WHERE не выполнилось — значит владелец другой
		return TVRoom{}, ErrConflict
	}
	return r, err
}

// TVRoomOwner — чья комната. Приставка не авторизована, поэтому подключиться к
// уже закреплённой комнате она может только под тем же ключом.
func (s *Store) TVRoomOwner(ctx context.Context, key string) (int64, error) {
	var userID int64
	err := s.pool.QueryRow(ctx,
		`SELECT user_id FROM tv_rooms WHERE key = $1`,
		strings.TrimSpace(strings.ToLower(key))).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return userID, err
}

func (s *Store) ListTVRooms(ctx context.Context, userID int64) ([]TVRoom, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT key, label, created_at, last_seen_at FROM tv_rooms
		WHERE user_id = $1 ORDER BY last_seen_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[TVRoom])
}

func (s *Store) DeleteTVRoom(ctx context.Context, userID int64, key string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM tv_rooms WHERE user_id = $1 AND key = $2`, userID, key)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
