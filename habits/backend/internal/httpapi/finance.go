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

type financeHandlers struct {
	store *store.Store
	rates *rates.Cache
}

// сколько вхождений одного плана максимум показываем в окне: у ежедневных
// повторов их иначе набегают сотни
const maxOccurrencesPerPlan = 40

var (
	repeatKinds = map[string]bool{
		"once": true, "monthly": true, "yearly": true,
		"interval_months": true, "interval_days": true,
	}
	debtDirections = map[string]bool{"owed_to_me": true, "i_owe": true}
	debtStatuses   = map[string]bool{"open": true, "partial": true, "paid": true, "forgiven": true}
)

// occurrence — конкретный платёж конкретной даты. Будущие платежи не хранятся
// в базе, а раскладываются здесь из плана и правила повтора.
type occurrence struct {
	PlanID     int64     `json:"plan_id"`
	Name       string    `json:"name"`
	Amount     float64   `json:"amount"`
	Currency   string    `json:"currency"`
	AmountBase float64   `json:"amount_base"`
	DueDate    time.Time `json:"due_date"`
	IsEstimate bool      `json:"is_estimate"`
	Autopay    bool      `json:"autopay"`
	RepeatKind string    `json:"repeat_kind"`
	IntervalN  int       `json:"interval_n"`
	Category   string    `json:"category"`
	Note       string    `json:"note"`
	Overdue    bool      `json:"overdue"`
	// первое вхождение плана — по нему рисуются действия «Оплатил»/«Пропустить»
	First bool `json:"first"`
}

// GET /finance/overview?tz_off=<минуты> — сводка и списки.
func (h *financeHandlers) overview(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	ctx := r.Context()

	// часовой пояс запоминаем: воркер уведомлений шлёт утром по местному времени
	if v := r.URL.Query().Get("tz_off"); v != "" {
		if off, err := strconv.Atoi(v); err == nil && off >= -840 && off <= 840 {
			_ = h.store.SetFinanceTz(ctx, user.ID, off)
		}
	}
	settings, err := h.store.GetFinanceSettings(ctx, user.ID)
	if err != nil {
		internalError(w)
		return
	}
	plans, err := h.store.ListFinancePlans(ctx, user.ID, false)
	if err != nil {
		internalError(w)
		return
	}
	debts, err := h.store.ListFinanceDebts(ctx, user.ID, false)
	if err != nil {
		internalError(w)
		return
	}

	today := localToday(settings.TzOff)
	monthEnd := endOfMonth(today)
	nextMonthEnd := endOfMonth(monthEnd.AddDate(0, 0, 1))

	conv := h.converter(settings.BaseCurrency)

	buckets := map[string][]occurrence{
		"overdue": {}, "this_month": {}, "next_month": {}, "later": {},
	}
	var sumThis, sumNext, sumOverdue, sumAutopay, monthly float64

	for _, p := range plans {
		if !p.Enabled {
			continue
		}
		if p.PausedUntil != nil && !p.PausedUntil.Before(today) {
			continue // заморожен
		}
		monthly += conv(monthlyCost(p), p.Currency)

		for i, due := range projectDues(p, nextMonthEnd) {
			occ := occurrence{
				PlanID: p.ID, Name: p.Name, Amount: p.Amount, Currency: p.Currency,
				AmountBase: conv(p.Amount, p.Currency), DueDate: due,
				IsEstimate: p.IsEstimate, Autopay: p.Autopay, RepeatKind: p.RepeatKind,
				IntervalN: p.IntervalN, Category: p.Category, Note: p.Note,
				First: i == 0,
			}
			switch {
			case due.Before(today):
				occ.Overdue = true
				buckets["overdue"] = append(buckets["overdue"], occ)
				sumOverdue += occ.AmountBase
			case !due.After(monthEnd):
				buckets["this_month"] = append(buckets["this_month"], occ)
				sumThis += occ.AmountBase
				if p.Autopay {
					sumAutopay += occ.AmountBase
				}
			case !due.After(nextMonthEnd):
				buckets["next_month"] = append(buckets["next_month"], occ)
				sumNext += occ.AmountBase
			default:
				if i == 0 {
					buckets["later"] = append(buckets["later"], occ)
				}
			}
		}
	}
	for _, k := range []string{"overdue", "this_month", "next_month", "later"} {
		sortOccurrences(buckets[k])
	}

	// долги
	var owedToMe, iOwe, debtsOverdue, debtsThisMonth float64
	for _, d := range debts {
		if d.Status == "paid" || d.Status == "forgiven" {
			continue
		}
		rem := conv(d.Remaining, d.Currency)
		if d.Direction == "i_owe" {
			iOwe += rem
		} else {
			owedToMe += rem
		}
		if d.ExpectedOn != nil {
			switch {
			case d.ExpectedOn.Before(today):
				debtsOverdue += rem
			case !d.ExpectedOn.After(monthEnd):
				debtsThisMonth += rem
			}
		}
	}

	// Факт текущего месяца рядом с планом: сводка обязана показывать не только
	// то, что предстоит, но и то, что уже потрачено.
	var spentThis, earnedThis float64
	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	if rows, err := h.store.FinanceStatRows(ctx, user.ID, monthStart, monthEnd); err == nil {
		for _, row := range rows {
			v := inBase(row.Amount, row.RateToBase, row.BaseCurrency, settings.BaseCurrency,
				conv, row.Currency)
			if row.Kind == "income" {
				earnedThis += v
			} else {
				spentThis += v
			}
		}
	}
	var accountsTotal float64
	if acc, err := h.accountsWithBalance(r, settings.BaseCurrency); err == nil {
		if v, ok := acc.totals["balance_base"].(float64); ok {
			accountsTotal = v
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"base_currency": settings.BaseCurrency,
		"today":         today.Format("2006-01-02"),
		"summary": map[string]any{
			"spent_this_month":   round2(spentThis),
			"earned_this_month":  round2(earnedThis),
			"accounts_total":     round2(accountsTotal),
			"this_month":         round2(sumThis),
			"next_month":         round2(sumNext),
			"overdue":            round2(sumOverdue),
			"autopay_this_month": round2(sumAutopay),
			"monthly_normalized": round2(monthly),
			"debts_owed_to_me":   round2(owedToMe),
			"debts_i_owe":        round2(iOwe),
			"debts_overdue":      round2(debtsOverdue),
			"debts_this_month":   round2(debtsThisMonth),
		},
		"buckets":      buckets,
		"debts":        debts,
		"hide_amounts": settings.HideAmounts,
	})
}

// converter возвращает функцию пересчёта в базовую валюту. Курсы берутся
// из общего кэша; если курса нет (нет сети), сумма идёт как есть — лучше
// приблизительная сводка, чем пустая страница.
func (h *financeHandlers) converter(base string) func(amount float64, cur string) float64 {
	base = strings.ToLower(base)
	cache := map[string]float64{}
	return func(amount float64, cur string) float64 {
		cur = strings.ToLower(cur)
		if cur == base || cur == "" {
			return amount
		}
		if rate, ok := cache[cur]; ok {
			return amount * rate
		}
		_, rate, err := h.rates.Convert(1, cur, base)
		if err != nil {
			cache[cur] = 1
			return amount
		}
		cache[cur] = rate
		return amount * rate
	}
}

// projectDues раскладывает план на конкретные даты до конца окна.
// Просроченная дата всегда попадает в результат первой.
func projectDues(p store.FinancePlan, until time.Time) []time.Time {
	due := dateOnly(p.NextDueDate)
	if p.RepeatKind == "once" {
		return []time.Time{due}
	}
	out := []time.Time{due}
	for len(out) < maxOccurrencesPerPlan {
		next := store.AdvanceDue(out[len(out)-1], p.RepeatKind, p.IntervalN, 1)
		if !next.After(out[len(out)-1]) || next.After(until) {
			break
		}
		out = append(out, next)
	}
	return out
}

// monthlyCost — во сколько план обходится в месяц (годовой ÷ 12 и т.д.).
// Разовые траты в «сколько уходит в месяц» не считаются.
func monthlyCost(p store.FinancePlan) float64 {
	switch p.RepeatKind {
	case "monthly":
		return p.Amount
	case "yearly":
		return p.Amount / 12
	case "interval_months":
		if p.IntervalN > 0 {
			return p.Amount / float64(p.IntervalN)
		}
	case "interval_days":
		if p.IntervalN > 0 {
			return p.Amount * 30.44 / float64(p.IntervalN)
		}
	}
	return 0
}

func sortOccurrences(list []occurrence) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].DueDate.Before(list[j-1].DueDate); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

func localToday(tzOff int) time.Time {
	t := time.Now().UTC().Add(time.Duration(tzOff) * time.Minute)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func endOfMonth(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month()+1, 0, 0, 0, 0, 0, time.UTC)
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5*sign(v))) / 100
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// --- планы ---

func (h *financeHandlers) listPlans(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	archived := r.URL.Query().Get("archived") == "1"
	plans, err := h.store.ListFinancePlans(r.Context(), user.ID, archived)
	if err != nil {
		internalError(w)
		return
	}
	if plans == nil {
		plans = []store.FinancePlan{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": plans})
}

type planRequest struct {
	Name             string  `json:"name"`
	Amount           float64 `json:"amount"`
	Currency         string  `json:"currency"`
	IsEstimate       bool    `json:"is_estimate"`
	NextDueDate      string  `json:"next_due_date"`
	RepeatKind       string  `json:"repeat_kind"`
	IntervalN        int     `json:"interval_n"`
	Category         string  `json:"category"`
	Note             string  `json:"note"`
	Autopay          bool    `json:"autopay"`
	NotifyDaysBefore int     `json:"notify_days_before"`
	PausedUntil      string  `json:"paused_until"`
	Enabled          *bool   `json:"enabled"`
	CategoryID       *int64  `json:"category_id"`
	AccountID        *int64  `json:"account_id"`
}

func (req planRequest) toPlan() (store.FinancePlan, string) {
	p := store.FinancePlan{
		Name: strings.TrimSpace(req.Name), Amount: req.Amount,
		Currency:   strings.ToLower(strings.TrimSpace(req.Currency)),
		IsEstimate: req.IsEstimate, RepeatKind: req.RepeatKind,
		IntervalN: req.IntervalN, Category: strings.TrimSpace(req.Category),
		Note: req.Note, Autopay: req.Autopay,
		NotifyDaysBefore: req.NotifyDaysBefore, Enabled: true,
		CategoryID: req.CategoryID, AccountID: req.AccountID,
	}
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	if p.CategoryID != nil && *p.CategoryID <= 0 {
		p.CategoryID = nil
	}
	if p.AccountID != nil && *p.AccountID <= 0 {
		p.AccountID = nil
	}
	if p.Name == "" || len(p.Name) > 200 {
		return p, "name is required (1-200 chars)"
	}
	if p.Amount < 0 {
		return p, "amount must be >= 0"
	}
	if !rates.CodeRe.MatchString(p.Currency) {
		return p, "invalid currency"
	}
	if p.RepeatKind == "" {
		p.RepeatKind = "monthly"
	}
	if !repeatKinds[p.RepeatKind] {
		return p, "repeat_kind must be once|monthly|yearly|interval_months|interval_days"
	}
	if p.IntervalN < 1 {
		p.IntervalN = 1
	}
	if p.IntervalN > 365 {
		return p, "interval_n must be 1-365"
	}
	if p.NotifyDaysBefore < 0 || p.NotifyDaysBefore > 30 {
		return p, "notify_days_before must be 0-30"
	}
	due, err := time.Parse("2006-01-02", req.NextDueDate)
	if err != nil {
		return p, "next_due_date must be YYYY-MM-DD"
	}
	p.NextDueDate = due
	if req.PausedUntil != "" {
		t, err := time.Parse("2006-01-02", req.PausedUntil)
		if err != nil {
			return p, "paused_until must be YYYY-MM-DD"
		}
		p.PausedUntil = &t
	}
	return p, ""
}

// syncPlanCategory держит текстовое имя категории в плане в согласии с
// деревом: списки и вхождения показывают его без джойна.
func (h *financeHandlers) syncPlanCategory(r *http.Request, p *store.FinancePlan) error {
	if p.CategoryID == nil {
		return nil
	}
	user := auth.UserFromContext(r.Context())
	c, err := h.store.FinanceCategoryByID(r.Context(), user.ID, *p.CategoryID)
	if err != nil {
		return err
	}
	p.Category = c.Name
	return nil
}

func (h *financeHandlers) createPlan(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req planRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	p, msg := req.toPlan()
	if msg != "" {
		badRequest(w, msg)
		return
	}
	if err := h.syncPlanCategory(r, &p); err != nil {
		badRequest(w, "category not found")
		return
	}
	out, err := h.store.CreateFinancePlan(r.Context(), user.ID, p)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"plan": out})
}

func (h *financeHandlers) updatePlan(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid plan id")
		return
	}
	var req planRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	p, msg := req.toPlan()
	if msg != "" {
		badRequest(w, msg)
		return
	}
	p.ID = id
	if err := h.syncPlanCategory(r, &p); err != nil {
		badRequest(w, "category not found")
		return
	}
	out, err := h.store.UpdateFinancePlan(r.Context(), user.ID, p)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "plan not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"plan": out})
	}
}

func (h *financeHandlers) archivePlan(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid plan id")
		return
	}
	var req struct {
		Archived bool `json:"archived"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	err = h.store.ArchiveFinancePlan(r.Context(), user.ID, id, req.Archived)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "plan not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"archived": req.Archived})
	}
}

func (h *financeHandlers) deletePlan(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid plan id")
		return
	}
	err = h.store.DeleteFinancePlan(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "plan not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// POST /finance/plans/{id}/pay — «Оплатил».
// Курс к базовой валюте фиксируется на дату платежа: без этого прошлые месяцы
// пересчитывались бы при каждом изменении курса.
func (h *financeHandlers) pay(w http.ResponseWriter, r *http.Request) {
	h.payOrSkip(w, r, false)
}

// POST /finance/plans/{id}/skip — «Пропустить»: дату двигаем, деньги не тратим.
func (h *financeHandlers) skip(w http.ResponseWriter, r *http.Request) {
	h.payOrSkip(w, r, true)
}

func (h *financeHandlers) payOrSkip(w http.ResponseWriter, r *http.Request, skipped bool) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid plan id")
		return
	}
	var req struct {
		PaidOn  string   `json:"paid_on"`
		Amount  *float64 `json:"amount"`
		Periods int      `json:"periods"`
		Note    string   `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	plan, err := h.store.FinancePlanByID(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "plan not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}

	settings, err := h.store.GetFinanceSettings(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	paidOn := localToday(settings.TzOff)
	if req.PaidOn != "" {
		t, err := time.Parse("2006-01-02", req.PaidOn)
		if err != nil {
			badRequest(w, "paid_on must be YYYY-MM-DD")
			return
		}
		paidOn = t
	}
	periods := req.Periods
	if periods < 1 {
		periods = 1
	}
	if periods > 120 {
		badRequest(w, "periods must be 1-120")
		return
	}
	amount := plan.Amount
	if req.Amount != nil {
		if *req.Amount < 0 {
			badRequest(w, "amount must be >= 0")
			return
		}
		amount = *req.Amount
	}
	if skipped {
		amount = 0
	}

	rate := 1.0
	if !skipped {
		if _, r2, err := h.rates.Convert(1, plan.Currency, settings.BaseCurrency); err == nil {
			rate = r2
		}
	}

	out, err := h.store.PayFinancePlan(r.Context(), user.ID, store.PayFinancePlanParams{
		PlanID: id, PaidOn: paidOn, Amount: amount, Currency: plan.Currency,
		BaseCurrency: settings.BaseCurrency, RateToBase: rate, Periods: periods,
		Skipped: skipped, Note: req.Note,
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "plan not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"plan": out})
	}
}

// GET /finance/payments?plan_id=&limit= — история оплат.
func (h *financeHandlers) listPayments(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	planID, _ := strconv.ParseInt(r.URL.Query().Get("plan_id"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	payments, err := h.store.ListFinancePayments(r.Context(), user.ID, planID, limit)
	if err != nil {
		internalError(w)
		return
	}
	if payments == nil {
		payments = []store.FinancePayment{}
	}
	out := map[string]any{"payments": payments}
	// для примерных трат подсказываем среднее — подставится в форму оплаты
	if planID > 0 {
		if avg, ok, err := h.store.AverageFinancePayment(r.Context(), user.ID, planID, 3); err == nil && ok {
			out["average"] = round2(avg)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// --- долги ---

func (h *financeHandlers) listDebts(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	archived := r.URL.Query().Get("archived") == "1"
	debts, err := h.store.ListFinanceDebts(r.Context(), user.ID, archived)
	if err != nil {
		internalError(w)
		return
	}
	if debts == nil {
		debts = []store.FinanceDebt{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"debts": debts})
}

type debtRequest struct {
	Direction     string  `json:"direction"`
	Person        string  `json:"person"`
	ContactUserID *int64  `json:"contact_user_id"`
	Subject       string  `json:"subject"`
	Amount        float64 `json:"amount"`
	Currency      string  `json:"currency"`
	ExpectedOn    string  `json:"expected_on"`
	Note          string  `json:"note"`
}

func (req debtRequest) toDebt() (store.FinanceDebt, string) {
	d := store.FinanceDebt{
		Direction: req.Direction, Person: strings.TrimSpace(req.Person),
		ContactUserID: req.ContactUserID, Subject: strings.TrimSpace(req.Subject),
		Amount:   req.Amount,
		Currency: strings.ToLower(strings.TrimSpace(req.Currency)),
		Note:     req.Note,
	}
	if d.Direction == "" {
		d.Direction = "owed_to_me"
	}
	if !debtDirections[d.Direction] {
		return d, "direction must be owed_to_me|i_owe"
	}
	if d.Person == "" || len(d.Person) > 200 {
		return d, "person is required (1-200 chars)"
	}
	if d.Amount <= 0 {
		return d, "amount must be > 0"
	}
	if !rates.CodeRe.MatchString(d.Currency) {
		return d, "invalid currency"
	}
	if req.ExpectedOn != "" {
		t, err := time.Parse("2006-01-02", req.ExpectedOn)
		if err != nil {
			return d, "expected_on must be YYYY-MM-DD"
		}
		d.ExpectedOn = &t
	}
	return d, ""
}

func (h *financeHandlers) createDebt(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req debtRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	d, msg := req.toDebt()
	if msg != "" {
		badRequest(w, msg)
		return
	}
	out, err := h.store.CreateFinanceDebt(r.Context(), user.ID, d)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"debt": out})
}

func (h *financeHandlers) updateDebt(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid debt id")
		return
	}
	var req debtRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	d, msg := req.toDebt()
	if msg != "" {
		badRequest(w, msg)
		return
	}
	d.ID = id
	out, err := h.store.UpdateFinanceDebt(r.Context(), user.ID, d)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "debt not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"debt": out})
	}
}

// POST /finance/debts/{id}/payments — частичный или полный возврат.
func (h *financeHandlers) addDebtPayment(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid debt id")
		return
	}
	var req struct {
		PaidOn string  `json:"paid_on"`
		Amount float64 `json:"amount"`
		Note   string  `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Amount <= 0 {
		badRequest(w, "amount must be > 0")
		return
	}
	settings, _ := h.store.GetFinanceSettings(r.Context(), user.ID)
	paidOn := localToday(settings.TzOff)
	if req.PaidOn != "" {
		t, err := time.Parse("2006-01-02", req.PaidOn)
		if err != nil {
			badRequest(w, "paid_on must be YYYY-MM-DD")
			return
		}
		paidOn = t
	}
	out, err := h.store.AddFinanceDebtPayment(r.Context(), user.ID, id,
		store.FinanceDebtPayment{PaidOn: paidOn, Amount: req.Amount, Note: req.Note})
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "debt not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"debt": out})
	}
}

func (h *financeHandlers) listDebtPayments(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid debt id")
		return
	}
	payments, err := h.store.ListFinanceDebtPayments(r.Context(), user.ID, id)
	if err != nil {
		internalError(w)
		return
	}
	if payments == nil {
		payments = []store.FinanceDebtPayment{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"payments": payments})
}

// POST /finance/debts/{id}/status — простить остаток / вернуть в работу.
func (h *financeHandlers) setDebtStatus(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid debt id")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if !debtStatuses[req.Status] {
		badRequest(w, "status must be open|partial|paid|forgiven")
		return
	}
	out, err := h.store.SetFinanceDebtStatus(r.Context(), user.ID, id, req.Status)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "debt not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"debt": out})
	}
}

func (h *financeHandlers) archiveDebt(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid debt id")
		return
	}
	var req struct {
		Archived bool `json:"archived"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	err = h.store.ArchiveFinanceDebt(r.Context(), user.ID, id, req.Archived)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "debt not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"archived": req.Archived})
	}
}

func (h *financeHandlers) deleteDebt(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid debt id")
		return
	}
	err = h.store.DeleteFinanceDebt(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "debt not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- настройки страницы ---

func (h *financeHandlers) getSettings(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	s, err := h.store.GetFinanceSettings(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (h *financeHandlers) setSettings(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	cur, err := h.store.GetFinanceSettings(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	var req struct {
		BaseCurrency *string `json:"base_currency"`
		NotifyHour   *int    `json:"notify_hour"`
		HideAmounts  *bool   `json:"hide_amounts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.BaseCurrency != nil {
		code := strings.ToLower(strings.TrimSpace(*req.BaseCurrency))
		if !rates.CodeRe.MatchString(code) {
			badRequest(w, "invalid base currency")
			return
		}
		cur.BaseCurrency = code
	}
	if req.NotifyHour != nil {
		if *req.NotifyHour < 0 || *req.NotifyHour > 23 {
			badRequest(w, "notify_hour must be 0-23")
			return
		}
		cur.NotifyHour = *req.NotifyHour
	}
	if req.HideAmounts != nil {
		cur.HideAmounts = *req.HideAmounts
	}
	if err := h.store.SetFinanceSettings(r.Context(), user.ID, cur); err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, cur)
}
