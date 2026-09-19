// Package tvrelay — шина сообщений между ТВ-приставкой и пультом.
//
// Прод здесь СОЗНАТЕЛЬНО глупый: он не знает ни про папки, ни про очереди, ни
// про то, что играет. Он только пересылает непрозрачный JSON внутри комнаты.
// Вся логика живёт на приставке, которая ходит к своему агенту по локальной
// сети напрямую — а значит, ни байта медиа и ни одного пути через сервер не
// проходит.
//
// Почему комната, а не сессия с кодом: приставка и телефон в одной локальной
// сети, и достаточно назвать адрес агента. Код с QR был бы церемонией ради
// того, что и так известно.
package tvrelay

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Role — кто подключился. Приставка в комнате обычно одна, пультов может быть
// несколько (два телефона — штатный случай).
type Role int

const (
	RoleTV Role = iota
	RoleRemote
)

type client struct {
	role Role
	ws   *websocket.Conn
	mu   sync.Mutex // пишет и хаб, и читающая горутина — сериализуем
}

func (c *client) send(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.ws.WriteMessage(websocket.TextMessage, data)
}

// Ping держит соединение живым. Идёт через тот же замок, что и send: две
// параллельные записи в один веб-сокет — это паника в gorilla, а пинг тикает
// из своей горутины.
func (c *client) Ping() {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
		_ = c.ws.Close()
	}
}

type room struct {
	clients map[*client]bool
}

// Send — отправить одному клиенту (а не всей комнате). Нужно, чтобы вручить
// приставке её код подключения.
func (c *client) Send(data []byte) { _ = c.send(data) }

// Hub — комнаты по ключу и коды подключения к ним.
type Hub struct {
	mu    sync.RWMutex
	rooms map[string]*room
	codes map[string]string // код → ключ комнаты
	keys  map[string]string // ключ комнаты → её текущий код
}

func NewHub() *Hub {
	return &Hub{rooms: map[string]*room{}, codes: map[string]string{}, keys: map[string]string{}}
}

// Алфавит кода без похожих знаков: код читают с телевизора через комнату и
// набирают на телефоне, и «0» против «O» тут стоило бы отдельного вечера.
const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// Code выдаёт комнате код подключения — он живёт, пока приставка на связи.
// Повторный вызов для той же комнаты возвращает прежний код: перезагрузили
// страницу на телевизоре — код на экране не должен меняться.
func (h *Hub) Code(key string, random func(n int) []byte) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if code, ok := h.keys[key]; ok {
		return code
	}
	for attempt := 0; attempt < 10; attempt++ {
		b := random(8)
		out := make([]byte, 8)
		for i, v := range b {
			out[i] = codeAlphabet[int(v)%len(codeAlphabet)]
		}
		code := string(out)
		if _, taken := h.codes[code]; taken {
			continue
		}
		h.codes[code] = key
		h.keys[key] = code
		return code
	}
	return ""
}

// Resolve — какой комнате принадлежит код. Пустая строка, если код не жив:
// приставку выключили или код устарел.
func (h *Hub) Resolve(code string) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.codes[code]
}

// dropCode убирает код, когда из комнаты ушла последняя приставка.
func (h *Hub) dropCode(key string) {
	if code, ok := h.keys[key]; ok {
		delete(h.codes, code)
		delete(h.keys, key)
	}
}

// Join добавляет соединение в комнату и возвращает функцию выхода.
func (h *Hub) Join(key string, role Role, ws *websocket.Conn) (*client, func()) {
	c := &client{role: role, ws: ws}
	h.mu.Lock()
	r := h.rooms[key]
	if r == nil {
		r = &room{clients: map[*client]bool{}}
		h.rooms[key] = r
	}
	r.clients[c] = true
	h.mu.Unlock()

	return c, func() {
		h.mu.Lock()
		if r := h.rooms[key]; r != nil {
			delete(r.clients, c)
			tvLeft := true
			for other := range r.clients {
				if other.role == RoleTV {
					tvLeft = false
				}
			}
			// код живёт, пока жива приставка: иначе он остался бы годным для
			// выключенного телевизора
			if tvLeft {
				h.dropCode(key)
			}
			if len(r.clients) == 0 {
				delete(h.rooms, key)
			}
		}
		h.mu.Unlock()
	}
}

// Broadcast пересылает сообщение всем в комнате, КРОМЕ отправителя и тех, кто
// той же роли: приставке интересны только команды пультов, пультам — только
// ответы приставки. Пульты между собой не общаются.
func (h *Hub) Broadcast(key string, from *client, data []byte) {
	h.mu.RLock()
	r := h.rooms[key]
	var targets []*client
	if r != nil {
		for c := range r.clients {
			if c != from && c.role != from.role {
				targets = append(targets, c)
			}
		}
	}
	h.mu.RUnlock()
	for _, c := range targets {
		if err := c.send(data); err != nil {
			_ = c.ws.Close() // читающая горутина увидит закрытие и выйдет сама
		}
	}
}

// BroadcastAll шлёт всем в комнате, включая отправителя роли. Нужно для
// присутствия: и приставке, и пультам полезно знать, кто ещё на связи.
func (h *Hub) BroadcastAll(key string, data []byte) {
	h.mu.RLock()
	r := h.rooms[key]
	var targets []*client
	if r != nil {
		for c := range r.clients {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range targets {
		if err := c.send(data); err != nil {
			_ = c.ws.Close()
		}
	}
}

// Present — сколько кого сейчас в комнате. Нужно пульту: без приставки
// команды отправлять некуда, и честнее сказать об этом сразу.
func (h *Hub) Present(key string) (tv, remotes int) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	r := h.rooms[key]
	if r == nil {
		return 0, 0
	}
	for c := range r.clients {
		if c.role == RoleTV {
			tv++
		} else {
			remotes++
		}
	}
	return tv, remotes
}
