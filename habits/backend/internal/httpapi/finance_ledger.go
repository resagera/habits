package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/rates"
	"streaks-backend/internal/store"
)

// Фазы 3–4: фактические траты с деревом категорий, счета и цели «отложено на»,
// плюс отчёт по группам и месяцам.

var (
	txKinds      = map[string]bool{"expense": true, "income": true, "transfer": true}
	catKinds     = map[string]bool{"expense": true, "income": true}
	accountKinds = map[string]bool{
		"cash": true, "card": true, "bank": true, "savings": true, "other": true,
	}
)

// inBase приводит сумму записи к нынешней базовой валюте. Если курс был
// зафиксирован к той же базе — берём его (прошлые месяцы не должны «плыть»
// при изменении курса). Если базовую валюту с тех пор сменили, пересчитываем
// живым курсом: другого честного варианта нет.
func inBase(amount, rateToBase float64, rowBase, base string, conv func(float64, string) float64, cur string) float64 {
	if rowBase != "" && strings.EqualFold(rowBase, base) {
		return amount * rateToBase
	}
	return conv(amount, cur)
}

// --- справочники одним запросом ---

// GET /finance/refs — категории, счета с балансами, цели и валюты.
// Страница берёт их одним запросом: три отдельных похода за справочниками при
// каждом открытии формы заметны на мобильном интернете.
func (h *financeHandlers) refs(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	ctx := r.Context()

	settings, err := h.store.GetFinanceSettings(ctx, user.ID)
	if err != nil {
		internalError(w)
		return
	}
	cats, err := h.store.ListFinanceCategories(ctx, user.ID, false)
	if err != nil {
		internalError(w)
		return
	}
	accounts, err := h.accountsWithBalance(r, settings.BaseCurrency)
	if err != nil {
		internalError(w)
		return
	}
	goals, err := h.store.ListFinanceGoals(ctx, user.ID, false)
	if err != nil {
		internalError(w)
		return
	}
	currencies, err := h.store.FinanceTxCurrencies(ctx, user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if cats == nil {
		cats = []store.FinanceCategory{}
	}
	if goals == nil {
		goals = []store.FinanceGoal{}
	}
	if currencies == nil {
		currencies = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"base_currency": settings.BaseCurrency,
		"categories":    cats,
		"accounts":      accounts.list,
		"goals":         goals,
		"currencies":    currencies,
		"totals":        accounts.totals,
	})
}

// --- категории ---

type categoryRequest struct {
	ParentID *int64 `json:"parent_id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Icon     string `json:"icon"`
	Color    string `json:"color"`
	Position int    `json:"position"`
}

func (req categoryRequest) toCategory() (store.FinanceCategory, string) {
	c := store.FinanceCategory{
		ParentID: req.ParentID, Name: strings.TrimSpace(req.Name),
		Kind: req.Kind, Icon: strings.TrimSpace(req.Icon),
		Color: strings.TrimSpace(req.Color), Position: req.Position,
	}
	if c.Name == "" || len([]rune(c.Name)) > 100 {
		return c, "name is required (1-100 chars)"
	}
	if c.Kind == "" {
		c.Kind = "expense"
	}
	if !catKinds[c.Kind] {
		return c, "kind must be expense|income"
	}
	if c.ParentID != nil && *c.ParentID <= 0 {
		c.ParentID = nil
	}
	return c, ""
}

func (h *financeHandlers) listCategories(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	cats, err := h.store.ListFinanceCategories(r.Context(), user.ID,
		r.URL.Query().Get("archived") == "1")
	if err != nil {
		internalError(w)
		return
	}
	if cats == nil {
		cats = []store.FinanceCategory{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": cats})
}

func (h *financeHandlers) createCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req categoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	c, msg := req.toCategory()
	if msg != "" {
		badRequest(w, msg)
		return
	}
	out, err := h.store.CreateFinanceCategory(r.Context(), user.ID, c)
	switch {
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "такая категория здесь уже есть")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "parent not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"category": out})
	}
}

func (h *financeHandlers) updateCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid category id")
		return
	}
	var req categoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	c, msg := req.toCategory()
	if msg != "" {
		badRequest(w, msg)
		return
	}
	c.ID = id
	out, err := h.store.UpdateFinanceCategory(r.Context(), user.ID, c)
	switch {
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict",
			"имя занято или категорию нельзя вложить в саму себя")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"category": out})
	}
}

// DELETE /finance/categories/{id} — узел удаляется, дети и записи поднимаются
// к родителю. Траты при уборке справочника теряться не должны.
func (h *financeHandlers) deleteCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid category id")
		return
	}
	err = h.store.DeleteFinanceCategory(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *financeHandlers) archiveCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid category id")
		return
	}
	var req struct {
		Archived bool `json:"archived"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	err = h.store.ArchiveFinanceCategory(r.Context(), user.ID, id, req.Archived)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"archived": req.Archived})
	}
}

// POST /finance/categories/seed — типовое дерево одним нажатием.
func (h *financeHandlers) seedCategories(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	n, err := h.store.SeedFinanceCategories(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	cats, err := h.store.ListFinanceCategories(r.Context(), user.ID, false)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"created": n, "categories": cats})
}

// --- транзакции ---

type txRequest struct {
	Kind        string  `json:"kind"`
	SpentOn     string  `json:"spent_on"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	CategoryID  *int64  `json:"category_id"`
	AccountID   *int64  `json:"account_id"`
	ToAccountID *int64  `json:"to_account_id"`
	Merchant    string  `json:"merchant"`
	Note        string  `json:"note"`
	ExternalID  *string `json:"external_id"`
}

func (req txRequest) toTx() (store.FinanceTx, string) {
	t := store.FinanceTx{
		Kind: req.Kind, Amount: req.Amount,
		Currency:   strings.ToLower(strings.TrimSpace(req.Currency)),
		CategoryID: req.CategoryID, AccountID: req.AccountID,
		ToAccountID: req.ToAccountID, Merchant: strings.TrimSpace(req.Merchant),
		Note: strings.TrimSpace(req.Note), ExternalID: req.ExternalID,
	}
	if t.Kind == "" {
		t.Kind = "expense"
	}
	if !txKinds[t.Kind] {
		return t, "kind must be expense|income|transfer"
	}
	if t.Amount < 0 {
		return t, "amount must be >= 0"
	}
	if !rates.CodeRe.MatchString(t.Currency) {
		return t, "invalid currency"
	}
	spent, err := time.Parse("2006-01-02", req.SpentOn)
	if err != nil {
		return t, "spent_on must be YYYY-MM-DD"
	}
	t.SpentOn = spent
	if t.Kind == "transfer" {
		if t.AccountID == nil || t.ToAccountID == nil {
			return t, "transfer needs account_id and to_account_id"
		}
		if *t.AccountID == *t.ToAccountID {
			return t, "transfer between the same account"
		}
		t.CategoryID = nil // перевод — не расход, категории у него нет
	} else {
		t.ToAccountID = nil
	}
	if t.CategoryID != nil && *t.CategoryID <= 0 {
		t.CategoryID = nil
	}
	if t.AccountID != nil && *t.AccountID <= 0 {
		t.AccountID = nil
	}
	return t, ""
}

// GET /finance/transactions?from=&to=&category_id=&account_id=&kind=&q=&limit=&offset=
func (h *financeHandlers) listTx(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	q := r.URL.Query()
	f := store.FinanceTxFilter{
		Kind:  q.Get("kind"),
		Query: strings.TrimSpace(q.Get("q")),
	}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			badRequest(w, "from must be YYYY-MM-DD")
			return
		}
		f.From = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			badRequest(w, "to must be YYYY-MM-DD")
			return
		}
		f.To = &t
	}
	if f.Kind != "" && !txKinds[f.Kind] {
		badRequest(w, "kind must be expense|income|transfer")
		return
	}
	f.CategoryID, _ = strconv.ParseInt(q.Get("category_id"), 10, 64)
	f.AccountID, _ = strconv.ParseInt(q.Get("account_id"), 10, 64)
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	f.Offset, _ = strconv.Atoi(q.Get("offset"))

	list, total, err := h.store.ListFinanceTx(r.Context(), user.ID, f)
	if err != nil {
		internalError(w)
		return
	}
	if list == nil {
		list = []store.FinanceTx{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"transactions": list, "total": total})
}

func (h *financeHandlers) createTx(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req txRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	t, msg := req.toTx()
	if msg != "" {
		badRequest(w, msg)
		return
	}
	settings, err := h.store.GetFinanceSettings(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	t.BaseCurrency = settings.BaseCurrency
	t.RateToBase = h.rateTo(t.Currency, settings.BaseCurrency)

	out, err := h.store.CreateFinanceTx(r.Context(), user.ID, t)
	switch {
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "такая запись уже импортирована")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"transaction": out})
	}
}

func (h *financeHandlers) updateTx(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid transaction id")
		return
	}
	var req txRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	t, msg := req.toTx()
	if msg != "" {
		badRequest(w, msg)
		return
	}
	old, err := h.store.FinanceTxByID(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "transaction not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	t.ID = id
	// курс не трогаем, пока сумма и валюта те же: правка заметки не должна
	// переписывать историю чужим курсом
	t.BaseCurrency, t.RateToBase = old.BaseCurrency, old.RateToBase
	if !strings.EqualFold(old.Currency, t.Currency) || old.BaseCurrency == "" {
		settings, err := h.store.GetFinanceSettings(r.Context(), user.ID)
		if err != nil {
			internalError(w)
			return
		}
		t.BaseCurrency = settings.BaseCurrency
		t.RateToBase = h.rateTo(t.Currency, settings.BaseCurrency)
	}
	out, err := h.store.UpdateFinanceTx(r.Context(), user.ID, t)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "transaction not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"transaction": out})
	}
}

func (h *financeHandlers) deleteTx(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid transaction id")
		return
	}
	err = h.store.DeleteFinanceTx(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "transaction not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *financeHandlers) rateTo(cur, base string) float64 {
	if _, rate, err := h.rates.Convert(1, cur, base); err == nil {
		return rate
	}
	return 1
}

// --- счета ---

type accountView struct {
	store.FinanceAccount
	Balance     float64 `json:"balance"`      // в валюте счёта
	BalanceBase float64 `json:"balance_base"` // в базовой валюте
}

type accountsResult struct {
	list   []accountView
	totals map[string]any
}

// accountsWithBalance считает остатки: стартовый баланс плюс движения. Суммы в
// чужой валюте приводятся к валюте счёта через базовую — иначе счёт в драмах с
// одной покупкой в долларах показывал бы бессмыслицу.
func (h *financeHandlers) accountsWithBalance(r *http.Request, base string) (accountsResult, error) {
	user := auth.UserFromContext(r.Context())
	ctx := r.Context()
	accounts, err := h.store.ListFinanceAccounts(ctx, user.ID, false)
	if err != nil {
		return accountsResult{}, err
	}
	moves, err := h.store.FinanceAccountMoves(ctx, user.ID)
	if err != nil {
		return accountsResult{}, err
	}
	conv := h.converter(base)

	byID := map[int64]*accountView{}
	out := make([]accountView, len(accounts))
	for i, a := range accounts {
		out[i] = accountView{FinanceAccount: a, Balance: a.StartBalance}
		byID[a.ID] = &out[i]
	}
	// Остаток — величина «на сейчас», поэтому суммы в чужой валюте приводятся
	// живым курсом (в отличие от отчёта, где важен курс на дату траты).
	// Совпадающая валюта не пересчитывается вовсе — копейка в копейку.
	value := func(m store.FinanceAccountMove, acc *accountView) float64 {
		if strings.EqualFold(m.Currency, acc.Currency) {
			return m.Amount
		}
		v := conv(m.Amount, m.Currency) // → базовая
		if strings.EqualFold(acc.Currency, base) {
			return v
		}
		return v * h.rateTo(base, strings.ToLower(acc.Currency))
	}
	for _, m := range moves {
		if m.AccountID != nil {
			if a, ok := byID[*m.AccountID]; ok {
				switch m.Kind {
				case "income":
					a.Balance += value(m, a)
				default: // expense и уход перевода
					a.Balance -= value(m, a)
				}
			}
		}
		if m.Kind == "transfer" && m.ToAccountID != nil {
			if a, ok := byID[*m.ToAccountID]; ok {
				a.Balance += value(m, a)
			}
		}
	}

	var total float64
	for i := range out {
		out[i].Balance = round2(out[i].Balance)
		out[i].BalanceBase = round2(conv(out[i].Balance, out[i].Currency))
		if out[i].IncludeInTotal {
			total += out[i].BalanceBase
		}
	}
	return accountsResult{
		list:   out,
		totals: map[string]any{"balance_base": round2(total), "base_currency": base},
	}, nil
}

func (h *financeHandlers) listAccounts(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	settings, err := h.store.GetFinanceSettings(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	res, err := h.accountsWithBalance(r, settings.BaseCurrency)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accounts": res.list, "totals": res.totals,
	})
}

type accountRequest struct {
	Name           string  `json:"name"`
	Kind           string  `json:"kind"`
	Currency       string  `json:"currency"`
	StartBalance   float64 `json:"start_balance"`
	IncludeInTotal *bool   `json:"include_in_total"`
	Note           string  `json:"note"`
	Position       *int    `json:"position"`
}

func (req accountRequest) toAccount() (store.FinanceAccount, string) {
	a := store.FinanceAccount{
		Name: strings.TrimSpace(req.Name), Kind: req.Kind,
		Currency:     strings.ToLower(strings.TrimSpace(req.Currency)),
		StartBalance: req.StartBalance, IncludeInTotal: true,
		Note: strings.TrimSpace(req.Note),
	}
	if req.IncludeInTotal != nil {
		a.IncludeInTotal = *req.IncludeInTotal
	}
	if req.Position != nil {
		a.Position = *req.Position
	}
	if a.Name == "" || len([]rune(a.Name)) > 100 {
		return a, "name is required (1-100 chars)"
	}
	if a.Kind == "" {
		a.Kind = "card"
	}
	if !accountKinds[a.Kind] {
		return a, "kind must be cash|card|bank|savings|other"
	}
	if !rates.CodeRe.MatchString(a.Currency) {
		return a, "invalid currency"
	}
	return a, ""
}

func (h *financeHandlers) createAccount(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req accountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	a, msg := req.toAccount()
	if msg != "" {
		badRequest(w, msg)
		return
	}
	out, err := h.store.CreateFinanceAccount(r.Context(), user.ID, a)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"account": out})
}

func (h *financeHandlers) updateAccount(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid account id")
		return
	}
	var req accountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	a, msg := req.toAccount()
	if msg != "" {
		badRequest(w, msg)
		return
	}
	a.ID = id
	out, err := h.store.UpdateFinanceAccount(r.Context(), user.ID, a)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "account not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"account": out})
	}
}

func (h *financeHandlers) archiveAccount(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid account id")
		return
	}
	var req struct {
		Archived bool `json:"archived"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	err = h.store.ArchiveFinanceAccount(r.Context(), user.ID, id, req.Archived)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "account not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"archived": req.Archived})
	}
}

func (h *financeHandlers) deleteAccount(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid account id")
		return
	}
	err = h.store.DeleteFinanceAccount(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "account not found")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict",
			"по счёту есть движения — уберите его в архив")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- цели «отложено на» ---

func (h *financeHandlers) listGoals(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	goals, err := h.store.ListFinanceGoals(r.Context(), user.ID,
		r.URL.Query().Get("archived") == "1")
	if err != nil {
		internalError(w)
		return
	}
	if goals == nil {
		goals = []store.FinanceGoal{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"goals": goals})
}

type goalRequest struct {
	Name         string  `json:"name"`
	TargetAmount float64 `json:"target_amount"`
	Currency     string  `json:"currency"`
	AccountID    *int64  `json:"account_id"`
	DueDate      string  `json:"due_date"`
	Note         string  `json:"note"`
}

func (req goalRequest) toGoal() (store.FinanceGoal, string) {
	g := store.FinanceGoal{
		Name: strings.TrimSpace(req.Name), TargetAmount: req.TargetAmount,
		Currency:  strings.ToLower(strings.TrimSpace(req.Currency)),
		AccountID: req.AccountID, Note: strings.TrimSpace(req.Note),
	}
	if g.Name == "" || len([]rune(g.Name)) > 200 {
		return g, "name is required (1-200 chars)"
	}
	if g.TargetAmount <= 0 {
		return g, "target_amount must be > 0"
	}
	if !rates.CodeRe.MatchString(g.Currency) {
		return g, "invalid currency"
	}
	if g.AccountID != nil && *g.AccountID <= 0 {
		g.AccountID = nil
	}
	if req.DueDate != "" {
		t, err := time.Parse("2006-01-02", req.DueDate)
		if err != nil {
			return g, "due_date must be YYYY-MM-DD"
		}
		g.DueDate = &t
	}
	return g, ""
}

func (h *financeHandlers) createGoal(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req goalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	g, msg := req.toGoal()
	if msg != "" {
		badRequest(w, msg)
		return
	}
	out, err := h.store.CreateFinanceGoal(r.Context(), user.ID, g)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"goal": out})
}

func (h *financeHandlers) updateGoal(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid goal id")
		return
	}
	var req goalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	g, msg := req.toGoal()
	if msg != "" {
		badRequest(w, msg)
		return
	}
	g.ID = id
	out, err := h.store.UpdateFinanceGoal(r.Context(), user.ID, g)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "goal not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"goal": out})
	}
}

func (h *financeHandlers) archiveGoal(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid goal id")
		return
	}
	var req struct {
		Archived bool `json:"archived"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	err = h.store.ArchiveFinanceGoal(r.Context(), user.ID, id, req.Archived)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "goal not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"archived": req.Archived})
	}
}

func (h *financeHandlers) deleteGoal(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid goal id")
		return
	}
	err = h.store.DeleteFinanceGoal(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "goal not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// POST /finance/goals/{id}/moves — отложить (плюс) или снять (минус).
func (h *financeHandlers) addGoalMove(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid goal id")
		return
	}
	var req struct {
		MovedOn string  `json:"moved_on"`
		Amount  float64 `json:"amount"`
		Note    string  `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Amount == 0 {
		badRequest(w, "amount must not be zero")
		return
	}
	settings, _ := h.store.GetFinanceSettings(r.Context(), user.ID)
	movedOn := localToday(settings.TzOff)
	if req.MovedOn != "" {
		t, err := time.Parse("2006-01-02", req.MovedOn)
		if err != nil {
			badRequest(w, "moved_on must be YYYY-MM-DD")
			return
		}
		movedOn = t
	}
	out, err := h.store.AddFinanceGoalMove(r.Context(), user.ID, id,
		store.FinanceGoalMove{MovedOn: movedOn, Amount: req.Amount, Note: req.Note})
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "goal not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"goal": out})
	}
}

func (h *financeHandlers) listGoalMoves(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid goal id")
		return
	}
	moves, err := h.store.ListFinanceGoalMoves(r.Context(), user.ID, id)
	if err != nil {
		internalError(w)
		return
	}
	if moves == nil {
		moves = []store.FinanceGoalMove{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"moves": moves})
}

func (h *financeHandlers) deleteGoalMove(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid goal id")
		return
	}
	moveID, err := strconv.ParseInt(r.PathValue("move_id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid move id")
		return
	}
	out, err := h.store.DeleteFinanceGoalMove(r.Context(), user.ID, id, moveID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "move not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"goal": out})
	}
}

// --- отчёт ---

type monthStat struct {
	Month   string  `json:"month"`
	Expense float64 `json:"expense"`
	Income  float64 `json:"income"`
}

type catStat struct {
	ID       int64   `json:"id"`
	ParentID *int64  `json:"parent_id"`
	Name     string  `json:"name"`
	Icon     string  `json:"icon"`
	Depth    int     `json:"depth"`
	Own      float64 `json:"own"`   // траты, лежащие прямо в этой категории
	Total    float64 `json:"total"` // вместе с подкатегориями
	Share    float64 `json:"share"` // доля в расходах периода, %
	Prev     float64 `json:"prev"`  // столько же было в прошлом периоде
}

// GET /finance/stats?months=6[&from=&to=][&category_id=]
//
// Отчёт по группам и месяцам. Считается в Go по «сырым» строкам: свернуть
// дерево категорий и сравнить с прошлым периодом в SQL можно, но читать и
// править такой запрос потом невозможно, а объёмы личных финансов ничтожны.
func (h *financeHandlers) stats(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	ctx := r.Context()
	q := r.URL.Query()

	settings, err := h.store.GetFinanceSettings(ctx, user.ID)
	if err != nil {
		internalError(w)
		return
	}
	today := localToday(settings.TzOff)

	months := 6
	if v, err := strconv.Atoi(q.Get("months")); err == nil && v >= 1 && v <= 36 {
		months = v
	}
	to := endOfMonth(today)
	from := time.Date(to.Year(), to.Month()-time.Month(months-1), 1, 0, 0, 0, 0, time.UTC)
	if v := q.Get("from"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			badRequest(w, "from must be YYYY-MM-DD")
			return
		}
		from = t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			badRequest(w, "to must be YYYY-MM-DD")
			return
		}
		to = t
	}
	if to.Before(from) {
		badRequest(w, "to must not be before from")
		return
	}
	// прошлый период той же длины — чтобы «стало больше/меньше» было честным
	span := to.Sub(from)
	prevTo := from.AddDate(0, 0, -1)
	prevFrom := prevTo.Add(-span)

	rows, err := h.store.FinanceStatRows(ctx, user.ID, prevFrom, to)
	if err != nil {
		internalError(w)
		return
	}
	cats, err := h.store.ListFinanceCategories(ctx, user.ID, true)
	if err != nil {
		internalError(w)
		return
	}

	base := settings.BaseCurrency
	conv := h.converter(base)

	parent := map[int64]*int64{}
	byID := map[int64]store.FinanceCategory{}
	for _, c := range cats {
		byID[c.ID] = c
		parent[c.ID] = c.ParentID
	}

	// ограничение отчёта поддеревом выбранной категории
	root, _ := strconv.ParseInt(q.Get("category_id"), 10, 64)
	inScope := func(cat *int64) bool {
		if root <= 0 {
			return true
		}
		for id := cat; id != nil; {
			if *id == root {
				return true
			}
			id = parent[*id]
		}
		return false
	}

	monthIdx := map[string]int{}
	var monthsOut []monthStat
	for d := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC); !d.After(to); d = d.AddDate(0, 1, 0) {
		monthIdx[d.Format("2006-01")] = len(monthsOut)
		monthsOut = append(monthsOut, monthStat{Month: d.Format("2006-01")})
	}

	own := map[int64]float64{}
	prevOwn := map[int64]float64{}
	var totalExpense, totalIncome, prevExpense, uncategorized float64

	for _, row := range rows {
		if !inScope(row.CategoryID) {
			continue
		}
		v := inBase(row.Amount, row.RateToBase, row.BaseCurrency, base, conv, row.Currency)
		day := dateOnly(row.SpentOn)
		if day.Before(from) { // прошлый период — только для сравнения
			if row.Kind == "expense" {
				prevExpense += v
				if row.CategoryID != nil {
					prevOwn[*row.CategoryID] += v
				}
			}
			continue
		}
		if i, ok := monthIdx[day.Format("2006-01")]; ok {
			if row.Kind == "income" {
				monthsOut[i].Income += v
			} else {
				monthsOut[i].Expense += v
			}
		}
		if row.Kind == "income" {
			totalIncome += v
			continue
		}
		totalExpense += v
		if row.CategoryID == nil {
			uncategorized += v
			continue
		}
		own[*row.CategoryID] += v
	}

	// свёртка по дереву: сумма категории включает подкатегории
	total := map[int64]float64{}
	prevTotal := map[int64]float64{}
	rollup := func(src, dst map[int64]float64) {
		for id, v := range src {
			for cur := &id; cur != nil; cur = parent[*cur] {
				dst[*cur] += v
			}
		}
	}
	rollup(own, total)
	rollup(prevOwn, prevTotal)

	// плоский список в порядке дерева: показываем только те ветки, где деньги
	// есть — пустой справочник в отчёте лишь мешает
	children := map[int64][]store.FinanceCategory{}
	var roots []store.FinanceCategory
	for _, c := range cats {
		if c.ParentID == nil {
			roots = append(roots, c)
		} else {
			children[*c.ParentID] = append(children[*c.ParentID], c)
		}
	}
	sortCats := func(list []store.FinanceCategory) {
		sort.SliceStable(list, func(i, j int) bool {
			return total[list[i].ID] > total[list[j].ID]
		})
	}
	sortCats(roots)
	out := []catStat{}
	var walk func(list []store.FinanceCategory, depth int)
	walk = func(list []store.FinanceCategory, depth int) {
		for _, c := range list {
			if total[c.ID] == 0 && prevTotal[c.ID] == 0 {
				continue
			}
			share := 0.0
			if totalExpense > 0 {
				share = total[c.ID] / totalExpense * 100
			}
			out = append(out, catStat{
				ID: c.ID, ParentID: c.ParentID, Name: c.Name, Icon: c.Icon, Depth: depth,
				Own: round2(own[c.ID]), Total: round2(total[c.ID]),
				Share: round2(share), Prev: round2(prevTotal[c.ID]),
			})
			kids := children[c.ID]
			sortCats(kids)
			walk(kids, depth+1)
		}
	}
	if root > 0 {
		if c, ok := byID[root]; ok {
			walk([]store.FinanceCategory{c}, 0)
		}
	} else {
		walk(roots, 0)
	}

	for i := range monthsOut {
		monthsOut[i].Expense = round2(monthsOut[i].Expense)
		monthsOut[i].Income = round2(monthsOut[i].Income)
	}
	// средний месяц считаем по месяцам с записями: пустые будущие месяцы
	// занижали бы среднее
	var sum, n float64
	for _, m := range monthsOut {
		if m.Expense > 0 {
			sum += m.Expense
			n++
		}
	}
	avg := 0.0
	if n > 0 {
		avg = sum / n
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"base_currency":  base,
		"from":           from.Format("2006-01-02"),
		"to":             to.Format("2006-01-02"),
		"months":         monthsOut,
		"categories":     out,
		"uncategorized":  round2(uncategorized),
		"total_expense":  round2(totalExpense),
		"total_income":   round2(totalIncome),
		"prev_expense":   round2(prevExpense),
		"avg_month":      round2(avg),
		"hide_amounts":   settings.HideAmounts,
		"prev_from":      prevFrom.Format("2006-01-02"),
		"prev_to":        prevTo.Format("2006-01-02"),
		"category_scope": root,
	})
}
