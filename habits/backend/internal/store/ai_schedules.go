package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// AISchedule — периодический запуск задачи для coding-агента.
type AISchedule struct {
	ID         int64      `json:"id"`
	MachineID  int64      `json:"machine_id"`
	Tool       string     `json:"tool"`
	Workdir    string     `json:"workdir"`
	Model      string     `json:"model"`
	Params     string     `json:"params"`
	Prompt     string     `json:"prompt"`
	Period     string     `json:"period"` // daily | weekly | hours
	AtMinute   int32      `json:"at_minute"`
	Dow        int32      `json:"dow"`
	EveryHours int32      `json:"every_hours"`
	TzOff      int32      `json:"tz_off"`
	Enabled    bool       `json:"enabled"`
	NextRunAt  *time.Time `json:"next_run_at,omitempty"`
	LastTaskID *int64     `json:"last_task_id,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// NextAIScheduleRun — следующий запуск после from. daily/weekly — по локальному
// времени (tzOff в минутах, как checker_recurring); hours — просто from+N часов.
func NextAIScheduleRun(period string, atMinute, dow, everyHours, tzOff int, from time.Time) (time.Time, bool) {
	switch period {
	case "hours":
		if everyHours < 1 {
			return time.Time{}, false
		}
		return from.Add(time.Duration(everyHours) * time.Hour).UTC(), true
	case "daily", "weekly":
		loc := time.FixedZone("tz", tzOff*60)
		local := from.In(loc)
		y, m, d := local.Date()
		h, mi := atMinute/60, atMinute%60
		cand := time.Date(y, m, d, h, mi, 0, 0, loc)
		if period == "daily" {
			if !cand.After(local) {
				cand = cand.AddDate(0, 0, 1)
			}
			return cand.UTC(), true
		}
		for cand.Weekday() != time.Weekday(dow%7) || !cand.After(local) {
			cand = cand.AddDate(0, 0, 1)
		}
		return cand.UTC(), true
	}
	return time.Time{}, false
}

const aiScheduleCols = `id, machine_id, tool, workdir, model, params, prompt, period, at_minute, dow, every_hours, tz_off, enabled, next_run_at, last_task_id, created_at`

func (s *Store) ListAISchedules(ctx context.Context, userID int64) ([]AISchedule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+aiScheduleCols+` FROM ai_schedules
		WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[AISchedule])
}

func (s *Store) CreateAISchedule(ctx context.Context, userID int64, sc AISchedule) (AISchedule, error) {
	var next *time.Time
	if n, ok := NextAIScheduleRun(sc.Period, int(sc.AtMinute), int(sc.Dow), int(sc.EveryHours), int(sc.TzOff), time.Now().UTC()); ok {
		next = &n
	}
	rows, err := s.pool.Query(ctx, `
		INSERT INTO ai_schedules (user_id, machine_id, tool, workdir, model, params, prompt,
			period, at_minute, dow, every_hours, tz_off, enabled, next_run_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,TRUE,$13)
		RETURNING `+aiScheduleCols,
		userID, sc.MachineID, sc.Tool, sc.Workdir, sc.Model, sc.Params, sc.Prompt,
		sc.Period, sc.AtMinute, sc.Dow, sc.EveryHours, sc.TzOff, next)
	if err != nil {
		return AISchedule{}, err
	}
	return pgx.CollectOneRow(rows, pgx.RowToStructByPos[AISchedule])
}

// UpdateAISchedule — правка полей + пересчёт next_run_at (и при включении).
func (s *Store) UpdateAISchedule(ctx context.Context, userID, id int64, sc AISchedule) (AISchedule, error) {
	var next *time.Time
	if sc.Enabled {
		if n, ok := NextAIScheduleRun(sc.Period, int(sc.AtMinute), int(sc.Dow), int(sc.EveryHours), int(sc.TzOff), time.Now().UTC()); ok {
			next = &n
		}
	}
	rows, err := s.pool.Query(ctx, `
		UPDATE ai_schedules SET tool=$3, workdir=$4, model=$5, params=$6, prompt=$7,
			period=$8, at_minute=$9, dow=$10, every_hours=$11, tz_off=$12,
			enabled=$13, next_run_at=$14
		WHERE id = $2 AND user_id = $1
		RETURNING `+aiScheduleCols,
		userID, id, sc.Tool, sc.Workdir, sc.Model, sc.Params, sc.Prompt,
		sc.Period, sc.AtMinute, sc.Dow, sc.EveryHours, sc.TzOff, sc.Enabled, next)
	if err != nil {
		return AISchedule{}, err
	}
	out, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[AISchedule])
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, err
}

func (s *Store) DeleteAISchedule(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM ai_schedules WHERE id = $2 AND user_id = $1`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DueAISchedule — расписание, которому пора запускаться (для воркера).
type DueAISchedule struct {
	ID        int64
	UserID    int64
	MachineID int64
	Tool      string
	Workdir   string
	Model     string
	Params    string
	Prompt    string
}

func (s *Store) DueAISchedules(ctx context.Context, now time.Time) ([]DueAISchedule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, machine_id, tool, workdir, model, params, prompt
		FROM ai_schedules
		WHERE enabled AND next_run_at IS NOT NULL AND next_run_at <= $1
		ORDER BY id`, now)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[DueAISchedule])
}

// AdvanceAISchedule — сдвиг next_run_at после срабатывания + ссылка на задачу.
func (s *Store) AdvanceAISchedule(ctx context.Context, id, taskID int64, now time.Time) error {
	var period string
	var atMinute, dow, everyHours, tzOff int32
	err := s.pool.QueryRow(ctx, `
		SELECT period, at_minute, dow, every_hours, tz_off FROM ai_schedules WHERE id = $1`,
		id).Scan(&period, &atMinute, &dow, &everyHours, &tzOff)
	if err != nil {
		return err
	}
	var next *time.Time
	if n, ok := NextAIScheduleRun(period, int(atMinute), int(dow), int(everyHours), int(tzOff), now); ok {
		next = &n
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE ai_schedules SET next_run_at = $2, last_task_id = $3 WHERE id = $1`,
		id, next, taskID)
	return err
}
