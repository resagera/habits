package receipts

import "testing"

// Названия — настоящие из чеков Yerevan City (товары, не личные данные).
func TestGuessGroupOnRealItemNames(t *testing.T) {
	cases := map[string]string{
		`Beef fillet kg (local)`:                            "food",
		`Cilantro pc`:                                       "food",
		`Lettuce leaf pc`:                                   "food",
		`Wafer "Yashkino" glazed 200g`:                      "food",
		`Russian dumplings "Atenk" 1000g`:                   "food",
		`Carbonated drink "Coca Cola" 0.5l`:                 "food",
		`Soy sauce "Sen Soy" classic 1l`:                    "food",
		`Potato (armenian) kg`:                              "food",
		`Beef tenderloin with bones kg (local)`:             "food",
		`Bread ciabatta, black 310g`:                        "food",
		`Fresh chicken breast fillet "Taarm" 1kg`:           "food",
		`Chicken nuggets with cheese "Miratorg" 250g`:       "food",
		`Stewed beef "Atenk" with opener 525g`:              "food",
		`Cheese for frying "Marianna" 250g`:                 "food",
		`Processed cheese "President" cheddar 150g`:         "food",
		`Hot-dog bread "Sevani" 4pcs 320g`:                  "food",
		`Vegetable mix pc`:                                  "food",
		`Beer "Kotayk" Erebuni g/b 0.5l`:                    "alcohol",
		`Beer "Kotayk" Erebuni g/b 0.33l`:                   "alcohol",
		`Napkins "Zhi Yin" Hey Nose 4ply 115pcs 8270`:       "household",
		`Polyethylene pack "Yerevan City" 50micron, medium`: "household",
	}
	for name, want := range cases {
		if got := GuessGroup(name); got != want {
			t.Errorf("GuessGroup(%q) = %q, ожидалось %q", name, got, want)
		}
	}
}

func TestUnknownItemsStayUnknown(t *testing.T) {
	// Молчаливое «Прочее» скрывало бы проблему: неопознанное должно быть видно
	// отдельным куском диаграммы.
	for _, name := range []string{
		`Kvass "Oblomov" (can) 0.45l`, // квас — не алкоголь и не в словаре
		`Something entirely new 123`,
		``,
	} {
		if got := GuessGroup(name); got != "" {
			t.Errorf("GuessGroup(%q) = %q, ожидалась пустая группа", name, got)
		}
	}
}

func TestAlcoholWinsOverFood(t *testing.T) {
	// порядок правил: пиво не должно попасть в продукты из-за «drink»
	if got := GuessGroup(`Beer "Kotayk" light drink 0.5l`); got != "alcohol" {
		t.Errorf("получено %q, ожидался alcohol", got)
	}
}

func TestNameKeyKeepsVolume(t *testing.T) {
	// разный объём — разные товары: для истории цен смешивать нельзя
	a := NameKey(`Beer "Kotayk" Erebuni g/b 0.5l`)
	b := NameKey(`Beer "Kotayk" Erebuni g/b 0.33l`)
	if a == b {
		t.Error("объём не должен отбрасываться при нормализации")
	}
	// регистр и лишние пробелы — не различие
	if NameKey("  Beef   FILLET kg ") != NameKey("beef fillet kg") {
		t.Error("регистр и пробелы должны нормализоваться")
	}
}
