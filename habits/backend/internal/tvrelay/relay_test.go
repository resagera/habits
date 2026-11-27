package tvrelay

import "testing"

// В одной комнате (у одного агента) плееров бывает несколько: на телевизоре и
// на компьютере. У каждого свой номер, и пульт должен видеть их списком.
func TestScreens(t *testing.T) {
	h := NewHub()
	tv1, leave1 := h.Join("room", RoleTV, nil)
	tv2, _ := h.Join("room", RoleTV, nil)
	_, _ = h.Join("room", RoleRemote, nil)
	tv1.SetName("Приложение на ТВ")
	tv2.SetName("Браузер компьютера")

	screens := h.Screens("room")
	if len(screens) != 2 {
		t.Fatalf("экранов %d, ожидалось 2 (пульт в список не идёт)", len(screens))
	}
	if screens[0].ID != tv1.ID() || screens[0].Name != "Приложение на ТВ" || screens[1].ID != tv2.ID() {
		t.Fatalf("список экранов: %+v", screens)
	}
	if tv1.ID() == tv2.ID() || tv1.ID() == "" {
		t.Fatalf("номера экранов должны быть разными и непустыми: %q и %q", tv1.ID(), tv2.ID())
	}
	if tv, remotes := h.Present("room"); tv != 2 || remotes != 1 {
		t.Fatalf("присутствие: %d экранов, %d пультов", tv, remotes)
	}
	// ушедший экран пропадает из списка, а несуществующему не отправить
	leave1()
	if s := h.Screens("room"); len(s) != 1 || s[0].ID != tv2.ID() {
		t.Fatalf("после ухода: %+v", s)
	}
	if h.SendTo("room", tv1.ID(), []byte("{}")) {
		t.Fatal("отправка ушедшему экрану должна возвращать false")
	}
	if h.SendTo("room", "нет такого", []byte("{}")) {
		t.Fatal("отправка в никуда должна возвращать false")
	}
}
