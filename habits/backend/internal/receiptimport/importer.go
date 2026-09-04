// Package receiptimport связывает разбор писем магазинов с Finance: чек из
// письма превращается в трату.
//
// Вынесен отдельно, потому что нужен двоим: приёмнику почты (разбор на лету) и
// HTTP-обработчику (кнопка «разобрать ещё раз» для писем, пришедших до
// настройки).
package receiptimport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"

	"streaks-backend/internal/notify"
	"streaks-backend/internal/rates"
	"streaks-backend/internal/receipts"
	"streaks-backend/internal/store"
)

// Feature — код персонального доступа. Разбор чужих писем в чужие финансы —
// не то, что стоит включать всем: опция выдаётся в Админке поимённо.
const Feature = "mail.receipts"

type Importer struct {
	Store    *store.Store
	Rates    *rates.Cache
	Bot      *notify.Bot
	AdminIDs map[int64]bool
	Logger   *slog.Logger
}

// Allowed — можно ли пользователю разбирать чеки. Админ проходит всегда, как и
// на страницах.
func (i *Importer) Allowed(ctx context.Context, userID int64) bool {
	if i.AdminIDs[userID] {
		return true
	}
	ok, err := i.Store.FeatureAllowed(ctx, userID, Feature)
	if err != nil {
		i.Logger.Error("receipts: проверка доступа", "user_id", userID, "error", err)
		return false
	}
	return ok
}

// ImportFromMail разбирает письмо и записывает трату. Возвращает ошибку только
// для журнала: письмо уже сохранено, и потерять его разбор нельзя.
func (i *Importer) ImportFromMail(ctx context.Context, addr store.MailAddress, messageID int64, subject, body string) error {
	if addr.Parser == "" {
		return nil
	}
	if !i.Allowed(ctx, addr.UserID) {
		return fmt.Errorf("разбор чеков не разрешён пользователю %d", addr.UserID)
	}
	_, _, err := i.Import(ctx, addr, &messageID, subject, body, true)
	return err
}

// Refresh перечитывает уже разобранный чек новым разбором и пересобирает доли.
// Нужен, когда разбор поумнел: старые чеки иначе остаются с прежними данными.
func (i *Importer) Refresh(ctx context.Context, addr store.MailAddress, subject, body string) (store.MailReceipt, error) {
	parsed, err := receipts.Parse(addr.Parser, subject, body)
	if err != nil {
		return store.MailReceipt{}, err
	}
	old, err := i.Store.MailReceiptByOrder(ctx, addr.UserID, parsed.Parser, parsed.OrderNo)
	if err != nil {
		return store.MailReceipt{}, err
	}
	out, err := i.Store.RefreshMailReceipt(ctx, addr.UserID, old.ID, parsed)
	if err != nil {
		return out, err
	}
	return out, i.Classify(ctx, addr.UserID, out.ID)
}

// Import — общий путь для приёмника и кнопки «разобрать». notify выключается
// при ручном запуске: пользователь и так смотрит на экран.
func (i *Importer) Import(ctx context.Context, addr store.MailAddress, messageID *int64, subject, body string, notifyUser bool) (store.MailReceipt, store.FinanceTx, error) {
	parsed, err := receipts.Parse(addr.Parser, subject, body)
	if err != nil {
		return store.MailReceipt{}, store.FinanceTx{}, err
	}

	rec, err := i.Store.SaveMailReceipt(ctx, addr.UserID, messageID, parsed)
	if err != nil {
		// повторная пересылка того же заказа — не ошибка, а ожидаемый случай
		return store.MailReceipt{}, store.FinanceTx{}, err
	}

	settings, err := i.Store.GetFinanceSettings(ctx, addr.UserID)
	if err != nil {
		_ = i.Store.SetMailReceiptStatus(ctx, rec.ID, "failed", err.Error())
		return rec, store.FinanceTx{}, err
	}
	rate := 1.0
	if _, r, err := i.Rates.Convert(1, parsed.Currency, settings.BaseCurrency); err == nil {
		rate = r
	}

	// счёт из письма важнее счёта по умолчанию: в отчёте о поездке прямо
	// написано, откуда списали — с карты или наличными
	account := addr.ParserAccountID
	if id, ok := i.accountFor(ctx, addr.UserID, parsed.PaidWith); ok {
		account = id
	}

	out, tx, err := i.Store.ImportMailReceipt(ctx, addr.UserID, rec.ID, store.ImportReceiptParams{
		CategoryID:   addr.ParserCategoryID,
		AccountID:    account,
		BaseCurrency: settings.BaseCurrency,
		RateToBase:   rate,
	})
	if err != nil {
		if !errors.Is(err, store.ErrReceiptExists) {
			_ = i.Store.SetMailReceiptStatus(ctx, rec.ID, "failed", err.Error())
		}
		return out, tx, err
	}

	// сразу раскладываем по группам: ради этого разбор и затевался
	if err := i.Classify(ctx, addr.UserID, out.ID); err != nil {
		i.Logger.Error("receipts: разбор по группам", "receipt_id", out.ID, "error", err)
	}
	i.Logger.Info("receipts: чек записан в траты", "user_id", addr.UserID,
		"merchant", out.Merchant, "order", out.OrderNo, "total", out.Total, "tx_id", tx.ID)
	if notifyUser {
		i.notify(ctx, addr.UserID, out, len(parsed.Items))
	}
	return out, tx, nil
}

func (i *Importer) notify(ctx context.Context, userID int64, r store.MailReceipt, items int) {
	if i.Bot == nil {
		return
	}
	text := fmt.Sprintf("🧾 %s — чек на %s %s записан в траты\nЗаказ #%s, позиций: %d",
		r.Merchant, trimZeros(r.Total), strings.ToUpper(r.Currency), r.OrderNo, items)
	if err := i.Bot.SendMessage(ctx, userID, text); err != nil {
		i.Logger.Warn("receipts: уведомление", "error", err)
	}
}

// reLast4 — последние цифры карты в способе оплаты: «•••• 6307».
var reLast4 = regexp.MustCompile(`(\d{4})\s*$`)

// accountFor подбирает счёт по способу оплаты из письма.
//
// Совпадение ищется по названию и заметке счёта: пользователь называет карту
// «Visa 6307» — этого достаточно, отдельного поля «номер карты» заводить не
// стоит ради одной подстроки. Не нашли — молча оставляем счёт по умолчанию:
// приписать трату не тому кошельку хуже, чем не приписать никакому.
func (i *Importer) accountFor(ctx context.Context, userID int64, paidWith string) (*int64, bool) {
	p := strings.ToLower(strings.TrimSpace(paidWith))
	if p == "" {
		return nil, false
	}
	list, err := i.Store.ListFinanceAccounts(ctx, userID, false)
	if err != nil || len(list) == 0 {
		return nil, false
	}
	if m := reLast4.FindStringSubmatch(p); m != nil {
		for _, a := range list {
			if strings.Contains(a.Name, m[1]) || strings.Contains(a.Note, m[1]) {
				id := a.ID
				return &id, true
			}
		}
		return nil, false
	}
	if strings.Contains(p, "нал") || strings.Contains(p, "cash") {
		for _, a := range list {
			if a.Kind == "cash" {
				id := a.ID
				return &id, true
			}
		}
	}
	return nil, false
}

func trimZeros(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	s = strings.TrimSuffix(s, ".00")
	return s
}

// --- группы товаров и доли траты ---

// Classify расставляет категории позициям чека и пересобирает доли траты.
//
// Порядок источников: память решений пользователя → встроенный словарь →
// «Не разобрано». Молча не угадываем: неопознанное остаётся видимым куском.
func (i *Importer) Classify(ctx context.Context, userID, receiptID int64) error {
	rec, err := i.Store.MailReceiptByID(ctx, userID, receiptID)
	if err != nil {
		return err
	}
	items, err := i.Store.ListMailReceiptItems(ctx, receiptID)
	if err != nil {
		return err
	}
	groups, err := i.Store.ItemGroupMap(ctx, userID)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(items))
	for _, it := range items {
		keys = append(keys, it.NameKey)
	}
	rules, err := i.Store.ItemRulesFor(ctx, userID, rec.Merchant, keys)
	if err != nil {
		return err
	}
	words, err := i.Store.ListWordRules(ctx, userID)
	if err != nil {
		return err
	}

	for _, it := range items {
		// решение пользователя не трогаем: оно главнее любого словаря
		if it.Source == "manual" && it.CategoryID != nil {
			continue
		}
		var cat *int64
		source := ""
		if r, ok := rules[it.NameKey]; ok {
			id := r.CategoryID
			cat, source = &id, "memory"
		} else if id, ok := matchWord(words, it.Name); ok {
			// свои слова идут ДО встроенного словаря: ими переопределяют его
			cat, source = &id, "word"
		} else if code := receipts.GuessGroup(it.Name); code != "" {
			if id, ok := groups[code]; ok {
				cat, source = &id, "rule"
			}
		}
		if err := i.Store.SetReceiptItemCategory(ctx, userID, it.ID, cat, source); err != nil {
			return err
		}
	}
	return i.Resplit(ctx, userID, receiptID)
}

// matchWord — первое подходящее своё правило. Порядок задаёт пользователь:
// правила часто пересекаются («сок» и «сок берёзовый»), и побеждать должно то,
// что выше.
func matchWord(words []store.WordRule, name string) (int64, bool) {
	n := strings.ToLower(name)
	for _, w := range words {
		if strings.Contains(n, strings.ToLower(w.Pattern)) {
			return w.CategoryID, true
		}
	}
	return 0, false
}

// Resplit пересобирает доли траты по категориям позиций.
//
// Сумма позиций не сходится с итогом чека (магазин показывает каталожные цены,
// а считает по факту), поэтому товарные доли масштабируются пропорционально.
// Доставка и сервис идут отдельной долей, а не размазываются: видно, сколько
// в месяц стоит само удобство доставки.
func (i *Importer) Resplit(ctx context.Context, userID, receiptID int64) error {
	rec, err := i.Store.MailReceiptByID(ctx, userID, receiptID)
	if err != nil {
		return err
	}
	if rec.TxID == nil {
		return nil // чек не записан в траты — делить нечего
	}
	items, err := i.Store.ListMailReceiptItems(ctx, receiptID)
	if err != nil {
		return err
	}
	groups, err := i.Store.ItemGroupMap(ctx, userID)
	if err != nil {
		return err
	}

	byCat := map[int64]float64{}
	unknown := 0.0
	itemsTotal := 0.0
	for _, it := range items {
		itemsTotal += it.Amount
		if it.CategoryID == nil {
			unknown += it.Amount
			continue
		}
		byCat[*it.CategoryID] += it.Amount
	}

	fees := rec.DeliveryFee + rec.ServiceFee + rec.Tip
	goods := rec.Total - fees
	scale := 1.0
	if itemsTotal > 0 && goods > 0 {
		scale = goods / itemsTotal
	}

	splits := make([]store.TxSplit, 0, len(byCat)+2)
	for cat, sum := range byCat {
		id := cat
		splits = append(splits, store.TxSplit{CategoryID: &id, Amount: sum * scale})
	}
	// «Не разобрано» — доля без категории: честнее, чем свалить в «Прочее»
	if unknown > 0 {
		splits = append(splits, store.TxSplit{Amount: unknown * scale})
	}
	if fees > 0 {
		var cat *int64
		if id, ok := groups["delivery"]; ok {
			cat = &id
		}
		splits = append(splits, store.TxSplit{CategoryID: cat, Amount: fees})
	}
	// порядок долей стабильный: иначе они прыгают при каждом пересчёте
	sort.Slice(splits, func(a, b int) bool {
		return splits[a].Amount > splits[b].Amount
	})
	return i.Store.SetTxSplits(ctx, userID, *rec.TxID, splits)
}

// AssignItem — решение пользователя по товару. Применяется ко ВСЕМ чекам с этим
// товаром, включая прошлые: разметил один раз — отчёт стал правильным целиком.
func (i *Importer) AssignItem(ctx context.Context, userID int64, merchant, nameKey, sample string, categoryID *int64, remember bool, source string) error {
	if remember && categoryID != nil {
		if err := i.Store.RememberItemRule(ctx, userID, store.ItemRule{
			Merchant: merchant, NameKey: nameKey, NameSample: sample,
			CategoryID: *categoryID, Source: source,
		}); err != nil {
			return err
		}
	}
	receiptIDs, err := i.Store.ApplyItemRuleEverywhere(ctx, userID, nameKey, categoryID, "manual")
	if err != nil {
		return err
	}
	for _, id := range receiptIDs {
		if err := i.Resplit(ctx, userID, id); err != nil {
			return err
		}
	}
	return nil
}

// ClassifyAll перебирает все чеки пользователя заново.
//
// Нужен после любой правки соответствия групп категориям: без него смена
// привязки не трогала бы уже разобранные чеки, и настройка выглядела бы
// сломанной — отчёт оставался бы прежним.
func (i *Importer) ClassifyAll(ctx context.Context, userID int64) (int, error) {
	list, err := i.Store.ListMailReceipts(ctx, userID, 200)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range list {
		if err := i.Classify(ctx, userID, r.ID); err != nil {
			i.Logger.Error("receipts: пересбор чека", "receipt_id", r.ID, "error", err)
			continue
		}
		n++
	}
	return n, nil
}
