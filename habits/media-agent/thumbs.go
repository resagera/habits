package main

// Превью альбомов заранее. Вид «плиткой» показывает сразу все снимки папки, и
// собирать превью при первом открытии — это минута пустых квадратов на
// большой папке. Поэтому превью всех альбомов собираются в тихие часы (вместе
// с постерами) или кнопкой в админке. Лежат там же, где собранные по
// требованию, — в кэше агента (photos/), а не рядом со снимками: в папке фото
// не появляется чужих файлов, и замена снимка сама даёт новое превью.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// thumbNeeded — у файла нет превью и прошлая сборка не падала.
func (s *server) thumbNeeded(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	if !decodableExt[ext] && !videoExt[ext] {
		return false // такие отдаются как есть
	}
	st, err := os.Stat(p)
	if err != nil {
		return false
	}
	thumb, _ := s.photoCopyPaths(p, st)
	return !fileReady(thumb) && !exists(thumb+".miss")
}

// thumbTargets — папки фотобиблиотек, где превью собраны не у всех.
func (s *server) thumbTargets() []string {
	var out []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		dirs, files := photoDir(dir)
		for _, f := range files {
			if s.thumbNeeded(f) {
				out = append(out, dir)
				break
			}
		}
		if depth < photoDepth {
			for _, d := range dirs {
				walk(d, depth+1)
			}
		}
	}
	for _, lib := range s.cfg.roots {
		if lib.Kind == "photo" && lib.available() {
			walk(lib.Path, 0)
		}
	}
	return out
}

// quietThumbs — превью одной папки. Не собралось (битый файл, ролик без
// кадра) — отметка рядом с превью, чтобы не пробовать каждую ночь.
func (s *server) quietThumbs(ctx context.Context, t *task, dir string) error {
	_, files := photoDir(dir)
	failed := 0
	for _, p := range files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !s.thumbNeeded(p) {
			continue
		}
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if _, err := s.photoVariant(p, st, "thumb"); err != nil {
			failed++
			thumb, _ := s.photoCopyPaths(p, st)
			_ = os.MkdirAll(filepath.Dir(thumb), 0o700)
			_ = os.WriteFile(thumb+".miss", nil, 0o644)
		}
	}
	if failed > 0 {
		return fmt.Errorf("не собралось превью у %d файлов", failed)
	}
	return nil
}

// POST /api/admin/thumbs/run — собрать превью альбомов сейчас.
func (s *server) adminThumbsRun(w http.ResponseWriter, r *http.Request) {
	items := s.thumbTargets()
	if len(items) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "делать нечего: превью у всех снимков уже есть"})
		return
	}
	for _, j := range s.quiet.list() {
		if j.Kind == "thumbs" {
			s.quiet.drop(j.ID)
		}
	}
	job := s.quiet.add(quietJob{Kind: "thumbs", Now: true, Items: items,
		Title: fmt.Sprintf("Превью фото: %d папок", len(items))})
	s.startTask(w, "quiet-thumbs", "🖼 "+job.Title, func(ctx context.Context, t *task) error {
		return s.runQuietJob(ctx, t, job.ID)
	})
}
