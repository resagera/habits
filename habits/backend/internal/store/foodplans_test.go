package store

import "testing"

func ptr64(v int64) *int64 { return &v }

// exactItem — точная позиция плана (КБЖУ считаются).
func exactItem(name string, grams, per100 float64) FoodPlanItem {
	it := FoodPlanItem{Name: name, Kind: "product", Approx: false, Amount: grams,
		Unit: "g", Grams: grams, BaseType: "g", CaloriesPer: per100}
	normalizeFoodPlanItem(&it)
	return it
}

func approxItem(name string) FoodPlanItem {
	it := FoodPlanItem{Name: name, Kind: "free", Approx: true}
	normalizeFoodPlanItem(&it)
	return it
}

// Приблизительная позиция не даёт КБЖУ, даже если в неё что-то положили:
// иначе сводка врала бы, выдавая неполную сумму за точную.
func TestNormalizeFoodPlanItemApproxHasNoTotals(t *testing.T) {
	it := FoodPlanItem{Name: "суп", Approx: true, Grams: 300, CaloriesPer: 50}
	normalizeFoodPlanItem(&it)
	if it.Calories != 0 || it.Protein != 0 || it.Fat != 0 || it.Carbs != 0 {
		t.Fatalf("approx item got totals: %+v", it)
	}
	it.Approx = false
	normalizeFoodPlanItem(&it)
	if !almost(it.Calories, 150) {
		t.Fatalf("exact item calories = %v, want 150", it.Calories)
	}
}

// Общий слот входит в итог каждого участника со своим коэффициентом порции,
// персональный — только в итог своего участника и без коэффициента.
func TestFoodPlanSummaryParticipants(t *testing.T) {
	parts := []FoodPlanParticipant{
		{ID: 1, Name: "Я", PortionCoef: 1},
		{ID: 2, Name: "Ребёнок", PortionCoef: 0.5},
	}
	slots := []FoodPlanSlot{
		{DayIndex: 0, Items: []FoodPlanItem{exactItem("гречка", 100, 330), approxItem("к чаю")}},
		{DayIndex: 0, ParticipantID: ptr64(2), Items: []FoodPlanItem{exactItem("яблоко", 200, 50)}},
	}
	sum := foodPlanSummary(3, parts, slots)
	if len(sum) != 3 {
		t.Fatalf("summary len = %d, want 3", len(sum))
	}
	day := sum[0]
	if day.Counted != 2 || day.Approx != 1 || day.Slots != 2 {
		t.Fatalf("counted=%d approx=%d slots=%d", day.Counted, day.Approx, day.Slots)
	}
	if len(day.ByParticipant) != 2 {
		t.Fatalf("by_participant len = %d", len(day.ByParticipant))
	}
	if !almost(day.ByParticipant[0].Calories, 330) {
		t.Fatalf("participant 1 = %v, want 330", day.ByParticipant[0].Calories)
	}
	// 330 × 0.5 (общий слот) + 100 (свой слот, без коэффициента)
	if !almost(day.ByParticipant[1].Calories, 265) {
		t.Fatalf("participant 2 = %v, want 265", day.ByParticipant[1].Calories)
	}
	if !almost(day.Calories, 595) {
		t.Fatalf("day total = %v, want 595", day.Calories)
	}
	if sum[1].Calories != 0 || sum[1].Slots != 0 {
		t.Fatalf("empty day not empty: %+v", sum[1])
	}
}

// Без участников считается один общий итог, коэффициенты не применяются.
func TestFoodPlanSummaryNoParticipants(t *testing.T) {
	slots := []FoodPlanSlot{{DayIndex: 1, Items: []FoodPlanItem{exactItem("рис", 150, 130)}}}
	sum := foodPlanSummary(2, nil, slots)
	if !almost(sum[1].Calories, 195) {
		t.Fatalf("day total = %v, want 195", sum[1].Calories)
	}
	if len(sum[1].ByParticipant) != 0 {
		t.Fatalf("by_participant must be empty without participants")
	}
}

// Слот за границей укороченного плана не должен ломать сводку (и не теряется
// в БД — вернув длину, пользователь получит его обратно).
func TestFoodPlanSummaryIgnoresOutOfRangeSlots(t *testing.T) {
	slots := []FoodPlanSlot{
		{DayIndex: 0, Items: []FoodPlanItem{exactItem("овсянка", 100, 350)}},
		{DayIndex: 9, Items: []FoodPlanItem{exactItem("забытое", 100, 900)}},
	}
	sum := foodPlanSummary(2, nil, slots)
	if len(sum) != 2 || !almost(sum[0].Calories, 350) {
		t.Fatalf("summary = %+v", sum)
	}
}

// Применение слота в дневник: точные позиции становятся снимком состава,
// приблизительные уходят в описание, коэффициент масштабирует вес.
func TestFoodMealFromPlanSlot(t *testing.T) {
	slot := FoodPlanSlot{
		DayIndex: 0, MealType: "breakfast", AtTime: "08:30", Title: "Каша", Note: "не солить",
		Items: []FoodPlanItem{exactItem("гречка", 100, 330), approxItem("что-нибудь к чаю")},
	}
	meal, skipped := foodMealFromPlanSlot(slot, "2026-08-03", 7, 0.5, nil)
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
	if meal.Day != "2026-08-03" || meal.MealType != "breakfast" || meal.AtTime != "08:30" {
		t.Fatalf("meal head = %+v", meal)
	}
	if meal.SourceType != "plan" || meal.SourceID == nil || *meal.SourceID != 7 {
		t.Fatalf("meal source = %v %v", meal.SourceType, meal.SourceID)
	}
	if len(meal.Items) != 1 || !almost(meal.Items[0].Grams, 50) || !almost(meal.Items[0].Amount, 50) {
		t.Fatalf("items = %+v", meal.Items)
	}
	if meal.Items[0].CaloriesPer != 330 {
		t.Fatalf("per100 lost: %+v", meal.Items[0])
	}
	if meal.Description != "не солить\nПримерно: что-нибудь к чаю" {
		t.Fatalf("description = %q", meal.Description)
	}
}

// Свежий снимок продукта перебивает кэш плана — дневник должен получить
// актуальные КБЖУ, а не то, что было при составлении плана.
func TestFoodMealFromPlanSlotUsesFreshProduct(t *testing.T) {
	it := exactItem("гречка", 100, 330)
	it.RefID = ptr64(42)
	slot := FoodPlanSlot{DayIndex: 0, Title: "Каша", Items: []FoodPlanItem{it}}
	fresh := map[int64]FoodProduct{42: {ID: 42, BaseType: "g", Calories: 300, Protein: 11}}
	meal, _ := foodMealFromPlanSlot(slot, "2026-08-03", 1, 1, fresh)
	if len(meal.Items) != 1 {
		t.Fatalf("items = %+v", meal.Items)
	}
	got := meal.Items[0]
	if got.ProductID == nil || *got.ProductID != 42 {
		t.Fatalf("product_id = %v", got.ProductID)
	}
	if !almost(got.CaloriesPer, 300) || !almost(got.ProteinPer, 11) {
		t.Fatalf("stale per100: %+v", got)
	}
}

// Слот без названия не должен давать безымянную запись в дневнике.
func TestFoodMealFromPlanSlotNameFallback(t *testing.T) {
	meal, _ := foodMealFromPlanSlot(
		FoodPlanSlot{Items: []FoodPlanItem{exactItem("рис", 100, 130)}}, "2026-08-03", 1, 1, nil)
	if meal.Name != "рис" {
		t.Fatalf("name = %q, want рис", meal.Name)
	}
	meal, _ = foodMealFromPlanSlot(
		FoodPlanSlot{Items: []FoodPlanItem{approxItem("что-то мясное")}}, "2026-08-03", 1, 1, nil)
	if meal.Name != "что-то мясное" {
		t.Fatalf("name = %q", meal.Name)
	}
	meal, _ = foodMealFromPlanSlot(FoodPlanSlot{}, "2026-08-03", 1, 1, nil)
	if meal.Name != "Из плана" {
		t.Fatalf("name = %q", meal.Name)
	}
}
