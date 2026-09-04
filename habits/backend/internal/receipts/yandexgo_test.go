package receipts

import (
	"errors"
	"strings"
	"testing"
)

// Письмо синтетическое: адреса, машина и водитель выдуманы. Разбор проверяем на
// структуре письма, а не на чьих-то настоящих поездках.
const goTripMail = `---------- Forwarded message ---------
От: Yandex Go <no-reply@taxi.yandex.ru>
Date: чт, 6 авг. 2026 г. в 14:47
Subject: Яндекс Go — отчёт о поездке 6 августа 2026 г.
To: <someone@example.com>


[image: Яндекс Go] <https://go.yandex>
[image: Маршрут]
Маршрут

Тестовая улица, 1

14:29

Вторая улица, 2/3

14:47
Оплата
Общая стоимость 1600 ֏
Поездка 1600 ֏
Способ оплаты
•••• 6307
Получатель платежа LLC EXAMPLE
Детали
Автомобиль

белый Equus

00XX000
Тариф Комфорт+
Дата 6 августа 2026 г.
Время в пути

17 мин

6,3 км
Поддержка <https://yandex.ru/support/taxi/>
Перевозчик

Водитель

Ivan Ivanov
Партнёр

Партнёр

LLC EXAMPLE

Юридический адрес

0054, Yerevan, Test district, 1
Отписаться от отчётов
<https://taxi.yandex.ru/email/unsubscribe/?confirmation_code=deadbeef&lang=ru>
`

func TestParseYandexGo(t *testing.T) {
	r, err := Parse("yandexgo", "Fwd: Яндекс Go — отчёт о поездке 6 августа 2026 г.", goTripMail)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if r.Total != 1600 {
		t.Errorf("итог = %v, ждали 1600", r.Total)
	}
	if r.Currency != "amd" {
		t.Errorf("валюта = %q, ждали amd", r.Currency)
	}
	if r.PaidWith != "•••• 6307" {
		t.Errorf("способ оплаты = %q", r.PaidWith)
	}
	if len(r.Items) != 0 {
		t.Errorf("у поездки не должно быть позиций, их %d", len(r.Items))
	}
	if r.PurchasedAt == nil {
		t.Fatal("нет даты поездки")
	}
	if got := r.PurchasedAt.Format("2006-01-02 15:04"); got != "2026-08-06 14:29" {
		t.Errorf("дата и время = %q, ждали 2026-08-06 14:29", got)
	}
	// номер собран из даты и времени подачи: повторная пересылка не удвоит трату
	if r.OrderNo != "20260806-1429" {
		t.Errorf("номер заказа = %q", r.OrderNo)
	}
	for _, want := range []string{
		"Маршрут: Тестовая улица, 1 (14:29) → Вторая улица, 2/3 (14:47)",
		"Автомобиль: белый Equus, 00XX000, тариф Комфорт+",
		"В пути: 17 мин, 6,3 км",
		"Оплата: •••• 6307",
		"Перевозчик: LLC EXAMPLE, водитель Ivan Ivanov",
	} {
		if !strings.Contains(r.Note, want) {
			t.Errorf("в заметке нет строки %q\nзаметка:\n%s", want, r.Note)
		}
	}
}

// Пересланное письмо приходит процитированным — разбор не должен от этого
// разваливаться.
func TestParseYandexGoQuoted(t *testing.T) {
	var b strings.Builder
	for _, line := range strings.Split(goTripMail, "\n") {
		b.WriteString("> " + line + "\n")
	}
	r, err := Parse("yandexgo", "", b.String())
	if err != nil {
		t.Fatalf("разбор цитаты: %v", err)
	}
	if r.Total != 1600 || r.OrderNo != "20260806-1429" {
		t.Errorf("итог %v, заказ %q", r.Total, r.OrderNo)
	}
}

// Дата бывает только в теме: тело пересланного письма иногда обрезают.
func TestParseYandexGoDateFromSubject(t *testing.T) {
	body := strings.ReplaceAll(goTripMail, "Дата 6 августа 2026 г.", "")
	r, err := Parse("yandexgo", "Яндекс Go — отчёт о поездке 6 августа 2026 г.", body)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if got := r.PurchasedAt.Format("2006-01-02 15:04"); got != "2026-08-06 14:29" {
		t.Errorf("дата из темы = %q", got)
	}
}

func TestParseYandexGoCash(t *testing.T) {
	body := strings.ReplaceAll(goTripMail, "•••• 6307", "Наличные")
	r, err := Parse("yandexgo", "", body)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if r.PaidWith != "Наличные" {
		t.Errorf("способ оплаты = %q", r.PaidWith)
	}
}

// Рубли: тот же отчёт, другая страна.
func TestParseYandexGoRubles(t *testing.T) {
	body := strings.ReplaceAll(goTripMail, "Общая стоимость 1600 ֏", "Общая стоимость 1 250,50 ₽")
	r, err := Parse("yandexgo", "", body)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if r.Total != 1250.50 || r.Currency != "rub" {
		t.Errorf("итог %v %s", r.Total, r.Currency)
	}
}

// Обычное письмо сервиса чеком считаться не должно.
func TestParseYandexGoNotReceipt(t *testing.T) {
	_, err := Parse("yandexgo", "Подтверждение пересылки",
		"Вы запросили пересылку писем. Подтвердите адрес по ссылке.")
	if !errors.Is(err, ErrNotReceipt) {
		t.Errorf("ждали ErrNotReceipt, получили %v", err)
	}
}

// Без даты записывать трату нельзя: она уедет на сегодня и тихо соврёт.
func TestParseYandexGoNoDate(t *testing.T) {
	body := strings.ReplaceAll(goTripMail, "Дата 6 августа 2026 г.", "")
	_, err := Parse("yandexgo", "Fwd: отчёт о поездке", body)
	if !errors.Is(err, ErrNotReceipt) {
		t.Errorf("ждали ErrNotReceipt, получили %v", err)
	}
}
