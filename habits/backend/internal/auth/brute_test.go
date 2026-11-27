package auth

import (
	"testing"
	"time"
)

func TestBruteGuardBlocksAfterLimit(t *testing.T) {
	g := newBruteGuard()
	now := time.Now()
	ip := "1.2.3.4"

	for i := 1; i < bruteMaxFails; i++ {
		if g.Fail(ip, now) {
			t.Fatalf("блокировка на попытке %d — слишком рано", i)
		}
		if g.Blocked(ip, now) {
			t.Fatalf("IP заблокирован после %d неудач", i)
		}
	}
	if !g.Fail(ip, now) {
		t.Fatal("на пороговой попытке должна включиться блокировка")
	}
	if !g.Blocked(ip, now) {
		t.Fatal("IP должен быть заблокирован")
	}
	// другие адреса не задеты
	if g.Blocked("9.9.9.9", now) {
		t.Fatal("заблокирован чужой IP")
	}
	// блокировка снимается по истечении срока
	if g.Blocked(ip, now.Add(bruteBlock+time.Second)) {
		t.Fatal("блокировка не истекла")
	}
}

func TestBruteGuardWindowResets(t *testing.T) {
	g := newBruteGuard()
	now := time.Now()
	ip := "5.6.7.8"

	// неудачи «размазаны» шире окна — до блокировки не доходит
	for i := 0; i < bruteMaxFails*2; i++ {
		if g.Fail(ip, now) {
			t.Fatalf("блокировка на редких попытках (итерация %d)", i)
		}
		now = now.Add(bruteWindow + time.Second)
	}
	if g.Blocked(ip, now) {
		t.Fatal("редкие неудачи не должны блокировать")
	}
}

func TestBruteGuardSuccessResets(t *testing.T) {
	g := newBruteGuard()
	now := time.Now()
	ip := "10.0.0.1"

	for i := 1; i < bruteMaxFails; i++ {
		g.Fail(ip, now)
	}
	g.Success(ip) // удачный вход обнуляет счётчик
	for i := 1; i < bruteMaxFails; i++ {
		if g.Fail(ip, now) {
			t.Fatalf("после успеха счётчик не сброшен (итерация %d)", i)
		}
	}
}

func TestBruteGuardIgnoresEmptyIP(t *testing.T) {
	g := newBruteGuard()
	now := time.Now()
	for i := 0; i < bruteMaxFails*2; i++ {
		g.Fail("", now)
	}
	if g.Blocked("", now) {
		t.Fatal("пустой IP не должен блокироваться (иначе заденет всех)")
	}
}
