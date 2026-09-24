// Package receipts разбирает письма магазинов в чеки, которые Finance
// записывает как траты.
//
// Разбирается ТЕКСТОВАЯ часть письма, а не HTML: вёрстка рассылок меняется от
// кампании к кампании, а текстовая версия у одного магазина годами одна и та
// же. Если текстовой части нет, вызывающий приводит HTML к тексту сам.
package receipts

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrNotReceipt — письмо не похоже на чек этого магазина. Не ошибка: на адрес
// приходят и обычные письма (подтверждения, реклама).
var ErrNotReceipt = errors.New("письмо не похоже на чек")

// Item — строка чека.
type Item struct {
	Name   string  `json:"name"`
	Qty    float64 `json:"qty"`
	Unit   string  `json:"unit"`
	Amount float64 `json:"amount"`
}

// Receipt — разобранный чек.
type Receipt struct {
	Parser      string     `json:"parser"`
	Merchant    string     `json:"merchant"`
	OrderNo     string     `json:"order_no"`
	PurchasedAt *time.Time `json:"purchased_at"`
	Currency    string     `json:"currency"`
	Subtotal    float64    `json:"subtotal"`
	DeliveryFee float64    `json:"delivery_fee"`
	ServiceFee  float64    `json:"service_fee"`
	Tip         float64    `json:"tip"`
	Total       float64    `json:"total"`
	PaidWith    string     `json:"paid_with"`
	Items       []Item     `json:"items"`
	// Note — подробности, которые в трате важнее списка позиций: маршрут
	// поездки, машина, тариф. Пусто у чеков магазинов: там за смысл отвечают
	// сами позиции.
	Note string `json:"note"`
}

// Parser — описание разборщика для интерфейса.
type Parser struct {
	Code     string `json:"code"`
	Title    string `json:"title"`
	Merchant string `json:"merchant"`
	// From — домен, с которого магазин шлёт письма (подсказка для only_from)
	From string `json:"from"`
}

// Parsers — что умеем разбирать. Добавление магазина = новая функция и строка
// здесь; интерфейс подхватит сам.
var Parsers = []Parser{
	{Code: "yerevancity", Title: "Yerevan City (доставка)", Merchant: "Yerevan City", From: "yerevan-city.am"},
	{Code: "yandexgo", Title: "Яндекс Go (поездки)", Merchant: "Яндекс Go", From: "taxi.yandex.ru"},
}

func Known(code string) bool {
	for _, p := range Parsers {
		if p.Code == code {
			return true
		}
	}
	return false
}

// Parse разбирает письмо выбранным разборщиком. subject нужен как запасной
// источник номера заказа: в пересланных письмах тело иногда обрезается, а
// тема остаётся целой.
func Parse(code, subject, body string) (Receipt, error) {
	switch code {
	case "yerevancity":
		return parseYerevanCity(subject, body)
	case "yandexgo":
		return parseYandexGo(subject, body)
	}
	return Receipt{}, fmt.Errorf("неизвестный разборщик %q", code)
}

// --- общие помощники ---

var (
	// пересланное письмо приходит процитированным: «> » в начале строк,
	// иногда в несколько уровней
	reQuote = regexp.MustCompile(`(?m)^[ \t]*(?:>[ \t]?)+`)
	// В письмах живут неразрывные и узкие пробелы — числа с ними не парсятся.
	// Записаны кодами: невидимые символы в исходнике потом не найти глазами.
	spaceFixer = strings.NewReplacer(
		"\u00a0", " ", // неразрывный
		"\u202f", " ", // узкий неразрывный
		"\u2009", " ", // тонкий
		"\ufeff", "", // BOM
		"\u200b", "", // нулевой ширины
	)
)

// clean приводит письмо к виду, пригодному для разбора: снимает цитирование и
// экзотические пробелы, схлопывает пустые строки.
func clean(s string) string {
	s = spaceFixer.Replace(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = reQuote.ReplaceAllString(s, "")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.Join(lines, "\n")
}

// num разбирает сумму: «4608.50», «1 060,00», «23757.22».
func num(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, ",", ".")
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
