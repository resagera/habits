package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Server struct {
	ID        int64           `json:"id"`
	Kind      string          `json:"kind"` // pull | push
	Name      string          `json:"name"`
	URL       string          `json:"url"`
	Token     string          `json:"token,omitempty"`
	PushToken string          `json:"push_token,omitempty"`
	LastOkAt  *time.Time      `json:"last_ok_at,omitempty"`
	LastError string          `json:"last_error"`
	LastData  json.RawMessage `json:"last_data,omitempty"`
	Alerts    ServerAlerts    `json:"alerts"`
}

// ServerAlerts — пороги уведомлений по серверу.
type ServerAlerts struct {
	Enabled       bool       `json:"enabled"`
	DiskMinFreeMB int64      `json:"disk_min_free_mb"` // порог по умолчанию (для UI)
	DiskRules     []DiskRule `json:"disk_rules"`       // пороги по конкретным дискам
	RAMPct        int16      `json:"ram_pct"`
	RAMMinutes    int16      `json:"ram_minutes"`
	CPUPct        int16      `json:"cpu_pct"`
	CPUMinutes    int16      `json:"cpu_minutes"`
}

// DiskRule — порог свободного места для конкретной точки монтирования.
type DiskRule struct {
	Mount     string `json:"mount"`
	MinFreeMB int64  `json:"min_free_mb"`
}

// scanner — общий интерфейс pgx.Row / pgx.CollectableRow.
type scanner interface{ Scan(dest ...any) error }

func scanServer(row scanner) (Server, error) {
	var s Server
	var diskRules []byte
	err := row.Scan(&s.ID, &s.Kind, &s.Name, &s.URL, &s.Token, &s.PushToken,
		&s.LastOkAt, &s.LastError, &s.LastData,
		&s.Alerts.Enabled, &s.Alerts.DiskMinFreeMB, &s.Alerts.RAMPct,
		&s.Alerts.RAMMinutes, &s.Alerts.CPUPct, &s.Alerts.CPUMinutes, &diskRules)
	if err != nil {
		return s, err
	}
	if len(diskRules) > 0 {
		_ = json.Unmarshal(diskRules, &s.Alerts.DiskRules)
	}
	if s.Alerts.DiskRules == nil {
		s.Alerts.DiskRules = []DiskRule{}
	}
	return s, nil
}

type ServerSample struct {
	At       time.Time `json:"at"`
	CPUPct   float32   `json:"cpu_pct"`
	RAMUsed  int64     `json:"ram_used"`
	RAMTotal int64     `json:"ram_total"`
}

const serverCols = `id, kind, name, COALESCE(url, ''), token, COALESCE(push_token, ''),
	last_ok_at, last_error, last_data,
	alerts_enabled, disk_min_free_mb, ram_pct, ram_minutes, cpu_pct, cpu_minutes, disk_rules`

func (s *Store) ListServers(ctx context.Context, userID int64) ([]Server, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+serverCols+` FROM servers WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Server, error) {
		return scanServer(row)
	})
}

// CreateServer создаёт pull-сервер (url) или push-машину (pushToken, url NULL).
func (s *Store) CreateServer(ctx context.Context, userID int64, kind, name, url, token, pushToken string) (Server, error) {
	srv, err := scanServer(s.pool.QueryRow(ctx, `
		INSERT INTO servers (user_id, kind, name, url, token, push_token)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, NULLIF($6, ''))
		RETURNING `+serverCols, userID, kind, name, url, token, pushToken))
	return srv, err
}

func (s *Store) UpdateServer(ctx context.Context, userID, id int64, name, url, token string) (Server, error) {
	srv, err := scanServer(s.pool.QueryRow(ctx, `
		UPDATE servers SET name = $3,
			url = CASE WHEN kind = 'pull' THEN NULLIF($4, '') ELSE url END,
			token = CASE WHEN kind = 'pull' THEN $5 ELSE token END
		WHERE id = $1 AND user_id = $2
		RETURNING `+serverCols, id, userID, name, url, token))
	if errors.Is(err, pgx.ErrNoRows) {
		return srv, ErrNotFound
	}
	return srv, err
}

// UpdateServerAlerts обновляет пороги уведомлений и сбрасывает флаги «уже
// уведомили», чтобы новая конфигурация оценивалась с чистого листа.
func (s *Store) UpdateServerAlerts(ctx context.Context, userID, id int64, a ServerAlerts) (Server, error) {
	if a.DiskRules == nil {
		a.DiskRules = []DiskRule{}
	}
	rules, err := json.Marshal(a.DiskRules)
	if err != nil {
		return Server{}, err
	}
	srv, err := scanServer(s.pool.QueryRow(ctx, `
		UPDATE servers SET alerts_enabled = $3, disk_min_free_mb = $4, ram_pct = $5,
			ram_minutes = $6, cpu_pct = $7, cpu_minutes = $8, disk_rules = $9,
			disk_alerted = false, ram_alerted = false, cpu_alerted = false,
			disk_alerted_mounts = '[]'
		WHERE id = $1 AND user_id = $2
		RETURNING `+serverCols,
		id, userID, a.Enabled, a.DiskMinFreeMB, a.RAMPct, a.RAMMinutes, a.CPUPct, a.CPUMinutes, rules))
	if errors.Is(err, pgx.ErrNoRows) {
		return srv, ErrNotFound
	}
	return srv, err
}

func (s *Store) DeleteServer(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM servers WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ServerSamples — история для графиков за последние hours часов.
func (s *Store) ServerSamples(ctx context.Context, userID, id int64, hours int) ([]ServerSample, error) {
	var owned bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM servers WHERE id = $1 AND user_id = $2)`,
		id, userID).Scan(&owned); err != nil {
		return nil, err
	}
	if !owned {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `
		SELECT at, cpu_pct, ram_used, ram_total FROM server_samples
		WHERE server_id = $1 AND at > now() - make_interval(hours => $2)
		ORDER BY at`, id, hours)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ServerSample])
}

// --- поллер и push ---

type PollTarget struct {
	ID    int64
	URL   string
	Token string
}

func (s *Store) AllPollTargets(ctx context.Context) ([]PollTarget, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, url, token FROM servers WHERE kind = 'pull' AND url IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[PollTarget])
}

// ServerByPushToken находит push-машину по токену агента.
func (s *Store) ServerByPushToken(ctx context.Context, token string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM servers WHERE kind = 'push' AND push_token = $1`, token).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

// PollOutcome — что произошло при сохранении удачного отчёта: если машина
// была помечена offline, поллер/хендлер шлёт «снова online».
type PollOutcome struct {
	WasOffline bool
	Name       string
	UserID     int64
}

// SavePollResult пишет результат опроса; при ok сохраняет снапшот и сэмпл,
// снимает флаг offline_notified и сообщает, был ли он установлен.
func (s *Store) SavePollResult(ctx context.Context, id int64, data json.RawMessage, cpu float32, ramUsed, ramTotal int64, pollErr string) (PollOutcome, error) {
	var out PollOutcome
	if pollErr != "" {
		_, err := s.pool.Exec(ctx, `UPDATE servers SET last_error = $2 WHERE id = $1`, id, pollErr)
		return out, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err := tx.QueryRow(ctx, `
		UPDATE servers s SET last_ok_at = now(), last_error = '', last_data = $2,
			offline_notified = false
		FROM (SELECT offline_notified FROM servers WHERE id = $1 FOR UPDATE) old
		WHERE s.id = $1
		RETURNING old.offline_notified, s.name, s.user_id`,
		id, data).Scan(&out.WasOffline, &out.Name, &out.UserID); err != nil {
		return out, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO server_samples (server_id, cpu_pct, ram_used, ram_total)
		VALUES ($1, $2, $3, $4)`, id, cpu, ramUsed, ramTotal); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

// OfflineServer — машина, переставшая выходить на связь (для уведомления).
type OfflineServer struct {
	ID       int64
	Name     string
	UserID   int64
	LastOkAt time.Time
}

// MarkOfflineServers помечает серверы без свежих данных (last_ok_at старше
// threshold) и возвращает их для уведомления. Флаг ставится атомарно,
// поэтому каждое событие offline даёт ровно одно сообщение.
func (s *Store) MarkOfflineServers(ctx context.Context, threshold time.Duration) ([]OfflineServer, error) {
	rows, err := s.pool.Query(ctx, `
		UPDATE servers SET offline_notified = true, last_error = 'нет данных от агента'
		WHERE offline_notified = false
		  AND last_ok_at IS NOT NULL
		  AND last_ok_at < now() - make_interval(secs => $1)
		RETURNING id, name, user_id, last_ok_at`,
		threshold.Seconds())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[OfflineServer])
}

// PruneServerSamples удаляет историю старше 48 часов.
func (s *Store) PruneServerSamples(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM server_samples WHERE at < now() - interval '48 hours'`)
	return err
}

// --- пороговые уведомления ---

// AlertConfig — конфигурация порогов сервера + текущее состояние (для поллера).
type AlertConfig struct {
	ID                int64
	Name              string
	UserID            int64
	LastOkAt          *time.Time
	RAMPct            int16
	RAMMinutes        int16
	CPUPct            int16
	CPUMinutes        int16
	RAMAlerted        bool
	CPUAlerted        bool
	DiskRules         []DiskRule
	DiskAlertedMounts []string
	LastData          json.RawMessage
}

// AlertConfigs возвращает серверы с включёнными уведомлениями.
func (s *Store) AlertConfigs(ctx context.Context) ([]AlertConfig, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, user_id, last_ok_at, ram_pct, ram_minutes, cpu_pct, cpu_minutes,
		       ram_alerted, cpu_alerted, disk_rules, disk_alerted_mounts, last_data
		FROM servers WHERE alerts_enabled = true`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertConfig
	for rows.Next() {
		var c AlertConfig
		var diskRules, alertedMounts []byte
		if err := rows.Scan(&c.ID, &c.Name, &c.UserID, &c.LastOkAt, &c.RAMPct, &c.RAMMinutes,
			&c.CPUPct, &c.CPUMinutes, &c.RAMAlerted, &c.CPUAlerted,
			&diskRules, &alertedMounts, &c.LastData); err != nil {
			return nil, err
		}
		if len(diskRules) > 0 {
			_ = json.Unmarshal(diskRules, &c.DiskRules)
		}
		if len(alertedMounts) > 0 {
			_ = json.Unmarshal(alertedMounts, &c.DiskAlertedMounts)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetAlertFlag ставит/снимает флаг «уже уведомили» для условия kind (ram/cpu).
func (s *Store) SetAlertFlag(ctx context.Context, id int64, kind string, val bool) error {
	col := map[string]string{
		"ram": "ram_alerted",
		"cpu": "cpu_alerted",
	}[kind]
	if col == "" {
		return errors.New("unknown alert kind")
	}
	_, err := s.pool.Exec(ctx, `UPDATE servers SET `+col+` = $2 WHERE id = $1`, id, val)
	return err
}

// SetDiskAlertedMounts сохраняет множество точек монтирования, находящихся
// сейчас в состоянии дискового алерта.
func (s *Store) SetDiskAlertedMounts(ctx context.Context, id int64, mounts []string) error {
	if mounts == nil {
		mounts = []string{}
	}
	raw, err := json.Marshal(mounts)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE servers SET disk_alerted_mounts = $2 WHERE id = $1`, id, raw)
	return err
}

// sustained проверяет, что метрика непрерывно превышала порог thr на протяжении
// последних minutes минут (нужно достаточное покрытие окна сэмплами).
func (s *Store) sustained(ctx context.Context, serverID int64, minutes int, cond string, thr int16) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) >= 2
		   AND bool_and(`+cond+`)
		   AND min(at) <= now() - make_interval(mins => $2) * 0.7
		FROM server_samples
		WHERE server_id = $1 AND at > now() - make_interval(mins => $2)`,
		serverID, minutes, thr).Scan(&ok)
	return ok, err
}

// SustainedCPU — CPU держался выше thr% в течение minutes минут.
func (s *Store) SustainedCPU(ctx context.Context, serverID int64, minutes int, thr int16) (bool, error) {
	return s.sustained(ctx, serverID, minutes, `cpu_pct >= $3`, thr)
}

// SustainedRAM — RAM держалась выше thr% в течение minutes минут.
func (s *Store) SustainedRAM(ctx context.Context, serverID int64, minutes int, thr int16) (bool, error) {
	return s.sustained(ctx, serverID, minutes,
		`(ram_total > 0 AND ram_used::float8 / ram_total * 100 >= $3)`, thr)
}
