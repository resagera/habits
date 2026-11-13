package main

// «⚠ Не читается» — битые и недокачанные файлы видеобиблиотек.
//
// Раньше они молча выпадали: разбор качества пропускал видео, которое
// ffprobe не осилил, а файл загрузки «….mp4.part» не видео вовсе, и его не
// показывал никто. Так в «Аниме» месяц пролежал обрывок неудавшейся отправки
// на 1,5 ГБ и занимал имя сериала, не давая перевезти туда настоящий.
//
// Диск отдельно не обходим: обход файлов (fileindex.go) уже разбирает каждое
// видео и помнит итог. Видео без кодека картинки — значит, ffprobe его не
// разобрал (оборван, битый) или в нём нет видеодорожки.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// partialExt — файлы, которые пишет недоделанная загрузка: торрент-клиенты,
// браузеры, наша собственная отправка.
var partialExt = map[string]bool{".part": true, ".partial": true, ".!qb": true, ".!ut": true,
	".crdownload": true, ".download": true, ".tmp": true}

type brokenItem struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"` // внутри библиотеки
	Lib    string `json:"lib"`
	Size   int64  `json:"size"`
	MTime  int64  `json:"mtime"`
	Reason string `json:"reason"`
}

// brokenReason — почему файл не годится, или пусто, если годится.
func brokenReason(path string, r fileRec) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case partialExt[ext]:
		return "недокачано — это файл незаконченной загрузки"
	case r.Kind != "video":
		return ""
	case r.Size == 0:
		return "пустой файл"
	case r.VC == "":
		return "не читается — файл битый или оборван на середине"
	}
	return ""
}

func (s *server) brokenFiles() []brokenItem {
	out := []brokenItem{}
	for _, lib := range s.cfg.roots {
		if !isVideoLib(lib.Kind) || !lib.available() {
			continue
		}
		for _, p := range s.index.under(lib.Path) {
			r, ok := s.index.get(p)
			if !ok {
				continue
			}
			why := brokenReason(p, r)
			if why == "" {
				continue
			}
			id := shortID(p)
			agentStore.rememberPath(id, p)
			rel, _ := filepath.Rel(lib.Path, p)
			out = append(out, brokenItem{ID: id, Name: filepath.Base(p), Path: rel, Lib: lib.Title,
				Size: r.Size, MTime: r.MTime, Reason: why})
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Size > out[b].Size })
	return out
}

// GET /api/admin/broken
func (s *server) adminBroken(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.brokenFiles()})
}

// reportBroken — после обхода: новые битые файлы — в отчёт задачи и одним
// сообщением в Telegram. Про уже известные молчим, иначе каждый обход (раз в
// 6 часов) повторял бы одно и то же. Список известных — broken-seen.json.
func (s *server) reportBroken(t *task) {
	list := s.brokenFiles()
	file := filepath.Join(s.cfg.cache, "broken-seen.json")
	seen := map[string]bool{}
	if b, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(b, &seen)
	}
	now := map[string]bool{}
	var fresh []brokenItem
	for _, it := range list {
		now[it.ID] = true
		if !seen[it.ID] {
			fresh = append(fresh, it)
		}
	}
	if b, err := json.Marshal(now); err == nil {
		_ = os.WriteFile(file, b, 0o644)
	}
	if len(list) > 0 {
		t.say("не читается: %d — админка → Обслуживание → «⚠ Не читается»", len(list))
	}
	if len(fresh) == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "⚠ Обход файлов нашёл то, что не сыграет (%d):", len(fresh))
	for i, it := range fresh {
		if i == 8 {
			fmt.Fprintf(&b, "\n…и ещё %d", len(fresh)-i)
			break
		}
		fmt.Fprintf(&b, "\n• %s / %s — %s", it.Lib, it.Path, it.Reason)
	}
	b.WriteString("\nУбрать — в админке на ТВ: Обслуживание → «⚠ Не читается».")
	s.notifyAsync(b.String())
}
