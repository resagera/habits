// Package rates — курсы валют с кэшем в памяти.
//
// Выделен из httpapi/converter.go, когда курсы понадобились второму
// потребителю: Finance сводит суммы в базовую валюту на сервере (чтобы цифра
// в приложении и в уведомлении бота совпадала) и фиксирует курс на дату
// платежа — иначе прошлые месяцы «плывут» при каждом изменении курса.
//
// Источник — @fawazahmed0/currency-api через jsDelivr CDN: бесплатный,
// без ключа. Кэш общий на процесс, TTL час.
package rates

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	api = "https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@%s/v1/currencies/%s.json"
	// список валют меняется раз в пятилетку — держим его сутки
	listAPI = "https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies.json"
	ttl     = time.Hour
	listTTL = 24 * time.Hour
)

// CodeRe — допустимый код валюты (в API они в нижнем регистре).
var CodeRe = regexp.MustCompile(`^[a-z0-9]{2,10}$`)

type entry struct {
	rate    float64
	date    string
	expires time.Time
}

type listEntry struct {
	items   []Currency
	expires time.Time
}

// Cache — потокобезопасный кэш курсов.
type Cache struct {
	client *http.Client
	cache  sync.Map // "base:target" -> entry
	list   sync.Map // "all" -> listEntry
}

func New() *Cache {
	return &Cache{client: &http.Client{Timeout: 15 * time.Second}}
}

// Rates возвращает курсы base → targets и дату курсов.
func (c *Cache) Rates(base string, targets []string) (map[string]float64, string, error) {
	result := make(map[string]float64, len(targets))
	date := time.Now().Format("2006-01-02")

	var missing []string
	for _, t := range targets {
		if v, ok := c.cache.Load(base + ":" + t); ok {
			if e := v.(entry); time.Now().Before(e.expires) {
				result[t] = e.rate
				date = e.date
				continue
			}
		}
		missing = append(missing, t)
	}
	if len(missing) == 0 {
		return result, date, nil
	}

	all, apiDate, err := c.fetch("latest", base)
	if err != nil {
		return nil, date, err
	}
	date = apiDate

	expires := time.Now().Add(ttl)
	for _, t := range missing {
		rate, ok := all[t]
		if !ok {
			continue // неизвестная валюта — просто не попадёт в ответ
		}
		result[t] = float64(rate)
		c.cache.Store(base+":"+t, entry{rate: float64(rate), date: date, expires: expires})
	}
	return result, date, nil
}

// fetch тянет срез курсов base → все на указанную дату. Дата «latest» — на
// сегодня; иначе «ГГГГ-ММ-ДД»: источник держит датированные срезы, и без них
// историю пришлось бы копить месяцами, прежде чем показать первый график.
func (c *Cache) fetch(dateTag, base string) (map[string]flexFloat, string, error) {
	date := time.Now().Format("2006-01-02")
	resp, err := c.client.Get(fmt.Sprintf(api, dateTag, base))
	if err != nil {
		return nil, date, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, date, fmt.Errorf("rates api status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, date, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, date, err
	}
	if d, ok := raw["date"]; ok {
		date = strings.Trim(string(d), `" `)
	}
	ratesRaw, ok := raw[base]
	if !ok {
		return nil, date, fmt.Errorf("base %q not in response", base)
	}
	var all map[string]flexFloat
	if err := json.Unmarshal(ratesRaw, &all); err != nil {
		return nil, date, err
	}
	return all, date, nil
}

// RatesOn — курсы base → targets на конкретный день. Кэш здесь не нужен:
// прошедший день не меняется, и результат сразу уходит в базу.
func (c *Cache) RatesOn(day time.Time, base string, targets []string) (map[string]float64, error) {
	all, _, err := c.fetch(day.Format("2006-01-02"), base)
	if err != nil {
		return nil, err
	}
	out := make(map[string]float64, len(targets))
	for _, t := range targets {
		if v, ok := all[t]; ok && float64(v) > 0 {
			out[t] = float64(v)
		}
	}
	return out, nil
}

// Currency — валюта из справочника источника.
type Currency struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// Available — весь справочник источника: и фиат, и криптовалюты. Нужен, чтобы
// валюту выбирали из списка, а не угадывали код.
func (c *Cache) Available() ([]Currency, error) {
	if v, ok := c.list.Load("all"); ok {
		if e := v.(listEntry); time.Now().Before(e.expires) {
			return e.items, nil
		}
	}
	resp, err := c.client.Get(listAPI)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("currencies api status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, err
	}
	var raw map[string]string
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	items := make([]Currency, 0, len(raw))
	for code, name := range raw {
		if !CodeRe.MatchString(code) {
			continue
		}
		if strings.TrimSpace(name) == "" {
			name = strings.ToUpper(code)
		}
		items = append(items, Currency{Code: code, Name: name})
	}
	sort.Slice(items, func(a, b int) bool { return items[a].Code < items[b].Code })
	c.list.Store("all", listEntry{items: items, expires: time.Now().Add(listTTL)})
	return items, nil
}

// Convert пересчитывает сумму из from в to. Одинаковые валюты — курс 1.
// Возвращает и применённый курс: Finance сохраняет его в истории платежа.
func (c *Cache) Convert(amount float64, from, to string) (converted, rate float64, err error) {
	from, to = strings.ToLower(from), strings.ToLower(to)
	if from == to || from == "" || to == "" {
		return amount, 1, nil
	}
	r, _, err := c.Rates(from, []string{to})
	if err != nil {
		return 0, 0, err
	}
	rate, ok := r[to]
	if !ok || rate <= 0 {
		return 0, 0, fmt.Errorf("no rate %s→%s", from, to)
	}
	return amount * rate, rate, nil
}

// flexFloat: API иногда отдаёт числа строками.
type flexFloat float64

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	var num float64
	if err := json.Unmarshal(b, &num); err == nil {
		*f = flexFloat(num)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	num, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*f = flexFloat(num)
	return nil
}
