package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

// Food → План питания: планы на N дней, участники, слоты по дням плана,
// применение дня в свой дневник, совместный доступ и ссылка-приглашение.
type foodPlanHandlers struct {
	store *store.Store
	bot   *notify.Bot
}

var foodPlanItemKinds = map[string]bool{"free": true, "product": true, "recipe": true, "template": true}

var foodPlanTokenRe = regexp.MustCompile(`^[0-9a-f]{24}$`)

// planID — id плана из пути.
func foodPlanID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

// writeFoodPlanErr — единый перевод ошибок стора в HTTP.
func writeFoodPlanErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "план не найден")
	case errors.Is(err, store.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "нет прав на это действие")
	case errors.Is(err, store.ErrLimit):
		badRequest(w, "достигнут лимит для этого плана")
	default:
		internalError(w)
	}
}

// --- планы ---

type foodPlanReq struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Days        *int    `json:"days"`
	StartDate   *string `json:"start_date"`
	Archived    *bool   `json:"archived"`
}

func applyFoodPlanReq(p *store.FoodPlan, req foodPlanReq) string {
	if req.Name != nil {
		p.Name = strings.TrimSpace(*req.Name)
	}
	if l := len(p.Name); l == 0 || l > 200 {
		return "название плана обязательно (до 200 символов)"
	}
	if req.Description != nil {
		if len(*req.Description) > 2000 {
			return "описание до 2000 символов"
		}
		p.Description = *req.Description
	}
	if req.Days != nil {
		p.Days = *req.Days
	}
	if p.Days < 1 || p.Days > store.FoodPlanMaxDays {
		return fmt.Sprintf("длительность плана — от 1 до %d дней", store.FoodPlanMaxDays)
	}
	if req.StartDate != nil {
		if *req.StartDate != "" && !foodValidDate(*req.StartDate) {
			return "invalid start_date"
		}
		p.StartDate = *req.StartDate
	}
	if req.Archived != nil {
		p.Archived = *req.Archived
	}
	return ""
}

// GET /food/plans?archived=
func (h *foodPlanHandlers) list(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	archived := r.URL.Query().Get("archived") == "true"
	plans, err := h.store.ListFoodPlans(r.Context(), user.ID, archived)
	if err != nil {
		internalError(w)
		return
	}
	if plans == nil {
		plans = []store.FoodPlanCard{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": plans})
}

// POST /food/plans
func (h *foodPlanHandlers) create(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req foodPlanReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid json")
		return
	}
	p := store.FoodPlan{Days: 7}
	if msg := applyFoodPlanReq(&p, req); msg != "" {
		badRequest(w, msg)
		return
	}
	created, err := h.store.CreateFoodPlan(r.Context(), user.ID, p)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"plan": created})
}

// GET /food/plans/{id} — план целиком со сводкой по дням.
func (h *foodPlanHandlers) get(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := foodPlanID(r)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	plan, err := h.store.GetFoodPlan(r.Context(), user.ID, id)
	if err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plan": plan})
}

// PUT /food/plans/{id} — частичное обновление (только владелец).
func (h *foodPlanHandlers) update(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := foodPlanID(r)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	cur, err := h.store.GetFoodPlan(r.Context(), user.ID, id)
	if err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	var req foodPlanReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid json")
		return
	}
	if msg := applyFoodPlanReq(cur, req); msg != "" {
		badRequest(w, msg)
		return
	}
	updated, err := h.store.UpdateFoodPlan(r.Context(), user.ID, id, *cur)
	if err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plan": updated})
}

// DELETE /food/plans/{id}
func (h *foodPlanHandlers) delete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := foodPlanID(r)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	if err := h.store.DeleteFoodPlan(r.Context(), user.ID, id); err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /food/plans/{id}/duplicate {name}
func (h *foodPlanHandlers) duplicate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := foodPlanID(r)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	name := strings.TrimSpace(req.Name)
	if len(name) > 200 {
		badRequest(w, "название до 200 символов")
		return
	}
	plan, err := h.store.DuplicateFoodPlan(r.Context(), user.ID, id, name)
	if err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"plan": plan})
}

// POST /food/plans/{id}/copy-days {from_day, to_day, count} — копия дня/недели
// поверх целевых дней.
func (h *foodPlanHandlers) copyDays(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := foodPlanID(r)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	var req struct {
		FromDay int `json:"from_day"`
		ToDay   int `json:"to_day"`
		Count   int `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid json")
		return
	}
	if req.Count <= 0 {
		req.Count = 1
	}
	max := store.FoodPlanMaxDays
	if req.FromDay < 0 || req.ToDay < 0 || req.FromDay >= max || req.ToDay >= max ||
		req.Count > max || req.FromDay+req.Count > max || req.ToDay+req.Count > max {
		badRequest(w, "дни вне диапазона плана")
		return
	}
	if req.FromDay == req.ToDay {
		badRequest(w, "исходные и целевые дни совпадают")
		return
	}
	// пересечение диапазонов сделало бы результат зависящим от порядка вставки
	if req.FromDay < req.ToDay+req.Count && req.ToDay < req.FromDay+req.Count {
		badRequest(w, "диапазоны дней пересекаются — выберите другие дни")
		return
	}
	n, err := h.store.CopyFoodPlanDays(r.Context(), user.ID, id, req.FromDay, req.ToDay, req.Count)
	if err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"copied": n})
}

// --- участники ---

type foodPlanParticipantReq struct {
	UserID *int64 `json:"user_id"`
	// User — id или @username (как в остальных «поделиться»); пустая строка
	// снимает привязку. Резолвится в UserID до применения запроса.
	User           *string  `json:"user"`
	Name           *string  `json:"name"`
	Emoji          *string  `json:"emoji"`
	PortionCoef    *float64 `json:"portion_coef"`
	CaloriesTarget *float64 `json:"calories_target"`
}

// resolveParticipantUser превращает поле user (id/@username) в user_id.
func (h *foodPlanHandlers) resolveParticipantUser(r *http.Request, req *foodPlanParticipantReq) string {
	if req.User == nil {
		return ""
	}
	q := strings.TrimSpace(*req.User)
	if q == "" {
		var zero int64
		req.UserID = &zero // «снять привязку»
		return ""
	}
	u, err := h.store.FindUserExact(r.Context(), q)
	if errors.Is(err, store.ErrNotFound) {
		return "пользователь не найден — участник может быть и без аккаунта в Habits"
	}
	if err != nil {
		return "не удалось найти пользователя"
	}
	req.UserID = &u.ID
	return ""
}

func applyFoodPlanParticipantReq(p *store.FoodPlanParticipant, req foodPlanParticipantReq) string {
	if req.Name != nil {
		p.Name = strings.TrimSpace(*req.Name)
	}
	if l := len(p.Name); l == 0 || l > 100 {
		return "имя участника обязательно (до 100 символов)"
	}
	if req.Emoji != nil {
		p.Emoji = strings.TrimSpace(*req.Emoji)
		if len([]rune(p.Emoji)) > 8 {
			return "эмодзи до 8 символов"
		}
	}
	if req.PortionCoef != nil {
		p.PortionCoef = *req.PortionCoef
	}
	if p.PortionCoef <= 0 || p.PortionCoef > 10 {
		return "коэффициент порции — больше 0 и не больше 10"
	}
	if req.CaloriesTarget != nil {
		p.CaloriesTarget = *req.CaloriesTarget
	}
	if p.CaloriesTarget < 0 || p.CaloriesTarget > 100000 {
		return "цель по калориям вне диапазона"
	}
	if req.UserID != nil {
		if *req.UserID <= 0 {
			p.UserID = nil
		} else {
			p.UserID = req.UserID
		}
	}
	return ""
}

// POST /food/plans/{id}/participants
func (h *foodPlanHandlers) createParticipant(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := foodPlanID(r)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	var req foodPlanParticipantReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid json")
		return
	}
	if msg := h.resolveParticipantUser(r, &req); msg != "" {
		badRequest(w, msg)
		return
	}
	p := store.FoodPlanParticipant{PortionCoef: 1}
	if msg := applyFoodPlanParticipantReq(&p, req); msg != "" {
		badRequest(w, msg)
		return
	}
	created, err := h.store.CreateFoodPlanParticipant(r.Context(), user.ID, id, p)
	if err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"participant": created})
}

// PUT /food/plans/{id}/participants/{pid}
func (h *foodPlanHandlers) updateParticipant(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := foodPlanID(r)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	pid, err := strconv.ParseInt(r.PathValue("pid"), 10, 64)
	if err != nil {
		badRequest(w, "invalid participant id")
		return
	}
	var req foodPlanParticipantReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid json")
		return
	}
	// PUT частичный: начинаем с текущих значений, иначе не присланные поля
	// (в первую очередь привязка к пользователю) молча обнулялись бы
	cur, err := h.store.GetFoodPlanParticipant(r.Context(), id, pid)
	if err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	if msg := h.resolveParticipantUser(r, &req); msg != "" {
		badRequest(w, msg)
		return
	}
	if msg := applyFoodPlanParticipantReq(cur, req); msg != "" {
		badRequest(w, msg)
		return
	}
	updated, err := h.store.UpdateFoodPlanParticipant(r.Context(), user.ID, id, pid, *cur)
	if err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"participant": updated})
}

// DELETE /food/plans/{id}/participants/{pid} — вместе с его личными слотами.
func (h *foodPlanHandlers) deleteParticipant(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := foodPlanID(r)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	pid, err := strconv.ParseInt(r.PathValue("pid"), 10, 64)
	if err != nil {
		badRequest(w, "invalid participant id")
		return
	}
	if err := h.store.DeleteFoodPlanParticipant(r.Context(), user.ID, id, pid); err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- слоты ---

type foodPlanSlotReq struct {
	ParticipantID *int64                `json:"participant_id"`
	DayIndex      *int                  `json:"day_index"`
	MealType      *string               `json:"meal_type"`
	AtTime        *string               `json:"time"`
	Title         *string               `json:"title"`
	Note          *string               `json:"note"`
	Items         *[]store.FoodPlanItem `json:"items"`
}

// validateFoodPlanItems — позиции слота. Приблизительная позиция (approx)
// количества и КБЖУ не требует: это и есть «примерно/ориентировочно».
func validateFoodPlanItems(items []store.FoodPlanItem) ([]store.FoodPlanItem, string) {
	if len(items) > store.FoodPlanMaxItems {
		return nil, fmt.Sprintf("слишком много позиций (максимум %d)", store.FoodPlanMaxItems)
	}
	for i := range items {
		it := &items[i]
		it.Name = strings.TrimSpace(it.Name)
		if it.Name == "" || len(it.Name) > 200 {
			return nil, "у каждой позиции должно быть название (до 200 символов)"
		}
		if it.Kind == "" {
			it.Kind = "free"
		}
		if !foodPlanItemKinds[it.Kind] {
			return nil, "invalid item kind"
		}
		if it.Kind == "free" {
			// свободная позиция ни на что не ссылается, но может нести
			// ручные КБЖУ (в т.ч. у копии плана, где ссылки сняты)
			it.RefID = nil
		}
		if it.RefID != nil && *it.RefID <= 0 {
			it.RefID = nil
		}
		if !foodUnits[it.Unit] {
			it.Unit = "g"
		}
		if it.BaseType != "ml" {
			it.BaseType = "g"
		}
		if it.Approx {
			it.Amount, it.Grams = 0, 0
			it.CaloriesPer, it.ProteinPer, it.FatPer, it.CarbsPer = 0, 0, 0, 0
			continue
		}
		if it.Amount <= 0 || it.Amount > 1000000 {
			return nil, "количество должно быть больше нуля (или отметьте позицию как примерную)"
		}
		if it.Grams <= 0 || it.Grams > 1000000 {
			return nil, "укажите вес/объём позиции (или отметьте её как примерную)"
		}
		if it.CaloriesPer < 0 || it.CaloriesPer > 10000 || it.ProteinPer < 0 || it.ProteinPer > 1000 ||
			it.FatPer < 0 || it.FatPer > 1000 || it.CarbsPer < 0 || it.CarbsPer > 1000 {
			return nil, "КБЖУ на 100 г вне допустимого диапазона"
		}
	}
	return items, ""
}

// применяет req к слоту; возвращает (replaceItems, errMsg)
func applyFoodPlanSlotReq(sl *store.FoodPlanSlot, req foodPlanSlotReq, days int) (bool, string) {
	if req.DayIndex != nil {
		sl.DayIndex = *req.DayIndex
	}
	if sl.DayIndex < 0 || sl.DayIndex >= days {
		return false, "день вне диапазона плана"
	}
	if req.MealType != nil {
		if !foodMealTypes[*req.MealType] {
			return false, "invalid meal_type"
		}
		sl.MealType = *req.MealType
	}
	if req.AtTime != nil {
		if *req.AtTime != "" && !foodTimeRe.MatchString(*req.AtTime) {
			return false, "время должно быть в формате ЧЧ:ММ"
		}
		sl.AtTime = *req.AtTime
	}
	if req.Title != nil {
		sl.Title = strings.TrimSpace(*req.Title)
		if len(sl.Title) > 200 {
			return false, "название до 200 символов"
		}
	}
	if req.Note != nil {
		if len(*req.Note) > 2000 {
			return false, "заметка до 2000 символов"
		}
		sl.Note = *req.Note
	}
	if req.ParticipantID != nil {
		if *req.ParticipantID <= 0 {
			sl.ParticipantID = nil
		} else {
			sl.ParticipantID = req.ParticipantID
		}
	}
	if req.Items != nil {
		items, msg := validateFoodPlanItems(*req.Items)
		if msg != "" {
			return false, msg
		}
		sl.Items = items
		return true, ""
	}
	return false, ""
}

// POST /food/plans/{id}/slots
func (h *foodPlanHandlers) createSlot(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := foodPlanID(r)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	plan, err := h.store.GetFoodPlan(r.Context(), user.ID, id)
	if err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	var req foodPlanSlotReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid json")
		return
	}
	sl := store.FoodPlanSlot{MealType: "none", Items: []store.FoodPlanItem{}}
	if _, msg := applyFoodPlanSlotReq(&sl, req, plan.Days); msg != "" {
		badRequest(w, msg)
		return
	}
	created, err := h.store.CreateFoodPlanSlot(r.Context(), user.ID, id, sl)
	if err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"slot": created})
}

// PUT /food/plans/{id}/slots/{sid} — items заменяются, если присланы.
func (h *foodPlanHandlers) updateSlot(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := foodPlanID(r)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	sid, err := strconv.ParseInt(r.PathValue("sid"), 10, 64)
	if err != nil {
		badRequest(w, "invalid slot id")
		return
	}
	plan, err := h.store.GetFoodPlan(r.Context(), user.ID, id)
	if err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	var cur *store.FoodPlanSlot
	for i := range plan.Slots {
		if plan.Slots[i].ID == sid {
			cur = &plan.Slots[i]
		}
	}
	if cur == nil {
		writeError(w, http.StatusNotFound, "not_found", "слот не найден")
		return
	}
	var req foodPlanSlotReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid json")
		return
	}
	replaceItems, msg := applyFoodPlanSlotReq(cur, req, plan.Days)
	if msg != "" {
		badRequest(w, msg)
		return
	}
	updated, err := h.store.UpdateFoodPlanSlot(r.Context(), user.ID, id, sid, *cur, replaceItems)
	if err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"slot": updated})
}

// DELETE /food/plans/{id}/slots/{sid}
func (h *foodPlanHandlers) deleteSlot(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := foodPlanID(r)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	sid, err := strconv.ParseInt(r.PathValue("sid"), 10, 64)
	if err != nil {
		badRequest(w, "invalid slot id")
		return
	}
	if err := h.store.DeleteFoodPlanSlot(r.Context(), user.ID, id, sid); err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- применение в дневник ---

// POST /food/plans/{id}/apply {day_index, date, days, participant_id, mode}
// Записи создаются ТОЛЬКО в дневнике вызывающего — в чужой дневник план
// не пишет никогда. mode: "" — проверка (existing > 0 → ничего не создаём),
// add — добавить поверх, replace — заменить прежние записи из этого плана.
func (h *foodPlanHandlers) apply(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := foodPlanID(r)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	var req struct {
		DayIndex      int    `json:"day_index"`
		Date          string `json:"date"`
		Days          int    `json:"days"`
		ParticipantID *int64 `json:"participant_id"`
		Mode          string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid json")
		return
	}
	if !foodValidDate(req.Date) {
		badRequest(w, "date=YYYY-MM-DD is required")
		return
	}
	if req.Days <= 0 {
		req.Days = 1
	}
	if req.DayIndex < 0 || req.DayIndex >= store.FoodPlanMaxDays ||
		req.Days > store.FoodPlanMaxDays || req.DayIndex+req.Days > store.FoodPlanMaxDays {
		badRequest(w, "дни вне диапазона плана")
		return
	}
	switch req.Mode {
	case "", "add", "replace":
	default:
		badRequest(w, "invalid mode")
		return
	}
	if req.ParticipantID != nil && *req.ParticipantID <= 0 {
		req.ParticipantID = nil
	}
	res, err := h.store.ApplyFoodPlanDays(r.Context(), user.ID, id,
		req.DayIndex, req.Days, req.Date, req.ParticipantID, req.Mode)
	if err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": res})
}

// GET /food/plans/today?date= — что запланировано на дату (подсказка в Дневнике).
func (h *foodPlanHandlers) today(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	date := r.URL.Query().Get("date")
	if !foodValidDate(date) {
		badRequest(w, "date=YYYY-MM-DD is required")
		return
	}
	items, err := h.store.FoodPlansForDate(r.Context(), user.ID, date)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// --- шаринг ---

// POST /food/plans/{id}/share {to} — доступ через контакт (deliverShare).
func (h *foodPlanHandlers) share(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := foodPlanID(r)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	var req struct {
		To string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.To) == "" {
		badRequest(w, "to (user id or @username) is required")
		return
	}
	recipient, err := h.store.FindUserExact(r.Context(), strings.TrimSpace(req.To))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	if recipient.ID == user.ID {
		badRequest(w, "cannot share with yourself")
		return
	}
	queued, _, err := deliverShare(r.Context(), h.store, h.bot, user, recipient.ID, "food_plan", id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "план не найден")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shared_with": recipient, "queued": queued})
}

// GET /food/plans/{id}/shares
func (h *foodPlanHandlers) listShares(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := foodPlanID(r)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	users, err := h.store.ListFoodPlanShares(r.Context(), user.ID, id)
	if err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// PATCH /food/plans/{id}/shares/{userId} {can_edit}
func (h *foodPlanHandlers) updateShare(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := foodPlanID(r)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	targetID, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil {
		badRequest(w, "invalid user id")
		return
	}
	var req struct {
		CanEdit bool `json:"can_edit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid json")
		return
	}
	if err := h.store.UpdateFoodPlanShare(r.Context(), user.ID, id, targetID, req.CanEdit); err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"can_edit": req.CanEdit})
}

// DELETE /food/plans/{id}/shares/{userId}
func (h *foodPlanHandlers) revokeShare(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := foodPlanID(r)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	targetID, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil {
		badRequest(w, "invalid user id")
		return
	}
	if err := h.store.RevokeFoodPlanShare(r.Context(), user.ID, id, targetID); err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /food/plans/{id}/leave — убрать у себя чужой план.
func (h *foodPlanHandlers) leave(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := foodPlanID(r)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	if err := h.store.LeaveFoodPlan(r.Context(), user.ID, id); err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /food/plans/{id}/share-link — ссылка-приглашение (приём = копия плана).
func (h *foodPlanHandlers) shareLink(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := foodPlanID(r)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	token, err := h.store.EnsureFoodPlanShareToken(r.Context(), user.ID, id)
	if err != nil {
		writeFoodPlanErr(w, err)
		return
	}
	link := ""
	if username := h.bot.Username(r.Context()); username != "" {
		link = fmt.Sprintf("https://t.me/%s?startapp=fpl_%s", username, token)
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token, "link": link})
}

// POST /food/plans/redeem {token} — принять план по ссылке (независимая копия).
func (h *foodPlanHandlers) redeem(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid json")
		return
	}
	req.Token = strings.TrimPrefix(strings.TrimSpace(req.Token), "fpl_")
	if !foodPlanTokenRe.MatchString(req.Token) {
		badRequest(w, "invalid token")
		return
	}
	name, err := h.store.RedeemFoodPlanShareToken(r.Context(), user.ID, req.Token)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "invite not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plan": map[string]string{"name": name}})
}
