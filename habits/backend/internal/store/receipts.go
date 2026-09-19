package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"streaks-backend/internal/receipts"
)

// MailReceipt — чек, разобранный из письма магазина.
type MailReceipt struct {
	ID          int64      `json:"id"`
	MessageID   *int64     `json:"message_id"`
	Parser      string     `json:"parser"`
	Merchant    string     `json:"merchant"`
	OrderNo     string     `json:"order_no"`
	PurchasedAt *time.Time `json:"purchased_at"`
	PurchasedOn *time.Time `json:"purchased_on"`
	Currency    string     `json:"currency"`
	Subtotal    float64    `json:"subtotal"`
	DeliveryFee float64    `json:"delivery_fee"`
	ServiceFee  float64    `json:"service_fee"`
	Tip         float64    `json:"tip"`
	Total       float64    `json:"total"`
	PaidWith    string     `json:"paid_with"`
	TxID        *int64     `json:"tx_id"`
	Status      string     `json:"status"`
	Error       string     `json:"error"`
	Note        string     `json:"note"`
	CreatedAt   time.Time  `json:"created_at"`
}

// MailReceiptItem — позиция чека. В Finance не попадает: поход в магазин это
// одна трата, а не двадцать.
type MailReceiptItem struct {
	ID       int64   `json:"id"`
	Position int     `json:"position"`
	Name     string  `json:"name"`
	Qty      float64 `json:"qty"`
	Unit     string  `json:"unit"`
	Amount   float64 `json:"amount"`
	// группа товара: категория из дерева и откуда она взялась
	CategoryID *int64 `json:"category_id"`
	Source     string `json:"source"`
	NameKey    string `json:"name_key"`
}

const mailReceiptCols = `id, message_id, parser, merchant, order_no, purchased_at,
	purchased_on, currency, subtotal, delivery_fee, service_fee, tip, total, paid_with, tx_id,
	status, error, note, created_at`

// ErrReceiptExists — чек этого заказа уже разобран. Письмо переслали дважды.
var ErrReceiptExists = errors.New("чек уже разобран")

// SaveMailReceipt пишет чек с позициями. Повтор заказа отбивается уникальным
// индексом: письмо легко переслать дважды, и трата не должна удвоиться.
func (s *Store) SaveMailReceipt(ctx context.Context, userID int64, messageID *int64, r receipts.Receipt) (MailReceipt, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MailReceipt{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO mail_receipts (user_id, message_id, parser, merchant, order_no,
			purchased_at, purchased_on, currency, subtotal, delivery_fee, service_fee,
			tip, total, paid_with, note)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING id`,
		userID, messageID, r.Parser, r.Merchant, r.OrderNo, r.PurchasedAt,
		purchasedOn(r.PurchasedAt), r.Currency,
		r.Subtotal, r.DeliveryFee, r.ServiceFee, r.Tip, r.Total, r.PaidWith, r.Note).Scan(&id)
	if err != nil {
		if errors.Is(uniqueAsConflict(err), ErrConflict) {
			return MailReceipt{}, ErrReceiptExists
		}
		return MailReceipt{}, err
	}
	for i, it := range r.Items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO mail_receipt_items (receipt_id, position, name, qty, unit,
				amount, name_key)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			id, i, it.Name, it.Qty, it.Unit, it.Amount,
			receipts.NameKey(it.Name)); err != nil {
			return MailReceipt{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return MailReceipt{}, err
	}
	return s.MailReceiptByID(ctx, userID, id)
}

func (s *Store) MailReceiptByID(ctx context.Context, userID, id int64) (MailReceipt, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+mailReceiptCols+`
		FROM mail_receipts WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return MailReceipt{}, err
	}
	r, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[MailReceipt])
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (s *Store) MailReceiptByMessage(ctx context.Context, userID, messageID int64) (MailReceipt, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+mailReceiptCols+`
		FROM mail_receipts WHERE user_id = $1 AND message_id = $2
		ORDER BY id DESC LIMIT 1`, userID, messageID)
	if err != nil {
		return MailReceipt{}, err
	}
	r, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[MailReceipt])
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (s *Store) ListMailReceipts(ctx context.Context, userID int64, limit int) ([]MailReceipt, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+mailReceiptCols+`
		FROM mail_receipts WHERE user_id = $1
		ORDER BY COALESCE(purchased_at, created_at) DESC, id DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[MailReceipt])
}

func (s *Store) ListMailReceiptItems(ctx context.Context, receiptID int64) ([]MailReceiptItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, position, name, qty, unit, amount, category_id, source, name_key
		FROM mail_receipt_items WHERE receipt_id = $1 ORDER BY position, id`, receiptID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[MailReceiptItem])
}

func (s *Store) SetMailReceiptStatus(ctx context.Context, id int64, status, errText string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE mail_receipts SET status = $2, error = $3, updated_at = now()
		WHERE id = $1`, id, status, errText)
	return err
}

// DeleteMailReceipt убирает чек. Созданная трата остаётся: удаление разбора не
// должно молча стирать деньги из отчёта — за трату отвечает Finance.
func (s *Store) DeleteMailReceipt(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM mail_receipts WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ImportReceiptParams — куда записывать трату.
type ImportReceiptParams struct {
	CategoryID   *int64
	AccountID    *int64
	BaseCurrency string
	RateToBase   float64
	TzOff        int
}

// ImportMailReceipt создаёт трату по чеку и связывает её с ним.
//
// Сумма — Total: позиции у магазина показаны по каталожным ценам и с итогом не
// сходятся (вес, скидки), а из кошелька уходит именно итог. external_id даёт
// вторую защиту от повторов помимо уникального номера заказа.
func (s *Store) ImportMailReceipt(ctx context.Context, userID, receiptID int64, p ImportReceiptParams) (MailReceipt, FinanceTx, error) {
	r, err := s.MailReceiptByID(ctx, userID, receiptID)
	if err != nil {
		return MailReceipt{}, FinanceTx{}, err
	}
	if r.TxID != nil {
		return r, FinanceTx{}, ErrConflict // уже записан
	}
	if r.Total <= 0 {
		return r, FinanceTx{}, fmt.Errorf("нулевой итог")
	}

	items, err := s.ListMailReceiptItems(ctx, receiptID)
	if err != nil {
		return r, FinanceTx{}, err
	}
	// Дата траты — календарная дата чека, а не момент времени: покупка в 23:23
	// по местному времени не должна уезжать в следующий день из-за зон.
	spent := time.Now().UTC()
	if r.PurchasedOn != nil {
		spent = *r.PurchasedOn
	}
	ext := r.Parser + ":" + r.OrderNo
	note := receiptNote(r, len(items))

	tx, err := s.CreateFinanceTx(ctx, userID, FinanceTx{
		Kind:         "expense",
		SpentOn:      time.Date(spent.Year(), spent.Month(), spent.Day(), 0, 0, 0, 0, time.UTC),
		Amount:       r.Total,
		Currency:     r.Currency,
		BaseCurrency: p.BaseCurrency,
		RateToBase:   p.RateToBase,
		CategoryID:   p.CategoryID,
		AccountID:    p.AccountID,
		Merchant:     r.Merchant,
		Note:         note,
		ExternalID:   &ext,
	})
	if err != nil {
		if errors.Is(err, ErrConflict) {
			_ = s.SetMailReceiptStatus(ctx, receiptID, "skipped", "трата с таким заказом уже есть")
			r.Status = "skipped"
			return r, FinanceTx{}, ErrReceiptExists
		}
		return r, FinanceTx{}, err
	}

	if _, err := s.pool.Exec(ctx, `
		UPDATE mail_receipts SET tx_id = $2, status = 'imported', error = '',
			updated_at = now()
		WHERE id = $1`, receiptID, tx.ID); err != nil {
		return r, tx, err
	}
	r.TxID, r.Status = &tx.ID, "imported"
	return r, tx, nil
}

// receiptNote — заметка к трате. Разборщик, у которого есть что рассказать
// (маршрут поездки, машина), пишет её сам; чекам магазинов рассказывать нечего,
// кроме номера заказа и числа позиций — у них смысл в самих позициях.
func receiptNote(r MailReceipt, items int) string {
	if strings.TrimSpace(r.Note) != "" {
		return r.Note
	}
	note := fmt.Sprintf("Заказ #%s, позиций: %d", r.OrderNo, items)
	if r.PaidWith != "" {
		note += ", оплата: " + r.PaidWith
	}
	return note
}

// purchasedOn — календарная дата чека. Время из письма разобрано как UTC,
// поэтому компоненты берём из него же, не переводя зоны.
func purchasedOn(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	d := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	return &d
}

// --- настройки разбора у адреса ---

// MailAddressParser — что делать с письмами на адрес.
type MailAddressParser struct {
	Parser     string
	CategoryID *int64
	AccountID  *int64
}

func (s *Store) SetMailAddressParser(ctx context.Context, userID, id int64, p MailAddressParser) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE mail_addresses SET parser = $3, parser_category_id = $4,
			parser_account_id = $5, updated_at = now()
		WHERE user_id = $1 AND id = $2`,
		userID, id, strings.TrimSpace(p.Parser), p.CategoryID, p.AccountID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// FeatureAllowed — выдана ли пользователю персональная опция. Админ проходит
// везде, как и на страницах.
func (s *Store) FeatureAllowed(ctx context.Context, userID int64, feature string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM feature_access WHERE user_id = $1 AND feature = $2)`,
		userID, feature).Scan(&ok)
	return ok, err
}

// MailReceiptByOrder — чек этого заказа, если он уже разобран.
func (s *Store) MailReceiptByOrder(ctx context.Context, userID int64, parser, orderNo string) (MailReceipt, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+mailReceiptCols+`
		FROM mail_receipts
		WHERE user_id = $1 AND parser = $2 AND order_no = $3`, userID, parser, orderNo)
	if err != nil {
		return MailReceipt{}, err
	}
	r, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[MailReceipt])
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// RefreshMailReceipt перечитывает уже разобранный чек новым разбором: суммы,
// дату, позиции — и подтягивает за ними созданную трату.
//
// Нужен, когда разбор поумнел. Пример из жизни: в письмах 2024 года дата
// доставки записана иначе, и чеки легли сегодняшним числом; без перечитывания
// исправить их можно было бы только руками в базе.
func (s *Store) RefreshMailReceipt(ctx context.Context, userID, receiptID int64, r receipts.Receipt) (MailReceipt, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MailReceipt{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var txID *int64
	if err := tx.QueryRow(ctx, `
		UPDATE mail_receipts SET merchant = $3, purchased_at = $4, purchased_on = $5,
			currency = $6, subtotal = $7, delivery_fee = $8, service_fee = $9,
			tip = $10, total = $11, paid_with = $12, note = $13, updated_at = now()
		WHERE user_id = $1 AND id = $2
		RETURNING tx_id`,
		userID, receiptID, r.Merchant, r.PurchasedAt, purchasedOn(r.PurchasedAt),
		r.Currency, r.Subtotal, r.DeliveryFee, r.ServiceFee, r.Tip, r.Total,
		r.PaidWith, r.Note).Scan(&txID); errors.Is(err, pgx.ErrNoRows) {
		return MailReceipt{}, ErrNotFound
	} else if err != nil {
		return MailReceipt{}, err
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM mail_receipt_items WHERE receipt_id = $1`, receiptID); err != nil {
		return MailReceipt{}, err
	}
	for i, it := range r.Items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO mail_receipt_items (receipt_id, position, name, qty, unit,
				amount, name_key)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			receiptID, i, it.Name, it.Qty, it.Unit, it.Amount,
			receipts.NameKey(it.Name)); err != nil {
			return MailReceipt{}, err
		}
	}

	// трата идёт за чеком: иначе дата и сумма разъедутся с ним
	if txID != nil {
		spent := time.Now().UTC()
		if d := purchasedOn(r.PurchasedAt); d != nil {
			spent = *d
		}
		note := receiptNote(MailReceipt{OrderNo: r.OrderNo, PaidWith: r.PaidWith, Note: r.Note}, len(r.Items))
		if _, err := tx.Exec(ctx, `
			UPDATE finance_transactions
			SET spent_on = $3, amount = $4, note = $5, updated_at = now()
			WHERE user_id = $1 AND id = $2`,
			userID, *txID, time.Date(spent.Year(), spent.Month(), spent.Day(), 0, 0, 0, 0, time.UTC),
			r.Total, note); err != nil {
			return MailReceipt{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return MailReceipt{}, err
	}
	return s.MailReceiptByID(ctx, userID, receiptID)
}
