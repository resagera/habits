package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/store"
)

// Источник курсов — @fawazahmed0/currency-api через jsDelivr CDN
// (бесплатный, без ключа); кэш курсов в памяти на 1 час — как в старом
// бэкенде habits/bakend/exchange.go.
const (
	ratesAPI = "https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies/%s.json"
	ratesTTL = time.Hour
)

var currencyCodeRe = regexp.MustCompile(`^[a-z0-9]{2,10}$`)

type converterHandlers struct {
	store *store.Store

	cache sync.Map // "base:target" -> ratesCacheEntry
}

type ratesCacheEntry struct {
	rate    float64
	date    string
	expires time.Time
}

func (h *converterHandlers) listCurrencies(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	currencies, err := h.store.ListCurrencies(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if currencies == nil {
		currencies = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"currencies": currencies})
}

func (h *converterHandlers) addCurrency(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	code := strings.ToLower(strings.TrimSpace(req.Code))
	if !currencyCodeRe.MatchString(code) {
		badRequest(w, "code must be 2-10 latin letters/digits")
		return
	}
	if err := h.store.AddCurrency(r.Context(), user.ID, code); err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"code": code})
}

func (h *converterHandlers) removeCurrency(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	code := strings.ToLower(r.PathValue("code"))
	switch err := h.store.RemoveCurrency(r.Context(), user.ID, code); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "currency not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *converterHandlers) rates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	base := strings.ToLower(strings.TrimSpace(q.Get("base")))
	if !currencyCodeRe.MatchString(base) {
		badRequest(w, "invalid base currency")
		return
	}
	var targets []string
	for _, t := range strings.Split(q.Get("targets"), ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && t != base && currencyCodeRe.MatchString(t) {
			targets = append(targets, t)
		}
	}
	if len(targets) == 0 || len(targets) > 50 {
		badRequest(w, "targets must contain 1-50 currency codes")
		return
	}

	rates, date, err := h.fetchRates(base, targets)
	if err != nil {
		writeError(w, http.StatusBadGateway, "rates_unavailable", "failed to fetch exchange rates")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"base": base, "date": date, "rates": rates})
}

func (h *converterHandlers) fetchRates(base string, targets []string) (map[string]float64, string, error) {
	result := make(map[string]float64, len(targets))
	date := time.Now().Format("2006-01-02")

	var missing []string
	for _, t := range targets {
		if v, ok := h.cache.Load(base + ":" + t); ok {
			if e := v.(ratesCacheEntry); time.Now().Before(e.expires) {
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

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(fmt.Sprintf(ratesAPI, base))
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

	expires := time.Now().Add(ratesTTL)
	for _, t := range missing {
		rate, ok := all[t]
		if !ok {
			continue // неизвестная валюта — просто не попадёт в ответ
		}
		result[t] = float64(rate)
		h.cache.Store(base+":"+t, ratesCacheEntry{rate: float64(rate), date: date, expires: expires})
	}
	return result, date, nil
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
