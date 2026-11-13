package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// clampDom ограничивает день месяца числом дней в этом месяце.
func clampDom(year int, month time.Month, dom int) int {
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if dom > last {
		return last
	}
	if dom < 1 {
		return 1
	}
	return dom
}

// nextResetAt вычисляет следующий момент сброса (UTC) по расписанию. tzOff —
// смещение часового пояса в минутах к востоку от UTC. Возвращает (t, true) или
// (zero, false) для period=="none".
func nextResetAt(period string, minute, dow, dom, tzOff int, from time.Time) (time.Time, bool) {
	if period == "none" || period == "" {
		return time.Time{}, false
	}
	loc := time.FixedZone("tz", tzOff*60)
	local := from.In(loc)
	y, m, d := local.Date()
	h, mi := minute/60, minute%60

	switch period {
	case "daily":
		cand := time.Date(y, m, d, h, mi, 0, 0, loc)
		if !cand.After(local) {
			cand = cand.AddDate(0, 0, 1)
		}
		return cand.UTC(), true
	case "weekly":
		cand := time.Date(y, m, d, h, mi, 0, 0, loc)
		for cand.Weekday() != time.Weekday(dow%7) || !cand.After(local) {
			cand = cand.AddDate(0, 0, 1)
		}
		return cand.UTC(), true
	case "monthly":
		cand := time.Date(y, m, clampDom(y, m, dom), h, mi, 0, 0, loc)
		if !cand.After(local) {
			ny, nm := y, m+1
			if nm > 12 {
				ny, nm = y+1, 1
			}
			cand = time.Date(ny, nm, clampDom(ny, nm, dom), h, mi, 0, 0, loc)
		}
		return cand.UTC(), true
	}
	return time.Time{}, false
}

// SetCheckerRecurrence задаёт расписание сброса группы верхнего уровня (владелец).
func (s *Store) SetCheckerRecurrence(ctx context.Context, userID, groupID int64, period string, minute, dow, dom, tzOff int) (CheckGroup, error) {
	var next *time.Time
	if n, ok := nextResetAt(period, minute, dow, dom, tzOff, time.Now().UTC()); ok {
		next = &n
	}
	g := CheckGroup{Items: []CheckItem{}}
	err := s.pool.QueryRow(ctx, `
		UPDATE checker_groups
		SET reset_period = $3, reset_minute = $4, reset_dow = $5, reset_dom = $6,
		    reset_tz_off = $7, next_reset_at = $8
		WHERE id = $1 AND user_id = $2 AND parent_id IS NULL AND deleted_at IS NULL
		RETURNING id, parent_id, name, position, hide_done,
		          reset_period, reset_minute, reset_dow, reset_dom, reset_tz_off`,
		groupID, userID, period, minute, dow, dom, tzOff, next).
		Scan(&g.ID, &g.ParentID, &g.Name, &g.Position, &g.HideDone,
			&g.ResetPeriod, &g.ResetMinute, &g.ResetDow, &g.ResetDom, &g.ResetTzOff)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, ErrNotFound
	}
	g.Mine = true
	return g, err
}

// --- снимки состояния (day-history) ---

type snapItem struct {
	Name string `json:"name"`
	Done bool   `json:"done"`
}
type snapNode struct {
	Name      string     `json:"name"`
	Items     []snapItem `json:"items"`
	Subgroups []snapNode `json:"subgroups"`
}

func snapshotFromNode(n groupFullNode) (snapNode, int, int) {
	out := snapNode{Name: n.name, Items: []snapItem{}, Subgroups: []snapNode{}}
	done, total := 0, 0
	for _, it := range n.items {
		out.Items = append(out.Items, snapItem{Name: it.Name, Done: it.Done})
		total++
		if it.Done {
			done++
		}
	}
	for _, sub := range n.subs {
		sn, sd, st := snapshotFromNode(sub)
		out.Subgroups = append(out.Subgroups, sn)
		done += sd
		total += st
	}
	return out, done, total
}

// snapshotAndReset сохраняет снимок состояния списка на день day (локальная дата)
// и снимает все отметки в поддереве. Возвращает (done, total).
func (s *Store) snapshotAndReset(ctx context.Context, rootID int64, day string) error {
	node, err := s.groupFullSnapshot(ctx, rootID)
	if err != nil {
		return err
	}
	data, done, total := snapshotFromNode(node)
	buf, _ := json.Marshal(data)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO checker_snapshots (root_id, day, done, total, data)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (root_id, day) DO UPDATE
		SET done = EXCLUDED.done, total = EXCLUDED.total, data = EXCLUDED.data, created_at = now()`,
		rootID, day, done, total, buf); err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		WITH RECURSIVE t AS (
			SELECT id FROM checker_groups WHERE id = $1
			UNION ALL SELECT c.id FROM checker_groups c JOIN t ON c.parent_id = t.id
		)
		UPDATE checker_items SET done = false, in_progress = false
		WHERE group_id IN (SELECT id FROM t)`, rootID)
	return err
}

// ManualResetChecker — ручной сброс списка (владелец или участник): снимок + снятие.
func (s *Store) ManualResetChecker(ctx context.Context, userID, groupID int64) error {
	if ok, err := s.checkerAccess(ctx, userID, groupID); err != nil {
		return err
	} else if !ok {
		return ErrNotFound
	}
	// корень + смещение TZ
	var rootID int64
	var tzOff int
	err := s.pool.QueryRow(ctx, `
		WITH RECURSIVE up AS (
			SELECT id, parent_id, reset_tz_off FROM checker_groups WHERE id = $1
			UNION ALL SELECT g.id, g.parent_id, g.reset_tz_off FROM checker_groups g JOIN up ON g.id = up.parent_id
		)
		SELECT id, reset_tz_off FROM up WHERE parent_id IS NULL LIMIT 1`, groupID).Scan(&rootID, &tzOff)
	if err != nil {
		return err
	}
	day := time.Now().UTC().In(time.FixedZone("tz", tzOff*60)).Format("2006-01-02")
	if err := s.snapshotAndReset(ctx, rootID, day); err != nil {
		return err
	}
	s.logCheckerHistory(ctx, userID, rootID, "сбросил список")
	return nil
}

// DueReset — список, которому пора сброситься.
type DueReset struct {
	RootID  int64
	OwnerID int64
	Period  string
	Minute  int
	Dow     int
	Dom     int
	TzOff   int
}

func (s *Store) DueCheckerResets(ctx context.Context, now time.Time, limit int) ([]DueReset, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, reset_period, reset_minute, reset_dow, reset_dom, reset_tz_off
		FROM checker_groups
		WHERE parent_id IS NULL AND deleted_at IS NULL AND reset_period <> 'none'
		  AND next_reset_at IS NOT NULL AND next_reset_at <= $1
		ORDER BY next_reset_at LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[DueReset])
}

// RunCheckerReset выполняет сброс due-списка: снимок на локальную дату + снятие
// отметок + вычисление следующего срабатывания.
func (s *Store) RunCheckerReset(ctx context.Context, d DueReset, now time.Time) error {
	day := now.In(time.FixedZone("tz", d.TzOff*60)).Format("2006-01-02")
	if err := s.snapshotAndReset(ctx, d.RootID, day); err != nil {
		return err
	}
	var next *time.Time
	if n, ok := nextResetAt(d.Period, d.Minute, d.Dow, d.Dom, d.TzOff, now); ok {
		next = &n
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE checker_groups SET next_reset_at = $2 WHERE id = $1`, d.RootID, next); err != nil {
		return err
	}
	s.logCheckerHistory(ctx, d.OwnerID, d.RootID, "сброс списка (по расписанию)")
	return nil
}

// SnapshotDay — запись календаря (день + прогресс).
type SnapshotDay struct {
	Day   string `json:"day"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

// ListCheckerSnapshots — дни со снимками (для календаря; доступ владельцу/участнику).
func (s *Store) ListCheckerSnapshots(ctx context.Context, userID, groupID int64) ([]SnapshotDay, error) {
	if ok, err := s.checkerAccess(ctx, userID, groupID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		WITH RECURSIVE up AS (
			SELECT id, parent_id FROM checker_groups WHERE id = $1
			UNION ALL SELECT g.id, g.parent_id FROM checker_groups g JOIN up ON g.id = up.parent_id
		), root AS (SELECT id FROM up WHERE parent_id IS NULL LIMIT 1)
		SELECT to_char(day, 'YYYY-MM-DD'), done, total
		FROM checker_snapshots WHERE root_id = (SELECT id FROM root)
		ORDER BY day DESC LIMIT 400`, groupID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[SnapshotDay])
	if out == nil {
		out = []SnapshotDay{}
	}
	return out, err
}

// GetCheckerSnapshot — снимок конкретного дня (JSON-дерево).
func (s *Store) GetCheckerSnapshot(ctx context.Context, userID, groupID int64, day string) (json.RawMessage, error) {
	if ok, err := s.checkerAccess(ctx, userID, groupID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNotFound
	}
	var data json.RawMessage
	err := s.pool.QueryRow(ctx, `
		WITH RECURSIVE up AS (
			SELECT id, parent_id FROM checker_groups WHERE id = $1
			UNION ALL SELECT g.id, g.parent_id FROM checker_groups g JOIN up ON g.id = up.parent_id
		), root AS (SELECT id FROM up WHERE parent_id IS NULL LIMIT 1)
		SELECT data FROM checker_snapshots
		WHERE root_id = (SELECT id FROM root) AND day = $2`, groupID, day).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return data, err
}
