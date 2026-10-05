// Package deadlinks — фоновая проверка битых ссылок (опция links.dead_check):
// раз в час берёт ссылки пользователей с выданной опцией, не проверявшиеся
// более 12 часов, ходит по URL и помечает мёртвые.
package deadlinks

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"streaks-backend/internal/store"
)

const (
	tickInterval = time.Hour
	startDelay   = 2 * time.Minute
	batchLimit   = 200
	concurrency  = 8
	reqTimeout   = 15 * time.Second
)

type Worker struct {
	Store  *store.Store
	Logger *slog.Logger
}

func (w *Worker) Run(ctx context.Context) {
	// первый прогон вскоре после старта, дальше раз в час
	select {
	case <-ctx.Done():
		return
	case <-time.After(startDelay):
	}
	w.tick(ctx)
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	candidates, err := w.Store.DeadCheckCandidates(ctx, batchLimit)
	if err != nil {
		w.Logger.Error("deadlinks: candidates", "error", err)
		return
	}
	if len(candidates) == 0 {
		return
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	checked, marked := 0, 0
	var mu sync.Mutex
	for _, c := range candidates {
		wg.Add(1)
		sem <- struct{}{}
		go func(c store.DeadCheckCandidate) {
			defer wg.Done()
			defer func() { <-sem }()
			dead := IsDead(ctx, c.URL)
			if err := w.Store.SetLinkDead(ctx, c.ID, dead); err != nil {
				w.Logger.Error("deadlinks: save", "id", c.ID, "error", err)
				return
			}
			mu.Lock()
			checked++
			if dead {
				marked++
			}
			mu.Unlock()
		}(c)
	}
	wg.Wait()
	w.Logger.Info("deadlinks: pass done", "checked", checked, "dead", marked)
}

// IsDead: HEAD, при отказе — GET; мёртвой считаем сетевую ошибку или 4xx/5xx
// (кроме 401/403/405/429 — сайт жив, просто не пускает робота).
func IsDead(ctx context.Context, url string) bool {
	status, err := probe(ctx, http.MethodHead, url)
	if err != nil || status == http.StatusMethodNotAllowed {
		status, err = probe(ctx, http.MethodGet, url)
	}
	if err != nil {
		return true
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden,
		http.StatusMethodNotAllowed, http.StatusTooManyRequests:
		return false
	}
	return status >= 400
}

func probe(ctx context.Context, method, url string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, reqTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; HabitsLinkCheck/1.0)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}
