package connector

import (
	"strings"
	"testing"
	"time"

	"streaks-backend/internal/store"
)

// событие-образец: настоящее письмо «Яндекс Go — отчёт о поездке»
func tripEvent() Event {
	return Event{
		Source: SourceMail,
		ID:     42,
		At:     time.Date(2026, 11, 10, 21, 14, 0, 0, time.UTC),
		Text: map[string]string{
			"from":      "no-reply@taxi.yandex.ru",
			"from_name": "Yandex Go",
			"envelope":  "bounce@taxi.yandex.ru",
			"subject":   "Яндекс Go — отчёт о поездке 10 ноября 2026 г.",
			"text":      "Поездка\nМаршрут: дом → работа\nИтого: 1500 ֏",
			"rcpt":      "me@resager.ru",
			"ip":        "95.108.213.1",
			"spf":       "pass",
		},
		Flags: map[string]bool{"spam": false, "attachments": false, "tls": true},
	}
}

func leafNode(field, cmp, value string) store.ConnectorNode {
	return store.ConnectorNode{Field: field, Cmp: cmp, Value: value}
}

func group(op string, nodes ...store.ConnectorNode) store.ConnectorNode {
	return store.ConnectorNode{Op: op, Nodes: nodes}
}

// Правило из жизни: письмо от Яндекс Go ИЛИ с его темой (пересылка теряет
// отправителя), и при этом не спам. Проверяем, что вложенность работает.
func TestEvalNestedRule(t *testing.T) {
	rule := group("and",
		group("or",
			leafNode("from", "contains", "taxi.yandex.ru"),
			leafNode("subject", "contains", "Яндекс Go"),
		),
		leafNode("spam", "is_false", ""),
	)

	ev := tripEvent()
	ok, tr := Eval(rule, ev)
	if !ok {
		t.Fatalf("правило должно подойти, разбор: %+v", tr)
	}
	if len(tr.Nodes) != 2 || len(tr.Nodes[0].Nodes) != 2 {
		t.Fatalf("разбор не повторяет форму дерева: %+v", tr)
	}
	if !tr.Nodes[0].Nodes[0].OK || tr.Nodes[0].Nodes[0].Got != "no-reply@taxi.yandex.ru" {
		t.Fatalf("в разборе должно быть видно, что лежало в поле: %+v", tr.Nodes[0].Nodes[0])
	}

	// пересылка через Gmail: отправитель уже не Яндекс, но тема осталась
	fwd := tripEvent()
	fwd.Text["from"] = "me@gmail.com"
	fwd.Text["subject"] = "Fwd: Яндекс Go — отчёт о поездке"
	if ok, tr := Eval(rule, fwd); !ok {
		t.Fatalf("пересланное письмо должно подойти по теме: %+v", tr)
	}

	// спам не подходит, даже если отправитель тот самый
	spam := tripEvent()
	spam.Flags["spam"] = true
	if ok, _ := Eval(rule, spam); ok {
		t.Fatal("письмо в спаме подходить не должно")
	}

	// чужое письмо не подходит ни по одной ветке «или»
	other := tripEvent()
	other.Text["from"] = "news@ozon.ru"
	other.Text["subject"] = "Скидки недели"
	if ok, _ := Eval(rule, other); ok {
		t.Fatal("чужое письмо подходить не должно")
	}
}

// Группа «или» внутри «или» и глубина 4 уровня: собирается ли дерево вообще.
func TestEvalDeepNesting(t *testing.T) {
	rule := group("or",
		leafNode("from", "contains", "никогда"),
		group("and",
			leafNode("spf", "equals", "pass"),
			group("or",
				leafNode("subject", "starts", "нет"),
				group("and",
					leafNode("text", "contains", "маршрут"),
					leafNode("attachments", "is_false", ""),
				),
			),
		),
	)
	ok, tr := Eval(rule, tripEvent())
	if !ok {
		t.Fatalf("глубокое дерево должно сойтись: %+v", tr)
	}
}

func TestEvalComparisons(t *testing.T) {
	ev := tripEvent()
	cases := []struct {
		field, cmp, value string
		want              bool
	}{
		{"subject", "contains", "ЯНДЕКС GO", true}, // регистр не важен
		{"subject", "not_contains", "Ozon", true},
		{"from", "equals", "NO-REPLY@TAXI.YANDEX.RU", true},
		{"from", "not_equals", "other@example.com", true},
		{"from", "starts", "no-reply@", true},
		{"from", "ends", ".yandex.ru", true},
		{"from", "ends", ".example.com", false},
		{"subject", "regex", `отчёт о поездке \d+ ноября`, true},
		{"subject", "regex", `^\d+$`, false},
		{"helo", "empty", "", true}, // поля нет в событии
		{"helo", "not_empty", "", false},
		{"text", "not_empty", "", true},
		{"tls", "is_true", "", true},
		{"tls", "is_false", "", false},
		{"attachments", "is_true", "", false},
	}
	for _, c := range cases {
		got, tr := Eval(leafNode(c.field, c.cmp, c.value), ev)
		if got != c.want {
			t.Errorf("%s %s %q = %v, ждали %v (в поле было %q)",
				c.field, c.cmp, c.value, got, c.want, tr.Got)
		}
	}
}

// Пустое дерево значит «условий нет» и подходит под любое событие источника —
// и для «и», и для «или»: правило, которое никогда не срабатывает, человеку
// объяснить нечем.
func TestEvalEmptyGroupMatchesAll(t *testing.T) {
	for _, op := range []string{"and", "or", ""} {
		if ok, _ := Eval(store.ConnectorNode{Op: op}, tripEvent()); !ok {
			t.Errorf("пустая группа %q должна подходить", op)
		}
	}
}

// Неизвестное поле не должно «подходить» молча: иначе правило после
// переименования поля срабатывало бы на всё.
func TestEvalUnknownFieldFails(t *testing.T) {
	ok, tr := Eval(leafNode("нет_такого", "contains", "что-то"), tripEvent())
	if ok {
		t.Fatal("неизвестное поле подходить не должно")
	}
	if !strings.Contains(tr.Got, "неизвестно") {
		t.Fatalf("разбор должен объяснять причину, а не молчать: %q", tr.Got)
	}
}

func validRule() store.ConnectorRule {
	return store.ConnectorRule{
		Name:    "Поездки из дома",
		Sources: []string{SourceMail},
		Match:   group("and", leafNode("from", "contains", "taxi.yandex.ru")),
		Actions: []store.ConnectorAction{{
			Type: ActionTrackerMark, CategoryID: 7, Color: "#4caf50", Day: "event",
		}},
	}
}

func TestValidateOK(t *testing.T) {
	if err := Validate(validRule()); err != nil {
		t.Fatalf("правило должно быть принято: %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name  string
		spoil func(r *store.ConnectorRule)
	}{
		{"без названия", func(r *store.ConnectorRule) { r.Name = "  " }},
		{"без источника", func(r *store.ConnectorRule) { r.Sources = nil }},
		{"чужой источник", func(r *store.ConnectorRule) { r.Sources = []string{"telepathy"} }},
		{"без результата", func(r *store.ConnectorRule) { r.Actions = nil }},
		{"неизвестное поле", func(r *store.ConnectorRule) {
			r.Match = group("and", leafNode("нет_такого", "contains", "x"))
		}},
		{"операция не для поля", func(r *store.ConnectorRule) {
			r.Match = group("and", leafNode("spam", "contains", "x"))
		}},
		{"пустое значение", func(r *store.ConnectorRule) {
			r.Match = group("and", leafNode("subject", "contains", " "))
		}},
		{"битая регулярка", func(r *store.ConnectorRule) {
			r.Match = group("and", leafNode("subject", "regex", "([a-z"))
		}},
		{"группа не и/или", func(r *store.ConnectorRule) {
			r.Match = group("xor", leafNode("subject", "contains", "x"))
		}},
		{"трекер не выбран", func(r *store.ConnectorRule) {
			r.Actions = []store.ConnectorAction{{Type: ActionTrackerMark}}
		}},
		{"цвет не цвет", func(r *store.ConnectorRule) {
			r.Actions = []store.ConnectorAction{{
				Type: ActionTrackerMark, CategoryID: 1, Color: "red; background: url(x)",
			}}
		}},
		{"сообщение без текста", func(r *store.ConnectorRule) {
			r.Actions = []store.ConnectorAction{{Type: ActionNotify}}
		}},
		{"неизвестная пометка", func(r *store.ConnectorRule) {
			r.Actions = []store.ConnectorAction{{Type: ActionMailFlag, Flag: "burn"}}
		}},
		{"неизвестный результат", func(r *store.ConnectorRule) {
			r.Actions = []store.ConnectorAction{{Type: "launch_rocket"}}
		}},
		{"пояс за пределами", func(r *store.ConnectorRule) { r.TzOffsetMinutes = 2000 }},
	}
	for _, c := range cases {
		r := validRule()
		c.spoil(&r)
		if err := Validate(r); err == nil {
			t.Errorf("%s: правило должно быть отвергнуто", c.name)
		}
	}
}

// Глубина и количество узлов ограничены: дерево приходит из браузера целиком.
func TestValidateTreeLimits(t *testing.T) {
	deep := leafNode("subject", "contains", "x")
	for i := 0; i < maxDepth+1; i++ {
		deep = group("and", deep)
	}
	r := validRule()
	r.Match = deep
	if err := Validate(r); err == nil {
		t.Fatal("слишком глубокое дерево должно быть отвергнуто")
	}

	wide := store.ConnectorNode{Op: "or"}
	for i := 0; i < maxNodes+1; i++ {
		wide.Nodes = append(wide.Nodes, leafNode("subject", "contains", "x"))
	}
	r = validRule()
	r.Match = wide
	if err := Validate(r); err == nil {
		t.Fatal("слишком широкое дерево должно быть отвергнуто")
	}

	r = validRule()
	for i := 0; i < maxActions+1; i++ {
		r.Actions = append(r.Actions, store.ConnectorAction{
			Type: ActionNotify, Text: "ещё",
		})
	}
	if err := Validate(r); err == nil {
		t.Fatal("слишком много результатов должно быть отвергнуто")
	}
}

func TestExpandPlaceholders(t *testing.T) {
	ev := tripEvent()
	got := expand("Поездка: {subject} от {from} на {rcpt} ({date})", ev)
	for _, want := range []string{"отчёт о поездке", "no-reply@taxi.yandex.ru",
		"me@resager.ru", "10.11.2026 21:14"} {
		if !strings.Contains(got, want) {
			t.Errorf("в тексте ждали %q, получили %q", want, got)
		}
	}
}
