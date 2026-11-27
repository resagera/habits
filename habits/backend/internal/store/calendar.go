package store

// Календарь — агрегатор по дням поверх существующих страниц: трекеры,
// напоминания (раскладка повторов по дням), дневник, задачи (сроки),
// чек-листы (снапшоты повторяющихся + дедлайны), еда (ккал против цели),
// AI-расписания. Даты дня — строки YYYY-MM-DD в таймзоне клиента (tz_off).

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

type CalendarReminder struct {
	Day   string `json:"day"`
	Time  string `json:"time"` // HH:MM или ''
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

type CalendarDiary struct {
	Day     string `json:"day"`
	Time    string `json:"time"`
	ID      int64  `json:"id"`
	Snippet string `json:"snippet"`
}

type CalendarTask struct {
	Day   string `json:"day"`
	Time  string `json:"time"`
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

type CalendarCheckerDay struct {
	Day    string `json:"day"`
	RootID int64  `json:"root_id"`
	Name   string `json:"name"`
	Done   int32  `json:"done"`
	Total  int32  `json:"total"`
}

type CalendarDeadline struct {
	Day     string `json:"day"`
	Time    string `json:"time"`
	Title   string `json:"title"`
	GroupID int64  `json:"group_id"`
}

type CalendarFoodDay struct {
	Day      string  `json:"day"`
	Meals    int64   `json:"meals"`
	Kcal     float64 `json:"kcal"`
	GoalKcal float64 `json:"goal_kcal"`
}

type CalendarAIRun struct {
	Day    string `json:"day"`
	Time   string `json:"time"`
	ID     int64  `json:"id"`
	Prompt string `json:"prompt"`
}

func dayStr(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02")
}

func timeStr(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("15:04")
}

// CalendarReminders раскладывает включённые напоминания по дням диапазона.
// Интервальные (каждые N минут) показываются только в день next_fire_at —
// иначе они заспамили бы сетку.
func (s *Store) CalendarReminders(ctx context.Context, userID int64, from, to time.Time, loc *time.Location) ([]CalendarReminder, error) {
	reminders, err := s.ListReminders(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := []CalendarReminder{}
	for _, r := range reminders {
		if !r.Enabled {
			continue
		}
		tod := ""
		if r.TimeOfDay != nil {
			tod = *r.TimeOfDay
		}
		add := func(day string) {
			out = append(out, CalendarReminder{Day: day, Time: tod, ID: r.ID, Title: r.Title})
		}
		switch r.Kind {
		case "once":
			if r.At != nil && !r.At.Before(from) && r.At.Before(to.AddDate(0, 0, 1)) {
				out = append(out, CalendarReminder{Day: dayStr(*r.At, loc), Time: timeStr(*r.At, loc), ID: r.ID, Title: r.Title})
			}
		case "interval":
			if r.NextFireAt != nil && !r.NextFireAt.Before(from) && r.NextFireAt.Before(to.AddDate(0, 0, 1)) {
				out = append(out, CalendarReminder{Day: dayStr(*r.NextFireAt, loc), Time: timeStr(*r.NextFireAt, loc), ID: r.ID, Title: r.Title + " (интервал)"})
			}
		default: // daily | weekly | monthly | yearly — обход дней диапазона
			for d := from; d.Before(to.AddDate(0, 0, 1)); d = d.AddDate(0, 0, 1) {
				ld := d.In(loc)
				hit := false
				switch r.Kind {
				case "daily":
					hit = true
				case "weekly":
					mask := r.DaysMask
					if mask == 0 {
						mask = 127
					}
					hit = mask&(1<<mondayIndex(ld.Weekday())) != 0
				case "monthly":
					if r.DayOfMonth != nil {
						dom := int(*r.DayOfMonth)
						last := time.Date(ld.Year(), ld.Month()+1, 0, 0, 0, 0, 0, loc).Day()
						if dom > last {
							dom = last // 31-е в коротком месяце → последний день
						}
						hit = ld.Day() == dom
					}
				case "yearly":
					if r.DayOfMonth != nil && r.Month != nil {
						hit = int(ld.Month()) == int(*r.Month) && ld.Day() == int(*r.DayOfMonth)
					}
				}
				if hit {
					add(ld.Format("2006-01-02"))
				}
			}
		}
	}
	return out, nil
}

// CalendarDiary — записи дневника по дням (день считается в таймзоне клиента).
func (s *Store) CalendarDiary(ctx context.Context, userID int64, from, to time.Time, loc *time.Location) ([]CalendarDiary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, at, left(text, 120) FROM diary_entries
		WHERE user_id = $1 AND at >= $2 AND at < $3
		ORDER BY at`, userID, from.Add(-14*time.Hour), to.AddDate(0, 0, 1).Add(14*time.Hour))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CalendarDiary{}
	fromDay, toDay := from.In(loc).Format("2006-01-02"), to.In(loc).Format("2006-01-02")
	for rows.Next() {
		var id int64
		var at time.Time
		var snippet string
		if err := rows.Scan(&id, &at, &snippet); err != nil {
			return nil, err
		}
		day := dayStr(at, loc)
		if day < fromDay || day > toDay {
			continue // запас по краям из-за таймзоны
		}
		out = append(out, CalendarDiary{Day: day, Time: timeStr(at, loc), ID: id, Snippet: snippet})
	}
	return out, rows.Err()
}

// CalendarTasks — задачи со сроком в диапазоне (свои).
func (s *Store) CalendarTasks(ctx context.Context, userID int64, from, to string) ([]CalendarTask, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT to_char(due_date, 'YYYY-MM-DD'), COALESCE(to_char(due_time, 'HH24:MI'), ''), id, title,
		       (status_kind = 'done')
		FROM tasks
		WHERE user_id = $1 AND due_date BETWEEN $2::date AND $3::date
		ORDER BY due_date, due_time NULLS LAST, id`, userID, from, to)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[CalendarTask])
}

// CalendarCheckerDays — снапшоты повторяющихся списков (история выполнения).
func (s *Store) CalendarCheckerDays(ctx context.Context, userID int64, from, to string) ([]CalendarCheckerDay, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT to_char(s.day, 'YYYY-MM-DD'), s.root_id, g.name, s.done, s.total
		FROM checker_snapshots s JOIN checker_groups g ON g.id = s.root_id
		WHERE g.user_id = $1 AND s.day BETWEEN $2::date AND $3::date
		ORDER BY s.day, s.root_id`, userID, from, to)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[CalendarCheckerDay])
}

// CalendarDeadlines — дедлайны чек-листов (remind_at пунктов и списков).
func (s *Store) CalendarDeadlines(ctx context.Context, userID int64, from, to time.Time, loc *time.Location) ([]CalendarDeadline, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT i.remind_at, i.name, i.group_id FROM checker_items i
		JOIN checker_groups g ON g.id = i.group_id
		WHERE g.user_id = $1 AND i.remind_at >= $2 AND i.remind_at < $3 AND NOT i.done
		UNION ALL
		SELECT g.remind_at, g.name || ' (список)', g.id FROM checker_groups g
		WHERE g.user_id = $1 AND g.remind_at >= $2 AND g.remind_at < $3
		ORDER BY 1`, userID, from.Add(-14*time.Hour), to.AddDate(0, 0, 1).Add(14*time.Hour))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CalendarDeadline{}
	fromDay, toDay := from.In(loc).Format("2006-01-02"), to.In(loc).Format("2006-01-02")
	for rows.Next() {
		var at time.Time
		var title string
		var groupID int64
		if err := rows.Scan(&at, &title, &groupID); err != nil {
			return nil, err
		}
		day := dayStr(at, loc)
		if day < fromDay || day > toDay {
			continue
		}
		out = append(out, CalendarDeadline{Day: day, Time: timeStr(at, loc), Title: title, GroupID: groupID})
	}
	return out, rows.Err()
}

// CalendarFood — ккал за день против цели.
func (s *Store) CalendarFood(ctx context.Context, userID int64, from, to string) ([]CalendarFoodDay, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT to_char(m.day, 'YYYY-MM-DD'), count(DISTINCT m.id), COALESCE(SUM(i.calories), 0)
		FROM food_meals m LEFT JOIN food_meal_items i ON i.meal_id = m.id
		WHERE m.user_id = $1 AND m.day BETWEEN $2::date AND $3::date
		GROUP BY m.day ORDER BY m.day`, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CalendarFoodDay{}
	for rows.Next() {
		var d CalendarFoodDay
		if err := rows.Scan(&d.Day, &d.Meals, &d.Kcal); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// цели: действующая на каждый день (последняя по date_from из покрывающих)
	grows, err := s.pool.Query(ctx, `
		SELECT to_char(date_from, 'YYYY-MM-DD'), COALESCE(to_char(date_to, 'YYYY-MM-DD'), ''), calories
		FROM food_goals
		WHERE user_id = $1 AND date_from <= $3::date AND (date_to IS NULL OR date_to >= $2::date)
		ORDER BY date_from`, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer grows.Close()
	type goal struct {
		from, to string
		kcal     float64
	}
	var goals []goal
	for grows.Next() {
		var g goal
		if err := grows.Scan(&g.from, &g.to, &g.kcal); err != nil {
			return nil, err
		}
		goals = append(goals, g)
	}
	for i := range out {
		for _, g := range goals { // отсортированы по date_from — последняя покрывающая побеждает
			if g.from <= out[i].Day && (g.to == "" || g.to >= out[i].Day) {
				out[i].GoalKcal = g.kcal
			}
		}
	}
	return out, grows.Err()
}

// CalendarAIRuns — запланированные запуски AI-расписаний.
func (s *Store) CalendarAIRuns(ctx context.Context, userID int64, from, to time.Time, loc *time.Location) ([]CalendarAIRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT next_run_at, id, left(prompt, 80) FROM ai_schedules
		WHERE user_id = $1 AND enabled AND next_run_at IS NOT NULL
		  AND next_run_at >= $2 AND next_run_at < $3
		ORDER BY next_run_at`, userID, from.Add(-14*time.Hour), to.AddDate(0, 0, 1).Add(14*time.Hour))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CalendarAIRun{}
	fromDay, toDay := from.In(loc).Format("2006-01-02"), to.In(loc).Format("2006-01-02")
	for rows.Next() {
		var at time.Time
		var r CalendarAIRun
		if err := rows.Scan(&at, &r.ID, &r.Prompt); err != nil {
			return nil, err
		}
		r.Day, r.Time = dayStr(at, loc), timeStr(at, loc)
		if r.Day < fromDay || r.Day > toDay {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- настройки слоёв ---

func (s *Store) GetCalendarPrefs(ctx context.Context, userID int64) (json.RawMessage, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT layers FROM calendar_prefs WHERE user_id = $1`, userID).Scan(&raw)
	if err == pgx.ErrNoRows {
		return json.RawMessage(`{}`), nil
	}
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (s *Store) SetCalendarPrefs(ctx context.Context, userID int64, layers json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO calendar_prefs (user_id, layers) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET layers = $2, updated_at = now()`,
		userID, layers)
	return err
}
