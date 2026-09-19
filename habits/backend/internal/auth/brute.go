package auth

import (
	"sync"
	"time"
)

// Защита от подбора токенов доступа. Сам токен — 192 бита случайности, так
// что перебор нереален математически; лимитер нужен, чтобы такие попытки не
// нагружали БД и были видны в логах, а также против подбора «похожих»
// токенов из утёкших фрагментов.
const (
	bruteWindow   = 5 * time.Minute  // окно накопления неудач
	bruteMaxFails = 10               // сколько неудач допускаем в окне
	bruteBlock    = 15 * time.Minute // на сколько блокируем IP после превышения
	bruteMaxIPs   = 10000            // предохранитель от разрастания памяти
)

type bruteEntry struct {
	fails        int
	windowStart  time.Time
	blockedUntil time.Time
}

// bruteGuard — счётчик неудачных попыток входа по токену, по IP.
type bruteGuard struct {
	mu      sync.Mutex
	entries map[string]*bruteEntry
}

func newBruteGuard() *bruteGuard {
	return &bruteGuard{entries: map[string]*bruteEntry{}}
}

// Blocked — IP временно заблокирован за перебор.
func (g *bruteGuard) Blocked(ip string, now time.Time) bool {
	if ip == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.entries[ip]
	return e != nil && now.Before(e.blockedUntil)
}

// Fail отмечает неудачную попытку; возвращает true, если IP только что
// заблокирован (для лога).
func (g *bruteGuard) Fail(ip string, now time.Time) bool {
	if ip == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.evictLocked(now)

	e := g.entries[ip]
	if e == nil {
		e = &bruteEntry{windowStart: now}
		g.entries[ip] = e
	}
	// окно истекло — начинаем счёт заново
	if now.Sub(e.windowStart) > bruteWindow {
		e.fails, e.windowStart = 0, now
	}
	e.fails++
	if e.fails >= bruteMaxFails && !now.Before(e.blockedUntil) {
		e.blockedUntil = now.Add(bruteBlock)
		e.fails, e.windowStart = 0, now
		return true
	}
	return false
}

// Success сбрасывает счётчик после удачного входа.
func (g *bruteGuard) Success(ip string) {
	if ip == "" {
		return
	}
	g.mu.Lock()
	delete(g.entries, ip)
	g.mu.Unlock()
}

// evictLocked чистит протухшие записи (вызывается под mu).
func (g *bruteGuard) evictLocked(now time.Time) {
	if len(g.entries) < bruteMaxIPs {
		return
	}
	for ip, e := range g.entries {
		if now.After(e.blockedUntil) && now.Sub(e.windowStart) > bruteWindow {
			delete(g.entries, ip)
		}
	}
}
