package store

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// FinanceCategory — узел дерева категорий любой вложенности.
type FinanceCategory struct {
	ID         int64      `json:"id"`
	ParentID   *int64     `json:"parent_id"`
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`
	Icon       string     `json:"icon"`
	Color      string     `json:"color"`
	Position   int        `json:"position"`
	ArchivedAt *time.Time `json:"archived_at"`
}

const financeCategoryCols = `id, parent_id, name, kind, icon, color, position, archived_at`

func (s *Store) ListFinanceCategories(ctx context.Context, userID int64, withArchived bool) ([]FinanceCategory, error) {
	cond := ""
	if !withArchived {
		cond = " AND archived_at IS NULL"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+financeCategoryCols+`
		FROM finance_categories
		WHERE user_id = $1`+cond+`
		ORDER BY position, lower(name), id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[FinanceCategory])
}

func (s *Store) FinanceCategoryByID(ctx context.Context, userID, id int64) (FinanceCategory, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+financeCategoryCols+`
		FROM finance_categories WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return FinanceCategory{}, err
	}
	c, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[FinanceCategory])
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func (s *Store) CreateFinanceCategory(ctx context.Context, userID int64, c FinanceCategory) (FinanceCategory, error) {
	if c.ParentID != nil {
		if _, err := s.FinanceCategoryByID(ctx, userID, *c.ParentID); err != nil {
			return c, err // чужой или несуществующий родитель
		}
	}
	rows, err := s.pool.Query(ctx, `
		INSERT INTO finance_categories (user_id, parent_id, name, kind, icon, color, position)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING `+financeCategoryCols,
		userID, c.ParentID, c.Name, c.Kind, c.Icon, c.Color, c.Position)
	if err != nil {
		return c, uniqueAsConflict(err)
	}
	// pgx выполняет запрос лениво: нарушение уникальности прилетает не из
	// Query, а отсюда — оборачивать нужно оба места
	out, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[FinanceCategory])
	return out, uniqueAsConflict(err)
}

func (s *Store) UpdateFinanceCategory(ctx context.Context, userID int64, c FinanceCategory) (FinanceCategory, error) {
	if c.ParentID != nil {
		if *c.ParentID == c.ID {
			return c, ErrConflict
		}
		if _, err := s.FinanceCategoryByID(ctx, userID, *c.ParentID); err != nil {
			return c, err
		}
		// перенос в собственного потомка оторвал бы поддерево от корня —
		// в дереве с parent_id это единственный способ получить цикл
		loop, err := s.financeCategoryIsDescendant(ctx, userID, *c.ParentID, c.ID)
		if err != nil {
			return c, err
		}
		if loop {
			return c, ErrConflict
		}
	}
	rows, err := s.pool.Query(ctx, `
		UPDATE finance_categories SET parent_id = $3, name = $4, kind = $5,
			icon = $6, color = $7, position = $8, updated_at = now()
		WHERE user_id = $1 AND id = $2
		RETURNING `+financeCategoryCols,
		userID, c.ID, c.ParentID, c.Name, c.Kind, c.Icon, c.Color, c.Position)
	if err != nil {
		return c, uniqueAsConflict(err)
	}
	out, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[FinanceCategory])
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, uniqueAsConflict(err)
}

// financeCategoryIsDescendant — является ли node потомком root.
func (s *Store) financeCategoryIsDescendant(ctx context.Context, userID, node, root int64) (bool, error) {
	var found bool
	err := s.pool.QueryRow(ctx, `
		WITH RECURSIVE up AS (
			SELECT id, parent_id FROM finance_categories WHERE user_id = $1 AND id = $2
			UNION ALL
			SELECT c.id, c.parent_id FROM finance_categories c
			JOIN up ON up.parent_id = c.id
		)
		SELECT EXISTS (SELECT 1 FROM up WHERE id = $3)`, userID, node, root).Scan(&found)
	return found, err
}

func (s *Store) ArchiveFinanceCategory(ctx context.Context, userID, id int64, archived bool) error {
	var at any
	if archived {
		at = time.Now()
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE finance_categories SET archived_at = $3, updated_at = now()
		WHERE user_id = $1 AND id = $2`, userID, id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteFinanceCategory удаляет узел, ПОДНИМАЯ детей и перевешивая записи на
// родителя. Каскадное удаление здесь недопустимо: уборка в справочнике не
// должна стирать траты, а ON DELETE SET NULL просто обезличила бы их.
func (s *Store) DeleteFinanceCategory(ctx context.Context, userID, id int64) error {
	c, err := s.FinanceCategoryByID(ctx, userID, id)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `
		UPDATE finance_categories SET parent_id = $3, updated_at = now()
		WHERE user_id = $1 AND parent_id = $2`, userID, id, c.ParentID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE finance_transactions SET category_id = $3, updated_at = now()
		WHERE user_id = $1 AND category_id = $2`, userID, id, c.ParentID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE finance_plans SET category_id = $3, updated_at = now()
		WHERE user_id = $1 AND category_id = $2`, userID, id, c.ParentID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM finance_categories WHERE user_id = $1 AND id = $2`, userID, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SeedFinanceCategories создаёт типовое дерево — пустой справочник отбивает
// желание записывать траты вообще. Уже существующие имена пропускаются.
func (s *Store) SeedFinanceCategories(ctx context.Context, userID int64) (int, error) {
	seed := []struct {
		name     string
		icon     string
		kind     string
		children []string
	}{
		{"Еда", "🍽", "expense", []string{"Продукты", "Кафе и доставка"}},
		{"Жильё", "🏠", "expense", []string{"Аренда", "ЖКХ", "Интернет и связь"}},
		{"Транспорт", "🚗", "expense", []string{"Такси", "Топливо", "Общественный"}},
		{"Здоровье", "💊", "expense", []string{"Аптека", "Врачи"}},
		{"Покупки", "🛍", "expense", []string{"Одежда", "Техника", "Дом"}},
		{"Развлечения", "🎬", "expense", []string{"Подписки", "Поездки"}},
		{"Прочее", "•", "expense", nil},
		{"Доходы", "💰", "income", []string{"Зарплата", "Подработка"}},
	}
	n := 0
	for i, top := range seed {
		parent, err := s.CreateFinanceCategory(ctx, userID, FinanceCategory{
			Name: top.name, Icon: top.icon, Kind: top.kind, Position: i,
		})
		if errors.Is(err, ErrConflict) {
			continue // такая категория уже есть — не трогаем
		}
		if err != nil {
			return n, err
		}
		n++
		for j, child := range top.children {
			id := parent.ID
			if _, err := s.CreateFinanceCategory(ctx, userID, FinanceCategory{
				ParentID: &id, Name: child, Kind: top.kind, Position: j,
			}); err != nil && !errors.Is(err, ErrConflict) {
				return n, err
			} else if err == nil {
				n++
			}
		}
	}
	return n, nil
}

func uniqueAsConflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return err
}

// --- транзакции ---

// FinanceTx — фактическая трата, доход или перевод между счетами.
type FinanceTx struct {
	ID           int64     `json:"id"`
	Kind         string    `json:"kind"`
	SpentOn      time.Time `json:"spent_on"`
	Amount       float64   `json:"amount"`
	Currency     string    `json:"currency"`
	BaseCurrency string    `json:"base_currency"`
	RateToBase   float64   `json:"rate_to_base"`
	CategoryID   *int64    `json:"category_id"`
	AccountID    *int64    `json:"account_id"`
	ToAccountID  *int64    `json:"to_account_id"`
	PlanID       *int64    `json:"plan_id"`
	PaymentID    *int64    `json:"payment_id"`
	Merchant     string    `json:"merchant"`
	Note         string    `json:"note"`
	ExternalID   *string   `json:"external_id"`
}

const financeTxCols = `id, kind, spent_on, amount, currency, base_currency,
	rate_to_base, category_id, account_id, to_account_id, plan_id, payment_id,
	merchant, note, external_id`

// FinanceTxFilter — параметры ленты трат.
type FinanceTxFilter struct {
	From       *time.Time
	To         *time.Time
	CategoryID int64 // с подкатегориями
	AccountID  int64
	Kind       string
	Query      string
	Limit      int
	Offset     int
}

func (s *Store) ListFinanceTx(ctx context.Context, userID int64, f FinanceTxFilter) ([]FinanceTx, int, error) {
	where := []string{"t.user_id = $1"}
	args := []any{userID}
	// один аргумент на условие: все «?» условия ссылаются на него же
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	if f.From != nil {
		add("t.spent_on >= ?", *f.From)
	}
	if f.To != nil {
		add("t.spent_on <= ?", *f.To)
	}
	if f.AccountID > 0 {
		add("(t.account_id = ? OR t.to_account_id = ?)", f.AccountID)
	}
	if f.Kind != "" {
		add("t.kind = ?", f.Kind)
	}
	if f.Query != "" {
		add("(t.note ILIKE ? OR t.merchant ILIKE ?)", "%"+f.Query+"%")
	}
	if f.CategoryID > 0 {
		// фильтр «по категории» включает всё поддерево: иначе выбор группы
		// «Еда» показывал бы пусто, когда траты лежат в подкатегориях
		// трата с разбивкой лежит в нескольких категориях сразу — иначе переход
		// из отчёта в ленту терял бы как раз разложенные чеки
		add(`(t.category_id IN (
			WITH RECURSIVE down AS (
				SELECT id FROM finance_categories WHERE user_id = $1 AND id = ?
				UNION ALL
				SELECT c.id FROM finance_categories c JOIN down ON c.parent_id = down.id
			) SELECT id FROM down)
			OR EXISTS (
				SELECT 1 FROM finance_tx_splits sp WHERE sp.tx_id = t.id
				  AND sp.category_id IN (
					WITH RECURSIVE down2 AS (
						SELECT id FROM finance_categories WHERE user_id = $1 AND id = ?
						UNION ALL
						SELECT c.id FROM finance_categories c JOIN down2 ON c.parent_id = down2.id
					) SELECT id FROM down2)))`, f.CategoryID)
	}
	cond := strings.Join(where, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM finance_transactions t WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	args = append(args, limit, f.Offset)
	rows, err := s.pool.Query(ctx, `
		SELECT `+financeTxCols+`
		FROM finance_transactions t
		WHERE `+cond+`
		ORDER BY t.spent_on DESC, t.id DESC
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[FinanceTx])
	return list, total, err
}

func (s *Store) FinanceTxByID(ctx context.Context, userID, id int64) (FinanceTx, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+financeTxCols+`
		FROM finance_transactions WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return FinanceTx{}, err
	}
	t, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[FinanceTx])
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

func (s *Store) CreateFinanceTx(ctx context.Context, userID int64, t FinanceTx) (FinanceTx, error) {
	rows, err := s.pool.Query(ctx, `
		INSERT INTO finance_transactions (user_id, kind, spent_on, amount, currency,
			base_currency, rate_to_base, category_id, account_id, to_account_id,
			plan_id, payment_id, merchant, note, external_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING `+financeTxCols,
		userID, t.Kind, t.SpentOn, t.Amount, t.Currency, t.BaseCurrency, t.RateToBase,
		t.CategoryID, t.AccountID, t.ToAccountID, t.PlanID, t.PaymentID, t.Merchant,
		t.Note, t.ExternalID)
	if err != nil {
		return t, uniqueAsConflict(err)
	}
	out, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[FinanceTx])
	return out, uniqueAsConflict(err)
}

func (s *Store) UpdateFinanceTx(ctx context.Context, userID int64, t FinanceTx) (FinanceTx, error) {
	rows, err := s.pool.Query(ctx, `
		UPDATE finance_transactions SET kind = $3, spent_on = $4, amount = $5,
			currency = $6, base_currency = $7, rate_to_base = $8, category_id = $9,
			account_id = $10, to_account_id = $11, merchant = $12, note = $13,
			updated_at = now()
		WHERE user_id = $1 AND id = $2
		RETURNING `+financeTxCols,
		userID, t.ID, t.Kind, t.SpentOn, t.Amount, t.Currency, t.BaseCurrency,
		t.RateToBase, t.CategoryID, t.AccountID, t.ToAccountID, t.Merchant, t.Note)
	if err != nil {
		return t, err
	}
	out, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[FinanceTx])
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, err
}

func (s *Store) DeleteFinanceTx(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM finance_transactions WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// FinanceStatRow — «сырая» строка для отчёта. Агрегация делается в Go: там же
// сворачивается дерево категорий и приводятся суммы, у которых базовая валюта
// отличается от нынешней.
type FinanceStatRow struct {
	SpentOn      time.Time
	Kind         string
	Amount       float64
	RateToBase   float64
	BaseCurrency string
	Currency     string
	CategoryID   *int64
	AccountID    *int64
}

// FinanceStatRows — все записи периода. Переводы между счетами исключены: это
// перекладывание своих денег, а не расход.
//
// LEFT JOIN на доли: у траты с разбивкой получается строка на каждую долю (со
// своей категорией и суммой), у остальных — одна строка с суммой целиком.
// Поэтому отчёт по категориям и круговая диаграмма считаются ОДНИМ механизмом,
// а месячные итоги не меняются: доли всегда сходятся с суммой траты.
func (s *Store) FinanceStatRows(ctx context.Context, userID int64, from, to time.Time) ([]FinanceStatRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.spent_on, t.kind, COALESCE(sp.amount, t.amount) AS amount,
		       t.rate_to_base, t.base_currency, t.currency,
		       CASE WHEN sp.id IS NULL THEN t.category_id ELSE sp.category_id END AS category_id,
		       t.account_id
		FROM finance_transactions t
		LEFT JOIN finance_tx_splits sp ON sp.tx_id = t.id
		WHERE t.user_id = $1 AND t.spent_on >= $2 AND t.spent_on <= $3
		  AND t.kind <> 'transfer'
		ORDER BY t.spent_on`, userID, from, to)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[FinanceStatRow])
}

// FinanceTxCurrencies — валюты, которыми пользователь уже платил: подсказка в
// формах, чтобы не набирать код руками.
func (s *Store) FinanceTxCurrencies(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT currency FROM (
			SELECT currency, count(*) AS n FROM finance_transactions
			WHERE user_id = $1 GROUP BY currency
			UNION ALL
			SELECT currency, count(*) FROM finance_accounts
			WHERE user_id = $1 AND archived_at IS NULL GROUP BY currency
		) t GROUP BY currency ORDER BY sum(n) DESC LIMIT 8`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
