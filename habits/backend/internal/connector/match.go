package connector

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"streaks-backend/internal/store"
)

// Границы дерева. Нужны не от жадности, а чтобы правило нельзя было превратить
// в обход защиты: дерево приходит из браузера целиком и проверяется рекурсией.
const (
	maxDepth   = 5
	maxNodes   = 50
	maxActions = 10
	maxValue   = 500
)

// Event — событие источника, приведённое к набору полей. Движок ничего не
// знает ни про письма, ни про трекеры: он работает с Text и Flags.
type Event struct {
	Source string
	// ID события в его источнике (id письма). По нему правило не срабатывает
	// на одно и то же событие дважды; 0 — события без идентификатора
	ID    int64
	Label string            // как событие видно человеку в журнале
	At    time.Time         // когда произошло: по нему считается «день события»
	Text  map[string]string // текстовые поля
	Flags map[string]bool   // флаги
}

// Trace — результат проверки узла. Повторяет форму дерева, чтобы страница
// могла нарисовать «что сошлось, а что нет» рядом с самими условиями.
type Trace struct {
	Op    string  `json:"op,omitempty"`
	Field string  `json:"field,omitempty"`
	Cmp   string  `json:"cmp,omitempty"`
	Value string  `json:"value,omitempty"`
	Got   string  `json:"got,omitempty"` // что было в событии
	OK    bool    `json:"ok"`
	Nodes []Trace `json:"nodes,omitempty"`
}

// Eval проверяет дерево условий и попутно собирает разбор по узлам.
//
// Пустая группа значит «условий нет» и подходит под любое событие источника —
// и для «и», и для «или». Логически пустое ИЛИ ложно, но правило, которое
// никогда не срабатывает, человеку объяснить нечем; интерфейс про такое
// предупреждает отдельно.
func Eval(n store.ConnectorNode, ev Event) (bool, Trace) {
	if n.Op != "" || len(n.Nodes) > 0 || n.Field == "" {
		op := strings.ToLower(n.Op)
		if op == "" {
			op = "and"
		}
		t := Trace{Op: op}
		if len(n.Nodes) == 0 {
			t.OK = true
			return true, t
		}
		result := op == "and"
		for _, child := range n.Nodes {
			ok, sub := Eval(child, ev)
			t.Nodes = append(t.Nodes, sub)
			if op == "and" {
				result = result && ok
			} else {
				result = result || ok
			}
		}
		t.OK = result
		return result, t
	}
	ok, got := leaf(n, ev)
	return ok, Trace{Field: n.Field, Cmp: n.Cmp, Value: n.Value, Got: got, OK: ok}
}

// leaf проверяет один лист и возвращает, что в событии лежало.
func leaf(n store.ConnectorNode, ev Event) (bool, string) {
	f, known := fieldByCode(n.Field)
	if !known {
		return false, "поле неизвестно"
	}
	if f.Kind == "flag" {
		v := ev.Flags[n.Field]
		got := "нет"
		if v {
			got = "да"
		}
		switch n.Cmp {
		case "is_true":
			return v, got
		case "is_false":
			return !v, got
		}
		return false, got
	}

	got := ev.Text[n.Field]
	hay, needle := strings.ToLower(got), strings.ToLower(n.Value)
	switch n.Cmp {
	case "contains":
		return strings.Contains(hay, needle), got
	case "not_contains":
		return !strings.Contains(hay, needle), got
	case "equals":
		return hay == needle, got
	case "not_equals":
		return hay != needle, got
	case "starts":
		return strings.HasPrefix(hay, needle), got
	case "ends":
		return strings.HasSuffix(hay, needle), got
	case "regex":
		re, err := regexp.Compile(n.Value)
		if err != nil {
			return false, got
		}
		return re.MatchString(got), got
	case "empty":
		return strings.TrimSpace(got) == "", got
	case "not_empty":
		return strings.TrimSpace(got) != "", got
	}
	return false, got
}

// markColorRe — цвет отметки: #rrggbb или пара для градиента. Значение уезжает
// в CSS на странице трекера, непроверенному там не место.
var markColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}(,#[0-9a-fA-F]{6})?$`)

// Validate проверяет правило целиком и возвращает человеческое объяснение,
// что не так. Сообщения видит человек в форме, поэтому они по-русски.
func Validate(r store.ConnectorRule) error {
	name := strings.TrimSpace(r.Name)
	if name == "" || len(name) > 100 {
		return fmt.Errorf("название: от 1 до 100 символов")
	}
	if len(r.Sources) == 0 {
		return fmt.Errorf("нужен хотя бы один источник")
	}
	for _, code := range r.Sources {
		if _, ok := sourceByCode(code); !ok {
			return fmt.Errorf("неизвестный источник: %s", code)
		}
	}
	nodes := 0
	if err := validateNode(r.Match, 1, &nodes); err != nil {
		return err
	}
	if len(r.Actions) == 0 {
		return fmt.Errorf("нужен хотя бы один результат")
	}
	if len(r.Actions) > maxActions {
		return fmt.Errorf("результатов в одном правиле не больше %d", maxActions)
	}
	for _, a := range r.Actions {
		if err := validateAction(a); err != nil {
			return err
		}
	}
	if r.TzOffsetMinutes < -840 || r.TzOffsetMinutes > 840 {
		return fmt.Errorf("часовой пояс вне допустимого диапазона")
	}
	return nil
}

func validateNode(n store.ConnectorNode, depth int, count *int) error {
	if depth > maxDepth {
		return fmt.Errorf("условия вложены глубже %d уровней", maxDepth)
	}
	*count++
	if *count > maxNodes {
		return fmt.Errorf("условий в одном правиле не больше %d", maxNodes)
	}
	// группа
	if n.Op != "" || len(n.Nodes) > 0 || n.Field == "" {
		op := strings.ToLower(n.Op)
		if op != "" && op != "and" && op != "or" {
			return fmt.Errorf("группа условий бывает «и» или «или», а не %q", n.Op)
		}
		for _, child := range n.Nodes {
			if err := validateNode(child, depth+1, count); err != nil {
				return err
			}
		}
		return nil
	}
	// лист
	f, ok := fieldByCode(n.Field)
	if !ok {
		return fmt.Errorf("неизвестное поле условия: %s", n.Field)
	}
	c, ok := cmpByCode(n.Cmp)
	if !ok {
		return fmt.Errorf("неизвестная операция: %s", n.Cmp)
	}
	if c.Kind != f.Kind {
		return fmt.Errorf("операция «%s» не применима к полю «%s»", c.Title, f.Title)
	}
	if len(n.Value) > maxValue {
		return fmt.Errorf("значение условия длиннее %d символов", maxValue)
	}
	if !c.NoValue && strings.TrimSpace(n.Value) == "" {
		return fmt.Errorf("у условия «%s %s» не заполнено значение", f.Title, c.Title)
	}
	if n.Cmp == "regex" {
		if _, err := regexp.Compile(n.Value); err != nil {
			return fmt.Errorf("регулярка не разбирается: %v", err)
		}
	}
	return nil
}

func validateAction(a store.ConnectorAction) error {
	switch a.Type {
	case ActionTrackerMark:
		if a.CategoryID <= 0 {
			return fmt.Errorf("в результате «отметка в трекере» не выбран трекер")
		}
		if a.Color != "" && !markColorRe.MatchString(a.Color) {
			return fmt.Errorf("цвет отметки должен быть #rrggbb")
		}
		if len(a.Emoji) > 16 {
			return fmt.Errorf("эмодзи отметки слишком длинное")
		}
		if a.Count < 0 || a.Count > 50 {
			return fmt.Errorf("прибавка счётчика — от 1 до 50")
		}
		if a.Day != "" && a.Day != "event" && a.Day != "today" {
			return fmt.Errorf("день отметки — «event» или «today»")
		}
	case ActionNotify:
		if strings.TrimSpace(a.Text) == "" {
			return fmt.Errorf("в результате «сообщение» не заполнен текст")
		}
		if len(a.Text) > 500 {
			return fmt.Errorf("текст сообщения длиннее 500 символов")
		}
	case ActionMailFlag:
		ok := false
		for _, f := range MailFlags {
			if f.Code == a.Flag {
				ok = true
			}
		}
		if !ok {
			return fmt.Errorf("неизвестная пометка письма: %s", a.Flag)
		}
	default:
		return fmt.Errorf("неизвестный результат: %s", a.Type)
	}
	return nil
}
