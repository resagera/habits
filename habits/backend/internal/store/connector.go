package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Коннектор: правила «источник → условия → результат», связывающие страницы.
//
// Условия хранятся деревом в JSONB. Реляционная схема потребовала бы
// рекурсивного запроса на каждую проверку, а правило всегда читается и
// пишется целиком — поэтому дерево.

// ConnectorNode — узел дерева условий. Либо ГРУППА (Op + Nodes), либо ЛИСТ
// (Field + Cmp + Value). Различаются по непустому Op: так дерево любой
// вложенности описывается одним типом и одной рекурсивной функцией.
type ConnectorNode struct {
	Op    string          `json:"op,omitempty"` // and | or
	Nodes []ConnectorNode `json:"nodes,omitempty"`
	Field string          `json:"field,omitempty"`
	Cmp   string          `json:"cmp,omitempty"`
	Value string          `json:"value,omitempty"`
}

// ConnectorAction — один результат правила.
type ConnectorAction struct {
	Type string `json:"type"`
	// отметка в трекере
	CategoryID int64  `json:"category_id,omitempty"`
	Color      string `json:"color,omitempty"`
	Emoji      string `json:"emoji,omitempty"`
	Count      int32  `json:"count,omitempty"`
	Day        string `json:"day,omitempty"` // event | today
	// сообщение в Telegram
	Text string `json:"text,omitempty"`
	// флаг письму
	Flag  string `json:"flag,omitempty"`
	Value bool   `json:"value,omitempty"`
}

type ConnectorRule struct {
	ID      int64             `json:"id"`
	UserID  int64             `json:"user_id"`
	Name    string            `json:"name"`
	Enabled bool              `json:"enabled"`
	Sources []string          `json:"sources"`
	Match   ConnectorNode     `json:"match"`
	Actions []ConnectorAction `json:"actions"`
	// часовой пояс человека: по нему считается «сегодня». Как у напоминаний —
	// приходит с устройства, глобального в приложении нет
	TzOffsetMinutes int32      `json:"tz_offset_minutes"`
	Runs            int32      `json:"runs"`
	LastRunAt       *time.Time `json:"last_run_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

const connectorRuleCols = `id, user_id, name, enabled, sources, match, actions,
	tz_offset_minutes, runs, last_run_at, created_at, updated_at`

// connectorJSON готовит поля-деревья к записи. pgx закодировал бы []string
// массивом Postgres, а структуру — композитным типом: в jsonb их надо
// отправлять явным JSON. Чтение обратное — pgx разбирает jsonb сам.
func connectorJSON(r ConnectorRule) (sources, match, actions []byte, err error) {
	if r.Sources == nil {
		r.Sources = []string{}
	}
	if r.Actions == nil {
		r.Actions = []ConnectorAction{}
	}
	if sources, err = json.Marshal(r.Sources); err != nil {
		return
	}
	if match, err = json.Marshal(r.Match); err != nil {
		return
	}
	actions, err = json.Marshal(r.Actions)
	return
}

func (s *Store) ListConnectorRules(ctx context.Context, userID int64) ([]ConnectorRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+connectorRuleCols+`
		FROM connector_rules WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ConnectorRule])
}

func (s *Store) ConnectorRuleByID(ctx context.Context, userID, id int64) (ConnectorRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+connectorRuleCols+`
		FROM connector_rules WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return ConnectorRule{}, err
	}
	r, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[ConnectorRule])
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// ConnectorRulesForSource — включённые правила этого источника, по всем
// пользователям: приёмник почты работает вне сессии и чужие правила его не
// касаются, поэтому фильтр по пользователю задаёт вызывающий.
func (s *Store) ConnectorRulesForSource(ctx context.Context, userID int64, source string) ([]ConnectorRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+connectorRuleCols+`
		FROM connector_rules
		WHERE user_id = $1 AND enabled AND sources @> to_jsonb($2::text)
		ORDER BY id`, userID, source)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ConnectorRule])
}

func (s *Store) CreateConnectorRule(ctx context.Context, userID int64, r ConnectorRule) (ConnectorRule, error) {
	sources, match, actions, err := connectorJSON(r)
	if err != nil {
		return r, err
	}
	rows, err := s.pool.Query(ctx, `
		INSERT INTO connector_rules (user_id, name, enabled, sources, match, actions,
			tz_offset_minutes)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING `+connectorRuleCols,
		userID, r.Name, r.Enabled, sources, match, actions, r.TzOffsetMinutes)
	if err != nil {
		return r, err
	}
	return pgx.CollectOneRow(rows, pgx.RowToStructByPos[ConnectorRule])
}

func (s *Store) UpdateConnectorRule(ctx context.Context, userID int64, r ConnectorRule) (ConnectorRule, error) {
	sources, match, actions, err := connectorJSON(r)
	if err != nil {
		return r, err
	}
	rows, err := s.pool.Query(ctx, `
		UPDATE connector_rules SET name = $3, enabled = $4, sources = $5,
			match = $6, actions = $7, tz_offset_minutes = $8, updated_at = now()
		WHERE user_id = $1 AND id = $2
		RETURNING `+connectorRuleCols,
		userID, r.ID, r.Name, r.Enabled, sources, match, actions, r.TzOffsetMinutes)
	if err != nil {
		return r, err
	}
	out, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[ConnectorRule])
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, err
}

func (s *Store) SetConnectorRuleEnabled(ctx context.Context, userID, id int64, on bool) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE connector_rules SET enabled = $3, updated_at = now()
		WHERE user_id = $1 AND id = $2`, userID, id, on)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteConnectorRule(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM connector_rules WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- журнал срабатываний ---

type ConnectorRun struct {
	ID         int64     `json:"id"`
	RuleID     int64     `json:"rule_id"`
	UserID     int64     `json:"user_id"`
	Source     string    `json:"source"`
	EventID    *int64    `json:"event_id"`
	EventLabel string    `json:"event_label"`
	Status     string    `json:"status"`
	Detail     string    `json:"detail"`
	At         time.Time `json:"at"`
}

const connectorRunCols = `id, rule_id, user_id, source, event_id, event_label,
	status, detail, at`

// ClaimConnectorRun занимает событие под правило. false — событие уже
// обработано этим правилом: ровно это не даёт отметить одно письмо дважды
// (повторный разбор, перезапуск, кнопка «применить»). force перезанимает —
// так работает ручное применение, когда человек просит сам.
func (s *Store) ClaimConnectorRun(ctx context.Context, ruleID, userID int64,
	source string, eventID *int64, label string, force bool) (int64, bool, error) {
	if eventID == nil {
		// событие без идентификатора защищать нечем — просто пишем строку
		var id int64
		err := s.pool.QueryRow(ctx, `
			INSERT INTO connector_runs (rule_id, user_id, source, event_label, status)
			VALUES ($1,$2,$3,$4,'running') RETURNING id`,
			ruleID, userID, source, label).Scan(&id)
		return id, err == nil, err
	}
	onConflict := `DO NOTHING`
	if force {
		onConflict = `DO UPDATE SET at = now(), status = 'running', detail = '',
			event_label = EXCLUDED.event_label`
	}
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO connector_runs (rule_id, user_id, source, event_id, event_label, status)
		VALUES ($1,$2,$3,$4,$5,'running')
		ON CONFLICT (rule_id, source, event_id) WHERE event_id IS NOT NULL `+onConflict+`
		RETURNING id`,
		ruleID, userID, source, *eventID, label).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil // уже обработано
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// FinishConnectorRun закрывает строку журнала и двигает счётчики правила.
func (s *Store) FinishConnectorRun(ctx context.Context, runID, ruleID int64, status, detail string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `
		UPDATE connector_runs SET status = $2, detail = $3, at = now() WHERE id = $1`,
		runID, status, clipStr(detail, 2000)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE connector_rules SET runs = runs + 1, last_run_at = now() WHERE id = $1`,
		ruleID); err != nil {
		return err
	}
	// журнал не должен расти вечно: у правила держим последние 100 строк
	if _, err := tx.Exec(ctx, `
		DELETE FROM connector_runs WHERE rule_id = $1 AND id NOT IN (
			SELECT id FROM connector_runs WHERE rule_id = $1 ORDER BY at DESC, id DESC LIMIT 100)`,
		ruleID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DropConnectorRun убирает строку, если до действий дело не дошло: иначе
// занятое, но не сработавшее событие осталось бы «обработанным» навсегда.
func (s *Store) DropConnectorRun(ctx context.Context, runID int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM connector_runs WHERE id = $1`, runID)
	return err
}

func (s *Store) ListConnectorRuns(ctx context.Context, userID int64, limit int) ([]ConnectorRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+connectorRunCols+`
		FROM connector_runs WHERE user_id = $1
		ORDER BY at DESC, id DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ConnectorRun])
}

// --- что коннектору нужно от других страниц ---

// CategoryByID — трекер с проверкой доступа (владелец или участник).
func (s *Store) CategoryByID(ctx context.Context, userID, id int64) (Category, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+categoryCols+`
		FROM tracker_categories c
		WHERE c.id = $2 AND `+trackerAccess, userID, id)
	if err != nil {
		return Category{}, err
	}
	c, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[Category])
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// SetMark ставит отметку дня, НЕ снимая существующую: коннектор отмечает, а не
// переключает — повторное событие не должно убирать отметку, поставленную
// первым. Возвращает true, если отметки раньше не было.
func (s *Store) SetMark(ctx context.Context, userID, categoryID int64, day time.Time,
	color, emoji *string) (bool, error) {
	allowed, err := s.canAccessCategory(ctx, userID, categoryID)
	if err != nil {
		return false, err
	}
	if !allowed {
		return false, ErrNotFound
	}
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO tracker_marks (category_id, day, color, emoji) VALUES ($1,$2,$3,$4)
		ON CONFLICT (category_id, day) DO NOTHING`, categoryID, day, color, emoji)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ConnectorMailMessages — последние письма для кнопки «проверить правило».
// Берём мало и без тел: нужны только тема, отправитель и дата.
func (s *Store) ConnectorMailMessages(ctx context.Context, userID int64, limit int) ([]MailMessage, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+mailListCols+`
		FROM mail_messages WHERE user_id = $1
		ORDER BY received_at DESC, id DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[MailMessage])
}
