package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"streaks-backend/internal/aicoder"
	"streaks-backend/internal/auth"
	"streaks-backend/internal/receiptimport"
	"streaks-backend/internal/receipts"
	"streaks-backend/internal/store"
)

// Группы товаров, разметка позиций чека и история цен.
//
// Разметка идёт по НАЗВАНИЮ товара, а не по конкретной строке чека: названия у
// магазина повторяются посимвольно, поэтому одно решение закрывает товар во
// всех чеках — и в прошлых тоже.

type itemsHandlers struct {
	store    *store.Store
	receipts *receiptimport.Importer
	hub      *aicoder.Hub
}

func (h *itemsHandlers) allowed(r *http.Request) bool {
	user := auth.UserFromContext(r.Context())
	return h.receipts.Allowed(r.Context(), user.ID)
}

func (h *itemsHandlers) deny(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "forbidden", "разбор чеков не подключён")
}

// GET /finance/item-groups — встроенные группы и то, на какие категории они
// сейчас отображаются.
func (h *itemsHandlers) groups(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(r) {
		h.deny(w)
		return
	}
	user := auth.UserFromContext(r.Context())
	m, err := h.store.ItemGroupMap(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	words, err := h.store.ListWordRules(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	stats, err := h.store.CategoryItemStats(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	// слова встроенного словаря показываем как есть: без них непонятно,
	// почему товар уехал именно в эту группу
	dict := map[string][]string{}
	for _, g := range receipts.Groups {
		dict[g.Code] = receipts.GroupWords(g.Code)
	}
	if words == nil {
		words = []store.WordRule{}
	}
	if stats == nil {
		stats = []store.CategoryItemStats{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"groups": receipts.Groups, "map": m, "dictionary": dict,
		"words": words, "stats": stats,
	})
}

// POST /finance/items/words {pattern, category_id} — своё словарное правило.
func (h *itemsHandlers) createWord(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(r) {
		h.deny(w)
		return
	}
	user := auth.UserFromContext(r.Context())
	var req struct {
		Pattern    string `json:"pattern"`
		CategoryID int64  `json:"category_id"`
		Position   int    `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Pattern = strings.ToLower(strings.TrimSpace(req.Pattern))
	if len([]rune(req.Pattern)) < 2 || len([]rune(req.Pattern)) > 100 {
		badRequest(w, "слово: от 2 до 100 символов")
		return
	}
	if _, err := h.store.FinanceCategoryByID(r.Context(), user.ID, req.CategoryID); err != nil {
		badRequest(w, "category not found")
		return
	}
	out, err := h.store.CreateWordRule(r.Context(), user.ID, store.WordRule{
		Pattern: req.Pattern, CategoryID: req.CategoryID, Position: req.Position,
	})
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "conflict", "такое слово уже есть")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	// новое правило ничего не значит, пока чеки не перебраны
	n, err := h.receipts.ClassifyAll(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"word": out, "reclassified": n})
}

func (h *itemsHandlers) deleteWord(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(r) {
		h.deny(w)
		return
	}
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid rule id")
		return
	}
	if err := h.store.DeleteWordRule(r.Context(), user.ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "rule not found")
			return
		}
		internalError(w)
		return
	}
	n, err := h.receipts.ClassifyAll(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "reclassified": n})
}

// POST /finance/item-groups/seed {reset} — завести категории под встроенные
// группы. reset перепривязывает все группы к своим категориям и перебирает уже
// разобранные чеки: иначе починить «все группы смотрят в одну категорию»
// изнутри интерфейса было бы нечем.
func (h *itemsHandlers) seedGroups(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(r) {
		h.deny(w)
		return
	}
	user := auth.UserFromContext(r.Context())
	var req struct {
		Reset bool `json:"reset"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	m, err := h.store.SeedItemGroups(r.Context(), user.ID, req.Reset)
	if err != nil {
		internalError(w)
		return
	}
	// новые привязки ничего не значат, пока чеки не перебраны заново
	reclassified, err := h.receipts.ClassifyAll(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	cats, err := h.store.ListFinanceCategories(r.Context(), user.ID, false)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"groups": receipts.Groups, "map": m, "categories": cats,
		"reclassified": reclassified,
	})
}

// PUT /finance/item-groups/{code} — переставить группу на другую категорию.
func (h *itemsHandlers) setGroup(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(r) {
		h.deny(w)
		return
	}
	user := auth.UserFromContext(r.Context())
	code := r.PathValue("code")
	var req struct {
		CategoryID int64 `json:"category_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.CategoryID <= 0 {
		badRequest(w, "category_id is required")
		return
	}
	if _, err := h.store.FinanceCategoryByID(r.Context(), user.ID, req.CategoryID); err != nil {
		badRequest(w, "category not found")
		return
	}
	if err := h.store.SetItemGroup(r.Context(), user.ID, code, req.CategoryID); err != nil {
		internalError(w)
		return
	}
	// без пересбора смена привязки не тронула бы уже разобранные чеки, и
	// настройка выглядела бы сломанной
	reclassified, err := h.receipts.ClassifyAll(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "reclassified": reclassified})
}

// GET /finance/items/unclassified — «Не разобрано»: с чего начинать разметку.
func (h *itemsHandlers) unclassified(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(r) {
		h.deny(w)
		return
	}
	user := auth.UserFromContext(r.Context())
	list, err := h.store.UnclassifiedItems(r.Context(), user.ID, 200)
	if err != nil {
		internalError(w)
		return
	}
	if list == nil {
		list = []store.TopItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

// POST /finance/items/assign — решение по товару.
// Применяется ко всем чекам с этим товаром, включая прошлые.
func (h *itemsHandlers) assign(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(r) {
		h.deny(w)
		return
	}
	user := auth.UserFromContext(r.Context())
	var req struct {
		Items []struct {
			NameKey    string `json:"name_key"`
			NameSample string `json:"name_sample"`
			Merchant   string `json:"merchant"`
			CategoryID *int64 `json:"category_id"`
		} `json:"items"`
		Remember *bool  `json:"remember"`
		Source   string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if len(req.Items) == 0 || len(req.Items) > 500 {
		badRequest(w, "items is required (1-500)")
		return
	}
	remember := true
	if req.Remember != nil {
		remember = *req.Remember
	}
	source := "manual"
	if req.Source == "ai" {
		source = "ai"
	}
	for _, it := range req.Items {
		key := strings.TrimSpace(it.NameKey)
		if key == "" {
			continue
		}
		if it.CategoryID != nil && *it.CategoryID <= 0 {
			it.CategoryID = nil
		}
		if it.CategoryID != nil {
			if _, err := h.store.FinanceCategoryByID(r.Context(), user.ID, *it.CategoryID); err != nil {
				badRequest(w, "category not found")
				return
			}
		}
		if err := h.receipts.AssignItem(r.Context(), user.ID, it.Merchant, key,
			it.NameSample, it.CategoryID, remember, source); err != nil {
			internalError(w)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"assigned": len(req.Items)})
}

// GET /finance/items/rules — что уже запомнено.
func (h *itemsHandlers) rules(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(r) {
		h.deny(w)
		return
	}
	user := auth.UserFromContext(r.Context())
	list, err := h.store.ListItemRules(r.Context(), user.ID, 500)
	if err != nil {
		internalError(w)
		return
	}
	if list == nil {
		list = []store.ItemRule{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": list})
}

func (h *itemsHandlers) deleteRule(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(r) {
		h.deny(w)
		return
	}
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid rule id")
		return
	}
	err = h.store.DeleteItemRule(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "rule not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// POST /finance/receipts/{id}/classify — перебрать чек заново словарём и памятью.
func (h *itemsHandlers) classify(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(r) {
		h.deny(w)
		return
	}
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid receipt id")
		return
	}
	if err := h.receipts.Classify(r.Context(), user.ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "receipt not found")
			return
		}
		internalError(w)
		return
	}
	items, err := h.store.ListMailReceiptItems(r.Context(), id)
	if err != nil {
		internalError(w)
		return
	}
	if items == nil {
		items = []store.MailReceiptItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// --- доли траты ---

func (h *itemsHandlers) txSplits(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid transaction id")
		return
	}
	if _, err := h.store.FinanceTxByID(r.Context(), user.ID, id); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "transaction not found")
		return
	}
	splits, err := h.store.ListTxSplits(r.Context(), id)
	if err != nil {
		internalError(w)
		return
	}
	if splits == nil {
		splits = []store.TxSplit{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"splits": splits})
}

// PUT /finance/transactions/{id}/splits — разложить любую трату руками.
func (h *itemsHandlers) setTxSplits(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid transaction id")
		return
	}
	var req struct {
		Splits []struct {
			CategoryID *int64  `json:"category_id"`
			Amount     float64 `json:"amount"`
		} `json:"splits"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if len(req.Splits) > 50 {
		badRequest(w, "too many splits")
		return
	}
	splits := make([]store.TxSplit, 0, len(req.Splits))
	for _, s := range req.Splits {
		if s.CategoryID != nil && *s.CategoryID <= 0 {
			s.CategoryID = nil
		}
		splits = append(splits, store.TxSplit{CategoryID: s.CategoryID, Amount: s.Amount})
	}
	err = h.store.SetTxSplits(r.Context(), user.ID, id, splits)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "transaction not found")
	case err != nil:
		internalError(w)
	default:
		out, _ := h.store.ListTxSplits(r.Context(), id)
		if out == nil {
			out = []store.TxSplit{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"splits": out})
	}
}

// --- история цен ---

// GET /finance/items/top?from=&to= — на что уходит больше всего и как менялась
// цена. Работает поверх позиций чеков.
func (h *itemsHandlers) topItems(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(r) {
		h.deny(w)
		return
	}
	user := auth.UserFromContext(r.Context())
	to := time.Now().UTC()
	from := to.AddDate(-1, 0, 0)
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			to = t
		}
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := h.store.TopItems(r.Context(), user.ID, from, to, limit)
	if err != nil {
		internalError(w)
		return
	}
	if list == nil {
		list = []store.TopItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": list,
		"from":  from.Format("2006-01-02"), "to": to.Format("2006-01-02"),
	})
}

// GET /finance/items/prices?name_key= — история цены одного товара.
func (h *itemsHandlers) itemPrices(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(r) {
		h.deny(w)
		return
	}
	user := auth.UserFromContext(r.Context())
	key := strings.TrimSpace(r.URL.Query().Get("name_key"))
	if key == "" {
		badRequest(w, "name_key is required")
		return
	}
	hist, err := h.store.ItemPrices(r.Context(), user.ID, key)
	if err != nil {
		internalError(w)
		return
	}
	if hist.Points == nil {
		hist.Points = []store.PricePoint{}
	}
	writeJSON(w, http.StatusOK, hist)
}

// --- подсказки от AI-агента ---

// POST /finance/items/suggest — попросить домашнего агента предложить группы
// для неопознанных товаров.
//
// Ничего не применяется само: возвращаются предложения, применяет их человек.
func (h *itemsHandlers) suggest(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(r) {
		h.deny(w)
		return
	}
	user := auth.UserFromContext(r.Context())
	var req struct {
		Names     []string `json:"names"`
		MachineID int64    `json:"machine_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if len(req.Names) == 0 || len(req.Names) > 200 {
		badRequest(w, "names is required (1-200)")
		return
	}

	machines, err := h.store.ListAIMachines(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	var machine store.AIMachine
	for _, m := range machines {
		if req.MachineID > 0 && m.ID != req.MachineID {
			continue
		}
		if h.hub.Online(m.ID) {
			machine = m
			break
		}
		if machine.ID == 0 {
			machine = m
		}
	}
	if machine.ID == 0 {
		writeError(w, http.StatusFailedDependency, "no_machine",
			"нет AI-агента: подключите машину на странице AI")
		return
	}
	workdir := "."
	if len(machine.Dirs) > 0 {
		workdir = machine.Dirs[0]
	}

	task, err := h.store.CreateAITask(r.Context(), user.ID, machine.ID, "claude",
		workdir, "", "", "Группы товаров из чека", "")
	if err != nil {
		internalError(w)
		return
	}
	run, err := h.store.CreateAIRun(r.Context(), task.ID, suggestPrompt(req.Names), "")
	if err != nil {
		internalError(w)
		return
	}
	queuedOffline := h.hub.Dispatch(machine.ID, aicoder.Frame{
		Kind: "run", RunID: run.ID, Tool: task.Tool, Workdir: task.Workdir,
		Prompt: run.Prompt,
	}) != nil
	writeJSON(w, http.StatusCreated, map[string]any{
		"run_id": run.ID, "machine": machine.Name, "queued_offline": queuedOffline,
	})
}

// suggestPrompt — строгая инструкция: агент должен вернуть только JSON.
func suggestPrompt(names []string) string {
	var b strings.Builder
	b.WriteString("Ты раскладываешь товары из чека супермаркета по группам.\n\nГруппы:\n")
	for _, g := range receipts.Groups {
		if g.Code == "delivery" {
			continue // сборы за доставку приходят отдельной строкой, не товаром
		}
		b.WriteString(fmt.Sprintf("- %s — %s\n", g.Code, g.Title))
	}
	b.WriteString("\nОтветь ТОЛЬКО JSON-массивом, без пояснений и без markdown:\n")
	b.WriteString(`[{"name":"<точное название из списка>","group":"<code>"}]` + "\n")
	b.WriteString("Если про товар не уверен — не включай его в ответ.\n\nТовары:\n")
	for _, n := range names {
		b.WriteString("- " + n + "\n")
	}
	return b.String()
}

// GET /finance/items/suggest/{run_id} — забрать результат.
func (h *itemsHandlers) suggestions(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(r) {
		h.deny(w)
		return
	}
	user := auth.UserFromContext(r.Context())
	runID, err := strconv.ParseInt(r.PathValue("run_id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid run id")
		return
	}
	run, err := h.store.AIRunResult(r.Context(), user.ID, runID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "run not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	out := map[string]any{"status": run.Status, "error": run.Error}
	if run.Status != "done" {
		writeJSON(w, http.StatusOK, out)
		return
	}

	groups, err := h.store.ItemGroupMap(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	parsed, err := parseSuggestions(run.Output)
	if err != nil {
		out["parse_error"] = err.Error()
		out["raw"] = clipRunes(run.Output, 2000)
		writeJSON(w, http.StatusOK, out)
		return
	}
	type suggestion struct {
		Name       string `json:"name"`
		NameKey    string `json:"name_key"`
		Group      string `json:"group"`
		GroupTitle string `json:"group_title"`
		CategoryID *int64 `json:"category_id"`
	}
	list := make([]suggestion, 0, len(parsed))
	for _, p := range parsed {
		s := suggestion{
			Name: p.Name, NameKey: receipts.NameKey(p.Name),
			Group: p.Group, GroupTitle: receipts.GroupTitle(p.Group),
		}
		if id, ok := groups[p.Group]; ok {
			s.CategoryID = &id
		}
		list = append(list, s)
	}
	out["suggestions"] = list
	writeJSON(w, http.StatusOK, out)
}

type aiSuggestion struct {
	Name  string `json:"name"`
	Group string `json:"group"`
}

// parseSuggestions достаёт JSON из ответа агента: модели любят обрамить его
// пояснениями и ```-блоками, поэтому берём кусок от первой «[» до последней «]».
func parseSuggestions(output string) ([]aiSuggestion, error) {
	start := strings.Index(output, "[")
	end := strings.LastIndex(output, "]")
	if start < 0 || end <= start {
		return nil, errors.New("в ответе агента нет JSON-массива")
	}
	var list []aiSuggestion
	if err := json.Unmarshal([]byte(output[start:end+1]), &list); err != nil {
		return nil, fmt.Errorf("не разобрать JSON: %w", err)
	}
	known := map[string]bool{}
	for _, g := range receipts.Groups {
		known[g.Code] = true
	}
	out := list[:0]
	for _, s := range list {
		if s.Name != "" && known[s.Group] {
			out = append(out, s)
		}
	}
	return out, nil
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
