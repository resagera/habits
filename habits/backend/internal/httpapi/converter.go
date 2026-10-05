package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/rates"
	"streaks-backend/internal/store"
)

// коды валют проверяем тем же выражением, что и пакет курсов
var currencyCodeRe = rates.CodeRe

type converterHandlers struct {
	store      *store.Store
	ratesCache *rates.Cache
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

	rates, date, err := h.ratesCache.Rates(base, targets)
	if err != nil {
		writeError(w, http.StatusBadGateway, "rates_unavailable", "failed to fetch exchange rates")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"base": base, "date": date, "rates": rates})
}

// --- справочник валют и история курсов ---

// GET /converter/available — все валюты источника, и фиат, и криптовалюты.
// Нужен, чтобы валюту выбирали из списка, а не угадывали её код.
func (h *converterHandlers) available(w http.ResponseWriter, r *http.Request) {
	list, err := h.ratesCache.Available()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"справочник валют сейчас недоступен")
		return
	}
	type out struct {
		Code   string `json:"code"`
		Name   string `json:"name"`
		Crypto bool   `json:"crypto"`
	}
	items := make([]out, 0, len(list))
	for _, c := range list {
		items = append(items, out{Code: c.Code, Name: c.Name, Crypto: !fiatCodes[c.Code]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"currencies": items})
}

// maxHistoryDays — потолок периода. Каждый недостающий день это отдельный
// запрос к источнику, и просить у него полгода за один заход невежливо.
const maxHistoryDays = 92

// GET /converter/history?base=&targets=&days=30 — курс за период.
//
// Недостающие дни докачиваются из источника и складываются в базу, поэтому
// первый график строится сразу, а не через месяц накопления. Хранится всё
// относительно доллара — пара получается делением.
func (h *converterHandlers) history(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	base := strings.ToLower(strings.TrimSpace(q.Get("base")))
	if !currencyCodeRe.MatchString(base) {
		badRequest(w, "invalid base currency")
		return
	}
	days, _ := strconv.Atoi(q.Get("days"))
	if days <= 0 {
		days = 30
	}
	if days > maxHistoryDays {
		days = maxHistoryDays
	}

	// доллар нужен всегда: он опорная валюта хранения
	codes := []string{"usd"}
	seen := map[string]bool{"usd": true}
	for _, t := range append(strings.Split(q.Get("targets"), ","), base) {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] && currencyCodeRe.MatchString(t) {
			codes = append(codes, t)
			seen[t] = true
		}
	}
	if len(codes) < 2 {
		badRequest(w, "не заданы валюты")
		return
	}

	now := time.Now().UTC()
	to := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	from := to.AddDate(0, 0, -days+1)

	have, err := h.store.CurrencyHistoryDays(r.Context(), codes, from, to)
	if err != nil {
		internalError(w)
		return
	}
	h.backfill(r, codes, from, to, have)

	points, err := h.store.CurrencyHistory(r.Context(), codes, from, to)
	if err != nil {
		internalError(w)
		return
	}

	// день → валюта → курс к доллару
	byDay := map[string]map[string]float64{}
	for _, p := range points {
		d := p.Day.Format("2006-01-02")
		if byDay[d] == nil {
			byDay[d] = map[string]float64{}
		}
		byDay[d][p.Code] = p.Rate
	}
	type series struct {
		Code  string    `json:"code"`
		Days  []string  `json:"days"`
		Rates []float64 `json:"rates"`
	}
	out := make([]series, 0, len(codes))
	for _, code := range codes {
		if code == base {
			continue
		}
		s := series{Code: code}
		for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
			key := d.Format("2006-01-02")
			row := byDay[key]
			// курс пары — деление двух долларовых: base и целевой
			if row == nil || row[base] <= 0 || row[code] <= 0 {
				continue
			}
			s.Days = append(s.Days, key)
			s.Rates = append(s.Rates, row[code]/row[base])
		}
		if len(s.Days) > 0 {
			out = append(out, s)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"base": base, "series": out})
}

// backfill докачивает недостающие дни. Ошибки намеренно молчаливые: пропуск
// одного дня — дырка в графике, а не повод отдать пользователю пустой экран.
func (h *converterHandlers) backfill(r *http.Request, codes []string, from, to time.Time, have map[string]bool) {
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if have[d.Format("2006-01-02")] {
			continue
		}
		if r.Context().Err() != nil {
			return // пользователь ушёл со страницы — качать дальше незачем
		}
		got, err := h.ratesCache.RatesOn(d, "usd", codes)
		if err != nil || len(got) == 0 {
			continue
		}
		got["usd"] = 1
		_ = h.store.SaveCurrencyRates(r.Context(), d, got)
	}
}

// fiatCodes — коды ISO 4217. Источник не помечает, что есть что, а в его
// справочнике полторы тысячи позиций, из которых фиат — меньше двухсот.
// Всё, чего нет в этом списке, показываем как криптовалюту: ошибиться тут
// не страшно, это лишь группировка в списке выбора.
var fiatCodes = map[string]bool{}

func init() {
	for _, c := range strings.Fields(`
		aed afn all amd ang aoa ars aud awg azn bam bbd bdt bgn bhd bif bmd bnd
		bob brl bsd btn bwp byn bzd cad cdf chf clp cny cop crc cup cve czk djf
		dkk dop dzd egp ern etb eur fjd fkp gbp gel ghs gip gmd gnf gtq gyd hkd
		hnl hrk htg huf idr ils inr iqd irr isk jmd jod jpy kes kgs khr kmf kpw
		krw kwd kyd kzt lak lbp lkr lrd lsl lyd mad mdl mga mkd mmk mnt mop mru
		mur mvr mwk mxn myr mzn nad ngn nio nok npr nzd omr pab pen pgk php pkr
		pln pyg qar ron rsd rub rwf sar sbd scr sdg sek sgd shp sll sos srd ssp
		stn svc syp szl thb tjs tmt tnd top try ttd twd tzs uah ugx usd uyu uzs
		ves vnd vuv wst xaf xcd xof xpf yer zar zmw zwl`) {
		fiatCodes[c] = true
	}
}
