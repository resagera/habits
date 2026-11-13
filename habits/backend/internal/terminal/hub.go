// Package terminal релеит интерактивные PTY-сессии между веб-консолью (xterm)
// и shell-агентом на домашней машине. Агент держит исходящий WebSocket (у
// машин нет внешнего IP), хаб мультиплексирует по нему несколько сессий:
// у каждой свой id, бинарные кадры данных префиксуются им.
package terminal

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ErrOffline — у машины нет активного соединения агента.
var ErrOffline = errors.New("machine offline")

// control — управляющее сообщение backend↔agent (JSON-кадр).
type control struct {
	T    string `json:"t"`              // open | resize | close | exit
	SID  uint64 `json:"sid"`            // id сессии
	Cols uint16 `json:"cols,omitempty"` // open | resize
	Rows uint16 `json:"rows,omitempty"`
}

// Session — одна PTY-сессия. Output получает stdout от агента; клиентский
// обработчик читает его и пишет в WebSocket браузера.
type Session struct {
	ID     uint64
	conn   *agentConn
	Output chan []byte
	once   sync.Once
}

// Write отправляет stdin в PTY (бинарный кадр [sid][data]).
func (s *Session) Write(data []byte) error {
	frame := make([]byte, 8+len(data))
	binary.BigEndian.PutUint64(frame[:8], s.ID)
	copy(frame[8:], data)
	return s.conn.writeBinary(frame)
}

// Resize меняет размер PTY.
func (s *Session) Resize(cols, rows uint16) error {
	return s.conn.writeJSON(control{T: "resize", SID: s.ID, Cols: cols, Rows: rows})
}

// Close завершает сессию: просит агента убить PTY и снимает регистрацию.
func (s *Session) Close() {
	s.once.Do(func() {
		s.conn.removeSession(s.ID)
		_ = s.conn.writeJSON(control{T: "close", SID: s.ID})
		close(s.Output)
	})
}

type Hub struct {
	mu    sync.RWMutex
	conns map[int64]*agentConn
}

func NewHub() *Hub {
	return &Hub{conns: map[int64]*agentConn{}}
}

func (h *Hub) Online(machineID int64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.conns[machineID] != nil
}

// Serve обслуживает соединение агента до обрыва (блокирует).
func (h *Hub) Serve(machineID int64, ws *websocket.Conn) {
	c := &agentConn{ws: ws, sessions: map[uint64]*Session{}}
	h.mu.Lock()
	if old := h.conns[machineID]; old != nil {
		old.shutdown()
	}
	h.conns[machineID] = c
	h.mu.Unlock()

	c.readLoop()

	h.mu.Lock()
	if h.conns[machineID] == c {
		delete(h.conns, machineID)
	}
	h.mu.Unlock()
	c.shutdown()
}

// OpenSession просит агента машины открыть новый PTY.
func (h *Hub) OpenSession(machineID int64, cols, rows uint16) (*Session, error) {
	h.mu.RLock()
	c := h.conns[machineID]
	h.mu.RUnlock()
	if c == nil {
		return nil, ErrOffline
	}
	return c.open(cols, rows)
}

type agentConn struct {
	ws       *websocket.Conn
	writeMu  sync.Mutex
	mu       sync.Mutex
	seq      uint64
	sessions map[uint64]*Session
	closed   bool
}

func (c *agentConn) writeBinary(b []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return c.ws.WriteMessage(websocket.BinaryMessage, b)
}

func (c *agentConn) writeJSON(v control) error {
	data, _ := json.Marshal(v)
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return c.ws.WriteMessage(websocket.TextMessage, data)
}

func (c *agentConn) open(cols, rows uint16) (*Session, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrOffline
	}
	c.seq++
	sid := c.seq
	s := &Session{ID: sid, conn: c, Output: make(chan []byte, 2048)}
	c.sessions[sid] = s
	c.mu.Unlock()

	if err := c.writeJSON(control{T: "open", SID: sid, Cols: cols, Rows: rows}); err != nil {
		c.removeSession(sid)
		return nil, ErrOffline
	}
	return s, nil
}

func (c *agentConn) removeSession(sid uint64) {
	c.mu.Lock()
	delete(c.sessions, sid)
	c.mu.Unlock()
}

func (c *agentConn) shutdown() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	sessions := c.sessions
	c.sessions = map[uint64]*Session{}
	c.mu.Unlock()
	for _, s := range sessions {
		s.once.Do(func() { close(s.Output) }) // разбудит клиентские обработчики
	}
	_ = c.ws.Close()
}

// readLoop разбирает кадры агента: бинарные — stdout (префикс sid) в канал
// сессии; текстовые — управляющие (exit); плюс keep-alive.
func (c *agentConn) readLoop() {
	c.ws.SetReadLimit(1 << 20)
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
			sid := binary.BigEndian.Uint64(data[:8])
			payload := append([]byte(nil), data[8:]...)
			c.mu.Lock()
			s := c.sessions[sid]
			c.mu.Unlock()
			if s != nil {
				// блокирующая отправка: агент притормозит при медленном клиенте
				select {
				case s.Output <- payload:
				case <-time.After(30 * time.Second):
					s.Close() // клиент завис — рвём сессию
				}
			}
		case websocket.TextMessage:
			var msg control
			if json.Unmarshal(data, &msg) != nil {
				continue
			}
			if msg.T == "exit" {
				c.mu.Lock()
				s := c.sessions[msg.SID]
				c.mu.Unlock()
				if s != nil {
					s.Close()
				}
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
