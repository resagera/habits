package receipts

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Разбор писем «Яндекс Go — отчёт о поездке» (no-reply@taxi.yandex.ru).
//
// Отчёт о поездке — не чек магазина: списка товаров в нём нет, есть одна сумма
// и куча подробностей (маршрут, машина, тариф, водитель). Поэтому позиции у
// такого чека пустые, а подробности уезжают в заметку к трате: делить поездку
// по группам товаров нечего, а вспомнить через полгода, что это была за
// поездка, хочется.
//
// Разбор идёт по «сжатому» списку строк: письмо свёрстано таблицей, и в
// текстовой части подписи и значения оказываются на соседних строках, между
// которыми то есть пустые строки, то нет. Убрав пустые строки, картинки и
// голые ссылки, получаем устойчивую последовательность «подпись → значение».

var (
	// «Общая стоимость 1600 ֏», «Общая стоимость 1 600,50 ₽»
	reGoTotal = regexp.MustCompile(`^Общая стоимость\s+([\d\s.,]+)\s*(\S*)$`)
	// строка разбивки оплаты: «Поездка 1600 ֏», «Чаевые 200 ֏»
	reGoFee = regexp.MustCompile(`^(\D[^\d]*?)\s+([\d\s.,]+)\s*(\S*)$`)
	// время в маршруте — отдельной строкой
	reGoTime  = regexp.MustCompile(`^(\d{1,2}):(\d{2})$`)
	reGoImage = regexp.MustCompile(`^\[image:[^\]]*\]`)
	reGoLink  = regexp.MustCompile(`^<https?://\S+>$`)
	// «Дата 6 августа 2026 г.» и та же дата в теме письма
	reGoDate = regexp.MustCompile(`(\d{1,2})\s+([А-Яа-яё]+)\s+(\d{4})`)
)

var ruMonths = map[string]time.Month{
	"января": time.January, "февраля": time.February, "марта": time.March,
	"апреля": time.April, "мая": time.May, "июня": time.June,
	"июля": time.July, "августа": time.August, "сентября": time.September,
	"октября": time.October, "ноября": time.November, "декабря": time.December,
}

// currencyBySign — валюта по знаку из письма. Яндекс Go работает в разных
// странах, и валюта в отчёте только знаком и обозначена.
var currencyBySign = map[string]string{
	"֏": "amd", "₽": "rub", "руб.": "rub", "₸": "kzt", "₾": "gel",
	"сум": "uzs", "₼": "azn", "$": "usd", "€": "eur",
}

func parseYandexGo(subject, body string) (Receipt, error) {
	lines := compactLines(clean(body))
	r := Receipt{Parser: "yandexgo", Merchant: "Яндекс Go", Currency: "amd"}

	var (
		route    []string // адреса маршрута
		times    []string // времена в маршруте: подача и высадка
		car      []string // «белый Equus», «56ОQ666»
		duration []string // «17 мин», «6,3 км»
		fees     []string // разбивка оплаты, кроме итога
		tariff   string
		driver   string
		partner  string
		date     time.Time
		haveDate bool
	)

	section := ""
	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// заголовки разделов идут отдельной строкой и переключают режим
		switch line {
		case "Маршрут", "Оплата", "Детали", "Перевозчик":
			section = line
			continue
		}

		switch {
		case strings.HasPrefix(line, "Общая стоимость"):
			if m := reGoTotal.FindStringSubmatch(line); m != nil {
				if v, ok := num(m[1]); ok {
					r.Total = v
					r.Currency = currencyOf(m[2], r.Currency)
				}
			}
			continue
		case line == "Способ оплаты":
			if i+1 < len(lines) {
				i++
				r.PaidWith = lines[i]
			}
			continue
		case strings.HasPrefix(line, "Получатель платежа"):
			partner = valueAfter(line, "Получатель платежа")
			continue
		case strings.HasPrefix(line, "Тариф "):
			tariff = valueAfter(line, "Тариф")
			continue
		case strings.HasPrefix(line, "Дата "):
			if t, ok := parseRuDate(valueAfter(line, "Дата")); ok {
				date, haveDate = t, true
			}
			continue
		case line == "Время в пути":
			duration = takeUntilLabel(lines, &i, 2)
			continue
		case line == "Автомобиль":
			car = takeUntilLabel(lines, &i, 3)
			continue
		case line == "Водитель":
			if i+1 < len(lines) && !isLabel(lines[i+1]) {
				i++
				driver = lines[i]
			}
			continue
		}

		switch section {
		case "Маршрут":
			if reGoTime.MatchString(line) {
				times = append(times, line)
			} else if !isLabel(line) {
				route = append(route, line)
			}
		case "Оплата":
			// «Поездка 1600 ֏», «Ожидание 120 ֏» — из чего сложился итог
			if m := reGoFee.FindStringSubmatch(line); m != nil {
				if _, ok := num(m[2]); ok {
					fees = append(fees, line)
				}
			}
		}
	}

	// Итог — единственное обязательное поле: без него это не отчёт о поездке,
	// а обычное письмо сервиса (реклама, подтверждение подписки).
	if r.Total == 0 {
		return r, ErrNotReceipt
	}
	if !haveDate {
		// в теле даты нет — берём из темы: «отчёт о поездке 6 августа 2026 г.»
		if t, ok := parseRuDate(subject); ok {
			date, haveDate = t, true
		}
	}
	if !haveDate {
		return r, ErrNotReceipt // без даты трата уедет на сегодня — так нельзя
	}
	// время поездки — момент подачи машины
	if len(times) > 0 {
		if h, m, ok := hhmm(times[0]); ok {
			date = date.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
		}
	}
	r.PurchasedAt = &date

	// Номер заказа в отчёте не печатают, а защита от повторной пересылки нужна:
	// собираем его из даты и времени подачи. Две поездки не начинаются в одну
	// и ту же минуту.
	r.OrderNo = date.Format("20060102-1504")
	r.Note = goNote(route, times, car, tariff, duration, fees, driver, partner, r.PaidWith)
	return r, nil
}

// goNote собирает заметку к трате: маршрут, машина, время в пути, перевозчик.
// Всё, ради чего отчёт вообще читают глазами.
func goNote(route, times, car []string, tariff string, duration, fees []string, driver, partner, paidWith string) string {
	var out []string
	if len(route) > 0 {
		pts := make([]string, 0, len(route))
		for i, addr := range route {
			if i < len(times) {
				addr += " (" + times[i] + ")"
			}
			pts = append(pts, addr)
		}
		out = append(out, "Маршрут: "+strings.Join(pts, " → "))
	}
	if len(car) > 0 || tariff != "" {
		s := "Автомобиль: " + strings.Join(car, ", ")
		if len(car) == 0 {
			s = "Автомобиль:"
		}
		if tariff != "" {
			s += ", тариф " + tariff
		}
		out = append(out, s)
	}
	if len(duration) > 0 {
		out = append(out, "В пути: "+strings.Join(duration, ", "))
	}
	if paidWith != "" {
		out = append(out, "Оплата: "+paidWith)
	}
	if len(fees) > 1 {
		// разбивку показываем, только если она не сводится к одной «Поездке»
		out = append(out, "Состав: "+strings.Join(fees, "; "))
	}
	who := []string{}
	if partner != "" {
		who = append(who, partner)
	}
	if driver != "" {
		who = append(who, "водитель "+driver)
	}
	if len(who) > 0 {
		out = append(out, "Перевозчик: "+strings.Join(who, ", "))
	}
	return strings.Join(out, "\n")
}

// compactLines убирает из письма всё, что не несёт данных: пустые строки,
// картинки-заглушки и голые ссылки. После этого подпись и её значение всегда
// стоят рядом, независимо от того, сколько пустых строк вставила вёрстка.
func compactLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || reGoLink.MatchString(line) {
			continue
		}
		// «[image: Маршрут]» — подпись картинки, дублирует заголовок раздела
		if reGoImage.MatchString(line) {
			continue
		}
		// у строк с текстом хвостом висит ссылка: «Поддержка <https://...>»
		if idx := strings.Index(line, " <http"); idx > 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// goLabels — подписи полей отчёта. Нужны, чтобы значение переменной длины
// (адрес, марка машины) не «съело» следующее поле.
var goLabels = []string{
	"Маршрут", "Оплата", "Детали", "Перевозчик", "Способ оплаты", "Автомобиль",
	"Тариф", "Дата", "Время в пути", "Водитель", "Партнёр", "Поддержка",
	"Общая стоимость", "Получатель платежа", "Юридический адрес",
	"Отписаться от отчётов",
}

func isLabel(line string) bool {
	for _, l := range goLabels {
		if line == l || strings.HasPrefix(line, l+" ") {
			return true
		}
	}
	return false
}

// takeUntilLabel забирает до max значений подряд, пока не встретится подпись
// следующего поля.
func takeUntilLabel(lines []string, i *int, max int) []string {
	var out []string
	for len(out) < max && *i+1 < len(lines) && !isLabel(lines[*i+1]) {
		*i++
		out = append(out, lines[*i])
	}
	return out
}

func valueAfter(line, label string) string {
	return strings.TrimSpace(strings.TrimPrefix(line, label))
}

func currencyOf(sign, def string) string {
	if c, ok := currencyBySign[strings.TrimSpace(sign)]; ok {
		return c
	}
	return def
}

func hhmm(s string) (int, int, bool) {
	m := reGoTime.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, false
	}
	h, err1 := strconv.Atoi(m[1])
	mi, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil || h > 23 || mi > 59 {
		return 0, 0, false
	}
	return h, mi, true
}

// parseRuDate разбирает «6 августа 2026 г.» — единственный вид даты в отчёте.
// Время письма считаем локальным для поездки и держим в UTC: календарную дату
// траты берут из тех же компонентов, без перевода зон.
func parseRuDate(s string) (time.Time, bool) {
	m := reGoDate.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	mon, ok := ruMonths[strings.ToLower(m[2])]
	if !ok {
		return time.Time{}, false
	}
	day, err1 := strconv.Atoi(m[1])
	year, err2 := strconv.Atoi(m[3])
	if err1 != nil || err2 != nil || day < 1 || day > 31 {
		return time.Time{}, false
	}
	return time.Date(year, mon, day, 0, 0, 0, 0, time.UTC), true
}
