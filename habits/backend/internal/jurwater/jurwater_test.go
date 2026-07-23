package jurwater

import (
	"testing"
	"time"
)

func TestFirstAvailableDate(t *testing.T) {
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	// завтра свободно
	c := &checkoutCfg{dateOff: map[string]bool{}}
	if got := c.firstAvailableDate(now); got != "2026-07-23" {
		t.Fatalf("got %s, want 2026-07-23", got)
	}
	// завтра и послезавтра закрыты → 25-е (формат dd/mm/yyyy с ведущими нулями)
	c = &checkoutCfg{dateOff: map[string]bool{"23/07/2026": true, "24/07/2026": true}}
	if got := c.firstAvailableDate(now); got != "2026-07-25" {
		t.Fatalf("got %s, want 2026-07-25", got)
	}
}

func TestFirstTimeSlot(t *testing.T) {
	c := &checkoutCfg{timeSlots: []string{"", "13:30:00", "16:30:00"}}
	if got := c.firstTimeSlot(); got != "13:30:00" {
		t.Fatalf("got %s", got)
	}
	c = &checkoutCfg{}
	if got := c.firstTimeSlot(); got == "" {
		t.Fatal("должен вернуть дефолтный слот")
	}
}

func TestAddressToMap(t *testing.T) {
	a := &address{
		ID: 22643, CountryID: "AM", Region: "Ереван", Street: []string{"ул. X"},
		City: "Ереван", Firstname: "V", Lastname: "K", Telephone: "+374",
	}
	m := a.toMap()
	if m["customer_address_id"] != int64(22643) {
		t.Fatalf("id: %v", m["customer_address_id"])
	}
	if s, ok := m["street"].([]string); !ok || len(s) != 1 {
		t.Fatalf("street: %v", m["street"])
	}
}

func TestOrderIDValidation(t *testing.T) {
	// только голое положительное число — валидный id заказа
	valid := []string{"1", "42", "1000123"}
	for _, s := range valid {
		if !orderIDRe.MatchString(s) {
			t.Fatalf("%q должен считаться номером заказа", s)
		}
	}
	// всё, что возвращает кастомный чекаут при неудаче, — не заказ
	invalid := []string{"", "0", "null", "false", "true", `{"message":"error"}`, "[]", "abc", "12a", " 5 "}
	for _, s := range invalid {
		if orderIDRe.MatchString(s) {
			t.Fatalf("%q НЕ должен считаться номером заказа", s)
		}
	}
}

func TestToStrings(t *testing.T) {
	if got := toStrings("a"); len(got) != 1 || got[0] != "a" {
		t.Fatalf("string: %v", got)
	}
	if got := toStrings([]any{"a", "b"}); len(got) != 2 {
		t.Fatalf("array: %v", got)
	}
}
