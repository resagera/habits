package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// NotifyPrefs — что бот присылает и когда молчит.
//
// Виды (Kind) знает приложение, сервер их не перечисляет: новая рассылка не
// должна требовать миграции. Off хранит выключенные — по умолчанию включено
// всё, и добавленный вид сразу работает.
type NotifyPrefs struct {
	QuietEnabled bool     `json:"quiet_enabled"`
	QuietFrom    int      `json:"quiet_from"` // минуты от полуночи по местному времени
	QuietTo      int      `json:"quiet_to"`
	TzOff        int      `json:"tz_off"` // минуты от UTC (часовой пояс устройства)
	Off          []string `json:"off"`
}

func defaultNotifyPrefs() NotifyPrefs {
	return NotifyPrefs{QuietFrom: 23 * 60, QuietTo: 8 * 60, Off: []string{}}
}

func (s *Store) GetNotifyPrefs(ctx context.Context, userID int64) (NotifyPrefs, error) {
	p := defaultNotifyPrefs()
	var raw []byte
	err := s.pool.QueryRow(ctx,
		`SELECT notify_prefs FROM user_settings WHERE user_id = $1`, userID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return defaultNotifyPrefs(), nil // битое значение не должно глушить уведомления
		}
	}
	if p.Off == nil {
		p.Off = []string{}
	}
	return p, nil
}

func (s *Store) SetNotifyPrefs(ctx context.Context, userID int64, p NotifyPrefs) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO user_settings (user_id, notify_prefs) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET notify_prefs = EXCLUDED.notify_prefs, updated_at = now()`,
		userID, raw)
	return err
}

// Muted — выключен ли этот вид уведомлений.
func (p NotifyPrefs) Muted(kind string) bool {
	for _, k := range p.Off {
		if k == kind {
			return true
		}
	}
	return false
}

// QuietUntil — до какого момента длится тишина, если она сейчас идёт.
// Второе значение false — тишины нет, можно слать.
//
// Окно задаётся в местном времени пользователя (TzOff), и обычно оно
// переходит через полночь (23:00–8:00) — поэтому два случая.
func (p NotifyPrefs) QuietUntil(now time.Time) (time.Time, bool) {
	if !p.QuietEnabled || p.QuietFrom == p.QuietTo {
		return time.Time{}, false
	}
	local := now.UTC().Add(time.Duration(p.TzOff) * time.Minute)
	minutes := local.Hour()*60 + local.Minute()

	quiet := false
	if p.QuietFrom < p.QuietTo {
		quiet = minutes >= p.QuietFrom && minutes < p.QuietTo
	} else {
		quiet = minutes >= p.QuietFrom || minutes < p.QuietTo
	}
	if !quiet {
		return time.Time{}, false
	}

	end := time.Date(local.Year(), local.Month(), local.Day(), p.QuietTo/60, p.QuietTo%60, 0, 0, time.UTC)
	if !end.After(local) {
		end = end.AddDate(0, 0, 1) // конец тишины уже за полночь
	}
	return end.Add(-time.Duration(p.TzOff) * time.Minute), true
}

// --- очередь отложенных сообщений ---

type QueuedNotification struct {
	ID     int64
	UserID int64
	Text   string
}

func (s *Store) QueueNotification(ctx context.Context, userID int64, kind, text string, after time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO notify_queue (user_id, kind, text, send_after) VALUES ($1, $2, $3, $4)`,
		userID, kind, text, after)
	return err
}

func (s *Store) DueNotifications(ctx context.Context, now time.Time, limit int) ([]QueuedNotification, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, text FROM notify_queue
		WHERE sent_at IS NULL AND send_after <= $1
		ORDER BY send_after LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[QueuedNotification])
}

// MarkNotificationSent помечает отправленным (и удаляет старое: очередь —
// не архив, храним неделю на случай разбора).
func (s *Store) MarkNotificationSent(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE notify_queue SET sent_at = now() WHERE id = $1`, id)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		DELETE FROM notify_queue WHERE sent_at < now() - interval '7 days'`)
	return err
}
