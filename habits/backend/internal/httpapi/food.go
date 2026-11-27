package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

// Food: дневник питания — профиль/цели, продукты, приёмы пищи (снимки КБЖУ),
// шаблоны, рецепты, шаринг дневника (read-only), статистика.
type foodHandlers struct {
	store   *store.Store
	bot     *notify.Bot
	dataDir string
}

var foodTimeRe = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

var foodMealTypes = map[string]bool{"breakfast": true, "lunch": true, "dinner": true, "snack": true, "none": true}
var foodUnits = map[string]bool{"g": true, "ml": true, "piece": true, "portion": true}

func foodValidDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// --- профиль и цели ---

func (h *foodHandlers) validateProfile(p store.FoodProfile) string {
	if p.Sex != "" && p.Sex != "male" && p.Sex != "female" {
		return "sex must be male or female"
	}
	if p.BirthDate != "" && !foodValidDate(p.BirthDate) {
		return "invalid birth_date"
	}
	if p.HeightCm < 0 || p.HeightCm > 300 {
		return "height_cm out of range"
	}
	if p.WeightKg < 0 || p.WeightKg > 500 || p.TargetWeight < 0 || p.TargetWeight > 500 {
		return "weight out of range"
	}
	if p.BodyFat < 0 || p.BodyFat > 70 {
		return "body_fat_percent out of range (0–70)"
	}
	if _, ok := store.FoodActivityCoef[p.ActivityLevel]; !ok {
		return "invalid activity_level"
	}
	if p.GoalType != "lose" && p.GoalType != "maintain" && p.GoalType != "gain" {
		return "invalid goal_type"
	}
	if p.RateKcal < 0 || p.RateKcal > 1500 {
		return "rate_kcal out of range (0–1500)"
	}
	switch p.ProteinBase {
	case "current", "target", "lean", "manual":
	default:
		return "invalid protein_base"
	}
	if p.ProteinBaseKg < 0 || p.ProteinBaseKg > 500 {
		return "protein_base_kg out of range"
	}
	if p.ProteinCoef < 0.5 || p.ProteinCoef > 4 {
		return "protein_coef out of range (0.5–4)"
	}
	return ""
}

// GET /food/profile — профиль + действующая цель (profile: null → onboarding).
func (h *foodHandlers) getProfile(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	p, err := h.store.GetFoodProfile(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	day := r.URL.Query().Get("date")
	if !foodValidDate(day) {
		day = time.Now().UTC().Format("2006-01-02")
	}
	goal, err := h.store.FoodGoalForDate(r.Context(), user.ID, day)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": p, "goal": goal})
}

// PUT /food/profile
func (h *foodHandlers) putProfile(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var p store.FoodProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		badRequest(w, "invalid json")
		return
	}
	if msg := h.validateProfile(p); msg != "" {
		badRequest(w, msg)
		return
	}
	p.UserID = user.ID
	if err := h.store.SaveFoodProfile(r.Context(), p); err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": p})
}

// POST /food/profile/calculate — расчёт целей по переданному профилю (без сохранения).
func (h *foodHandlers) calculate(w http.ResponseWriter, r *http.Request) {
	var p store.FoodProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		badRequest(w, "invalid json")
		return
	}
	if msg := h.validateProfile(p); msg != "" {
		badRequest(w, msg)
		return
	}
	targets, err := store.FoodCalcTargets(p, time.Now().UTC())
	if err != nil {
		badRequest(w, "заполните пол, дату рождения, рост и вес для расчёта")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"targets": targets})
}

// GET /food/goals — история периодов целей.
func (h *foodHandlers) listGoals(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	goals, err := h.store.ListFoodGoals(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if goals == nil {
		goals = []store.FoodGoal{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"goals": goals})
}

// POST /food/goals — новый период (прошлый закрывается днём раньше).
func (h *foodHandlers) createGoal(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var g store.FoodGoal
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		badRequest(w, "invalid json")
		return
	}
	if g.DateFrom == "" {
		g.DateFrom = time.Now().UTC().Format("2006-01-02")
	}
	if !foodValidDate(g.DateFrom) {
		badRequest(w, "invalid date_from")
		return
	}
	if g.GoalType != "lose" && g.GoalType != "maintain" && g.GoalType != "gain" {
		g.GoalType = "maintain"
	}
	if g.Source != "auto" {
		g.Source = "manual"
	}
	for _, v := range []float64{g.Calories, g.Protein, g.Fat, g.Carbs} {
		if v < 0 || v > 100000 {
			badRequest(w, "значения цели вне допустимого диапазона")
			return
		}
	}
	if len(g.Details) > 2000 {
		g.Details = g.Details[:2000]
	}
	created, err := h.store.CreateFoodGoal(r.Context(), user.ID, g)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"goal": created})
}

// --- продукты ---

func (h *foodHandlers) validateProduct(p store.FoodProduct) string {
	if l := len(strings.TrimSpace(p.Name)); l == 0 || l > 200 {
		return "название продукта обязательно (до 200 символов)"
	}
	if len(p.AltName) > 200 || len(p.Brand) > 200 || len(p.Category) > 100 {
		return "слишком длинные поля"
	}
	if p.BaseType != "g" && p.BaseType != "ml" {
		return "base_type must be g or ml"
	}
	if p.Calories < 0 || p.Calories > 10000 || p.Protein < 0 || p.Protein > 1000 ||
		p.Fat < 0 || p.Fat > 1000 || p.Carbs < 0 || p.Carbs > 1000 {
		return "КБЖУ вне допустимого диапазона"
	}
	if p.PieceGrams < 0 || p.PieceGrams > 100000 || p.PortionGrams < 0 || p.PortionGrams > 100000 {
		return "вес штуки/порции вне диапазона"
	}
	if p.Photo != "" && !strings.HasPrefix(p.Photo, "uploads/food/") {
		return "invalid photo url"
	}
	return ""
}

// GET /food/products?q=&archived=&limit=&offset=
func (h *foodHandlers) listProducts(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	products, err := h.store.ListFoodProducts(r.Context(), user.ID, q.Get("q"), q.Get("archived") == "true", limit, offset)
	if err != nil {
		internalError(w)
		return
	}
	if products == nil {
		products = []store.FoodProduct{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"products": products})
}

func (h *foodHandlers) createProduct(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var p store.FoodProduct
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		badRequest(w, "invalid json")
		return
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.BaseType == "" {
		p.BaseType = "g"
	}
	if msg := h.validateProduct(p); msg != "" {
		badRequest(w, msg)
		return
	}
	created, err := h.store.CreateFoodProduct(r.Context(), user.ID, p)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"product": created})
}

func (h *foodHandlers) getProduct(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	p, err := h.store.GetFoodProduct(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "product not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"product": p})
}

func (h *foodHandlers) updateProduct(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	existing, err := h.store.GetFoodProduct(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "product not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	// частичное обновление: применяем только присланные поля
	var raw map[string]json.RawMessage
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if json.Unmarshal(body, &raw) != nil {
		badRequest(w, "invalid json")
		return
	}
	p := *existing
	if json.Unmarshal(body, &p) != nil {
		badRequest(w, "invalid json")
		return
	}
	p.Name = strings.TrimSpace(p.Name)
	if msg := h.validateProduct(p); msg != "" {
		badRequest(w, msg)
		return
	}
	updated, err := h.store.UpdateFoodProduct(r.Context(), user.ID, id, p)
	if err != nil {
		internalError(w)
		return
	}
	if existing.Photo != "" && existing.Photo != updated.Photo {
		h.cleanupFoodPhoto(existing.Photo)
	}
	writeJSON(w, http.StatusOK, map[string]any{"product": updated})
}

// DELETE /food/products/{id} — архив, если использовался; иначе удаление.
func (h *foodHandlers) deleteProduct(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	archived, photo, err := h.store.DeleteFoodProduct(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "product not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	if photo != "" {
		h.cleanupFoodPhoto(photo)
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "archived": archived})
}

// --- элементы (общая валидация снимков) ---

type foodItemsReq struct {
	Items []store.FoodItem `json:"items"`
}

func validateFoodItems(items []store.FoodItem) ([]store.FoodItem, []int64, string) {
	if len(items) > 100 {
		return nil, nil, "слишком много элементов (максимум 100)"
	}
	var productIDs []int64
	for i := range items {
		it := &items[i]
		it.Name = strings.TrimSpace(it.Name)
		if it.Name == "" || len(it.Name) > 200 {
			return nil, nil, "у каждого элемента должно быть название (до 200 символов)"
		}
		if !foodUnits[it.Unit] {
			return nil, nil, "invalid unit"
		}
		if it.BaseType != "ml" {
			it.BaseType = "g"
		}
		if it.Amount <= 0 || it.Amount > 1000000 {
			return nil, nil, "количество должно быть больше нуля"
		}
		if it.Grams <= 0 || it.Grams > 1000000 {
			return nil, nil, "укажите вес/объём элемента (граммы или мл)"
		}
		if it.CaloriesPer < 0 || it.CaloriesPer > 10000 || it.ProteinPer < 0 || it.ProteinPer > 1000 ||
			it.FatPer < 0 || it.FatPer > 1000 || it.CarbsPer < 0 || it.CarbsPer > 1000 {
			return nil, nil, "КБЖУ на 100 г вне допустимого диапазона"
		}
		if it.ProductID != nil && *it.ProductID > 0 {
			productIDs = append(productIDs, *it.ProductID)
		}
	}
	return items, productIDs, ""
}

// --- дневник ---

type foodMealReq struct {
	Day         *string           `json:"day"`
	AtTime      *string           `json:"time"`
	MealType    *string           `json:"meal_type"`
	Name        *string           `json:"name"`
	Description *string           `json:"description"`
	Photo       *string           `json:"photo"`
	Items       *[]store.FoodItem `json:"items"`
	Calories    *float64          `json:"calories"`
	Protein     *float64          `json:"protein"`
	Fat         *float64          `json:"fat"`
	Carbs       *float64          `json:"carbs"`
}

// применяет req к m; возвращает (replaceItems, productIDs, errMsg)
func applyFoodMealReq(m *store.FoodMeal, req foodMealReq) (bool, []int64, string) {
	if req.Day != nil {
		if !foodValidDate(*req.Day) {
			return false, nil, "invalid day"
		}
		m.Day = *req.Day
	}
	if req.AtTime != nil {
		if *req.AtTime != "" && !foodTimeRe.MatchString(*req.AtTime) {
			return false, nil, "время должно быть в формате ЧЧ:ММ"
		}
		m.AtTime = *req.AtTime
	}
	if req.MealType != nil {
		if !foodMealTypes[*req.MealType] {
			return false, nil, "invalid meal_type"
		}
		m.MealType = *req.MealType
	}
	if req.Name != nil {
		m.Name = strings.TrimSpace(*req.Name)
		if len(m.Name) > 200 {
			return false, nil, "название до 200 символов"
		}
	}
	if req.Description != nil {
		if len(*req.Description) > 2000 {
			return false, nil, "описание до 2000 символов"
		}
		m.Description = *req.Description
	}
	if req.Photo != nil {
		if *req.Photo != "" && !strings.HasPrefix(*req.Photo, "uploads/food/") {
			return false, nil, "invalid photo url"
		}
		m.Photo = *req.Photo
	}
	var productIDs []int64
	replaceItems := false
	if req.Items != nil {
		items, ids, msg := validateFoodItems(*req.Items)
		if msg != "" {
			return false, nil, msg
		}
		m.Items = items
		productIDs = ids
		replaceItems = true
	}
	// ручные итоги — применяются только когда элементов нет
	if len(m.Items) == 0 {
		for _, pair := range []struct {
			src *float64
			dst *float64
		}{{req.Calories, &m.Calories}, {req.Protein, &m.Protein}, {req.Fat, &m.Fat}, {req.Carbs, &m.Carbs}} {
			if pair.src != nil {
				if *pair.src < 0 || *pair.src > 100000 {
					return false, nil, "значения КБЖУ вне диапазона"
				}
				*pair.dst = *pair.src
			}
		}
	}
	return replaceItems, productIDs, ""
}

// POST /food/meals
func (h *foodHandlers) createMeal(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req foodMealReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid json")
		return
	}
	m := store.FoodMeal{MealType: "none", Items: []store.FoodItem{}}
	_, productIDs, msg := applyFoodMealReq(&m, req)
	if msg != "" {
		badRequest(w, msg)
		return
	}
	if m.Day == "" {
		badRequest(w, "day is required")
		return
	}
	if len(m.Items) == 0 && m.Calories == 0 && m.Name == "" {
		badRequest(w, "добавьте хотя бы один элемент или укажите название и калории вручную")
		return
	}
	created, err := h.store.CreateFoodMeal(r.Context(), user.ID, m)
	if err != nil {
		internalError(w)
		return
	}
	h.store.TouchFoodProducts(r.Context(), user.ID, productIDs)
	writeJSON(w, http.StatusCreated, map[string]any{"meal": created})
}

// GET /food/meals/{id}
func (h *foodHandlers) getMeal(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	m, err := h.store.GetFoodMeal(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "meal not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"meal": m})
}

// PUT /food/meals/{id} — частичное обновление; items заменяются, если присланы.
func (h *foodHandlers) updateMeal(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	existing, err := h.store.GetFoodMeal(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "meal not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	var req foodMealReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid json")
		return
	}
	oldPhoto := existing.Photo
	m := *existing
	replaceItems, productIDs, msg := applyFoodMealReq(&m, req)
	if msg != "" {
		badRequest(w, msg)
		return
	}
	updated, err := h.store.UpdateFoodMeal(r.Context(), user.ID, id, m, replaceItems)
	if err != nil {
		internalError(w)
		return
	}
	h.store.TouchFoodProducts(r.Context(), user.ID, productIDs)
	if oldPhoto != "" && oldPhoto != updated.Photo {
		h.cleanupFoodPhoto(oldPhoto)
	}
	writeJSON(w, http.StatusOK, map[string]any{"meal": updated})
}

// DELETE /food/meals/{id}
func (h *foodHandlers) deleteMeal(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	photo, err := h.store.DeleteFoodMeal(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "meal not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	if photo != "" {
		h.cleanupFoodPhoto(photo)
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /food/meals/{id}/duplicate {day?}
func (h *foodHandlers) duplicateMeal(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	var req struct {
		Day string `json:"day"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Day != "" && !foodValidDate(req.Day) {
		badRequest(w, "invalid day")
		return
	}
	src, err := h.store.GetFoodMeal(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "meal not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	day := req.Day
	if day == "" {
		day = src.Day
	}
	created, err := h.store.DuplicateFoodMeal(r.Context(), user.ID, id, day)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"meal": created})
}

// POST /food/meals/{id}/save-as-template {name?}
func (h *foodHandlers) mealToTemplate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	tpl, err := h.store.SaveMealAsTemplate(r.Context(), user.ID, id, strings.TrimSpace(req.Name))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "meal not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"template": tpl})
}

// foodDiaryPayload — дневник за день: записи, сводка, цель.
func (h *foodHandlers) foodDiaryPayload(ctx context.Context, ownerID int64, day string) (map[string]any, error) {
	meals, err := h.store.FoodDiaryMeals(ctx, ownerID, day)
	if err != nil {
		return nil, err
	}
	if meals == nil {
		meals = []store.FoodMeal{}
	}
	var c, p, f, cb float64
	for _, m := range meals {
		c += m.Calories
		p += m.Protein
		f += m.Fat
		cb += m.Carbs
	}
	goal, err := h.store.FoodGoalForDate(ctx, ownerID, day)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"date":  day,
		"meals": meals,
		"summary": map[string]float64{
			"calories": c, "protein": p, "fat": f, "carbs": cb,
		},
		"goal": goal,
	}, nil
}

// GET /food/diary?date=
func (h *foodHandlers) diary(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	day := r.URL.Query().Get("date")
	if !foodValidDate(day) {
		badRequest(w, "date=YYYY-MM-DD is required")
		return
	}
	payload, err := h.foodDiaryPayload(r.Context(), user.ID, day)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

// --- шаблоны ---

type foodTemplateReq struct {
	Name        *string           `json:"name"`
	Description *string           `json:"description"`
	Photo       *string           `json:"photo"`
	MealType    *string           `json:"meal_type"`
	Archived    *bool             `json:"archived"`
	Items       *[]store.FoodItem `json:"items"`
}

func applyFoodTemplateReq(t *store.FoodTemplate, req foodTemplateReq) (bool, string) {
	if req.Name != nil {
		t.Name = strings.TrimSpace(*req.Name)
	}
	if t.Name == "" || len(t.Name) > 200 {
		return false, "название обязательно (до 200 символов)"
	}
	if req.Description != nil {
		if len(*req.Description) > 2000 {
			return false, "описание до 2000 символов"
		}
		t.Description = *req.Description
	}
	if req.Photo != nil {
		if *req.Photo != "" && !strings.HasPrefix(*req.Photo, "uploads/food/") {
			return false, "invalid photo url"
		}
		t.Photo = *req.Photo
	}
	if req.MealType != nil {
		if !foodMealTypes[*req.MealType] {
			return false, "invalid meal_type"
		}
		t.MealType = *req.MealType
	}
	if req.Archived != nil {
		t.Archived = *req.Archived
	}
	replace := false
	if req.Items != nil {
		items, _, msg := validateFoodItems(*req.Items)
		if msg != "" {
			return false, msg
		}
		t.Items = items
		replace = true
	}
	return replace, ""
}

func (h *foodHandlers) listTemplates(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	templates, err := h.store.ListFoodTemplates(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if templates == nil {
		templates = []store.FoodTemplate{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": templates})
}

func (h *foodHandlers) createTemplate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req foodTemplateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid json")
		return
	}
	t := store.FoodTemplate{MealType: "none", Items: []store.FoodItem{}}
	if _, msg := applyFoodTemplateReq(&t, req); msg != "" {
		badRequest(w, msg)
		return
	}
	created, err := h.store.CreateFoodTemplate(r.Context(), user.ID, t)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"template": created})
}

func (h *foodHandlers) updateTemplate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	templates, err := h.store.ListFoodTemplates(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	var existing *store.FoodTemplate
	for i := range templates {
		if templates[i].ID == id {
			existing = &templates[i]
			break
		}
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "not_found", "template not found")
		return
	}
	var req foodTemplateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid json")
		return
	}
	oldPhoto := existing.Photo
	t := *existing
	replace, msg := applyFoodTemplateReq(&t, req)
	if msg != "" {
		badRequest(w, msg)
		return
	}
	updated, err := h.store.UpdateFoodTemplate(r.Context(), user.ID, id, t, replace)
	if err != nil {
		internalError(w)
		return
	}
	if oldPhoto != "" && oldPhoto != updated.Photo {
		h.cleanupFoodPhoto(oldPhoto)
	}
	writeJSON(w, http.StatusOK, map[string]any{"template": updated})
}

func (h *foodHandlers) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	photo, err := h.store.DeleteFoodTemplate(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "template not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	if photo != "" {
		h.cleanupFoodPhoto(photo)
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /food/templates/{id}/create-meal {day, time?, meal_type?}
func (h *foodHandlers) templateToMeal(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	var req struct {
		Day      string `json:"day"`
		AtTime   string `json:"time"`
		MealType string `json:"meal_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !foodValidDate(req.Day) {
		badRequest(w, "day=YYYY-MM-DD is required")
		return
	}
	if req.AtTime != "" && !foodTimeRe.MatchString(req.AtTime) {
		badRequest(w, "время должно быть в формате ЧЧ:ММ")
		return
	}
	if req.MealType != "" && !foodMealTypes[req.MealType] {
		badRequest(w, "invalid meal_type")
		return
	}
	meal, err := h.store.CreateMealFromTemplate(r.Context(), user.ID, id, req.Day, req.AtTime, req.MealType)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "template not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"meal": meal})
}

// --- рецепты ---

type foodRecipeReq struct {
	Name        *string           `json:"name"`
	Description *string           `json:"description"`
	Steps       *string           `json:"steps"`
	Photo       *string           `json:"photo"`
	FinalWeight *float64          `json:"final_weight"`
	Portions    *float64          `json:"portions"`
	Archived    *bool             `json:"archived"`
	Items       *[]store.FoodItem `json:"items"`
}

func applyFoodRecipeReq(rec *store.FoodRecipe, req foodRecipeReq) (bool, string) {
	if req.Name != nil {
		rec.Name = strings.TrimSpace(*req.Name)
	}
	if rec.Name == "" || len(rec.Name) > 200 {
		return false, "название рецепта обязательно (до 200 символов)"
	}
	if req.Description != nil {
		if len(*req.Description) > 2000 {
			return false, "описание до 2000 символов"
		}
		rec.Description = *req.Description
	}
	if req.Steps != nil {
		if len(*req.Steps) > 10000 {
			return false, "шаги приготовления до 10000 символов"
		}
		rec.Steps = *req.Steps
	}
	if req.Photo != nil {
		if *req.Photo != "" && !strings.HasPrefix(*req.Photo, "uploads/food/") {
			return false, "invalid photo url"
		}
		rec.Photo = *req.Photo
	}
	if req.FinalWeight != nil {
		if *req.FinalWeight < 0 || *req.FinalWeight > 1000000 {
			return false, "итоговый вес вне диапазона"
		}
		rec.FinalWeight = *req.FinalWeight
	}
	if req.Portions != nil {
		if *req.Portions < 0 || *req.Portions > 1000 {
			return false, "количество порций вне диапазона"
		}
		rec.Portions = *req.Portions
	}
	if req.Archived != nil {
		rec.Archived = *req.Archived
	}
	replace := false
	if req.Items != nil {
		items, _, msg := validateFoodItems(*req.Items)
		if msg != "" {
			return false, msg
		}
		rec.Items = items
		replace = true
	}
	return replace, ""
}

// foodRecipeOut — рецепт + рассчитанные КБЖУ на 100 г и на порцию.
func foodRecipeOut(rec *store.FoodRecipe) map[string]any {
	out := map[string]any{"recipe": rec}
	if rec.FinalWeight > 0 {
		out["per100"] = map[string]float64{
			"calories": store.FoodPer100(rec.Calories, rec.FinalWeight),
			"protein":  store.FoodPer100(rec.Protein, rec.FinalWeight),
			"fat":      store.FoodPer100(rec.Fat, rec.FinalWeight),
			"carbs":    store.FoodPer100(rec.Carbs, rec.FinalWeight),
		}
	}
	if rec.Portions > 0 {
		out["per_portion"] = map[string]float64{
			"calories": rec.Calories / rec.Portions,
			"protein":  rec.Protein / rec.Portions,
			"fat":      rec.Fat / rec.Portions,
			"carbs":    rec.Carbs / rec.Portions,
		}
	}
	return out
}

func (h *foodHandlers) listRecipes(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	recipes, err := h.store.ListFoodRecipes(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if recipes == nil {
		recipes = []store.FoodRecipe{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"recipes": recipes})
}

func (h *foodHandlers) createRecipe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req foodRecipeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid json")
		return
	}
	rec := store.FoodRecipe{Items: []store.FoodItem{}}
	if _, msg := applyFoodRecipeReq(&rec, req); msg != "" {
		badRequest(w, msg)
		return
	}
	created, err := h.store.CreateFoodRecipe(r.Context(), user.ID, rec)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, foodRecipeOut(created))
}

func (h *foodHandlers) updateRecipe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	recipes, err := h.store.ListFoodRecipes(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	var existing *store.FoodRecipe
	for i := range recipes {
		if recipes[i].ID == id {
			existing = &recipes[i]
			break
		}
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "not_found", "recipe not found")
		return
	}
	var req foodRecipeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid json")
		return
	}
	oldPhoto := existing.Photo
	rec := *existing
	replace, msg := applyFoodRecipeReq(&rec, req)
	if msg != "" {
		badRequest(w, msg)
		return
	}
	updated, err := h.store.UpdateFoodRecipe(r.Context(), user.ID, id, rec, replace)
	if err != nil {
		internalError(w)
		return
	}
	if oldPhoto != "" && oldPhoto != updated.Photo {
		h.cleanupFoodPhoto(oldPhoto)
	}
	writeJSON(w, http.StatusOK, foodRecipeOut(updated))
}

func (h *foodHandlers) deleteRecipe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	photo, err := h.store.DeleteFoodRecipe(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "recipe not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	if photo != "" {
		h.cleanupFoodPhoto(photo)
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /food/recipes/{id}/create-meal {day, grams? | portions?, time?, meal_type?}
func (h *foodHandlers) recipeToMeal(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	var req struct {
		Day      string  `json:"day"`
		AtTime   string  `json:"time"`
		MealType string  `json:"meal_type"`
		Grams    float64 `json:"grams"`
		Portions float64 `json:"portions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !foodValidDate(req.Day) {
		badRequest(w, "day=YYYY-MM-DD is required")
		return
	}
	if req.AtTime != "" && !foodTimeRe.MatchString(req.AtTime) {
		badRequest(w, "время должно быть в формате ЧЧ:ММ")
		return
	}
	if req.MealType != "" && !foodMealTypes[req.MealType] {
		badRequest(w, "invalid meal_type")
		return
	}
	if req.Grams < 0 || req.Grams > 1000000 || req.Portions < 0 || req.Portions > 1000 {
		badRequest(w, "grams/portions out of range")
		return
	}
	meal, err := h.store.CreateMealFromRecipe(r.Context(), user.ID, id,
		req.Day, req.AtTime, req.MealType, req.Grams, req.Portions)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "recipe not found")
		return
	}
	if err != nil {
		badRequest(w, "укажите съеденный вес (нужен итоговый вес блюда в рецепте) или число порций")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"meal": meal})
}

// --- шаринг дневника ---

// POST /food/shares {to}
func (h *foodHandlers) createShare(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
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
	queued, _, err := deliverShare(r.Context(), h.store, h.bot, user, recipient.ID, "food", user.ID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shared_with": recipient, "queued": queued})
}

// GET /food/shares — кому доступен мой дневник.
func (h *foodHandlers) listShares(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	users, err := h.store.ListFoodShares(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if users == nil {
		users = []store.FoodShareUser{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// PATCH /food/shares/{userId} — флаги видимости.
func (h *foodHandlers) updateShare(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	targetID, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil {
		badRequest(w, "invalid user id")
		return
	}
	var f store.FoodShareFlags
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		badRequest(w, "invalid json")
		return
	}
	switch err := h.store.UpdateFoodShare(r.Context(), user.ID, targetID, f); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "share not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"flags": f})
	}
}

// DELETE /food/shares/{userId} — отзыв владельцем.
func (h *foodHandlers) revokeShare(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	targetID, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil {
		badRequest(w, "invalid user id")
		return
	}
	switch err := h.store.RevokeFoodShare(r.Context(), user.ID, targetID); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "share not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// GET /food/shared — чьи дневники доступны мне.
func (h *foodHandlers) listSharedWithMe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	owners, err := h.store.ListFoodSharedWithMe(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if owners == nil {
		owners = []store.FoodShareUser{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"owners": owners})
}

// DELETE /food/shared/{ownerId} — убрать у себя чужой дневник.
func (h *foodHandlers) leaveShared(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	ownerID, err := strconv.ParseInt(r.PathValue("ownerId"), 10, 64)
	if err != nil {
		badRequest(w, "invalid user id")
		return
	}
	switch err := h.store.RevokeFoodShare(r.Context(), ownerID, user.ID); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "share not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// GET /food/shared/{ownerId}/diary?date= — чужой дневник с учётом флагов.
func (h *foodHandlers) sharedDiary(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	ownerID, err := strconv.ParseInt(r.PathValue("ownerId"), 10, 64)
	if err != nil {
		badRequest(w, "invalid user id")
		return
	}
	flags, err := h.store.GetFoodShareFlags(r.Context(), ownerID, user.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "diary not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	day := r.URL.Query().Get("date")
	if !foodValidDate(day) {
		badRequest(w, "date=YYYY-MM-DD is required")
		return
	}
	payload, err := h.foodDiaryPayload(r.Context(), ownerID, day)
	if err != nil {
		internalError(w)
		return
	}
	meals := payload["meals"].([]store.FoodMeal)
	for i := range meals {
		if !flags.ShowPhotos {
			meals[i].Photo = ""
		}
		if !flags.ShowNotes {
			meals[i].Description = ""
		}
	}
	if !flags.ShowGoals {
		payload["goal"] = nil
	}
	if flags.ShowWeight {
		if p, err := h.store.GetFoodProfile(r.Context(), ownerID); err == nil && p != nil && p.WeightKg > 0 {
			payload["weight_kg"] = p.WeightKg
		}
	}
	writeJSON(w, http.StatusOK, payload)
}

// --- статистика ---

// GET /food/stats?from=&to= — дневные агрегаты + периоды целей.
func (h *foodHandlers) stats(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if !foodValidDate(from) || !foodValidDate(to) || from > to {
		badRequest(w, "from/to=YYYY-MM-DD are required")
		return
	}
	f, _ := time.Parse("2006-01-02", from)
	t, _ := time.Parse("2006-01-02", to)
	if t.Sub(f) > 400*24*time.Hour {
		badRequest(w, "период не больше 400 дней")
		return
	}
	days, err := h.store.FoodStats(r.Context(), user.ID, from, to)
	if err != nil {
		internalError(w)
		return
	}
	if days == nil {
		days = []store.FoodDayStat{}
	}
	goals, err := h.store.FoodGoalsInRange(r.Context(), user.ID, from, to)
	if err != nil {
		internalError(w)
		return
	}
	if goals == nil {
		goals = []store.FoodGoal{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": days, "goals": goals})
}

// GET /food/metrics/{key}?from=&to= — временной ряд по внутреннему
// идентификатору метрики (слой для будущей интеграции со страницей Metrics).
func (h *foodHandlers) metricSeries(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	key := r.PathValue("key")
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if !foodValidDate(from) || !foodValidDate(to) || from > to {
		badRequest(w, "from/to=YYYY-MM-DD are required")
		return
	}
	days, err := h.store.FoodStats(r.Context(), user.ID, from, to)
	if err != nil {
		internalError(w)
		return
	}
	goals, err := h.store.FoodGoalsInRange(r.Context(), user.ID, from, to)
	if err != nil {
		internalError(w)
		return
	}
	goalFor := func(day string) *store.FoodGoal {
		for i := range goals {
			g := &goals[i]
			if g.DateFrom <= day && (g.DateTo == "" || g.DateTo >= day) {
				return g
			}
		}
		return nil
	}
	type point struct {
		Day   string  `json:"day"`
		Value float64 `json:"value"`
	}
	points := []point{}
	for _, d := range days {
		var v float64
		ok := true
		switch key {
		case "food.calories.daily":
			v = d.Calories
		case "food.protein.daily":
			v = d.Protein
		case "food.fat.daily":
			v = d.Fat
		case "food.carbs.daily":
			v = d.Carbs
		case "food.meals.count":
			v = float64(d.Meals)
		case "food.goal.calories":
			if g := goalFor(d.Day); g != nil {
				v = g.Calories
			} else {
				ok = false
			}
		case "food.goal.protein":
			if g := goalFor(d.Day); g != nil {
				v = g.Protein
			} else {
				ok = false
			}
		case "food.goal.calories_completion":
			if g := goalFor(d.Day); g != nil && g.Calories > 0 {
				v = d.Calories / g.Calories * 100
			} else {
				ok = false
			}
		case "food.goal.protein_completion":
			if g := goalFor(d.Day); g != nil && g.Protein > 0 {
				v = d.Protein / g.Protein * 100
			} else {
				ok = false
			}
		default:
			writeError(w, http.StatusNotFound, "not_found", "unknown metric key")
			return
		}
		if ok {
			points = append(points, point{Day: d.Day, Value: v})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "points": points})
}

// --- фото ---

// POST /food/upload — только изображения, до 10 МБ.
func (h *foodHandlers) upload(w http.ResponseWriter, r *http.Request) {
	const maxMB = 10
	r.Body = http.MaxBytesReader(w, r.Body, maxMB<<20+(1<<20))
	file, hdr, err := r.FormFile("file")
	if err != nil {
		badRequest(w, fmt.Sprintf("multipart field 'file' is required (max %d MB)", maxMB))
		return
	}
	defer file.Close()
	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		internalError(w)
		return
	}
	var ext string
	switch http.DetectContentType(head[:n]) {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	default:
		badRequest(w, "поддерживаются только изображения (JPEG/PNG/WebP/GIF)")
		return
	}
	buf := make([]byte, 16)
	rand.Read(buf)
	filename := hex.EncodeToString(buf) + ext
	dir := filepath.Join(h.dataDir, "food")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		internalError(w)
		return
	}
	dst, err := os.Create(filepath.Join(dir, filename))
	if err != nil {
		internalError(w)
		return
	}
	defer dst.Close()
	if _, err := dst.Write(head[:n]); err != nil {
		internalError(w)
		return
	}
	if _, err := io.Copy(dst, file); err != nil {
		os.Remove(dst.Name())
		badRequest(w, fmt.Sprintf("файл слишком большой (максимум %d МБ)", maxMB))
		return
	}
	_ = hdr
	writeJSON(w, http.StatusCreated, map[string]any{"url": "uploads/food/" + filename})
}

// cleanupFoodPhoto — файл удаляется физически, только когда на него
// больше никто не ссылается (фото переиспользуются шаблоном и записями).
func (h *foodHandlers) cleanupFoodPhoto(url string) {
	if !strings.HasPrefix(url, "uploads/food/") {
		return
	}
	n, err := h.store.FoodPhotoRefCount(context.Background(), url)
	if err != nil || n > 0 {
		return
	}
	name := filepath.Base(strings.TrimPrefix(url, "uploads/food/"))
	if name == "." || name == "/" || name == "" {
		return
	}
	os.Remove(filepath.Join(h.dataDir, "food", name))
}
