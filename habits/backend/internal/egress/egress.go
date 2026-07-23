// Package egress — релей сетевого выхода: домашний агент (резидентный IP)
// держит исходящий WebSocket, а прод туннелирует через него HTTP-запросы к
// jur.am (датацентр-IP прод-сервера блокируется Cloudflare). Вся логика
// сценария остаётся на проде (internal/jurwater) — агент лишь выполняет
// одиночные HTTP-запросы к белому списку хостов и возвращает ответ как есть.
package egress

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	// ErrOffline — у пользователя нет активного соединения агента.
	ErrOffline = errors.New("agent offline")
	// ErrTimeout — агент не ответил в срок.
	ErrTimeout = errors.New("agent timeout")
)

// reqFrame — запрос агенту (одиночный HTTP). Body — base64 в JSON.
type reqFrame struct {
	ID      uint64              `json:"id"`
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers map[string][]string `json:"headers"`
	Body    []byte              `json:"body,omitempty"`
}

// respFrame — ответ агента.
type respFrame struct {
	ID      uint64              `json:"id"`
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers"`
	Body    []byte              `json:"body,omitempty"`
	Error   string              `json:"error,omitempty"`
}

// Hub хранит соединения агентов по user_id.
type Hub struct {
	mu    sync.RWMutex
	conns map[int64]*agentConn
}

func NewHub() *Hub { return &Hub{conns: map[int64]*agentConn{}} }

// Online — есть ли активный агент у пользователя.
func (h *Hub) Online(userID int64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c := h.conns[userID]
	return c != nil
}

// Serve обслуживает соединение агента до обрыва (блокирует).
func (h *Hub) Serve(userID int64, ws *websocket.Conn) {
	c := &agentConn{ws: ws, pending: map[uint64]chan *respFrame{}}
	h.mu.Lock()
	if old := h.conns[userID]; old != nil {
		old.close()
	}
	h.conns[userID] = c
	h.mu.Unlock()

	c.readLoop()

	h.mu.Lock()
	if h.conns[userID] == c {
		delete(h.conns, userID)
	}
	h.mu.Unlock()
	c.close()
}

type agentConn struct {
	ws      *websocket.Conn
	writeMu sync.Mutex
	mu      sync.Mutex
	seq     uint64
	pending map[uint64]chan *respFrame
	closed  bool
}

func (c *agentConn) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	for _, ch := range c.pending {
		close(ch)
	}
	c.pending = map[uint64]chan *respFrame{}
	c.mu.Unlock()
	_ = c.ws.Close()
}

func (c *agentConn) readLoop() {
	c.ws.SetReadLimit(16 << 20) // страница чекаута ~700 КБ, с запасом
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
		if typ != websocket.TextMessage {
			continue
		}
		var resp respFrame
		if json.Unmarshal(data, &resp) != nil {
			continue
		}
		c.mu.Lock()
		ch := c.pending[resp.ID]
		delete(c.pending, resp.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- &resp
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

func (c *agentConn) do(ctx context.Context, req reqFrame) (*respFrame, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrOffline
	}
	c.seq++
	req.ID = c.seq
	ch := make(chan *respFrame, 1)
	c.pending[req.ID] = ch
	c.mu.Unlock()

	buf, _ := json.Marshal(req)
	c.writeMu.Lock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(15 * time.Second))
	err := c.ws.WriteMessage(websocket.TextMessage, buf)
	c.writeMu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, req.ID)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp, ok := <-ch:
		if !ok {
			return nil, ErrOffline
		}
		return resp, nil
	case <-time.After(50 * time.Second):
		return nil, ErrTimeout
	}
}

// Transport возвращает http.RoundTripper, туннелирующий запросы через агента
// пользователя. Если агента нет — RoundTrip вернёт ErrOffline.
func (h *Hub) Transport(userID int64) http.RoundTripper {
	return &roundTripper{hub: h, userID: userID}
}

type roundTripper struct {
	hub    *Hub
	userID int64
}

func (rt *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.hub.mu.RLock()
	c := rt.hub.conns[rt.userID]
	rt.hub.mu.RUnlock()
	if c == nil {
		return nil, ErrOffline
	}
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		req.Body.Close()
	}
	resp, err := c.do(req.Context(), reqFrame{
		Method:  req.Method,
		URL:     req.URL.String(),
		Headers: req.Header,
		Body:    body,
	})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New("egress agent: " + resp.Error)
	}
	h := http.Header{}
	for k, vs := range resp.Headers {
		for _, v := range vs {
			h.Add(k, v)
		}
	}
	return &http.Response{
		StatusCode: resp.Status,
		Status:     http.StatusText(resp.Status),
		Header:     h,
		Body:       io.NopCloser(bytes.NewReader(resp.Body)),
		Request:    req,
	}, nil
}
