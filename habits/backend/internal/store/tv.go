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
	ID         int64     `json:"id"`
	// Shared — приставка чужая, доступ открыл владелец; Owner — его имя.
	Shared bool   `json:"shared"`
	Owner  string `json:"owner,omitempty"`
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
		RETURNING key, label, created_at, last_seen_at, id, false, ''`,
		key, userID, strings.TrimSpace(label))
	if err != nil {
		return TVRoom{}, err
	}
	r, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[TVRoom])
	if errors.Is(err, pgx.ErrNoRows) {
		// строка есть, но условие WHERE не выполнилось — владелец другой.
		// Пускаем, только если он открыл доступ этому аккаунту.
		return s.memberTVRoom(ctx, userID, key)
	}
	return r, err
}

// memberTVRoom — чужая приставка, к которой у userID открыт доступ, или
// ErrConflict, если доступа нет.
func (s *Store) memberTVRoom(ctx context.Context, userID int64, key string) (TVRoom, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.key, r.label, r.created_at, r.last_seen_at, r.id, true,
		       COALESCE(NULLIF(u.first_name, ''), u.username, '')
		FROM tv_rooms r
		JOIN tv_room_members m ON m.room_key = r.key AND m.user_id = $1
		JOIN users u ON u.id = r.user_id
		WHERE r.key = $2`, userID, key)
	if err != nil {
		return TVRoom{}, err
	}
	r, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[TVRoom])
	if errors.Is(err, pgx.ErrNoRows) {
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

// ListTVRooms — свои приставки и те, к которым открыли доступ.
func (s *Store) ListTVRooms(ctx context.Context, userID int64) ([]TVRoom, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT key, label, created_at, last_seen_at, id, false, '' FROM tv_rooms
		WHERE user_id = $1
		UNION ALL
		SELECT r.key, r.label, r.created_at, r.last_seen_at, r.id, true,
		       COALESCE(NULLIF(u.first_name, ''), u.username, '')
		FROM tv_rooms r
		JOIN tv_room_members m ON m.room_key = r.key AND m.user_id = $1
		JOIN users u ON u.id = r.user_id
		ORDER BY 4 DESC`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[TVRoom])
}

// DeleteTVRoom — владелец забывает приставку целиком (вместе с выданными
// доступами), участник — только свой доступ.
func (s *Store) DeleteTVRoom(ctx context.Context, userID int64, key string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM tv_rooms WHERE user_id = $1 AND key = $2`, userID, key)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		tag, err = s.pool.Exec(ctx,
			`DELETE FROM tv_room_members WHERE user_id = $1 AND room_key = $2`, userID, key)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
	}
	return nil
}

// tvRoomTitle — название приставки для шаринга с проверкой владения.
func (s *Store) tvRoomTitle(ctx context.Context, ownerID, roomID int64) (string, error) {
	var key, label string
	err := s.pool.QueryRow(ctx,
		`SELECT key, label FROM tv_rooms WHERE id = $1 AND user_id = $2`, roomID, ownerID).Scan(&key, &label)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if label == "" {
		label = key
	}
	return label, err
}

// ShareTVRoom — открыть доступ к приставке. Страница «Пульт ТВ» у всех,
// кроме админа, открывается по выдаче, поэтому выдаём и её: без неё доступ
// к приставке нечем было бы использовать.
func (s *Store) ShareTVRoom(ctx context.Context, ownerID, roomID, recipientID int64) (string, error) {
	title, err := s.tvRoomTitle(ctx, ownerID, roomID)
	if err != nil {
		return "", err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO tv_room_members (room_key, user_id)
		SELECT key, $2 FROM tv_rooms WHERE id = $1 ON CONFLICT DO NOTHING`, roomID, recipientID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO page_access (page, user_id) VALUES ('tv', $1) ON CONFLICT DO NOTHING`, recipientID); err != nil {
		return "", err
	}
	return title, tx.Commit(ctx)
}

// ListTVRoomMembers — кому владелец открыл приставку.
func (s *Store) ListTVRoomMembers(ctx context.Context, ownerID int64, key string) ([]AccessUser, error) {
	var ok bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM tv_rooms WHERE key = $1 AND user_id = $2)`, key, ownerID).Scan(&ok); err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, COALESCE(u.username, ''), COALESCE(u.first_name, '')
		FROM tv_room_members m JOIN users u ON u.id = m.user_id
		WHERE m.room_key = $1 ORDER BY m.created_at`, key)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[AccessUser])
}

// RevokeTVRoomMember — владелец закрывает доступ. Выданную страницу не
// отбираем: её мог выдать и админ, а без приставок она просто пуста.
func (s *Store) RevokeTVRoomMember(ctx context.Context, ownerID int64, key string, memberID int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM tv_room_members m USING tv_rooms r
		WHERE m.room_key = r.key AND r.key = $1 AND r.user_id = $2 AND m.user_id = $3`, key, ownerID, memberID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
