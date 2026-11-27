package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// GET список выбранных валют
func handleListUserCurrencies(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	rows, err := db.Query("SELECT currency_code FROM user_currencies WHERE user = ?", user)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	var codes []string
	for rows.Next() {
		var c string
		rows.Scan(&c)
		codes = append(codes, c)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"currencies": codes})
}

// POST добавить валюту
func handleAddUserCurrency(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	_, err := db.Exec("INSERT OR IGNORE INTO user_currencies(user, currency_code) VALUES(?,?)", user, body.Code)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(200)
}

// DELETE удалить валюту
func handleRemoveUserCurrency(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	code := r.URL.Query().Get("code")
	_, err := db.Exec("DELETE FROM user_currencies WHERE user = ? AND currency_code = ?", user, code)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(200)
}

// GET курсы
// GET /api/resagerhelper/currencies/rates?base=USD&target=EUR,JPY
func handleGetRates(w http.ResponseWriter, r *http.Request) {
	base := r.URL.Query().Get("base")
	targetParam := r.URL.Query().Get("target") // CSV: "EUR,JPY,AMD"
	targets := strings.Split(targetParam, ",")
	result, rateDate, err := fetchRateFromAPI(base, targets)
	if err != nil {
		http.Error(w, err.Error(), 500)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"base": base, "date": rateDate, "rates": result})
}

type cacheEntry struct {
	rate      float64
	expiresAt time.Time
}

var (
	cache = sync.Map{} // ключ: "base:target", значение: *cacheEntry
)

// fetchRateFromAPI получает курсы из base во все target валюты.
// Возвращает map[target]rate.
func fetchRateFromAPI(base string, targets []string) (map[string]float64, string, error) {
	base = strings.ToLower(base)
	result := make(map[string]float64)
	uncachedTargets := make([]string, 0, len(targets))
	rateDate := time.Now().Format(time.DateOnly)

	// Проверяем кеш для каждой целевой валюты
	for _, target := range targets {
		target = strings.ToLower(target)
		cacheKey := base + ":" + target

		if entry, ok := cache.Load(cacheKey); ok {
			if e, valid := entry.(*cacheEntry); valid && time.Now().Before(e.expiresAt) {
				result[target] = e.rate
				continue
			}
		}
		// Если нет в кеше или устарел — нужно запросить
		uncachedTargets = append(uncachedTargets, target)
	}

	// Если все валюты в кеше — возвращаем результат
	if len(uncachedTargets) == 0 {
		return result, rateDate, nil
	}

	// Запрашиваем свежие данные один раз для базовой валюты
	url := fmt.Sprintf("https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies/%s.json", base)
	log.Printf("fetching rates from %s for targets: %v", url, uncachedTargets)

	resp, err := http.Get(url)
	if err != nil {
		return nil, rateDate, fmt.Errorf("failed to fetch from API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, rateDate, fmt.Errorf("API returned non-OK status: %d", resp.StatusCode)
	}
	// Читаем всё тело ответа в байты
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, rateDate, fmt.Errorf("failed to read response body: %w", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		slog.Error("failed to parse top-level API response", "url", url, "error", err, "body", string(bodyBytes))
		return nil, rateDate, fmt.Errorf("failed to decode top-level API response: %w", err)
	}

	rateRealDate, ok := raw["date"]
	if ok {
		rateDate = string(rateRealDate)
	}

	ratesRaw, ok := raw[base]
	if !ok {
		return nil, rateDate, fmt.Errorf("base currency %q not found in API response", base)
	}

	var rates map[string]FlexibleFloat64
	if err := json.Unmarshal(ratesRaw, &rates); err != nil {
		slog.Error("failed to parse rates object", "base", base, "error", err, "raw", string(ratesRaw))
		return nil, rateDate, fmt.Errorf("failed to decode rates for %q: %w", base, err)
	}

	now := time.Now()
	expiry := now.Add(1 * time.Hour)

	// Обрабатываем только запрошенные (и не закешированные) валюты
	for _, target := range uncachedTargets {
		rate, exists := rates[target]
		if !exists {
			// Можно либо пропустить, либо вернуть ошибку.
			// Здесь — пропускаем с логированием.
			log.Printf("target currency %q not found in API response for base %q", target, base)
			continue
		}

		result[target] = float64(rate)

		// Кешируем отдельно каждую пару
		cacheKey := base + ":" + target
		cache.Store(cacheKey, &cacheEntry{
			rate:      float64(rate),
			expiresAt: expiry,
		})
	}

	// Проверяем, получили ли мы все запрошенные валюты
	if len(result) != len(targets) {
		// Опционально: можно вернуть частичный результат или ошибку.
		// Здесь возвращаем частичный результат (как есть), но можно изменить.
		log.Printf("partial result: only %d out of %d targets resolved", len(result), len(targets))
	}

	return result, rateDate, nil
}

type FlexibleFloat64 float64

func (f *FlexibleFloat64) UnmarshalJSON(b []byte) error {
	// Пробуем как число
	var num float64
	if err := json.Unmarshal(b, &num); err == nil {
		*f = FlexibleFloat64(num)
		return nil
	}

	// Пробуем как строку
	var str string
	if err := json.Unmarshal(b, &str); err == nil {
		n, err := strconv.ParseFloat(str, 64)
		if err != nil {
			return err
		}
		*f = FlexibleFloat64(n)
		return nil
	}

	return fmt.Errorf("invalid float format: %s", string(b))
}
