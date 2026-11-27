package store

import (
	"math"
	"strings"
	"testing"
	"time"
)

func almost(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestFoodBMR(t *testing.T) {
	// Mifflin–St Jeor: мужчина 84 кг, 178 см, 30 лет
	if got := FoodBMR("male", 84, 178, 30); !almost(got, 10*84+6.25*178-5*30+5) {
		t.Fatalf("male BMR = %v", got)
	}
	// женщина 60 кг, 165 см, 25 лет
	if got := FoodBMR("female", 60, 165, 25); !almost(got, 10*60+6.25*165-5*25-161) {
		t.Fatalf("female BMR = %v", got)
	}
}

func TestFoodAge(t *testing.T) {
	now := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		birth string
		want  int
	}{
		{"1990-07-21", 36}, // день рождения сегодня
		{"1990-07-22", 35}, // завтра — ещё не исполнилось
		{"1990-07-20", 36},
		{"2030-01-01", 0}, // не отрицательный
	}
	for _, c := range cases {
		b, _ := time.Parse("2006-01-02", c.birth)
		if got := FoodAge(b, now); got != c.want {
			t.Fatalf("age(%s) = %d, want %d", c.birth, got, c.want)
		}
	}
}

func TestFoodCalcTargets(t *testing.T) {
	p := FoodProfile{
		Sex: "male", BirthDate: "1996-01-15", HeightCm: 178, WeightKg: 84,
		ActivityLevel: "medium", GoalType: "lose", RateKcal: 400,
		ProteinBase: "current", ProteinCoef: 1.6,
	}
	now := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	tg, err := FoodCalcTargets(p, now)
	if err != nil {
		t.Fatal(err)
	}
	bmr := 10*84.0 + 6.25*178 - 5*30 + 5 // 30 лет на дату now
	tdee := bmr * 1.55
	if !almost(tg.BMR, math.Round(bmr)) || !almost(tg.TDEE, math.Round(tdee)) {
		t.Fatalf("BMR/TDEE = %v/%v, want %v/%v", tg.BMR, tg.TDEE, bmr, tdee)
	}
	if !almost(tg.Calories, math.Round(tdee-400)) {
		t.Fatalf("calories = %v", tg.Calories)
	}
	if !almost(tg.Protein, math.Round(84*1.6)) {
		t.Fatalf("protein = %v", tg.Protein)
	}
	if !strings.Contains(tg.Details, "Mifflin") || !strings.Contains(tg.Details, "1.6") {
		t.Fatalf("details missing calculation basis: %s", tg.Details)
	}
	// неполный профиль → ошибка
	if _, err := FoodCalcTargets(FoodProfile{Sex: "male"}, now); err == nil {
		t.Fatal("expected error for incomplete profile")
	}
}

func TestFoodProteinWeight(t *testing.T) {
	p := FoodProfile{WeightKg: 84, TargetWeight: 78, BodyFat: 20, ProteinBaseKg: 70}
	p.ProteinBase = "current"
	if kg, _ := FoodProteinWeight(p); kg != 84 {
		t.Fatalf("current = %v", kg)
	}
	p.ProteinBase = "target"
	if kg, _ := FoodProteinWeight(p); kg != 78 {
		t.Fatalf("target = %v", kg)
	}
	p.ProteinBase = "lean"
	if kg, _ := FoodProteinWeight(p); !almost(kg, 84*0.8) {
		t.Fatalf("lean = %v", kg)
	}
	p.ProteinBase = "manual"
	if kg, _ := FoodProteinWeight(p); kg != 70 {
		t.Fatalf("manual = %v", kg)
	}
	// fallback на текущий вес при незаполненной базе
	p.ProteinBase = "target"
	p.TargetWeight = 0
	if kg, _ := FoodProteinWeight(p); kg != 84 {
		t.Fatalf("fallback = %v", kg)
	}
}

func TestFoodGramsFor(t *testing.T) {
	if g := FoodGramsFor("g", 150, 0, 0); g != 150 {
		t.Fatalf("g = %v", g)
	}
	if g := FoodGramsFor("ml", 200, 0, 0); g != 200 {
		t.Fatalf("ml = %v", g)
	}
	if g := FoodGramsFor("piece", 10, 12, 0); g != 120 { // 10 пельменей × 12 г
		t.Fatalf("piece = %v", g)
	}
	if g := FoodGramsFor("portion", 2, 0, 250); g != 500 {
		t.Fatalf("portion = %v", g)
	}
	// коэффициент не задан → 0 (нужен ручной вес)
	if g := FoodGramsFor("piece", 10, 0, 0); g != 0 {
		t.Fatalf("piece unknown = %v", g)
	}
}

func TestFoodItemTotals(t *testing.T) {
	// пельмени 150 г, 275 ккал / 11.9 Б / 12.4 Ж / 29 У на 100 г
	c, p, f, cb := FoodItemTotals(150, 275, 11.9, 12.4, 29)
	if !almost(c, 412.5) || !almost(p, 17.85) || !almost(f, 18.6) || !almost(cb, 43.5) {
		t.Fatalf("totals = %v %v %v %v", c, p, f, cb)
	}
}

func TestFoodPer100(t *testing.T) {
	if v := FoodPer100(1200, 800); !almost(v, 150) {
		t.Fatalf("per100 = %v", v)
	}
	if v := FoodPer100(1200, 0); v != 0 {
		t.Fatalf("per100 zero weight = %v", v)
	}
}

func TestNormalizeFoodItems(t *testing.T) {
	items := []FoodItem{
		{Grams: 150, CaloriesPer: 275, ProteinPer: 11.9, FatPer: 12.4, CarbsPer: 29},
		{Grams: 20, CaloriesPer: 629, ProteinPer: 3.1, FatPer: 67, CarbsPer: 2.6},
	}
	c, p, f, cb := normalizeFoodItems(items)
	if !almost(c, 412.5+125.8) {
		t.Fatalf("sum calories = %v", c)
	}
	if !almost(items[1].Fat, 13.4) {
		t.Fatalf("item fat = %v", items[1].Fat)
	}
	if !almost(p, 17.85+0.62) || !almost(f, 18.6+13.4) || !almost(cb, 43.5+0.52) {
		t.Fatalf("sums = %v %v %v", p, f, cb)
	}
}
