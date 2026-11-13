package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
)

// --- Automation: автоматизированные действия по расписанию ---

// automationSecret — ключ AES-256 для шифрования учётных данных сайта.
// Устанавливается один раз при старте из config.AutomationKey.
var automationSecret [32]byte

// SetAutomationKey задаёт ключ шифрования (хэш строки из конфига).
func SetAutomationKey(key string) {
	automationSecret = sha256.Sum256([]byte(key))
}

// encrypt/decrypt — AES-256-GCM, результат base64 (nonce||ciphertext).
func encryptSecret(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	block, err := aes.NewCipher(automationSecret[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

func decryptSecret(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(automationSecret[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// AutomationConfig — параметры (login/password хранятся зашифрованными).
type AutomationConfig struct {
	LoginEnc    string `json:"login_enc,omitempty"`
	PasswordEnc string `json:"password_enc,omitempty"`
	Quantity    int    `json:"quantity"`
	TareMode    string `json:"tare_mode"` // auto/fixed
	TareQty     int    `json:"tare_qty"`
	TimeSlot    string `json:"time_slot"` // first/конкретное
	Payment     string `json:"payment"`   // checkmo
	Comment     string `json:"comment"`
}

// Creds расшифровывает логин/пароль для исполнителя.
func (c AutomationConfig) Creds() (login, password string, err error) {
	if login, err = decryptSecret(c.LoginEnc); err != nil {
		return
	}
	password, err = decryptSecret(c.PasswordEnc)
	return
}

type Automation struct {
	ID           int64            `json:"id"`
	Kind         string           `json:"kind"`
	Title        string           `json:"title"`
	Enabled      bool             `json:"enabled"`
	Config       AutomationConfig `json:"-"`
	IntervalDays int              `json:"interval_days"`
	NextRunAt    *time.Time       `json:"next_run_at"`
	LastRunAt    *time.Time       `json:"last_run_at"`
	LastStatus   string           `json:"last_status"`
	CreatedAt    time.Time        `json:"created_at"`
}

const automationCols = `id, kind, title, enabled, config, interval_days,
	next_run_at, last_run_at, last_status, created_at`

func scanAutomation(row pgx.Row) (*Automation, error) {
	var a Automation
	var cfg []byte
	err := row.Scan(&a.ID, &a.Kind, &a.Title, &a.Enabled, &cfg, &a.IntervalDays,
		&a.NextRunAt, &a.LastRunAt, &a.LastStatus, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(cfg, &a.Config)
	return &a, nil
}

func (s *Store) ListAutomations(ctx context.Context, userID int64) ([]Automation, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+automationCols+` FROM automations
		WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Automation
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (s *Store) GetAutomation(ctx context.Context, userID, id int64) (*Automation, error) {
	a, err := scanAutomation(s.pool.QueryRow(ctx, `SELECT `+automationCols+`
		FROM automations WHERE id = $1 AND user_id = $2`, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// GetAutomationInternal — без проверки владельца (для фонового воркера).
func (s *Store) GetAutomationInternal(ctx context.Context, id int64) (*Automation, error) {
	a, err := scanAutomation(s.pool.QueryRow(ctx, `SELECT `+automationCols+`
		FROM automations WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// SetCredentials шифрует и кладёт логин/пароль в конфиг (если непустые).
func (c *AutomationConfig) SetCredentials(login, password string) error {
	if login != "" {
		enc, err := encryptSecret(login)
		if err != nil {
			return err
		}
		c.LoginEnc = enc
	}
	if password != "" {
		enc, err := encryptSecret(password)
		if err != nil {
			return err
		}
		c.PasswordEnc = enc
	}
	return nil
}

func (s *Store) CreateAutomation(ctx context.Context, userID int64, a Automation) (*Automation, error) {
	cfg, _ := json.Marshal(a.Config)
	return scanAutomation(s.pool.QueryRow(ctx, `
		INSERT INTO automations (user_id, kind, title, enabled, config, interval_days, next_run_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING `+automationCols,
		userID, a.Kind, a.Title, a.Enabled, cfg, a.IntervalDays, a.NextRunAt))
}

func (s *Store) UpdateAutomation(ctx context.Context, userID, id int64, a Automation) (*Automation, error) {
	cfg, _ := json.Marshal(a.Config)
	// смена расписания/интервала сбрасывает флаги уведомлений
	updated, err := scanAutomation(s.pool.QueryRow(ctx, `
		UPDATE automations SET title=$3, enabled=$4, config=$5, interval_days=$6,
			next_run_at=$7, notified_day=FALSE, notified_hour=FALSE, updated_at=now()
		WHERE id = $1 AND user_id = $2 RETURNING `+automationCols,
		id, userID, a.Title, a.Enabled, cfg, a.IntervalDays, a.NextRunAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return updated, err
}

func (s *Store) DeleteAutomation(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM automations WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- запуски и планирование ---

type AutomationRun struct {
	ID         int64      `json:"id"`
	Status     string     `json:"status"`
	DryRun     bool       `json:"dry_run"`
	Trigger    string     `json:"trigger"`
	Steps      json.RawMessage `json:"steps"`
	Error      string     `json:"error"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

func (s *Store) ListAutomationRuns(ctx context.Context, userID, automationID int64, limit int) ([]AutomationRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := s.pool.Query(ctx, `SELECT id, status, dry_run, trigger, steps, error, started_at, finished_at
		FROM automation_runs WHERE automation_id = $1 AND user_id = $2
		ORDER BY started_at DESC LIMIT $3`, automationID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AutomationRun
	for rows.Next() {
		var r AutomationRun
		var steps []byte
		if err := rows.Scan(&r.ID, &r.Status, &r.DryRun, &r.Trigger, &steps, &r.Error, &r.StartedAt, &r.FinishedAt); err != nil {
			return nil, err
		}
		r.Steps = steps
		out = append(out, r)
	}
	return out, rows.Err()
}

// StartRun создаёт запись запуска (status=running).
func (s *Store) StartRun(ctx context.Context, userID, automationID int64, trigger string, dryRun bool) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO automation_runs (automation_id, user_id, status, dry_run, trigger)
		VALUES ($1,$2,'running',$3,$4) RETURNING id`, automationID, userID, dryRun, trigger).Scan(&id)
	return id, err
}

// FinishRun записывает итог запуска.
func (s *Store) FinishRun(ctx context.Context, runID int64, status string, steps any, errMsg string) error {
	b, _ := json.Marshal(steps)
	_, err := s.pool.Exec(ctx, `UPDATE automation_runs
		SET status=$2, steps=$3, error=$4, finished_at=now() WHERE id=$1`,
		runID, status, b, errMsg)
	return err
}

// TryLockAutomation ставит running=TRUE, если он был FALSE (защита от двойного
// запуска расписанием и вручную). Возвращает false, если уже запущена.
func (s *Store) TryLockAutomation(ctx context.Context, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE automations SET running=TRUE
		WHERE id=$1 AND running=FALSE`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) UnlockAutomation(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE automations SET running=FALSE WHERE id=$1`, id)
	return err
}

// AfterRun обновляет статус запуска. Если nextRunAt != nil — переносит
// расписание на эту дату и сбрасывает флаги уведомлений (иначе next_run_at не
// трогаем). Снимает блокировку running.
func (s *Store) AfterRun(ctx context.Context, id int64, status string, nextRunAt *time.Time, now time.Time) error {
	if nextRunAt != nil {
		_, err := s.pool.Exec(ctx, `UPDATE automations
			SET last_status=$2, last_run_at=$3, next_run_at=$4,
			    notified_day=FALSE, notified_hour=FALSE, running=FALSE, updated_at=now()
			WHERE id=$1`, id, status, now, *nextRunAt)
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE automations
		SET last_status=$2, last_run_at=$3, running=FALSE, updated_at=now() WHERE id=$1`,
		id, status, now)
	return err
}

// DueAutomationNotify — включённые автоматизации с назначенным next_run_at,
// для проверки уведомлений и запуска.
type DueAutomation struct {
	ID            int64
	UserID        int64
	Title         string
	Config        AutomationConfig
	IntervalDays  int
	NextRunAt     time.Time
	NotifiedDay   bool
	NotifiedHour  bool
}

func (s *Store) DueAutomations(ctx context.Context, horizon time.Time) ([]DueAutomation, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, user_id, title, config, interval_days,
		next_run_at, notified_day, notified_hour
		FROM automations
		WHERE enabled = TRUE AND running = FALSE AND next_run_at IS NOT NULL AND next_run_at <= $1
		ORDER BY next_run_at LIMIT 100`, horizon)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DueAutomation
	for rows.Next() {
		var d DueAutomation
		var cfg []byte
		if err := rows.Scan(&d.ID, &d.UserID, &d.Title, &cfg, &d.IntervalDays,
			&d.NextRunAt, &d.NotifiedDay, &d.NotifiedHour); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(cfg, &d.Config)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) MarkNotified(ctx context.Context, id int64, day, hour bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE automations SET notified_day=$2, notified_hour=$3 WHERE id=$1`,
		id, day, hour)
	return err
}

// --- агент сетевого выхода ---

// GetOrCreateAgentToken возвращает токен агента пользователя, создавая его при
// первом обращении.
func (s *Store) GetOrCreateAgentToken(ctx context.Context, userID int64) (string, error) {
	var token string
	err := s.pool.QueryRow(ctx, `SELECT token FROM automation_agents WHERE user_id=$1`, userID).Scan(&token)
	if err == nil {
		return token, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	_, err = s.pool.Exec(ctx, `INSERT INTO automation_agents (user_id, token) VALUES ($1,$2)
		ON CONFLICT (user_id) DO NOTHING`, userID, token)
	if err != nil {
		return "", err
	}
	// на случай гонки — перечитать
	return s.GetOrCreateAgentToken(ctx, userID)
}

// RegenAgentToken выдаёт новый токен (старый агент отвалится).
func (s *Store) RegenAgentToken(ctx context.Context, userID int64) (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	_, err := s.pool.Exec(ctx, `INSERT INTO automation_agents (user_id, token) VALUES ($1,$2)
		ON CONFLICT (user_id) DO UPDATE SET token=EXCLUDED.token`, userID, token)
	return token, err
}

// AutomationAgentUser возвращает user_id по токену агента.
func (s *Store) AutomationAgentUser(ctx context.Context, token string) (int64, error) {
	var uid int64
	err := s.pool.QueryRow(ctx, `SELECT user_id FROM automation_agents WHERE token=$1`, token).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return uid, err
}

func (s *Store) TouchAutomationAgent(ctx context.Context, userID int64) {
	_, _ = s.pool.Exec(ctx, `UPDATE automation_agents SET last_seen_at=now() WHERE user_id=$1`, userID)
}
