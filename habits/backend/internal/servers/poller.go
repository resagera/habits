// Package servers — поллер мониторинга: раз в минуту опрашивает адреса
// добавленных pull-серверов (агент отвечает JSON-метриками), сохраняет
// снапшот и точку истории CPU/RAM для графиков. Push-машины (домашние,
// без внешнего IP) сами шлют отчёты на /api/v1/agent/push; поллер лишь
// следит за свежестью данных и шлёт ботом offline/online-уведомления —
// это касается и pull, и push.
package servers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

const (
	pollInterval = time.Minute
	pollTimeout  = 10 * time.Second
	maxBody      = 256 * 1024
	concurrency  = 8

	// OfflineAfter — без удачного отчёта дольше этого срока сервер
	// считается offline (и владельцу уходит одно уведомление ботом).
	OfflineAfter = 3 * time.Minute
)

// AgentReport — формат ответа агента (habits/agent).
type AgentReport struct {
	CPUPct float32 `json:"cpu_pct"`
	RAM    struct {
		Total int64 `json:"total"`
		Used  int64 `json:"used"`
	} `json:"ram"`
}

type Poller struct {
	Store  *store.Store
	Bot    *notify.Bot
	Logger *slog.Logger
}

func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

func (p *Poller) tick(ctx context.Context) {
	targets, err := p.Store.AllPollTargets(ctx)
	if err != nil {
		p.Logger.Error("servers: list targets", "error", err)
		return
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(t store.PollTarget) {
			defer wg.Done()
			defer func() { <-sem }()
			p.poll(ctx, t)
		}(t)
	}
	wg.Wait()
	p.notifyOffline(ctx)
	p.evalAlerts(ctx)
	if err := p.Store.PruneServerSamples(ctx); err != nil {
		p.Logger.Error("servers: prune", "error", err)
	}
}

func (p *Poller) poll(ctx context.Context, t store.PollTarget) {
	data, err := Fetch(ctx, t.URL, t.Token)
	if err != nil {
		if _, saveErr := p.Store.SavePollResult(ctx, t.ID, nil, 0, 0, 0, err.Error()); saveErr != nil {
			p.Logger.Error("servers: save error result", "id", t.ID, "error", saveErr)
		}
		return
	}
	var report AgentReport
	if err := json.Unmarshal(data, &report); err != nil {
		_, _ = p.Store.SavePollResult(ctx, t.ID, nil, 0, 0, 0, "агент вернул не-JSON")
		return
	}
	out, err := p.Store.SavePollResult(ctx, t.ID, data, report.CPUPct, report.RAM.Used, report.RAM.Total, "")
	if err != nil {
		p.Logger.Error("servers: save result", "id", t.ID, "error", err)
		return
	}
	NotifyBackOnline(ctx, p.Bot, out)
}

// notifyOffline помечает серверы без свежих отчётов и шлёт владельцам
// по одному сообщению на событие.
func (p *Poller) notifyOffline(ctx context.Context) {
	gone, err := p.Store.MarkOfflineServers(ctx, OfflineAfter)
	if err != nil {
		p.Logger.Error("servers: mark offline", "error", err)
		return
	}
	for _, srv := range gone {
		mins := int(time.Since(srv.LastOkAt).Minutes())
		text := fmt.Sprintf("🔴 Сервер «%s» не выходит на связь (последние данные — %d мин назад)",
			srv.Name, mins)
		if err := p.Bot.SendMessage(ctx, srv.UserID, text); err != nil {
			p.Logger.Error("servers: offline notify", "id", srv.ID, "error", err)
		}
	}
}

// evalAlerts проверяет пороги (диск/RAM/CPU) для серверов с включёнными
// уведомлениями и шлёт по одному сообщению на событие (флаги *_alerted).
func (p *Poller) evalAlerts(ctx context.Context) {
	cfgs, err := p.Store.AlertConfigs(ctx)
	if err != nil {
		p.Logger.Error("servers: alert configs", "error", err)
		return
	}
	for _, c := range cfgs {
		// offline-серверы не оцениваем: данные устарели, offline — отдельное событие
		if c.LastOkAt == nil || time.Since(*c.LastOkAt) > OfflineAfter {
			continue
		}
		p.evalDisk(ctx, c)
		p.evalSustained(ctx, c, "ram", c.RAMAlerted, c.RAMPct, int(c.RAMMinutes),
			p.Store.SustainedRAM)
		p.evalSustained(ctx, c, "cpu", c.CPUAlerted, c.CPUPct, int(c.CPUMinutes),
			p.Store.SustainedCPU)
	}
}

// evalDisk оценивает каждый мониторимый диск отдельно и шлёт по одному
// сообщению на точку монтирования (fire при уходе ниже порога, recover при
// возврате). Множество «сейчас в алерте» точек хранится в disk_alerted_mounts.
func (p *Poller) evalDisk(ctx context.Context, c store.AlertConfig) {
	if len(c.DiskRules) == 0 {
		return
	}
	freeByMount := diskFreeByMount(c.LastData)
	was := map[string]bool{}
	for _, m := range c.DiskAlertedMounts {
		was[m] = true
	}
	nowLow := map[string]bool{}
	for _, rule := range c.DiskRules {
		free, ok := freeByMount[rule.Mount]
		if !ok {
			continue // диска сейчас нет в отчёте — состояние не меняем
		}
		if free < rule.MinFreeMB*1024*1024 {
			nowLow[rule.Mount] = true
			if !was[rule.Mount] {
				_ = p.Bot.SendMessage(ctx, c.UserID,
					fmt.Sprintf("🟠 Мало места на «%s»: раздел %s — свободно %s (порог %s)",
						c.Name, rule.Mount, fmtBytes(free), fmtMB(rule.MinFreeMB)))
			}
		}
	}
	// восстановившиеся: были в алерте, сейчас не в списке низких (и правило ещё есть)
	rules := map[string]bool{}
	for _, rule := range c.DiskRules {
		rules[rule.Mount] = true
	}
	changed := len(nowLow) != len(was)
	for m := range was {
		if !nowLow[m] {
			changed = true
			if rules[m] { // если правило удалили — молча забываем
				_ = p.Bot.SendMessage(ctx, c.UserID,
					fmt.Sprintf("🟢 Место на «%s»: раздел %s в норме", c.Name, m))
			}
		}
	}
	for m := range nowLow {
		if !was[m] {
			changed = true
		}
	}
	if changed {
		mounts := make([]string, 0, len(nowLow))
		for m := range nowLow {
			mounts = append(mounts, m)
		}
		if err := p.Store.SetDiskAlertedMounts(ctx, c.ID, mounts); err != nil {
			p.Logger.Error("servers: set disk alerted mounts", "id", c.ID, "error", err)
		}
	}
}

func (p *Poller) evalSustained(ctx context.Context, c store.AlertConfig, kind string,
	was bool, thr int16, minutes int,
	check func(context.Context, int64, int, int16) (bool, error)) {
	high, err := check(ctx, c.ID, minutes, thr)
	if err != nil {
		p.Logger.Error("servers: sustained check", "kind", kind, "id", c.ID, "error", err)
		return
	}
	metric := "RAM"
	if kind == "cpu" {
		metric = "CPU"
	}
	p.applyAlert(ctx, c.ID, c.UserID, was, high, kind,
		fmt.Sprintf("🔴 %s на «%s» держится выше %d%% дольше %d мин", metric, c.Name, thr, minutes),
		fmt.Sprintf("🟢 %s на «%s» вернулась в норму", metric, c.Name))
}

// applyAlert шлёт сообщение при смене состояния и обновляет флаг.
func (p *Poller) applyAlert(ctx context.Context, id, userID int64, was, now bool, kind, fireMsg, clearMsg string) {
	switch {
	case now && !was:
		if err := p.Store.SetAlertFlag(ctx, id, kind, true); err != nil {
			p.Logger.Error("servers: set alert flag", "kind", kind, "id", id, "error", err)
			return
		}
		_ = p.Bot.SendMessage(ctx, userID, fireMsg)
	case !now && was:
		if err := p.Store.SetAlertFlag(ctx, id, kind, false); err != nil {
			p.Logger.Error("servers: clear alert flag", "kind", kind, "id", id, "error", err)
			return
		}
		_ = p.Bot.SendMessage(ctx, userID, clearMsg)
	}
}

// diskFreeByMount разбирает снапшот агента в карту «точка монтирования → байт свободно».
func diskFreeByMount(lastData json.RawMessage) map[string]int64 {
	out := map[string]int64{}
	if len(lastData) == 0 {
		return out
	}
	var snap struct {
		Disks []struct {
			Mount string `json:"mount"`
			Free  int64  `json:"free"`
		} `json:"disks"`
	}
	if err := json.Unmarshal(lastData, &snap); err != nil {
		return out
	}
	for _, d := range snap.Disks {
		out[d.Mount] = d.Free
	}
	return out
}

func fmtMB(mb int64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%g ГБ", float64(mb)/1024)
	}
	return fmt.Sprintf("%d МБ", mb)
}

var byteUnits = []string{"Б", "КБ", "МБ", "ГБ", "ТБ"}

func fmtBytes(n int64) string {
	v := float64(n)
	u := 0
	for v >= 1024 && u < len(byteUnits)-1 {
		v /= 1024
		u++
	}
	if v >= 10 || u == 0 {
		return fmt.Sprintf("%.0f %s", v, byteUnits[u])
	}
	return fmt.Sprintf("%.1f %s", v, byteUnits[u])
}

// NotifyBackOnline шлёт «снова online», если машина была помечена offline.
// Используется поллером и хендлерами push/refresh.
func NotifyBackOnline(ctx context.Context, bot *notify.Bot, out store.PollOutcome) {
	if !out.WasOffline {
		return
	}
	_ = bot.SendMessage(ctx, out.UserID, fmt.Sprintf("🟢 Сервер «%s» снова на связи", out.Name))
}

// Fetch запрашивает метрики агента (используется и хендлером «обновить сейчас»).
func Fetch(ctx context.Context, url, token string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("некорректный адрес: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("нет ответа: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("агент ответил %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("агент вернул не-JSON")
	}
	return body, nil
}
