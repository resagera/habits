package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// FinanceAccount — счёт: «откуда деньги». Баланс не хранится полем: он равен
// стартовому остатку плюс движения — иначе неизбежно разъедется с историей
// (то же правило, что у остатка долга).
type FinanceAccount struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	Kind           string     `json:"kind"`
	Currency       string     `json:"currency"`
	StartBalance   float64    `json:"start_balance"`
	IncludeInTotal bool       `json:"include_in_total"`
	Note           string     `json:"note"`
	Position       int        `json:"position"`
	ArchivedAt     *time.Time `json:"archived_at"`
}

const financeAccountCols = `id, name, kind, currency, start_balance,
	include_in_total, note, position, archived_at`

func (s *Store) ListFinanceAccounts(ctx context.Context, userID int64, withArchived bool) ([]FinanceAccount, error) {
	cond := ""
	if !withArchived {
		cond = " AND archived_at IS NULL"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+financeAccountCols+`
		FROM finance_accounts WHERE user_id = $1`+cond+`
		ORDER BY position, id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[FinanceAccount])
}

func (s *Store) FinanceAccountByID(ctx context.Context, userID, id int64) (FinanceAccount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+financeAccountCols+`
		FROM finance_accounts WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return FinanceAccount{}, err
	}
	a, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[FinanceAccount])
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (s *Store) CreateFinanceAccount(ctx context.Context, userID int64, a FinanceAccount) (FinanceAccount, error) {
	rows, err := s.pool.Query(ctx, `
		INSERT INTO finance_accounts (user_id, name, kind, currency, start_balance,
			include_in_total, note, position)
		VALUES ($1,$2,$3,$4,$5,$6,$7,
			COALESCE((SELECT max(position) + 1 FROM finance_accounts WHERE user_id = $1), 0))
		RETURNING `+financeAccountCols,
		userID, a.Name, a.Kind, a.Currency, a.StartBalance, a.IncludeInTotal, a.Note)
	if err != nil {
		return a, err
	}
	return pgx.CollectOneRow(rows, pgx.RowToStructByPos[FinanceAccount])
}

func (s *Store) UpdateFinanceAccount(ctx context.Context, userID int64, a FinanceAccount) (FinanceAccount, error) {
	rows, err := s.pool.Query(ctx, `
		UPDATE finance_accounts SET name = $3, kind = $4, currency = $5,
			start_balance = $6, include_in_total = $7, note = $8, updated_at = now()
		WHERE user_id = $1 AND id = $2
		RETURNING `+financeAccountCols,
		userID, a.ID, a.Name, a.Kind, a.Currency, a.StartBalance, a.IncludeInTotal, a.Note)
	if err != nil {
		return a, err
	}
	out, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[FinanceAccount])
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, err
}

func (s *Store) ArchiveFinanceAccount(ctx context.Context, userID, id int64, archived bool) error {
	var at any
	if archived {
		at = time.Now()
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE finance_accounts SET archived_at = $3, updated_at = now()
		WHERE user_id = $1 AND id = $2`, userID, id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteFinanceAccount отказывает, если по счёту есть движения: удаление
// обезличило бы их (ON DELETE SET NULL), а история денег важнее порядка в
// списке — для этого есть архив.
func (s *Store) DeleteFinanceAccount(ctx context.Context, userID, id int64) error {
	var used int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM finance_transactions
		WHERE user_id = $1 AND (account_id = $2 OR to_account_id = $2)`,
		userID, id).Scan(&used); err != nil {
		return err
	}
	if used > 0 {
		return ErrConflict
	}
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM finance_accounts WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// FinanceAccountMove — движение по счёту в валюте самой записи.
type FinanceAccountMove struct {
	AccountID   *int64
	ToAccountID *int64
	Kind        string
	Amount      float64
	Currency    string
	RateToBase  float64
}

// FinanceAccountMoves — все движения по счетам пользователя. Баланс считается
// в Go: суммы бывают в чужой валюте, и пересчёт требует курсов.
func (s *Store) FinanceAccountMoves(ctx context.Context, userID int64) ([]FinanceAccountMove, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT account_id, to_account_id, kind, amount, currency, rate_to_base
		FROM finance_transactions
		WHERE user_id = $1 AND (account_id IS NOT NULL OR to_account_id IS NOT NULL)`,
		userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[FinanceAccountMove])
}

// --- цели «отложено на» ---

// FinanceGoal — цель накопления. Saved считается по движениям.
type FinanceGoal struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	TargetAmount float64    `json:"target_amount"`
	Currency     string     `json:"currency"`
	AccountID    *int64     `json:"account_id"`
	DueDate      *time.Time `json:"due_date"`
	Note         string     `json:"note"`
	ArchivedAt   *time.Time `json:"archived_at"`
	Saved        float64    `json:"saved"`
}

const financeGoalCols = `g.id, g.name, g.target_amount, g.currency, g.account_id,
	g.due_date, g.note, g.archived_at, COALESCE(m.saved, 0) AS saved`

const financeGoalFrom = `
	FROM finance_goals g
	LEFT JOIN (
		SELECT goal_id, sum(amount) AS saved FROM finance_goal_moves GROUP BY goal_id
	) m ON m.goal_id = g.id`

func (s *Store) ListFinanceGoals(ctx context.Context, userID int64, withArchived bool) ([]FinanceGoal, error) {
	cond := ""
	if !withArchived {
		cond = " AND g.archived_at IS NULL"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+financeGoalCols+financeGoalFrom+`
		WHERE g.user_id = $1`+cond+`
		ORDER BY g.due_date NULLS LAST, g.id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[FinanceGoal])
}

func (s *Store) FinanceGoalByID(ctx context.Context, userID, id int64) (FinanceGoal, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+financeGoalCols+financeGoalFrom+`
		WHERE g.user_id = $1 AND g.id = $2`, userID, id)
	if err != nil {
		return FinanceGoal{}, err
	}
	g, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[FinanceGoal])
	if errors.Is(err, pgx.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, err
}

func (s *Store) CreateFinanceGoal(ctx context.Context, userID int64, g FinanceGoal) (FinanceGoal, error) {
	var id int64
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO finance_goals (user_id, name, target_amount, currency, account_id,
			due_date, note)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		userID, g.Name, g.TargetAmount, g.Currency, g.AccountID, g.DueDate, g.Note).
		Scan(&id); err != nil {
		return g, err
	}
	return s.FinanceGoalByID(ctx, userID, id)
}

func (s *Store) UpdateFinanceGoal(ctx context.Context, userID int64, g FinanceGoal) (FinanceGoal, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE finance_goals SET name = $3, target_amount = $4, currency = $5,
			account_id = $6, due_date = $7, note = $8, updated_at = now()
		WHERE user_id = $1 AND id = $2`,
		userID, g.ID, g.Name, g.TargetAmount, g.Currency, g.AccountID, g.DueDate, g.Note)
	if err != nil {
		return g, err
	}
	if tag.RowsAffected() == 0 {
		return g, ErrNotFound
	}
	return s.FinanceGoalByID(ctx, userID, g.ID)
}

func (s *Store) ArchiveFinanceGoal(ctx context.Context, userID, id int64, archived bool) error {
	var at any
	if archived {
		at = time.Now()
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE finance_goals SET archived_at = $3, updated_at = now()
		WHERE user_id = $1 AND id = $2`, userID, id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteFinanceGoal(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM finance_goals WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// FinanceGoalMove — «отложил» (плюс) или «снял» (минус).
type FinanceGoalMove struct {
	ID      int64     `json:"id"`
	GoalID  int64     `json:"goal_id"`
	MovedOn time.Time `json:"moved_on"`
	Amount  float64   `json:"amount"`
	Note    string    `json:"note"`
}

func (s *Store) AddFinanceGoalMove(ctx context.Context, userID, goalID int64, m FinanceGoalMove) (FinanceGoal, error) {
	if _, err := s.FinanceGoalByID(ctx, userID, goalID); err != nil {
		return FinanceGoal{}, err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO finance_goal_moves (goal_id, moved_on, amount, note)
		VALUES ($1,$2,$3,$4)`, goalID, m.MovedOn, m.Amount, m.Note); err != nil {
		return FinanceGoal{}, err
	}
	return s.FinanceGoalByID(ctx, userID, goalID)
}

func (s *Store) ListFinanceGoalMoves(ctx context.Context, userID, goalID int64) ([]FinanceGoalMove, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.goal_id, m.moved_on, m.amount, m.note
		FROM finance_goal_moves m
		JOIN finance_goals g ON g.id = m.goal_id AND g.user_id = $1
		WHERE m.goal_id = $2
		ORDER BY m.moved_on DESC, m.id DESC`, userID, goalID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[FinanceGoalMove])
}

func (s *Store) DeleteFinanceGoalMove(ctx context.Context, userID, goalID, moveID int64) (FinanceGoal, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM finance_goal_moves m
		USING finance_goals g
		WHERE g.id = m.goal_id AND g.user_id = $1 AND m.goal_id = $2 AND m.id = $3`,
		userID, goalID, moveID)
	if err != nil {
		return FinanceGoal{}, err
	}
	if tag.RowsAffected() == 0 {
		return FinanceGoal{}, ErrNotFound
	}
	return s.FinanceGoalByID(ctx, userID, goalID)
}
