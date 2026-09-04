package store

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"streaks-backend/internal/receipts"
)

// Разбивка траты по категориям и память решений по товарам.

// TxSplit — доля траты. NULL-категория — «Не разобрано».
type TxSplit struct {
	ID         int64   `json:"id"`
	CategoryID *int64  `json:"category_id"`
	Amount     float64 `json:"amount"`
	Position   int     `json:"position"`
}

func (s *Store) ListTxSplits(ctx context.Context, txID int64) ([]TxSplit, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, category_id, amount, position
		FROM finance_tx_splits WHERE tx_id = $1 ORDER BY position, id`, txID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[TxSplit])
}

// SetTxSplits заменяет доли траты целиком.
//
// Суммы приводятся к сумме траты до копейки: расхождение неизбежно (доли
// масштабируются, суммы округляются), а разошедшийся отчёт хуже отсутствующего.
// Остаток от округления забирает самая крупная доля — там он незаметнее всего.
func (s *Store) SetTxSplits(ctx context.Context, userID, txID int64, splits []TxSplit) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var total float64
	if err := tx.QueryRow(ctx,
		`SELECT amount FROM finance_transactions WHERE user_id = $1 AND id = $2`,
		userID, txID).Scan(&total); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM finance_tx_splits WHERE tx_id = $1`, txID); err != nil {
		return err
	}
	splits = reconcile(splits, total)
	for i, sp := range splits {
		if sp.Amount == 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO finance_tx_splits (tx_id, category_id, amount, position)
			VALUES ($1,$2,$3,$4)`, txID, sp.CategoryID, sp.Amount, i); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// reconcile подгоняет доли под сумму траты.
func reconcile(splits []TxSplit, total float64) []TxSplit {
	if len(splits) == 0 {
		return splits
	}
	var sum float64
	biggest := 0
	for i := range splits {
		splits[i].Amount = round2(splits[i].Amount)
		sum += splits[i].Amount
		if math.Abs(splits[i].Amount) > math.Abs(splits[biggest].Amount) {
			biggest = i
		}
	}
	if diff := round2(total - sum); diff != 0 {
		splits[biggest].Amount = round2(splits[biggest].Amount + diff)
	}
	return splits
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// --- память решений по товарам ---

// ItemRule — «этот товар у этого магазина относится к этой категории».
type ItemRule struct {
	ID         int64  `json:"id"`
	Merchant   string `json:"merchant"`
	NameKey    string `json:"name_key"`
	NameSample string `json:"name_sample"`
	CategoryID int64  `json:"category_id"`
	Source     string `json:"source"`
	Hits       int    `json:"hits"`
}

// RememberItemRule запоминает решение. Повтор перезаписывает категорию: если
// человек передумал, спорить с ним не надо.
func (s *Store) RememberItemRule(ctx context.Context, userID int64, r ItemRule) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO finance_item_rules (user_id, merchant, name_key, name_sample,
			category_id, source, hits)
		VALUES ($1,$2,$3,$4,$5,$6,1)
		ON CONFLICT (user_id, merchant, name_key) DO UPDATE SET
			category_id = EXCLUDED.category_id,
			source = EXCLUDED.source,
			name_sample = EXCLUDED.name_sample,
			hits = finance_item_rules.hits + 1,
			updated_at = now()`,
		userID, r.Merchant, r.NameKey, r.NameSample, r.CategoryID, r.Source)
	return err
}

// ItemRulesFor — правила пользователя для набора названий: сначала правило
// магазина, затем общее (пустой merchant).
func (s *Store) ItemRulesFor(ctx context.Context, userID int64, merchant string, keys []string) (map[string]ItemRule, error) {
	out := map[string]ItemRule{}
	if len(keys) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, merchant, name_key, name_sample, category_id, source, hits
		FROM finance_item_rules
		WHERE user_id = $1 AND name_key = ANY($2) AND merchant IN ('', $3)
		ORDER BY merchant`, userID, keys, merchant)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[ItemRule])
	if err != nil {
		return nil, err
	}
	for _, r := range list {
		// правило магазина идёт последним в сортировке и перекрывает общее
		out[r.NameKey] = r
	}
	return out, nil
}

func (s *Store) ListItemRules(ctx context.Context, userID int64, limit int) ([]ItemRule, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, merchant, name_key, name_sample, category_id, source, hits
		FROM finance_item_rules WHERE user_id = $1
		ORDER BY hits DESC, name_key LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ItemRule])
}

func (s *Store) DeleteItemRule(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM finance_item_rules WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- соответствие встроенных групп категориям пользователя ---

func (s *Store) ItemGroupMap(ctx context.Context, userID int64) (map[string]int64, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT code, category_id FROM finance_item_groups WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var code string
		var id int64
		if err := rows.Scan(&code, &id); err != nil {
			return nil, err
		}
		out[code] = id
	}
	return out, rows.Err()
}

func (s *Store) SetItemGroup(ctx context.Context, userID int64, code string, categoryID int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO finance_item_groups (user_id, code, category_id) VALUES ($1,$2,$3)
		ON CONFLICT (user_id, code) DO UPDATE SET category_id = EXCLUDED.category_id`,
		userID, code, categoryID)
	return err
}

// SeedItemGroups заводит категории под встроенные группы и связывает их.
// Существующую категорию с таким же именем переиспользуем, а не плодим вторую.
//
// reset=true перепривязывает ВСЕ группы к своим категориям. Это не прихоть:
// если несколько групп смотрят в одну категорию, разбивка теряет смысл —
// диаграмма рисует один сектор, — и починить это иначе нечем.
func (s *Store) SeedItemGroups(ctx context.Context, userID int64, reset bool) (map[string]int64, error) {
	existing, err := s.ListFinanceCategories(ctx, userID, true)
	if err != nil {
		return nil, err
	}
	byName := map[string]int64{}
	for _, c := range existing {
		byName[receipts.NameKey(c.Name)] = c.ID
	}
	current, err := s.ItemGroupMap(ctx, userID)
	if err != nil {
		return nil, err
	}

	for i, g := range receipts.Groups {
		if _, ok := current[g.Code]; ok && !reset {
			continue
		}
		id, ok := byName[receipts.NameKey(g.Title)]
		if !ok {
			c, err := s.CreateFinanceCategory(ctx, userID, FinanceCategory{
				Name: g.Title, Icon: g.Icon, Kind: "expense", Position: 100 + i,
			})
			if err != nil {
				return nil, err
			}
			id = c.ID
		}
		if err := s.SetItemGroup(ctx, userID, g.Code, id); err != nil {
			return nil, err
		}
		current[g.Code] = id
	}
	return current, nil
}

// --- история цен ---

// PricePoint — цена товара в конкретном чеке.
type PricePoint struct {
	Date  time.Time `json:"date"`
	Qty   float64   `json:"qty"`
	Unit  string    `json:"unit"`
	Price float64   `json:"price"` // за единицу
	Total float64   `json:"total"`
}

// ItemPriceHistory — сколько стоил товар со временем. Цена за единицу, потому
// что количество в чеках плавает (вес, штуки).
type ItemPriceHistory struct {
	NameKey  string       `json:"name_key"`
	Name     string       `json:"name"`
	Currency string       `json:"currency"`
	Points   []PricePoint `json:"points"`
	Times    int          `json:"times"`
	Spent    float64      `json:"spent"`
	First    float64      `json:"first"`
	Last     float64      `json:"last"`
}

// TopItem — строка сводки «что покупаем чаще всего».
type TopItem struct {
	NameKey    string     `json:"name_key"`
	Name       string     `json:"name"`
	CategoryID *int64     `json:"category_id"`
	Times      int        `json:"times"`
	Qty        float64    `json:"qty"`
	Spent      float64    `json:"spent"`
	LastPrice  float64    `json:"last_price"`
	FirstPrice float64    `json:"first_price"`
	LastAt     *time.Time `json:"last_at"`
}

// TopItems — что покупается чаще и на что уходит больше. Цена за единицу
// берётся из первого и последнего чека: по ним видно подорожание.
func (s *Store) TopItems(ctx context.Context, userID int64, from, to time.Time, limit int) ([]TopItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		WITH pts AS (
			SELECT i.name_key, i.name, i.category_id, i.qty, i.amount,
			       COALESCE(r.purchased_on, r.created_at::date) AS day,
			       CASE WHEN i.qty > 0 THEN i.amount / i.qty ELSE i.amount END AS unit_price
			FROM mail_receipt_items i
			JOIN mail_receipts r ON r.id = i.receipt_id
			WHERE r.user_id = $1 AND i.name_key <> ''
			  AND COALESCE(r.purchased_on, r.created_at::date) BETWEEN $2 AND $3
		)
		SELECT name_key,
		       (array_agg(name ORDER BY day DESC))[1] AS name,
		       (array_agg(category_id ORDER BY day DESC))[1] AS category_id,
		       count(*) AS times,
		       sum(qty) AS qty,
		       sum(amount) AS spent,
		       (array_agg(unit_price ORDER BY day DESC))[1] AS last_price,
		       (array_agg(unit_price ORDER BY day))[1] AS first_price,
		       max(day) AS last_at
		FROM pts GROUP BY name_key
		ORDER BY sum(amount) DESC
		LIMIT $4`, userID, from, to, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[TopItem])
}

func (s *Store) ItemPrices(ctx context.Context, userID int64, nameKey string) (ItemPriceHistory, error) {
	h := ItemPriceHistory{NameKey: nameKey, Currency: "amd"}
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(r.purchased_on, r.created_at::date) AS day, i.qty, i.unit,
		       CASE WHEN i.qty > 0 THEN i.amount / i.qty ELSE i.amount END AS price,
		       i.amount, i.name, r.currency
		FROM mail_receipt_items i
		JOIN mail_receipts r ON r.id = i.receipt_id
		WHERE r.user_id = $1 AND i.name_key = $2
		ORDER BY day`, userID, nameKey)
	if err != nil {
		return h, err
	}
	defer rows.Close()
	for rows.Next() {
		var p PricePoint
		var name, cur string
		if err := rows.Scan(&p.Date, &p.Qty, &p.Unit, &p.Price, &p.Total, &name, &cur); err != nil {
			return h, err
		}
		h.Points = append(h.Points, p)
		h.Name, h.Currency = name, cur
		h.Spent += p.Total
	}
	h.Times = len(h.Points)
	if h.Times > 0 {
		h.First = h.Points[0].Price
		h.Last = h.Points[h.Times-1].Price
	}
	return h, rows.Err()
}

// --- позиции чека: категории ---

// SetReceiptItemCategory ставит категорию одной позиции.
func (s *Store) SetReceiptItemCategory(ctx context.Context, userID, itemID int64, categoryID *int64, source string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE mail_receipt_items i SET category_id = $3, source = $4
		FROM mail_receipts r
		WHERE r.id = i.receipt_id AND r.user_id = $1 AND i.id = $2`,
		userID, itemID, categoryID, source)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ApplyItemRuleEverywhere распространяет решение на ВСЕ чеки пользователя с
// этим товаром — и на прошлые тоже. Разметил один раз — отчёт стал правильным
// целиком, а не только начиная с сегодняшнего дня.
// Возвращает id чеков, у которых поменялись позиции: им нужно пересобрать доли.
func (s *Store) ApplyItemRuleEverywhere(ctx context.Context, userID int64, nameKey string, categoryID *int64, source string) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		UPDATE mail_receipt_items i SET category_id = $3, source = $4
		FROM mail_receipts r
		WHERE r.id = i.receipt_id AND r.user_id = $1 AND i.name_key = $2
		RETURNING i.receipt_id`, userID, nameKey, categoryID, source)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	out := []int64{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// UnclassifiedItems — позиции без категории: то самое «Не разобрано», по
// которому и ведётся разметка.
func (s *Store) UnclassifiedItems(ctx context.Context, userID int64, limit int) ([]TopItem, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT i.name_key,
		       (array_agg(i.name))[1] AS name,
		       NULL::bigint AS category_id,
		       count(*) AS times,
		       sum(i.qty) AS qty,
		       sum(i.amount) AS spent,
		       (array_agg(i.amount ORDER BY r.created_at DESC))[1] AS last_price,
		       (array_agg(i.amount ORDER BY r.created_at))[1] AS first_price,
		       max(COALESCE(r.purchased_on, r.created_at::date)) AS last_at
		FROM mail_receipt_items i
		JOIN mail_receipts r ON r.id = i.receipt_id
		WHERE r.user_id = $1 AND i.category_id IS NULL AND i.name_key <> ''
		GROUP BY i.name_key
		ORDER BY sum(i.amount) DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[TopItem])
}

// --- свои словарные правила ---

// WordRule — «всё, где есть это слово, — в эту категорию». Проверяется ДО
// встроенного словаря, поэтому им же можно его переопределить.
type WordRule struct {
	ID         int64  `json:"id"`
	Pattern    string `json:"pattern"`
	CategoryID int64  `json:"category_id"`
	Position   int    `json:"position"`
}

func (s *Store) ListWordRules(ctx context.Context, userID int64) ([]WordRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, pattern, category_id, position
		FROM finance_word_rules WHERE user_id = $1
		ORDER BY position, lower(pattern)`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[WordRule])
}

func (s *Store) CreateWordRule(ctx context.Context, userID int64, r WordRule) (WordRule, error) {
	rows, err := s.pool.Query(ctx, `
		INSERT INTO finance_word_rules (user_id, pattern, category_id, position)
		VALUES ($1,$2,$3,$4)
		RETURNING id, pattern, category_id, position`,
		userID, r.Pattern, r.CategoryID, r.Position)
	if err != nil {
		return r, uniqueAsConflict(err)
	}
	out, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[WordRule])
	return out, uniqueAsConflict(err)
}

func (s *Store) DeleteWordRule(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM finance_word_rules WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CategoryItemStats — сколько запомненных товаров и своих слов смотрит в
// категорию. Нужно экрану групп: без этого непонятно, что вообще настроено.
type CategoryItemStats struct {
	CategoryID int64 `json:"category_id"`
	Items      int   `json:"items"`
	Words      int   `json:"words"`
}

func (s *Store) CategoryItemStats(ctx context.Context, userID int64) ([]CategoryItemStats, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id,
		       (SELECT count(*) FROM finance_item_rules r
		          WHERE r.user_id = $1 AND r.category_id = c.id),
		       (SELECT count(*) FROM finance_word_rules w
		          WHERE w.user_id = $1 AND w.category_id = c.id)
		FROM finance_categories c
		WHERE c.user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[CategoryItemStats])
}
