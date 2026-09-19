// Package files реализует релей между UI и файловым агентом на домашней
// машине. Агент держит исходящий WebSocket (у машин нет внешнего IP), хаб
// спаривает пришедший от UI HTTP-запрос с этим соединением и гоняет байты.
package files

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ErrOffline — у машины нет активного соединения агента.
var ErrOffline = errors.New("machine offline")

// ErrTimeout — агент не ответил в срок.
var ErrTimeout = errors.New("agent timeout")

// Request — команда агенту. op: list|stat|read|write|mkdir|rename|remove.
type Request struct {
	ID     uint64 `json:"id"`
	Op     string `json:"op"`
	Path   string `json:"path"`
	To     string `json:"to,omitempty"`     // rename
	Offset int64  `json:"offset,omitempty"` // read | write
	Length int64  `json:"length,omitempty"` // read
	Data   []byte `json:"data,omitempty"`   // write (base64 в JSON)
	Trunc  bool   `json:"trunc,omitempty"`  // write: обрезать файл под конец записи
	IsDir  bool   `json:"is_dir,omitempty"` // remove
}

// Response — ответ агента (JSON-кадр). Данные чтения приходят отдельным
// бинарным кадром и подставляются в Binary.
type Response struct {
	ID     uint64          `json:"id"`
	OK     bool            `json:"ok"`
	Error  string          `json:"error,omitempty"`
	EOF    bool            `json:"eof,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Binary []byte          `json:"-"`
}

// Hub хранит активные соединения агентов по id машины.
type Hub struct {
	mu    sync.RWMutex
	conns map[int64]*agentConn
}

func NewHub() *Hub {
	return &Hub{conns: map[int64]*agentConn{}}
}

type pending struct {
	ch     chan *Response
	binary []byte
}

type agentConn struct {
	hub       *Hub
	machineID int64
	ws        *websocket.Conn
	writeMu   sync.Mutex
	mu        sync.Mutex
	seq       uint64
	pending   map[uint64]*pending
	closed    bool
}

// Online — есть ли активное соединение агента.
func (h *Hub) Online(machineID int64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c, ok := h.conns[machineID]
	return ok && c != nil
}

// Serve обслуживает соединение агента до его закрытия (блокирует).
func (h *Hub) Serve(machineID int64, ws *websocket.Conn) {
	c := &agentConn{hub: h, machineID: machineID, ws: ws, pending: map[uint64]*pending{}}
	h.mu.Lock()
	if old := h.conns[machineID]; old != nil {
		old.close() // одно соединение на машину: новое вытесняет старое
	}
	h.conns[machineID] = c
	h.mu.Unlock()

	c.readLoop()

	h.mu.Lock()
	if h.conns[machineID] == c {
		delete(h.conns, machineID)
	}
	h.mu.Unlock()
	c.close()
}

func (c *agentConn) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	for _, p := range c.pending {
		close(p.ch)
	}
	c.pending = map[uint64]*pending{}
	c.mu.Unlock()
	_ = c.ws.Close()
}

// readLoop разбирает кадры агента: бинарные (данные чтения, префикс — id
// запроса), текстовые (JSON-ответы) и пинги keep-alive.
func (c *agentConn) readLoop() {
	c.ws.SetReadLimit(2 << 20) // 2 МБ: чанк 512К + запас
	_ = c.ws.SetReadDeadline(time.Now().Add(90 * time.Second))
	c.ws.SetPongHandler(func(string) error {
		return c.ws.SetReadDeadline(time.Now().Add(90 * time.Second))
	})
	go c.pingLoop()
	for {
		typ, data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		_ = c.ws.SetReadDeadline(time.Now().Add(90 * time.Second))
		switch typ {
		case websocket.BinaryMessage:
			if len(data) < 8 {
				continue
			}
			id := binary.BigEndian.Uint64(data[:8])
			payload := append([]byte(nil), data[8:]...)
			c.mu.Lock()
			if p := c.pending[id]; p != nil {
				p.binary = payload
			}
			c.mu.Unlock()
		case websocket.TextMessage:
			var resp Response
			if json.Unmarshal(data, &resp) != nil {
				continue
			}
			c.mu.Lock()
			p := c.pending[resp.ID]
			if p != nil {
				resp.Binary = p.binary
				delete(c.pending, resp.ID)
			}
			c.mu.Unlock()
			if p != nil {
				p.ch <- &resp
			}
		}
	}
}

func (c *agentConn) pingLoop() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for range t.C {
		c.writeMu.Lock()
		_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
		err := c.ws.WriteMessage(websocket.PingMessage, nil)
		c.writeMu.Unlock()
		if err != nil {
			return
		}
		c.mu.Lock()
		closed := c.closed
		c.mu.Unlock()
		if closed {
			return
		}
	}
}

// Call отправляет запрос агенту машины и ждёт ответ.
func (h *Hub) Call(ctx context.Context, machineID int64, req Request) (*Response, error) {
	h.mu.RLock()
	c := h.conns[machineID]
	h.mu.RUnlock()
	if c == nil {
		return nil, ErrOffline
	}
	return c.call(ctx, req)
}

func (c *agentConn) call(ctx context.Context, req Request) (*Response, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrOffline
	}
	c.seq++
	req.ID = c.seq
	p := &pending{ch: make(chan *Response, 1)}
	c.pending[req.ID] = p
	c.mu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		c.drop(req.ID)
		return nil, err
	}
	c.writeMu.Lock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err = c.ws.WriteMessage(websocket.TextMessage, data)
	c.writeMu.Unlock()
	if err != nil {
		c.drop(req.ID)
		return nil, ErrOffline
	}

	timeout := 20 * time.Second
	if req.Op == "read" || req.Op == "write" {
		timeout = 40 * time.Second
	}
	select {
	case <-ctx.Done():
		c.drop(req.ID)
		return nil, ctx.Err()
	case <-time.After(timeout):
		c.drop(req.ID)
		return nil, ErrTimeout
	case resp := <-p.ch:
		if resp == nil {
			return nil, ErrOffline
		}
		if !resp.OK {
			return resp, errors.New(resp.Error)
		}
		return resp, nil
	}
}

func (c *agentConn) drop(id uint64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}
