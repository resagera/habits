package main

// Фоновые задачи агента: разбор звука для заставки, сборка кадров для «Инфо».
//
// Такая работа идёт минутами и десятками минут, поэтому она не должна висеть на
// запросе: страница её запускает, а потом спрашивает, как дела. Одновременно
// делаем ОДНУ задачу — и ffmpeg, и чтение с внешнего диска упираются в тот же
// мини-сервер, на котором идёт просмотр; две задачи разом просто отберут у
// зрителя картинку.
//
// Список последних задач держим в памяти: он нужен, чтобы посмотреть, чем
// кончилось, а переживать перезапуск агента ему незачем.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const tasksKept = 20

// taskView — задача так, как её видит страница.
type taskView struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`  // frames | marks
	Title   string   `json:"title"` // что разбираем
	Stage   string   `json:"stage"` // что делаем прямо сейчас
	Done    int      `json:"done"`
	Total   int      `json:"total"`
	Lines   []string `json:"lines"` // отчёт: по строке на разобранный элемент
	Err     string   `json:"error,omitempty"`
	Running bool     `json:"running"`
	Started int64    `json:"started"`
	Ended   int64    `json:"ended,omitempty"`
}

// task — та же задача изнутри: поля под замком, отдельно способ её остановить.
type task struct {
	mu     sync.Mutex
	v      taskView
	cancel context.CancelFunc
}

func (t *task) set(stage string, done, total int) {
	t.mu.Lock()
	t.v.Stage, t.v.Done, t.v.Total = stage, done, total
	t.mu.Unlock()
}

func (t *task) step(stage string) {
	t.mu.Lock()
	t.v.Stage, t.v.Done = stage, t.v.Done+1
	t.mu.Unlock()
}

func (t *task) say(format string, args ...any) {
	t.mu.Lock()
	t.v.Lines = append(t.v.Lines, fmt.Sprintf(format, args...))
	t.mu.Unlock()
}

// snapshot — копия для ответа: сам список строк дописывается дальше, а
// кодировать его будут уже без замка.
func (t *task) snapshot() taskView {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := t.v
	out.Lines = append([]string{}, t.v.Lines...)
	return out
}

type taskStore struct {
	mu    sync.Mutex
	items []*task // свежие в начале
	next  int
}

func newTaskStore() *taskStore { return &taskStore{} }

func (ts *taskStore) running() *task {
	for _, t := range ts.items {
		if t.snapshot().Running {
			return t
		}
	}
	return nil
}

// start ставит задачу в работу. Вторую одновременно не берём — говорим, чем
// заняты.
func (ts *taskStore) start(kind, title string, run func(ctx context.Context, t *task) error) (*task, error) {
	ts.mu.Lock()
	if busy := ts.running(); busy != nil {
		ts.mu.Unlock()
		return nil, fmt.Errorf("уже идёт: %s", busy.snapshot().Title)
	}
	ts.next++
	ctx, cancel := context.WithCancel(context.Background())
	t := &task{cancel: cancel, v: taskView{ID: "t" + strconv.Itoa(ts.next), Kind: kind, Title: title,
		Stage: "начинаем", Lines: []string{}, Running: true, Started: time.Now().Unix()}}
	ts.items = append([]*task{t}, ts.items...)
	if len(ts.items) > tasksKept {
		ts.items = ts.items[:tasksKept]
	}
	ts.mu.Unlock()

	go func() {
		err := run(ctx, t)
		t.mu.Lock()
		t.v.Running, t.v.Ended = false, time.Now().Unix()
		switch {
		case err != nil:
			t.v.Err, t.v.Stage = err.Error(), "не доделано"
		case ctx.Err() != nil:
			t.v.Err, t.v.Stage = "остановлено", "остановлено"
		default:
			t.v.Stage = "готово"
		}
		t.mu.Unlock()
		cancel()
	}()
	return t, nil
}

func (ts *taskStore) list() []taskView {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	out := make([]taskView, 0, len(ts.items))
	for _, t := range ts.items {
		out = append(out, t.snapshot())
	}
	return out
}

func (ts *taskStore) stop(id string) bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for _, t := range ts.items {
		if v := t.snapshot(); v.ID == id && v.Running {
			t.cancel()
			return true
		}
	}
	return false
}

// GET /api/admin/tasks — что делается и что делалось.
func (s *server) adminTasks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tasks": s.tasks.list()})
}

// POST /api/admin/tasks/{id}/stop
func (s *server) adminTaskStop(w http.ResponseWriter, r *http.Request) {
	if !s.tasks.stop(r.PathValue("id")) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "такая задача не идёт"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// startTask — общий ответ на запуск: сама задача или отказ, если агент занят.
func (s *server) startTask(w http.ResponseWriter, kind, title string, run func(ctx context.Context, t *task) error) {
	t, err := s.tasks.start(kind, title, run)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": t.snapshot()})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(v) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "не разобрал запрос"})
		return false
	}
	return true
}
