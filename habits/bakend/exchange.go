package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GET список выбранных валют
// GET /api/resagerhelper/currencies/list?user=...
func (rd *RouteData) handleListUserCurrencies(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	codes, err := rd.repo.GetUserCurrencies(user)
	if err != nil {
		rd.log.Error("handleListUserCurrencies", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"currencies": codes})
}

// POST /api/resagerhelper/currencies/add
func (rd *RouteData) handleAddUserCurrency(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := rd.repo.AddUserCurrency(user, body.Code); err != nil {
		rd.log.Error("handleAddUserCurrency", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// DELETE /api/resagerhelper/currencies/remove?code=...
func (rd *RouteData) handleRemoveUserCurrency(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	if err := rd.repo.RemoveUserCurrency(user, code); err != nil {
		rd.log.Error("handleRemoveUserCurrency", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GET /api/resagerhelper/currencies/rates?base=USD&target=EUR,JPY
func (rd *RouteData) handleGetRates(w http.ResponseWriter, r *http.Request) {
	base := r.URL.Query().Get("base")
	targetParam := r.URL.Query().Get("target")
	if base == "" || targetParam == "" {
		http.Error(w, "missing base or target", http.StatusBadRequest)
		return
	}

	targets := strings.Split(targetParam, ",")
	result, rateDate, err := rd.fetchRateFromAPI(base, targets)
	if err != nil {
		rd.log.Error("handleGetRates: fetch API", "error", err)
		http.Error(w, "failed to fetch rates", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"base":  base,
		"date":  rateDate,
		"rates": result,
	})
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
func (rd *RouteData) fetchRateFromAPI(base string, targets []string) (map[string]float64, string, error) {
	base = strings.ToLower(base)
	result := make(map[string]float64)
	uncachedTargets := make([]string, 0, len(targets))
	rateDate := time.Now().Format(time.DateOnly)

	for _, target := range targets {
		target = strings.ToLower(target)
		cacheKey := base + ":" + target

		if entry, ok := cache.Load(cacheKey); ok {
			if e, valid := entry.(*cacheEntry); valid && time.Now().Before(e.expiresAt) {
				result[target] = e.rate
				continue
			}
		}
		uncachedTargets = append(uncachedTargets, target)
	}

	if len(uncachedTargets) == 0 {
		return result, rateDate, nil
	}

	url := fmt.Sprintf("https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies/%s.json", base)
	rd.log.Info("fetching rates from API", "url", url, "targets", uncachedTargets)

	resp, err := http.Get(url)
	if err != nil {
		return nil, rateDate, fmt.Errorf("failed to fetch from API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, rateDate, fmt.Errorf("API returned non-OK status: %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, rateDate, fmt.Errorf("failed to read response body: %w", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		rd.log.Error("failed to parse top-level API response", "url", url, "error", err, "body", string(bodyBytes))
		return nil, rateDate, fmt.Errorf("failed to decode top-level API response: %w", err)
	}

	if dateRaw, ok := raw["date"]; ok {
		rateDate = strings.Trim(string(dateRaw), `" `)
	}

	ratesRaw, ok := raw[base]
	if !ok {
		return nil, rateDate, fmt.Errorf("base currency %q not found in API response", base)
	}

	var rates map[string]FlexibleFloat64
	if err := json.Unmarshal(ratesRaw, &rates); err != nil {
		rd.log.Error("failed to parse rates object", "base", base, "error", err, "raw", string(ratesRaw))
		return nil, rateDate, fmt.Errorf("failed to decode rates for %q: %w", base, err)
	}

	now := time.Now()
	expiry := now.Add(1 * time.Hour)

	for _, target := range uncachedTargets {
		rate, exists := rates[target]
		if !exists {
			rd.log.Warn("target currency not found in API response", "base", base, "target", target)
			continue
		}

		result[target] = float64(rate)
		cacheKey := base + ":" + target
		cache.Store(cacheKey, &cacheEntry{
			rate:      float64(rate),
			expiresAt: expiry,
		})
	}

	if len(result) != len(targets) {
		rd.log.Info("partial result for currency rates", "resolved", len(result), "requested", len(targets))
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
