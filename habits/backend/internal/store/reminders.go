package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type Reminder struct {
	ID              int64      `json:"id"`
	Title           string     `json:"title"`
	Note            string     `json:"note"`
	Kind            string     `json:"kind"`
	At              *time.Time `json:"at,omitempty"`
	TimeOfDay       *string    `json:"time_of_day,omitempty"`
	DaysMask        int32      `json:"days_mask"`
	DayOfMonth      *int32     `json:"day_of_month,omitempty"`
	Month           *int32     `json:"month,omitempty"` // для kind=yearly
	IntervalMinutes *int32     `json:"interval_minutes,omitempty"`
	CategoryID      *int64     `json:"category_id,omitempty"`
	GroupID         *int64     `json:"group_id,omitempty"` // категория напоминаний
	TzOffsetMinutes int32      `json:"tz_offset_minutes"`
	Enabled         bool       `json:"enabled"`
	NextFireAt      *time.Time `json:"next_fire_at,omitempty"`
	LastFiredAt     *time.Time `json:"last_fired_at,omitempty"`
}

const reminderCols = `id, title, note, kind, at, time_of_day, days_mask, day_of_month, month,
	interval_minutes, category_id, group_id, tz_offset_minutes, enabled, next_fire_at, last_fired_at`

// NextFire вычисляет следующий момент срабатывания (UTC) после after.
// Возвращает nil, если срабатываний больше не будет (once в прошлом).
func (r *Reminder) NextFire(after time.Time) *time.Time {
	loc := time.FixedZone("user", int(r.TzOffsetMinutes)*60)
	switch r.Kind {
	case "once":
		if r.At != nil && r.At.After(after) {
			t := r.At.UTC()
			return &t
		}
		return nil
	case "interval":
		if r.IntervalMinutes == nil {
			return nil
		}
		t := after.Add(time.Duration(*r.IntervalMinutes) * time.Minute).UTC()
		return &t
	case "daily", "weekly", "tracker":
		return r.nextByDaysMask(after, loc)
	case "monthly":
		return r.nextMonthly(after, loc)
	case "yearly":
		return r.nextYearly(after, loc)
	}
	return nil
}

func (r *Reminder) timeOfDayParts() (hh, mm int, ok bool) {
	if r.TimeOfDay == nil {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(*r.TimeOfDay, "%02d:%02d", &hh, &mm); err != nil {
		return 0, 0, false
	}
	return hh, mm, hh < 24
}

// mondayIndex: Пн=0 … Вс=6 (в days_mask бит 0 — понедельник).
func mondayIndex(d time.Weekday) int {
	return (int(d) + 6) % 7
}

func (r *Reminder) nextByDaysMask(after time.Time, loc *time.Location) *time.Time {
	hh, mm, ok := r.timeOfDayParts()
	if !ok {
		return nil
	}
	mask := r.DaysMask
	if mask == 0 {
		mask = 127
	}
	local := after.In(loc)
	for i := 0; i < 8; i++ {
		day := local.AddDate(0, 0, i)
		candidate := time.Date(day.Year(), day.Month(), day.Day(), hh, mm, 0, 0, loc)
		if candidate.After(after) && mask&(1<<mondayIndex(candidate.Weekday())) != 0 {
			t := candidate.UTC()
			return &t
		}
	}
	return nil
}

func (r *Reminder) nextMonthly(after time.Time, loc *time.Location) *time.Time {
	hh, mm, ok := r.timeOfDayParts()
	if !ok || r.DayOfMonth == nil {
		return nil
	}
	local := after.In(loc)
	for i := 0; i < 13; i++ {
		first := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, i, 0)
		// если в месяце нет такого числа (31 февраля) — берём последний день
		day := int(*r.DayOfMonth)
		lastDay := first.AddDate(0, 1, -1).Day()
		if day > lastDay {
			day = lastDay
		}
		candidate := time.Date(first.Year(), first.Month(), day, hh, mm, 0, 0, loc)
		if candidate.After(after) {
			t := candidate.UTC()
			return &t
		}
	}
	return nil
}

// nextYearly — ежегодно в месяц/число (праздники, дни рождения);
// 29 февраля в невисокосный год сдвигается на 28-е.
func (r *Reminder) nextYearly(after time.Time, loc *time.Location) *time.Time {
	hh, mm, ok := r.timeOfDayParts()
	if !ok || r.DayOfMonth == nil || r.Month == nil {
		return nil
	}
	local := after.In(loc)
	for i := 0; i < 2; i++ {
		year := local.Year() + i
		first := time.Date(year, time.Month(*r.Month), 1, 0, 0, 0, 0, loc)
		day := int(*r.DayOfMonth)
		if lastDay := first.AddDate(0, 1, -1).Day(); day > lastDay {
			day = lastDay
		}
		candidate := time.Date(year, time.Month(*r.Month), day, hh, mm, 0, 0, loc)
		if candidate.After(after) {
			t := candidate.UTC()
			return &t
		}
	}
	return nil
}

func (s *Store) ListReminders(ctx context.Context, userID int64) ([]Reminder, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+reminderCols+`
		FROM reminders WHERE user_id = $1
		ORDER BY enabled DESC, next_fire_at NULLS LAST, id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Reminder])
}

// UpcomingReminders — ближайшие включённые напоминания для главной страницы.
func (s *Store) UpcomingReminders(ctx context.Context, userID int64, limit int) ([]Reminder, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+reminderCols+`
		FROM reminders
		WHERE user_id = $1 AND enabled AND next_fire_at IS NOT NULL
		ORDER BY next_fire_at
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Reminder])
}

// ownCategory проверяет доступ к категории трекера (владелец или участник).
func (s *Store) ownCategory(ctx context.Context, userID, categoryID int64) error {
	ok, err := s.canAccessCategory(ctx, userID, categoryID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// ownReminderGroup проверяет, что категория напоминаний принадлежит пользователю.
func (s *Store) ownReminderGroup(ctx context.Context, userID, groupID int64) error {
	var owned bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM reminder_categories WHERE id = $1 AND user_id = $2)`,
		groupID, userID).Scan(&owned); err != nil {
		return err
	}
	if !owned {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateReminder(ctx context.Context, userID int64, r Reminder, now time.Time) (Reminder, error) {
	if r.CategoryID != nil {
		if err := s.ownCategory(ctx, userID, *r.CategoryID); err != nil {
			return Reminder{}, err
		}
	}
	if r.GroupID != nil {
		if err := s.ownReminderGroup(ctx, userID, *r.GroupID); err != nil {
			return Reminder{}, err
		}
	}
	next := (&r).NextFire(now)
	var out Reminder
	row := s.pool.QueryRow(ctx, `
		INSERT INTO reminders (user_id, title, note, kind, at, time_of_day, days_mask,
			day_of_month, month, interval_minutes, category_id, group_id,
			tz_offset_minutes, enabled, next_fire_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING `+reminderCols,
		userID, r.Title, r.Note, r.Kind, r.At, r.TimeOfDay, r.DaysMask,
		r.DayOfMonth, r.Month, r.IntervalMinutes, r.CategoryID, r.GroupID,
		r.TzOffsetMinutes, r.Enabled, next)
	if err := scanReminder(row, &out); err != nil {
		return Reminder{}, err
	}
	return out, nil
}

// UpdateReminder полностью заменяет параметры напоминания (кроме id/user_id)
// и пересчитывает next_fire_at.
func (s *Store) UpdateReminder(ctx context.Context, userID, id int64, r Reminder, now time.Time) (Reminder, error) {
	if r.CategoryID != nil {
		if err := s.ownCategory(ctx, userID, *r.CategoryID); err != nil {
			return Reminder{}, err
		}
	}
	if r.GroupID != nil {
		if err := s.ownReminderGroup(ctx, userID, *r.GroupID); err != nil {
			return Reminder{}, err
		}
	}
	next := (&r).NextFire(now)
	var out Reminder
	row := s.pool.QueryRow(ctx, `
		UPDATE reminders
		SET title = $3, note = $4, kind = $5, at = $6, time_of_day = $7, days_mask = $8,
		    day_of_month = $9, month = $10, interval_minutes = $11, category_id = $12,
		    group_id = $13, tz_offset_minutes = $14, enabled = $15, next_fire_at = $16,
		    updated_at = now()
		WHERE id = $1 AND user_id = $2
		RETURNING `+reminderCols,
		id, userID, r.Title, r.Note, r.Kind, r.At, r.TimeOfDay, r.DaysMask,
		r.DayOfMonth, r.Month, r.IntervalMinutes, r.CategoryID, r.GroupID,
		r.TzOffsetMinutes, r.Enabled, next)
	if err := scanReminder(row, &out); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Reminder{}, ErrNotFound
		}
		return Reminder{}, err
	}
	return out, nil
}

// SetReminderEnabled переключает напоминание, пересчитывая next_fire_at при включении.
func (s *Store) SetReminderEnabled(ctx context.Context, userID, id int64, enabled bool, now time.Time) (Reminder, error) {
	var r Reminder
	row := s.pool.QueryRow(ctx, `
		SELECT `+reminderCols+` FROM reminders WHERE id = $1 AND user_id = $2`, id, userID)
	if err := scanReminder(row, &r); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Reminder{}, ErrNotFound
		}
		return Reminder{}, err
	}
	var next *time.Time
	if enabled {
		next = (&r).NextFire(now)
	}
	row = s.pool.QueryRow(ctx, `
		UPDATE reminders SET enabled = $3, next_fire_at = $4, updated_at = now()
		WHERE id = $1 AND user_id = $2
		RETURNING `+reminderCols, id, userID, enabled, next)
	var out Reminder
	if err := scanReminder(row, &out); err != nil {
		return Reminder{}, err
	}
	return out, nil
}

func (s *Store) DeleteReminder(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM reminders WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- воркер ---

// DueReminder — напоминание, готовое к отправке, с данными для сообщения.
type DueReminder struct {
	Reminder
	UserID       int64
	CategoryName string // для kind=tracker
}

// DueReminders возвращает включённые напоминания с наступившим next_fire_at.
func (s *Store) DueReminders(ctx context.Context, now time.Time, limit int) ([]DueReminder, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.title, r.note, r.kind, r.at, r.time_of_day, r.days_mask, r.day_of_month,
		       r.month, r.interval_minutes, r.category_id, r.group_id, r.tz_offset_minutes,
		       r.enabled, r.next_fire_at, r.last_fired_at, r.user_id, COALESCE(c.name, '')
		FROM reminders r
		LEFT JOIN tracker_categories c ON c.id = r.category_id
		WHERE r.enabled AND r.next_fire_at IS NOT NULL AND r.next_fire_at <= $1
		ORDER BY r.next_fire_at
		LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []DueReminder
	for rows.Next() {
		var d DueReminder
		if err := rows.Scan(&d.ID, &d.Title, &d.Note, &d.Kind, &d.At, &d.TimeOfDay,
			&d.DaysMask, &d.DayOfMonth, &d.Month, &d.IntervalMinutes, &d.CategoryID,
			&d.GroupID, &d.TzOffsetMinutes, &d.Enabled, &d.NextFireAt, &d.LastFiredAt,
			&d.UserID, &d.CategoryName); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

// MarkedToday — отмечена ли категория за «сегодня» в локальном времени пользователя.
func (s *Store) MarkedToday(ctx context.Context, categoryID int64, tzOffsetMinutes int32, now time.Time) (bool, error) {
	loc := time.FixedZone("user", int(tzOffsetMinutes)*60)
	day := now.In(loc).Format("2006-01-02")
	var marked bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM tracker_marks WHERE category_id = $1 AND day = $2)`,
		categoryID, day).Scan(&marked)
	return marked, err
}

// AdvanceReminder фиксирует срабатывание: пишет last_fired_at и следующий
// момент; для once (next=nil) выключает напоминание.
func (s *Store) AdvanceReminder(ctx context.Context, id int64, firedAt time.Time, next *time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE reminders
		SET last_fired_at = $2, next_fire_at = $3::timestamptz,
		    enabled = ($3::timestamptz IS NOT NULL), updated_at = now()
		WHERE id = $1`, id, firedAt, next)
	return err
}

func scanReminder(row pgx.Row, r *Reminder) error {
	return row.Scan(&r.ID, &r.Title, &r.Note, &r.Kind, &r.At, &r.TimeOfDay, &r.DaysMask,
		&r.DayOfMonth, &r.Month, &r.IntervalMinutes, &r.CategoryID, &r.GroupID,
		&r.TzOffsetMinutes, &r.Enabled, &r.NextFireAt, &r.LastFiredAt)
}

// --- категории напоминаний ---

type ReminderCategory struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Position int32  `json:"position"`
}

func (s *Store) ListReminderCategories(ctx context.Context, userID int64) ([]ReminderCategory, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, position FROM reminder_categories
		WHERE user_id = $1 ORDER BY position, id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ReminderCategory])
}

func (s *Store) CreateReminderCategory(ctx context.Context, userID int64, name string) (ReminderCategory, error) {
	var c ReminderCategory
	err := s.pool.QueryRow(ctx, `
		INSERT INTO reminder_categories (user_id, name, position)
		VALUES ($1, $2, (SELECT COALESCE(MAX(position) + 1, 0) FROM reminder_categories WHERE user_id = $1))
		RETURNING id, name, position`, userID, name).Scan(&c.ID, &c.Name, &c.Position)
	return c, err
}

func (s *Store) RenameReminderCategory(ctx context.Context, userID, id int64, name string) (ReminderCategory, error) {
	var c ReminderCategory
	err := s.pool.QueryRow(ctx, `
		UPDATE reminder_categories SET name = $3 WHERE id = $1 AND user_id = $2
		RETURNING id, name, position`, id, userID, name).Scan(&c.ID, &c.Name, &c.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// DeleteReminderCategory удаляет категорию; напоминания остаются
// (group_id обнуляется по FK ON DELETE SET NULL).
func (s *Store) DeleteReminderCategory(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM reminder_categories WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- шаринг и импорт категории напоминаний ---

// remindersInCategory возвращает напоминания категории (без служебных полей —
// для копирования/экспорта). Напоминания kind=tracker пропускаются: они
// завязаны на привычки Tracker получателя.
func (s *Store) remindersInCategory(ctx context.Context, categoryID int64) ([]Reminder, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+reminderCols+` FROM reminders
		WHERE group_id = $1 AND kind <> 'tracker' ORDER BY id`, categoryID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Reminder])
}

// EnsureReminderCategoryShareToken выдаёт (или возвращает) токен-приглашение.
func (s *Store) EnsureReminderCategoryShareToken(ctx context.Context, userID, id int64) (string, error) {
	var token *string
	err := s.pool.QueryRow(ctx, `
		SELECT share_token FROM reminder_categories WHERE id = $1 AND user_id = $2`,
		id, userID).Scan(&token)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if token != nil {
		return *token, nil
	}
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	fresh := hex.EncodeToString(buf)
	_, err = s.pool.Exec(ctx, `
		UPDATE reminder_categories SET share_token = $3 WHERE id = $1 AND user_id = $2`,
		id, userID, fresh)
	return fresh, err
}

// ImportReminderCategory создаёт категорию и её напоминания у пользователя.
// Напоминания пересобираются с новым group_id и пересчётом next_fire_at.
func (s *Store) ImportReminderCategory(ctx context.Context, userID int64, name string, rems []Reminder, now time.Time) (ReminderCategory, error) {
	cat, err := s.CreateReminderCategory(ctx, userID, name)
	if err != nil {
		return ReminderCategory{}, err
	}
	for _, r := range rems {
		if r.Kind == "tracker" {
			continue // завязаны на привычки Tracker получателя
		}
		r.GroupID = &cat.ID
		r.CategoryID = nil
		if _, err := s.CreateReminder(ctx, userID, r, now); err != nil {
			return ReminderCategory{}, err
		}
	}
	return cat, nil
}

// copyReminderCategory копирует категорию источника получателю. Возвращает имя.
func (s *Store) copyReminderCategory(ctx context.Context, targetUserID, srcCategoryID int64, now time.Time) (string, error) {
	var name string
	if err := s.pool.QueryRow(ctx, `
		SELECT name FROM reminder_categories WHERE id = $1`, srcCategoryID).Scan(&name); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	rems, err := s.remindersInCategory(ctx, srcCategoryID)
	if err != nil {
		return "", err
	}
	if _, err := s.ImportReminderCategory(ctx, targetUserID, name, rems, now); err != nil {
		return "", err
	}
	return name, nil
}

// CopyReminderCategoryTo копирует категорию владельца получателю (send).
func (s *Store) CopyReminderCategoryTo(ctx context.Context, ownerID, categoryID, recipientID int64, now time.Time) (string, error) {
	if err := s.ownReminderGroup(ctx, ownerID, categoryID); err != nil {
		return "", err
	}
	return s.copyReminderCategory(ctx, recipientID, categoryID, now)
}

// RedeemReminderCategoryShareToken копирует категорию по токену-приглашению.
func (s *Store) RedeemReminderCategoryShareToken(ctx context.Context, userID int64, token string, now time.Time) (string, error) {
	var categoryID int64
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM reminder_categories WHERE share_token = $1`, token).Scan(&categoryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return s.copyReminderCategory(ctx, userID, categoryID, now)
}
