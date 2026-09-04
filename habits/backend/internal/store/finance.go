package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// FinancePlan — плановая трата (подписка, ЖКХ, аренда).
type FinancePlan struct {
	ID               int64      `json:"id"`
	Name             string     `json:"name"`
	Amount           float64    `json:"amount"`
	Currency         string     `json:"currency"`
	IsEstimate       bool       `json:"is_estimate"`
	NextDueDate      time.Time  `json:"next_due_date"`
	RepeatKind       string     `json:"repeat_kind"`
	IntervalN        int        `json:"interval_n"`
	Category         string     `json:"category"`
	Note             string     `json:"note"`
	Autopay          bool       `json:"autopay"`
	NotifyDaysBefore int        `json:"notify_days_before"`
	PausedUntil      *time.Time `json:"paused_until"`
	Enabled          bool       `json:"enabled"`
	ArchivedAt       *time.Time `json:"archived_at"`
	// категория из дерева и счёт списания (фазы 3–4). Текстовая Category —
	// денормализованное имя категории: списки и вхождения показывают его без
	// джойна, обновляется вместе с CategoryID.
	CategoryID *int64 `json:"category_id"`
	AccountID  *int64 `json:"account_id"`
}

const financePlanCols = `id, name, amount, currency, is_estimate, next_due_date,
	repeat_kind, interval_n, category, note, autopay, notify_days_before,
	paused_until, enabled, archived_at, category_id, account_id`

// FinancePayment — запись истории оплат.
type FinancePayment struct {
	ID           int64     `json:"id"`
	PlanID       int64     `json:"plan_id"`
	PlanName     string    `json:"plan_name"`
	PaidOn       time.Time `json:"paid_on"`
	DueDate      time.Time `json:"due_date"`
	Amount       float64   `json:"amount"`
	Currency     string    `json:"currency"`
	BaseCurrency string    `json:"base_currency"`
	RateToBase   float64   `json:"rate_to_base"`
	Periods      int       `json:"periods"`
	Skipped      bool      `json:"skipped"`
	Note         string    `json:"note"`
}

// FinanceDebt — долг в одну из сторон. Paid/Remaining считаются по истории.
type FinanceDebt struct {
	ID            int64      `json:"id"`
	Direction     string     `json:"direction"`
	Person        string     `json:"person"`
	ContactUserID *int64     `json:"contact_user_id"`
	Subject       string     `json:"subject"`
	Amount        float64    `json:"amount"`
	Currency      string     `json:"currency"`
	ExpectedOn    *time.Time `json:"expected_on"`
	Status        string     `json:"status"`
	Note          string     `json:"note"`
	ArchivedAt    *time.Time `json:"archived_at"`
	Paid          float64    `json:"paid"`
	Remaining     float64    `json:"remaining"`
}

// FinanceDebtPayment — частичный возврат.
type FinanceDebtPayment struct {
	ID     int64     `json:"id"`
	DebtID int64     `json:"debt_id"`
	PaidOn time.Time `json:"paid_on"`
	Amount float64   `json:"amount"`
	Note   string    `json:"note"`
}

// FinanceSettings — настройки страницы.
type FinanceSettings struct {
	BaseCurrency string `json:"base_currency"`
	NotifyHour   int    `json:"notify_hour"`
	TzOff        int    `json:"tz_off"` // минуты от UTC
	HideAmounts  bool   `json:"hide_amounts"`
}

// --- планы ---

func (s *Store) ListFinancePlans(ctx context.Context, userID int64, archived bool) ([]FinancePlan, error) {
	cond := "archived_at IS NULL"
	if archived {
		cond = "archived_at IS NOT NULL"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+financePlanCols+`
		FROM finance_plans
		WHERE user_id = $1 AND `+cond+`
		ORDER BY next_due_date, id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[FinancePlan])
}

func (s *Store) FinancePlanByID(ctx context.Context, userID, id int64) (FinancePlan, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+financePlanCols+`
		FROM finance_plans WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return FinancePlan{}, err
	}
	p, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[FinancePlan])
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *Store) CreateFinancePlan(ctx context.Context, userID int64, p FinancePlan) (FinancePlan, error) {
	rows, err := s.pool.Query(ctx, `
		INSERT INTO finance_plans (user_id, name, amount, currency, is_estimate,
			next_due_date, repeat_kind, interval_n, category, note, autopay,
			notify_days_before, paused_until, enabled, category_id, account_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		RETURNING `+financePlanCols,
		userID, p.Name, p.Amount, p.Currency, p.IsEstimate, p.NextDueDate,
		p.RepeatKind, p.IntervalN, p.Category, p.Note, p.Autopay,
		p.NotifyDaysBefore, p.PausedUntil, p.Enabled, p.CategoryID, p.AccountID)
	if err != nil {
		return FinancePlan{}, err
	}
	return pgx.CollectOneRow(rows, pgx.RowToStructByPos[FinancePlan])
}

func (s *Store) UpdateFinancePlan(ctx context.Context, userID int64, p FinancePlan) (FinancePlan, error) {
	rows, err := s.pool.Query(ctx, `
		UPDATE finance_plans SET
			name = $3, amount = $4, currency = $5, is_estimate = $6,
			next_due_date = $7, repeat_kind = $8, interval_n = $9, category = $10,
			note = $11, autopay = $12, notify_days_before = $13, paused_until = $14,
			enabled = $15, category_id = $16, account_id = $17, updated_at = now()
		WHERE user_id = $1 AND id = $2
		RETURNING `+financePlanCols,
		userID, p.ID, p.Name, p.Amount, p.Currency, p.IsEstimate, p.NextDueDate,
		p.RepeatKind, p.IntervalN, p.Category, p.Note, p.Autopay,
		p.NotifyDaysBefore, p.PausedUntil, p.Enabled, p.CategoryID, p.AccountID)
	if err != nil {
		return FinancePlan{}, err
	}
	out, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[FinancePlan])
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, err
}

// ArchiveFinancePlan — в архив и обратно (данные не теряются).
func (s *Store) ArchiveFinancePlan(ctx context.Context, userID, id int64, archived bool) error {
	var at any
	if archived {
		at = time.Now()
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE finance_plans SET archived_at = $3, updated_at = now()
		WHERE user_id = $1 AND id = $2`, userID, id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteFinancePlan(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM finance_plans WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AdvanceDue — следующая дата платежа после оплаты N периодов.
//
// Календарная арифметика Go нормализует переполнение (31 января + месяц = 3
// марта), поэтому для месячных и годовых повторов день прижимаем к концу
// месяца — как это уже сделано для напоминаний.
func AdvanceDue(due time.Time, kind string, intervalN, periods int) time.Time {
	if periods < 1 {
		periods = 1
	}
	switch kind {
	case "monthly":
		return addMonthsClamped(due, periods)
	case "yearly":
		return addMonthsClamped(due, 12*periods)
	case "interval_months":
		return addMonthsClamped(due, intervalN*periods)
	case "interval_days":
		return due.AddDate(0, 0, intervalN*periods)
	}
	return due // once — следующей даты нет
}

func addMonthsClamped(d time.Time, months int) time.Time {
	y, m, day := d.Date()
	first := time.Date(y, m, 1, 0, 0, 0, 0, d.Location()).AddDate(0, months, 0)
	last := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, d.Location()).Day()
	if day > last {
		day = last
	}
	return time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, d.Location())
}

// PayFinancePlanParams — что записываем при оплате.
type PayFinancePlanParams struct {
	PlanID       int64
	PaidOn       time.Time
	Amount       float64
	Currency     string
	BaseCurrency string
	RateToBase   float64
	Periods      int
	Skipped      bool
	Note         string
}

// PayFinancePlan пишет платёж и двигает дату следующего.
// Разовый план (once) после оплаты выключается: следующей даты у него нет.
func (s *Store) PayFinancePlan(ctx context.Context, userID int64, p PayFinancePlanParams) (FinancePlan, error) {
	var out FinancePlan
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	rows, err := tx.Query(ctx, `
		SELECT `+financePlanCols+`
		FROM finance_plans WHERE user_id = $1 AND id = $2 FOR UPDATE`, userID, p.PlanID)
	if err != nil {
		return out, err
	}
	plan, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[FinancePlan])
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}

	var paymentID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO finance_payments (user_id, plan_id, paid_on, due_date, amount,
			currency, base_currency, rate_to_base, periods, skipped, note)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
		userID, plan.ID, p.PaidOn, plan.NextDueDate, p.Amount, p.Currency,
		p.BaseCurrency, p.RateToBase, p.Periods, p.Skipped, p.Note).
		Scan(&paymentID); err != nil {
		return out, err
	}

	// Оплата планового платежа — такая же трата, как любая другая, и попадает
	// в общий реестр: иначе отчёт по категориям пришлось бы собирать из двух
	// источников. Пропуск денег не тратит, поэтому транзакции не создаёт.
	if !p.Skipped && p.Amount > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO finance_transactions (user_id, kind, spent_on, amount, currency,
				base_currency, rate_to_base, category_id, account_id, plan_id, payment_id, note)
			VALUES ($1,'expense',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			userID, p.PaidOn, p.Amount, p.Currency, p.BaseCurrency, p.RateToBase,
			plan.CategoryID, plan.AccountID, plan.ID, paymentID, p.Note); err != nil {
			return out, err
		}
	}

	next := AdvanceDue(plan.NextDueDate, plan.RepeatKind, plan.IntervalN, p.Periods)
	enabled := plan.Enabled
	if plan.RepeatKind == "once" {
		enabled = false // разовая трата закрыта
	}
	rows, err = tx.Query(ctx, `
		UPDATE finance_plans
		SET next_due_date = $3, enabled = $4, notified_for = NULL,
		    autopay_asked_for = NULL, updated_at = now()
		WHERE user_id = $1 AND id = $2
		RETURNING `+financePlanCols, userID, plan.ID, next, enabled)
	if err != nil {
		return out, err
	}
	out, err = pgx.CollectOneRow(rows, pgx.RowToStructByPos[FinancePlan])
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

// ListFinancePayments — история оплат (всех планов или одного).
func (s *Store) ListFinancePayments(ctx context.Context, userID int64, planID int64, limit int) ([]FinancePayment, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	cond := ""
	args := []any{userID, limit}
	if planID > 0 {
		cond = " AND p.plan_id = $3"
		args = append(args, planID)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.plan_id, pl.name, p.paid_on, p.due_date, p.amount, p.currency,
		       p.base_currency, p.rate_to_base, p.periods, p.skipped, p.note
		FROM finance_payments p
		JOIN finance_plans pl ON pl.id = p.plan_id
		WHERE p.user_id = $1`+cond+`
		ORDER BY p.paid_on DESC, p.id DESC
		LIMIT $2`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[FinancePayment])
}

// AverageFinancePayment — средняя фактическая сумма последних платежей.
// Нужна для примерных трат (ЖКХ): подставляется в форму оплаты.
func (s *Store) AverageFinancePayment(ctx context.Context, userID, planID, last int64) (float64, bool, error) {
	var avg *float64
	err := s.pool.QueryRow(ctx, `
		SELECT avg(amount) FROM (
			SELECT amount FROM finance_payments
			WHERE user_id = $1 AND plan_id = $2 AND NOT skipped
			ORDER BY paid_on DESC LIMIT $3
		) t`, userID, planID, last).Scan(&avg)
	if err != nil {
		return 0, false, err
	}
	if avg == nil {
		return 0, false, nil
	}
	return *avg, true, nil
}

// --- долги ---

const financeDebtCols = `d.id, d.direction, d.person, d.contact_user_id, d.subject,
	d.amount, d.currency, d.expected_on, d.status, d.note, d.archived_at,
	COALESCE(p.paid, 0) AS paid,
	CASE WHEN d.status IN ('paid','forgiven') THEN 0
	     ELSE d.amount - COALESCE(p.paid, 0) END AS remaining`

const financeDebtFrom = `
	FROM finance_debts d
	LEFT JOIN (
		SELECT debt_id, sum(amount) AS paid FROM finance_debt_payments GROUP BY debt_id
	) p ON p.debt_id = d.id`

func (s *Store) ListFinanceDebts(ctx context.Context, userID int64, archived bool) ([]FinanceDebt, error) {
	cond := "d.archived_at IS NULL"
	if archived {
		cond = "d.archived_at IS NOT NULL"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+financeDebtCols+financeDebtFrom+`
		WHERE d.user_id = $1 AND `+cond+`
		ORDER BY d.status <> 'open', d.expected_on NULLS LAST, d.id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[FinanceDebt])
}

func (s *Store) FinanceDebtByID(ctx context.Context, userID, id int64) (FinanceDebt, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+financeDebtCols+financeDebtFrom+`
		WHERE d.user_id = $1 AND d.id = $2`, userID, id)
	if err != nil {
		return FinanceDebt{}, err
	}
	d, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[FinanceDebt])
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

func (s *Store) CreateFinanceDebt(ctx context.Context, userID int64, d FinanceDebt) (FinanceDebt, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO finance_debts (user_id, direction, person, contact_user_id,
			subject, amount, currency, expected_on, note)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		userID, d.Direction, d.Person, d.ContactUserID, d.Subject, d.Amount,
		d.Currency, d.ExpectedOn, d.Note).Scan(&id)
	if err != nil {
		return FinanceDebt{}, err
	}
	return s.FinanceDebtByID(ctx, userID, id)
}

func (s *Store) UpdateFinanceDebt(ctx context.Context, userID int64, d FinanceDebt) (FinanceDebt, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE finance_debts SET direction = $3, person = $4, contact_user_id = $5,
			subject = $6, amount = $7, currency = $8, expected_on = $9, note = $10,
			updated_at = now()
		WHERE user_id = $1 AND id = $2`,
		userID, d.ID, d.Direction, d.Person, d.ContactUserID, d.Subject, d.Amount,
		d.Currency, d.ExpectedOn, d.Note)
	if err != nil {
		return FinanceDebt{}, err
	}
	if tag.RowsAffected() == 0 {
		return FinanceDebt{}, ErrNotFound
	}
	return s.recalcDebtStatus(ctx, userID, d.ID)
}

// AddFinanceDebtPayment — частичный или полный возврат.
func (s *Store) AddFinanceDebtPayment(ctx context.Context, userID, debtID int64, p FinanceDebtPayment) (FinanceDebt, error) {
	var owner int64
	err := s.pool.QueryRow(ctx,
		`SELECT user_id FROM finance_debts WHERE id = $1`, debtID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) || owner != userID {
		return FinanceDebt{}, ErrNotFound
	}
	if err != nil {
		return FinanceDebt{}, err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO finance_debt_payments (debt_id, paid_on, amount, note)
		VALUES ($1,$2,$3,$4)`, debtID, p.PaidOn, p.Amount, p.Note); err != nil {
		return FinanceDebt{}, err
	}
	return s.recalcDebtStatus(ctx, userID, debtID)
}

// recalcDebtStatus — статус выводится из суммы возвратов, а не ставится руками:
// иначе «частично» и «закрыт» разъедутся с историей.
func (s *Store) recalcDebtStatus(ctx context.Context, userID, debtID int64) (FinanceDebt, error) {
	if _, err := s.pool.Exec(ctx, `
		UPDATE finance_debts d SET status = CASE
			WHEN d.status = 'forgiven' THEN 'forgiven'
			WHEN COALESCE(p.paid, 0) >= d.amount THEN 'paid'
			WHEN COALESCE(p.paid, 0) > 0 THEN 'partial'
			ELSE 'open' END,
			updated_at = now()
		FROM (SELECT $2::bigint AS id) x
		LEFT JOIN (
			SELECT debt_id, sum(amount) AS paid FROM finance_debt_payments
			WHERE debt_id = $2 GROUP BY debt_id
		) p ON true
		WHERE d.id = x.id AND d.user_id = $1`, userID, debtID); err != nil {
		return FinanceDebt{}, err
	}
	return s.FinanceDebtByID(ctx, userID, debtID)
}

// SetFinanceDebtStatus — «простить остаток» или вернуть в работу.
func (s *Store) SetFinanceDebtStatus(ctx context.Context, userID, debtID int64, status string) (FinanceDebt, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE finance_debts SET status = $3, updated_at = now()
		WHERE user_id = $1 AND id = $2`, userID, debtID, status)
	if err != nil {
		return FinanceDebt{}, err
	}
	if tag.RowsAffected() == 0 {
		return FinanceDebt{}, ErrNotFound
	}
	if status == "open" { // вернули в работу — пересчитать по возвратам
		return s.recalcDebtStatus(ctx, userID, debtID)
	}
	return s.FinanceDebtByID(ctx, userID, debtID)
}

func (s *Store) ArchiveFinanceDebt(ctx context.Context, userID, id int64, archived bool) error {
	var at any
	if archived {
		at = time.Now()
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE finance_debts SET archived_at = $3, updated_at = now()
		WHERE user_id = $1 AND id = $2`, userID, id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteFinanceDebt(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM finance_debts WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListFinanceDebtPayments(ctx context.Context, userID, debtID int64) ([]FinanceDebtPayment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT dp.id, dp.debt_id, dp.paid_on, dp.amount, dp.note
		FROM finance_debt_payments dp
		JOIN finance_debts d ON d.id = dp.debt_id AND d.user_id = $1
		WHERE dp.debt_id = $2
		ORDER BY dp.paid_on, dp.id`, userID, debtID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[FinanceDebtPayment])
}

// --- настройки ---

func (s *Store) GetFinanceSettings(ctx context.Context, userID int64) (FinanceSettings, error) {
	f := FinanceSettings{BaseCurrency: "amd", NotifyHour: 10}
	err := s.pool.QueryRow(ctx, `
		SELECT finance_base_currency, finance_notify_hour, finance_tz_off, finance_hide_amounts
		FROM user_settings WHERE user_id = $1`, userID).Scan(
		&f.BaseCurrency, &f.NotifyHour, &f.TzOff, &f.HideAmounts)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, nil
	}
	return f, err
}

func (s *Store) SetFinanceSettings(ctx context.Context, userID int64, f FinanceSettings) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_settings (user_id, finance_base_currency, finance_notify_hour,
			finance_tz_off, finance_hide_amounts)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (user_id) DO UPDATE SET
			finance_base_currency = EXCLUDED.finance_base_currency,
			finance_notify_hour = EXCLUDED.finance_notify_hour,
			finance_tz_off = EXCLUDED.finance_tz_off,
			finance_hide_amounts = EXCLUDED.finance_hide_amounts,
			updated_at = now()`,
		userID, f.BaseCurrency, f.NotifyHour, f.TzOff, f.HideAmounts)
	return err
}

// SetFinanceTz — часовой пояс приходит с клиента при открытии страницы:
// уведомления шлются утром по местному времени, а сервер живёт в UTC.
func (s *Store) SetFinanceTz(ctx context.Context, userID int64, tzOff int) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_settings (user_id, finance_tz_off) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET finance_tz_off = EXCLUDED.finance_tz_off`,
		userID, tzOff)
	return err
}

// --- уведомления ---

// FinanceDuePlan — строка для воркера уведомлений.
type FinanceDuePlan struct {
	UserID      int64
	PlanID      int64
	Name        string
	Amount      float64
	Currency    string
	IsEstimate  bool
	NextDueDate time.Time
	Autopay     bool
	DaysBefore  int
	NotifyHour  int
	TzOff       int
}

// FinancePlansToNotify — планы, по которым пора написать: срок наступает не
// позже чем через notify_days_before дней, и за эту дату ещё не писали.
// Автоплатежи сюда не попадают — по ним отдельный вопрос «списалось?».
func (s *Store) FinancePlansToNotify(ctx context.Context) ([]FinanceDuePlan, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.user_id, p.id, p.name, p.amount, p.currency, p.is_estimate,
		       p.next_due_date, p.autopay, p.notify_days_before,
		       COALESCE(us.finance_notify_hour, 10), COALESCE(us.finance_tz_off, 0)
		FROM finance_plans p
		LEFT JOIN user_settings us ON us.user_id = p.user_id
		WHERE p.enabled AND p.archived_at IS NULL AND NOT p.autopay
		  AND (p.paused_until IS NULL OR p.paused_until < current_date)
		  AND (p.notified_for IS DISTINCT FROM p.next_due_date)
		  AND p.next_due_date <= current_date + p.notify_days_before
		ORDER BY p.user_id, p.next_due_date
		LIMIT 500`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[FinanceDuePlan])
}

// FinanceAutopaysToConfirm — автоплатежи, у которых дата прошла: на следующий
// день спрашиваем «списалось?», чтобы данные не расходились с реальностью.
func (s *Store) FinanceAutopaysToConfirm(ctx context.Context) ([]FinanceDuePlan, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.user_id, p.id, p.name, p.amount, p.currency, p.is_estimate,
		       p.next_due_date, p.autopay, p.notify_days_before,
		       COALESCE(us.finance_notify_hour, 10), COALESCE(us.finance_tz_off, 0)
		FROM finance_plans p
		LEFT JOIN user_settings us ON us.user_id = p.user_id
		WHERE p.enabled AND p.archived_at IS NULL AND p.autopay
		  AND (p.paused_until IS NULL OR p.paused_until < current_date)
		  AND (p.autopay_asked_for IS DISTINCT FROM p.next_due_date)
		  AND p.next_due_date < current_date
		ORDER BY p.user_id, p.next_due_date
		LIMIT 500`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[FinanceDuePlan])
}

func (s *Store) MarkFinanceNotified(ctx context.Context, planID int64, due time.Time, autopay bool) error {
	col := "notified_for"
	if autopay {
		col = "autopay_asked_for"
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE finance_plans SET `+col+` = $2 WHERE id = $1`, planID, due)
	return err
}
