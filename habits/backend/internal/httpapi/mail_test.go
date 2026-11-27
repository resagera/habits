package httpapi

import (
	"strings"
	"testing"
)

func TestValidLocalPartRejectsTricks(t *testing.T) {
	// имя адреса склеивается в адрес и показывается пользователю; экзотика
	// только ломает формы магазинов, поэтому набор сознательно уже RFC
	bad := []string{"", "с-кириллицей", "with space", "a/b", `a\b`, "../../etc",
		strings.Repeat("x", 65), "a@b", "quote'", "semi;colon"}
	for _, s := range bad {
		if validLocalPart(s) {
			t.Errorf("%q не должно приниматься", s)
		}
	}
	for _, s := range []string{"habits", "shop-a7f3", "a.b_c+d", "x1"} {
		if !validLocalPart(s) {
			t.Errorf("%q должно приниматься", s)
		}
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Ozon":            "ozon",
		"My Shop":         "my-shop",
		"Магазин":         "",
		"  Wildberries  ": "wildberries",
		"a_b":             "a-b",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, ожидалось %q", in, got, want)
		}
	}
}

func TestURLEscapeKeepsFilenameSafe(t *testing.T) {
	// имя вложения уходит в заголовок Content-Disposition: перевод строки там
	// означал бы подделку заголовков
	got := urlEscape("чек \"01\".pdf\r\nX-Evil: 1")
	if strings.ContainsAny(got, "\r\n\"") {
		t.Errorf("в заголовок просочились управляющие символы: %q", got)
	}
}

func TestParseSuggestionsSurvivesModelChatter(t *testing.T) {
	// Модели любят обрамить JSON пояснениями и ```-блоками — берём кусок от
	// первой «[» до последней «]».
	out := "Конечно! Вот классификация:\n```json\n" +
		`[{"name":"Kvass \"Oblomov\" 0.45l","group":"food"},` +
		`{"name":"Batteries AA","group":"tech"}]` +
		"\n```\nГотово."
	list, err := parseSuggestions(out)
	if err != nil {
		t.Fatalf("разбор не удался: %v", err)
	}
	if len(list) != 2 || list[0].Group != "food" || list[1].Group != "tech" {
		t.Errorf("разобрано неверно: %+v", list)
	}
}

func TestParseSuggestionsDropsUnknownGroups(t *testing.T) {
	// выдуманную группу применять нельзя: она никуда не отобразится
	list, err := parseSuggestions(`[{"name":"X","group":"выдуманное"},{"name":"Y","group":"food"}]`)
	if err != nil {
		t.Fatalf("разбор не удался: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Y" {
		t.Errorf("неизвестная группа должна отбрасываться: %+v", list)
	}
}

func TestParseSuggestionsNoJSON(t *testing.T) {
	if _, err := parseSuggestions("Извините, я не могу это сделать."); err == nil {
		t.Error("ответ без JSON должен давать ошибку, а не пустой список")
	}
}
