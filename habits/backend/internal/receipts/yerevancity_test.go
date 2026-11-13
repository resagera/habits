package receipts

import (
	"errors"
	"math"
	"testing"
)

// Письма в тестах синтетические: формат снят с настоящих, но имена, телефоны и
// адреса выдуманы — репозиторий публикуется, личным данным там не место.

// Пересылка из Gmail: «---------- Forwarded message ---------», без цитирования.
const gmailForward = `---------- Forwarded message ---------
От: Yerevan City Online <noreply@yerevan-city.am>
Date: ср, 25 февр. 2026 г. в 23:23
Subject: Order #3424178 delivered
To: <someone@example.com>


THANKS FOR YOUR PURCHASE

Hello NAME. Your order #3424178 was successfully delivered. Thank you for
choosing us!

Delivery address

село Примерное

Delivery date

2026-02-25 23:23

Recipient

NAME Surname,

+37400000000
Beef fillet kg (local) x 650 g 4608.50֏
Cilantro pc x 3 690.00֏
Napkins "Zhi Yin" Hey Nose 4ply 115pcs 8270 x 10 3900.00֏
Polyethylene pack "Yerevan City" 50micron, medium x 3 150.00֏
Subtotal 15128.46֏
Delivery fee 700.00֏
Driver tip 0.00֏
Service fee 199.00֏
Total 16027.46֏
16027.46֏ paid with Card

[image: footerLogo]
`

// Пересылка из почты Apple: всё процитировано «> », и суммы прижаты к слову.
const appleForward = `

> Начало переадресованного письма:
>
> Отправитель: Yerevan City Online <noreply@yerevan-city.am>
> Тема: Order #4168532 delivered
> Дата: 4 августа 2026 г. в 17:53:14 GMT+4
> Кому: someone@example.com
>
>
> THANKS FOR YOUR PURCHASE
>
> Hello NAME. Your order #4168532 was successfully delivered. Thank you for choosing us!
>
> Delivery address
> село Примерное
>
> Delivery date
> 2026-08-04 17:53
>
> Recipient
> NAME Surname,
> +37400000000
>
> Soy sauce "Sen Soy" classic 1l  x 1   1060.00֏
>
> Potato (armenian) kg  x 1.54 kg  377.30֏
>
> Beef tenderloin with bones kg (local)  x 1 kg  5290.00֏
>
> Polyethylene pack "Yerevan City" 50micron, medium  x 2   100.00֏
> Subtotal23757.22֏
> Delivery fee700.00֏
> Driver tip0.00֏
> Service fee199.00֏
> Total24656.22֏
> 24656.22֏ paid with Cash
`

func eq(a, b float64) bool { return math.Abs(a-b) < 0.005 }

func TestParseGmailForward(t *testing.T) {
	r, err := Parse("yerevancity", "Fwd: Order #3424178 delivered", gmailForward)
	if err != nil {
		t.Fatalf("разбор не удался: %v", err)
	}
	if r.OrderNo != "3424178" {
		t.Errorf("номер заказа: %q", r.OrderNo)
	}
	if r.PurchasedAt == nil || r.PurchasedAt.Format("2006-01-02 15:04") != "2026-02-25 23:23" {
		t.Errorf("дата покупки: %v", r.PurchasedAt)
	}
	if !eq(r.Total, 16027.46) || !eq(r.Subtotal, 15128.46) ||
		!eq(r.DeliveryFee, 700) || !eq(r.ServiceFee, 199) || !eq(r.Tip, 0) {
		t.Errorf("итоги: total=%v sub=%v dlv=%v svc=%v tip=%v",
			r.Total, r.Subtotal, r.DeliveryFee, r.ServiceFee, r.Tip)
	}
	if r.PaidWith != "card" {
		t.Errorf("способ оплаты: %q", r.PaidWith)
	}
	if r.Currency != "amd" {
		t.Errorf("валюта: %q", r.Currency)
	}
	if len(r.Items) != 4 {
		t.Fatalf("позиций %d, ожидалось 4: %+v", len(r.Items), r.Items)
	}
	// вес: количество с единицей
	if r.Items[0].Name != "Beef fillet kg (local)" || !eq(r.Items[0].Qty, 650) ||
		r.Items[0].Unit != "g" || !eq(r.Items[0].Amount, 4608.50) {
		t.Errorf("первая позиция разобрана неверно: %+v", r.Items[0])
	}
	// в названии есть своё « x » — количество берётся по последнему разделителю
	if r.Items[2].Name != `Napkins "Zhi Yin" Hey Nose 4ply 115pcs 8270` ||
		!eq(r.Items[2].Qty, 10) || !eq(r.Items[2].Amount, 3900) {
		t.Errorf("позиция с « x » в названии: %+v", r.Items[2])
	}
}

func TestParseAppleForwardQuoted(t *testing.T) {
	r, err := Parse("yerevancity", "Fwd: Order #4168532 delivered", appleForward)
	if err != nil {
		t.Fatalf("разбор не удался: %v", err)
	}
	if r.OrderNo != "4168532" {
		t.Errorf("номер заказа: %q", r.OrderNo)
	}
	// суммы прижаты к слову: «Subtotal23757.22֏»
	if !eq(r.Total, 24656.22) || !eq(r.Subtotal, 23757.22) || !eq(r.DeliveryFee, 700) {
		t.Errorf("итоги: %+v", r)
	}
	if r.PaidWith != "cash" {
		t.Errorf("способ оплаты: %q", r.PaidWith)
	}
	if len(r.Items) != 4 {
		t.Fatalf("позиций %d, ожидалось 4: %+v", len(r.Items), r.Items)
	}
	// дробное количество с единицей
	if r.Items[1].Name != "Potato (armenian) kg" || !eq(r.Items[1].Qty, 1.54) ||
		r.Items[1].Unit != "kg" || !eq(r.Items[1].Amount, 377.30) {
		t.Errorf("дробное количество: %+v", r.Items[1])
	}
	if r.PurchasedAt == nil || r.PurchasedAt.Format("2006-01-02 15:04") != "2026-08-04 17:53" {
		t.Errorf("дата покупки: %v", r.PurchasedAt)
	}
}

func TestOrderNumberFromSubjectWhenBodyTruncated(t *testing.T) {
	// пересылка иногда обрезает тело — номер должен взяться из темы
	body := "Delivery date\n2026-08-04 17:53\nMilk 1l x 1 500.00֏\nTotal 500.00֏"
	r, err := Parse("yerevancity", "Fwd: Order #999111 delivered", body)
	if err != nil {
		t.Fatalf("разбор не удался: %v", err)
	}
	if r.OrderNo != "999111" {
		t.Errorf("номер из темы: %q", r.OrderNo)
	}
}

func TestNotAReceipt(t *testing.T) {
	// на адрес приходит и обычная почта — она не должна становиться тратой
	for _, body := range []string{
		"Здравствуйте! У нас скидки на выходных, заходите.",
		"Your order #4168532 has been confirmed and is being prepared.",
		"",
	} {
		if _, err := Parse("yerevancity", "Weekly offers", body); !errors.Is(err, ErrNotReceipt) {
			t.Errorf("письмо %q не должно считаться чеком (err=%v)", body, err)
		}
	}
}

func TestReceiptWithoutOrderNumberRejected(t *testing.T) {
	// без номера заказа нет защиты от повторного импорта — такой чек не берём
	body := "Milk 1l x 1 500.00֏\nTotal 500.00֏"
	if _, err := Parse("yerevancity", "delivered", body); !errors.Is(err, ErrNotReceipt) {
		t.Errorf("чек без номера должен отклоняться, получено %v", err)
	}
}

func TestTotalRecoveredFromItems(t *testing.T) {
	// если строка «Total» не распозналась, итог собирается из позиций и сборов
	body := "Order #12345\nMilk 1l x 1 500.00֏\nBread x 2 300.00֏\n" +
		"Delivery fee 700.00֏\nService fee 199.00֏"
	r, err := Parse("yerevancity", "", body)
	if err != nil {
		t.Fatalf("разбор не удался: %v", err)
	}
	if !eq(r.Total, 1699) {
		t.Errorf("итог из позиций: %v, ожидалось 1699", r.Total)
	}
}

func TestItemNameWithMultipleSeparators(t *testing.T) {
	// «Item x 2 x 3 100.00֏»: количество — по последнему разделителю
	it, ok := itemLine("Battery AA x 4 pack x 2 500.00֏")
	if !ok {
		t.Fatal("строка не разобрана")
	}
	if it.Name != "Battery AA x 4 pack" || !eq(it.Qty, 2) || !eq(it.Amount, 500) {
		t.Errorf("разобрано неверно: %+v", it)
	}
}

func TestNonBreakingSpacesSurvive(t *testing.T) {
	// в письмах живут узкие и неразрывные пробелы — числа с ними не парсятся
	body := "Order #777\nMilk 1l  x 1   1 060.00֏\nTotal 1 060.00֏"
	r, err := Parse("yerevancity", "", body)
	if err != nil {
		t.Fatalf("разбор не удался: %v", err)
	}
	if !eq(r.Total, 1060) || len(r.Items) != 1 || !eq(r.Items[0].Amount, 1060) {
		t.Errorf("неразрывные пробелы сломали разбор: %+v", r)
	}
}

func TestUnknownParser(t *testing.T) {
	if _, err := Parse("wildberries", "", "что-то"); err == nil {
		t.Error("неизвестный разборщик должен возвращать ошибку")
	}
}

func TestDeliveryDateFormats(t *testing.T) {
	// Формат менялся: в 2024-м магазин слал «10/13/2024 2:24:54 PM», сейчас
	// «2026-08-04 17:53». Непонятая дата = трата уезжает в сегодняшний день,
	// то есть покупка 2024 года попадает в текущий месяц отчёта.
	cases := map[string]string{
		"2026-08-04 17:53":      "2026-08-04",
		"2026-08-04 17:53:10":   "2026-08-04",
		"10/13/2024 2:24:54 PM": "2024-10-13",
		"1/2/2024 11:05:00 AM":  "2024-01-02",
		"10/13/2024 14:24:54":   "2024-10-13",
		"13.10.2024 14:24":      "2024-10-13",
	}
	for in, want := range cases {
		body := "Order #777\nDelivery date\n" + in + "\nMilk x 1 500.00֏\nTotal 500.00֏"
		r, err := Parse("yerevancity", "", body)
		if err != nil {
			t.Fatalf("%q: разбор не удался: %v", in, err)
		}
		if r.PurchasedAt == nil {
			t.Errorf("%q: дата не разобрана", in)
			continue
		}
		if got := r.PurchasedAt.Format("2006-01-02"); got != want {
			t.Errorf("%q: дата %s, ожидалось %s", in, got, want)
		}
	}
}

func TestUnknownDateFormatIsNotFatal(t *testing.T) {
	// непонятная дата не должна ронять разбор — чек важнее даты
	body := "Order #777\nDelivery date\nкогда-то в среду\nMilk x 1 500.00֏\nTotal 500.00֏"
	r, err := Parse("yerevancity", "", body)
	if err != nil {
		t.Fatalf("разбор не удался: %v", err)
	}
	if r.PurchasedAt != nil {
		t.Error("непонятная дата не должна выдавать себя за настоящую")
	}
}
