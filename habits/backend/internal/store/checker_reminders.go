package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// SetItemReminder задаёт/снимает напоминание у пункта (доступ: владелец/участник).
// remindAt=nil — снять. При установке сбрасывает reminded_at (отправится заново).
func (s *Store) SetItemReminder(ctx context.Context, userID, itemID int64, remindAt *time.Time) error {
	var groupID int64
	err := s.pool.QueryRow(ctx, `SELECT group_id FROM checker_items WHERE id = $1`, itemID).Scan(&groupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if ok, err := s.checkerAccess(ctx, userID, groupID); err != nil {
		return err
	} else if !ok {
		return ErrNotFound
	}
	_, err = s.pool.Exec(ctx,
		`UPDATE checker_items SET remind_at = $2, reminded_at = NULL WHERE id = $1`, itemID, remindAt)
	return err
}

// SetGroupReminder задаёт/снимает напоминание о списке (только владелец).
func (s *Store) SetGroupReminder(ctx context.Context, userID, groupID int64, remindAt *time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE checker_groups SET remind_at = $3, reminded_at = NULL
		WHERE id = $1 AND user_id = $2 AND parent_id IS NULL AND deleted_at IS NULL`,
		groupID, userID, remindAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DueCheckerReminder — наступившее напоминание (пункт или список).
type DueCheckerReminder struct {
	Kind    string // "item" | "group"
	ID      int64
	GroupID int64  // для item — его группа; для group — сама группа (корень)
	Name    string // имя пункта или списка
}

// DueCheckerReminders — напоминания к отправке (remind_at<=now, ещё не отправлены,
// пункт не выполнен, список не в корзине).
func (s *Store) DueCheckerReminders(ctx context.Context, now time.Time, limit int) ([]DueCheckerReminder, error) {
	rows, err := s.pool.Query(ctx, `
		(SELECT 'item'::text, i.id, i.group_id, i.name
		 FROM checker_items i JOIN checker_groups g ON g.id = i.group_id
		 WHERE i.remind_at IS NOT NULL AND i.reminded_at IS NULL AND i.remind_at <= $1
		   AND NOT i.done AND g.deleted_at IS NULL)
		UNION ALL
		(SELECT 'group'::text, g.id, g.id, g.name
		 FROM checker_groups g
		 WHERE g.parent_id IS NULL AND g.deleted_at IS NULL
		   AND g.remind_at IS NOT NULL AND g.reminded_at IS NULL AND g.remind_at <= $1)
		LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[DueCheckerReminder])
}

// MarkCheckerReminded помечает напоминание отправленным.
func (s *Store) MarkCheckerReminded(ctx context.Context, kind string, id int64, at time.Time) error {
	table := "checker_items"
	if kind == "group" {
		table = "checker_groups"
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE `+table+` SET reminded_at = $2 WHERE id = $1`, id, at)
	return err
}

// CheckerReminderTargets — кому слать: владелец корня + участники; плюс имя списка.
func (s *Store) CheckerReminderTargets(ctx context.Context, groupID int64) ([]int64, string, error) {
	var rootID, ownerID int64
	var listName string
	err := s.pool.QueryRow(ctx, `
		WITH RECURSIVE up AS (
			SELECT id, parent_id, user_id, name FROM checker_groups WHERE id = $1
			UNION ALL SELECT g.id, g.parent_id, g.user_id, g.name FROM checker_groups g JOIN up ON g.id = up.parent_id
		)
		SELECT id, user_id, name FROM up WHERE parent_id IS NULL LIMIT 1`, groupID).Scan(&rootID, &ownerID, &listName)
	if err != nil {
		return nil, "", err
	}
	ids := []int64{ownerID}
	rows, err := s.pool.Query(ctx, `SELECT user_id FROM checker_shares WHERE group_id = $1`, rootID)
	if err != nil {
		return ids, listName, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return ids, listName, err
		}
		ids = append(ids, uid)
	}
	return ids, listName, rows.Err()
}
