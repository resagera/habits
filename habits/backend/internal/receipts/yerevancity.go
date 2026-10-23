package receipts

import (
	"regexp"
	"strings"
	"time"
)

// Разбор писем «Order #NNN delivered» магазина Yerevan City.
//
// Письмо пересылается вручную, поэтому формат приходит в двух видах: Gmail
// вставляет «---------- Forwarded message ---------», почта Apple цитирует всё
// через «> ». Разбор идёт по уже расцитированному тексту, так что оба вида
// сводятся к одному.
//
// Строка позиции: «<название>  x <кол-во>[ единица]  <сумма>֏». В названии
// встречается своё « x » («… 8270 x 10»), поэтому количество ищется жадно —
// по ПОСЛЕДНЕМУ разделителю перед суммой.

const dram = "֏" // ֏

var (
	reOrderNo = regexp.MustCompile(`(?i)order\s*#\s*(\d{3,})`)
	// после «Delivery date» дата стоит на следующей строке, но формат у
	// магазина менялся: в 2024-м «10/13/2024 2:24:54 PM», сейчас
	// «2026-08-04 17:53». Берём строку целиком и пробуем известные виды.
	reDateLine = regexp.MustCompile(`(?i)delivery date[ \t]*\n+[ \t]*([^\n]+)`)
	// имя жадное: разделитель берётся ПОСЛЕДНИЙ. При ленивом «Item x 2 x 3
	// 100.00֏» разобралось бы как количество 2 с единицей «x»
	reItem = regexp.MustCompile(
		`^(.+)\s+x\s+([\d.,]+)\s*([a-zA-Zа-яА-Я]{0,4})\s+([\d\s.,]+)` + dram + `\s*$`)
	rePaidWith = regexp.MustCompile(`(?i)` + dram + `\s*paid with\s+([A-Za-zА-Яа-я]+)`)
	// «Total 16027.46֏» и «Total24656.22֏» — пробел между словом и суммой
	// то есть, то нет
	reAmountLine = regexp.MustCompile(`^([A-Za-z][A-Za-z ]*?)\s*([\d\s.,]+)` + dram + `\s*$`)
)

func parseYerevanCity(subject, body string) (Receipt, error) {
	text := clean(body)
	r := Receipt{
		Parser: "yerevancity", Merchant: "Yerevan City", Currency: "amd",
	}

	if m := reOrderNo.FindStringSubmatch(text); m != nil {
		r.OrderNo = m[1]
	} else if m := reOrderNo.FindStringSubmatch(subject); m != nil {
		// тело пересланного письма иногда обрезано, тема — почти никогда
		r.OrderNo = m[1]
	}

	if m := reDateLine.FindStringSubmatch(text); m != nil {
		if t, ok := parseDate(m[1]); ok {
			r.PurchasedAt = &t
		}
	}
	if m := rePaidWith.FindStringSubmatch(text); m != nil {
		r.PaidWith = strings.ToLower(m[1])
	}

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, dram) {
			continue
		}
		// сначала итоговые строки: «Subtotal» содержит «total», поэтому
		// порядок проверок важен
		if name, v, ok := amountLine(line); ok {
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "subtotal":
				r.Subtotal = v
				continue
			case "delivery fee":
				r.DeliveryFee = v
				continue
			case "service fee":
				r.ServiceFee = v
				continue
			case "driver tip", "tip":
				r.Tip = v
				continue
			case "total":
				r.Total = v
				continue
			}
		}
		if it, ok := itemLine(line); ok {
			r.Items = append(r.Items, it)
		}
	}

	// Чек без итога импортировать нельзя: непонятно, сколько списывать.
	// Это же отсекает обычные письма магазина — рекламу и подтверждения.
	if r.Total == 0 && len(r.Items) == 0 {
		return r, ErrNotReceipt
	}
	if r.Total == 0 {
		// итог мог не распознаться, но позиции есть — собираем из них
		for _, it := range r.Items {
			r.Total += it.Amount
		}
		r.Total += r.DeliveryFee + r.ServiceFee + r.Tip
	}
	if r.OrderNo == "" {
		return r, ErrNotReceipt // без номера заказа нет защиты от повторов
	}
	return r, nil
}

// dateLayouts — виды даты доставки, встречавшиеся в письмах магазина.
// Однозначные «1/2/2006» в шаблоне не случайны: с ними Go разбирает и
// «10/13/2024», а с «01/02/2006» — только двузначные.
var dateLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"1/2/2006 3:04:05 PM",
	"1/2/2006 3:04 PM",
	"1/2/2006 15:04:05",
	"1/2/2006 15:04",
	"1/2/2006",
	"02.01.2006 15:04",
	"02.01.2006",
}

func parseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func itemLine(line string) (Item, bool) {
	m := reItem.FindStringSubmatch(line)
	if m == nil {
		return Item{}, false
	}
	qty, okQty := num(m[2])
	amount, okAmount := num(m[4])
	if !okQty || !okAmount {
		return Item{}, false
	}
	name := strings.TrimSpace(m[1])
	if name == "" {
		return Item{}, false
	}
	return Item{Name: name, Qty: qty, Unit: strings.ToLower(m[3]), Amount: amount}, true
}

func amountLine(line string) (string, float64, bool) {
	m := reAmountLine.FindStringSubmatch(line)
	if m == nil {
		return "", 0, false
	}
	v, ok := num(m[2])
	if !ok {
		return "", 0, false
	}
	return m[1], v, true
}
