package files

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Ticket — краткоживущий пропуск на стриминг конкретного файла. Нужен, чтобы
// <video>/<audio>/<img> в вебвью могли грузить файл по URL без заголовка
// Authorization (браузер его не шлёт для медиа-тегов).
type Ticket struct {
	MachineID int64
	Path      string
	Download  bool
	Name      string
	expires   time.Time
}

// Tickets — потокобезопасное хранилище пропусков в памяти.
type Tickets struct {
	mu sync.Mutex
	m  map[string]Ticket
}

func NewTickets() *Tickets {
	return &Tickets{m: map[string]Ticket{}}
}

const ticketTTL = 6 * time.Hour

// Issue создаёт пропуск и попутно чистит протухшие.
func (t *Tickets) Issue(machineID int64, path, name string, download bool) string {
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
	t.m[tok] = Ticket{MachineID: machineID, Path: path, Name: name, Download: download, expires: now.Add(ticketTTL)}
	return tok
}

// Get возвращает действующий пропуск.
func (t *Tickets) Get(tok string) (Ticket, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tk, ok := t.m[tok]
	if !ok || time.Now().After(tk.expires) {
		return Ticket{}, false
	}
	return tk, true
}
