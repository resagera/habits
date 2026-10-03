package terminal

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Ticket — краткоживущий пропуск на открытие консольной сессии. Нужен, чтобы
// браузер мог открыть WebSocket (в нём нельзя задать заголовок Authorization).
type Ticket struct {
	MachineID int64
	expires   time.Time
}

type Tickets struct {
	mu sync.Mutex
	m  map[string]Ticket
}

func NewTickets() *Tickets {
	return &Tickets{m: map[string]Ticket{}}
}

const ticketTTL = 60 * time.Second

func (t *Tickets) Issue(machineID int64) string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	tok := hex.EncodeToString(b)
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, v := range t.m {
		if now.After(v.expires) {
			delete(t.m, k)
		}
	}
	t.m[tok] = Ticket{MachineID: machineID, expires: now.Add(ticketTTL)}
	return tok
}

// Redeem одноразово гасит пропуск и возвращает id машины.
func (t *Tickets) Redeem(tok string) (int64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tk, ok := t.m[tok]
	if !ok {
		return 0, false
	}
	delete(t.m, tok)
	if time.Now().After(tk.expires) {
		return 0, false
	}
	return tk.MachineID, true
}
