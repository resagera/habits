// Package aicoder — релей между UI и coding-агентом (habits-ai-agent) на
// домашней машине. Агент держит исходящий WebSocket (у машин нет внешнего
// IP). Два вида обмена: синхронные вызовы (check — проверка инструмента,
// с ожиданием ответа) и асинхронные прогоны (run): сервер отправляет job,
// агент позже присылает run_status/run_result; результат подтверждается
// кадром run_ack — до подтверждения агент хранит его и повторяет после
// переподключения (доставка «хотя бы раз», FinishAIRun идемпотентен).
package aicoder

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var ErrOffline = errors.New("machine offline")
var ErrTimeout = errors.New("agent timeout")

// Frame — единый кадр протокола (в обе стороны, лишние поля игнорируются).
type Frame struct {
	Kind string `json:"kind"`
	// sync-вызовы (check)
	ID     uint64          `json:"id,omitempty"`
	Tool   string          `json:"tool,omitempty"`
	OK     bool            `json:"ok,omitempty"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	// прогоны
	RunID     int64           `json:"run_id,omitempty"`
	Workdir   string          `json:"workdir,omitempty"`
	Model     string          `json:"model,omitempty"`
	Params    string          `json:"params,omitempty"`
	Prompt    string          `json:"prompt,omitempty"`
	Mode      string          `json:"mode,omitempty"` // '' | 'plan'
	SessionID string          `json:"session_id,omitempty"`
	Status    string          `json:"status,omitempty"`
	Output    string          `json:"output,omitempty"`
	Meta      json.RawMessage `json:"meta,omitempty"`
}

// Hello — первый кадр агента: разрешённые папки, версия и живые прогоны.
type Hello struct {
	Dirs    []string `json:"dirs"`
	Version string   `json:"version"`
	// Active — прогоны, живые в подключившемся процессе агента. Всё, что на
	// сервере числится running для этой машины и отсутствует здесь, —
	// «осиротело» (агент перезапущен/упал) и закрывается ошибкой.
	Active []int64 `json:"active"`
}

// Hub хранит активные соединения агентов по id машины.
type Hub struct {
	mu    sync.RWMutex
	conns map[int64]*agentConn

	// колбэки обработки прогонов (ставятся до Serve; machineID — чей агент)
	OnRunStatus func(machineID, runID int64, status string)
	// OnRunResult возвращает ошибку, если результат сохранить не удалось —
	// тогда ack не шлём и агент повторит доставку.
	OnRunResult func(machineID int64, f Frame) error
	// OnConnect зовётся после регистрации соединения агента (например, для
	// доставки прогонов, скопившихся в очереди, пока машина была офлайн).
	OnConnect func(machineID int64)
	// OnRunLog — строка живого лога прогона (append).
	OnRunLog func(machineID, runID int64, line string)
}

func NewHub() *Hub {
	return &Hub{conns: map[int64]*agentConn{}}
}

type agentConn struct {
	hub       *Hub
	machineID int64
	ws        *websocket.Conn
	writeMu   sync.Mutex
	mu        sync.Mutex
	seq       uint64
	pending   map[uint64]chan *Frame
	closed    bool
}

func (h *Hub) Online(machineID int64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.conns[machineID] != nil
}

// Serve обслуживает соединение агента до его закрытия (блокирует).
func (h *Hub) Serve(machineID int64, ws *websocket.Conn) {
	c := &agentConn{hub: h, machineID: machineID, ws: ws, pending: map[uint64]chan *Frame{}}
	h.mu.Lock()
	if old := h.conns[machineID]; old != nil {
		old.close() // одно соединение на машину: новое вытесняет старое
	}
	h.conns[machineID] = c
	h.mu.Unlock()

	if h.OnConnect != nil {
		go h.OnConnect(machineID)
	}

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
	for _, ch := range c.pending {
		close(ch)
	}
	c.pending = map[uint64]chan *Frame{}
	c.mu.Unlock()
	_ = c.ws.Close()
}

func (c *agentConn) readLoop() {
	c.ws.SetReadLimit(8 << 20) // результат прогона может быть большим
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
		var f Frame
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		switch f.Kind {
		case "resp": // ответ на синхронный вызов
			c.mu.Lock()
			ch := c.pending[f.ID]
			delete(c.pending, f.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- &f
			}
		case "run_status":
			if c.hub.OnRunStatus != nil {
				c.hub.OnRunStatus(c.machineID, f.RunID, f.Status)
			}
		case "run_log":
			if c.hub.OnRunLog != nil {
				c.hub.OnRunLog(c.machineID, f.RunID, f.Output)
			}
		case "run_result":
			if c.hub.OnRunResult != nil {
				if err := c.hub.OnRunResult(c.machineID, f); err == nil {
					_ = c.write(Frame{Kind: "run_ack", RunID: f.RunID})
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

func (c *agentConn) write(f Frame) error {
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.ws.WriteMessage(websocket.TextMessage, data)
}

// Call — синхронный вызов (check) с ожиданием ответа.
func (h *Hub) Call(ctx context.Context, machineID int64, f Frame, timeout time.Duration) (*Frame, error) {
	h.mu.RLock()
	c := h.conns[machineID]
	h.mu.RUnlock()
	if c == nil {
		return nil, ErrOffline
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrOffline
	}
	c.seq++
	f.ID = c.seq
	ch := make(chan *Frame, 1)
	c.pending[f.ID] = ch
	c.mu.Unlock()

	if err := c.write(f); err != nil {
		c.drop(f.ID)
		return nil, ErrOffline
	}
	select {
	case <-ctx.Done():
		c.drop(f.ID)
		return nil, ctx.Err()
	case <-time.After(timeout):
		c.drop(f.ID)
		return nil, ErrTimeout
	case resp := <-ch:
		if resp == nil {
			return nil, ErrOffline
		}
		return resp, nil
	}
}

// Dispatch отправляет прогон агенту (без ожидания результата).
func (h *Hub) Dispatch(machineID int64, f Frame) error {
	h.mu.RLock()
	c := h.conns[machineID]
	h.mu.RUnlock()
	if c == nil {
		return ErrOffline
	}
	if err := c.write(f); err != nil {
		return ErrOffline
	}
	return nil
}

func (c *agentConn) drop(id uint64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}
