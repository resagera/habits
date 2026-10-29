package main

// Кадры из фильма для карточки «Инфо».
//
// Постер показывает, как фильм продавали, а кадры — как он выглядит. Берём их
// сами: ffmpeg вынимает десяток картинок из середины (начало и конец не берём —
// там заставка и титры), слишком тёмные пропускаем, иначе половина кадров была
// бы чёрным экраном.
//
// У сериала кадры — на весь сериал, а не на каждую серию: незачем сто картинок
// у «Тайного города», да и разбирать их полчаса. Берём по кадру из десятка
// случайных серий — разных сезонов, если они есть.
//
// Свои кадры добавляются из админки: с текущего места серии или файлом с
// компьютера. Они лежат рядом с автоматическими и в списке идут первыми.
//
// Хранится всё в кэше агента (`frames/<id>/`), а не рядом с фильмом: библиотека
// остаётся чистой, а потерять кадры не страшно — соберутся заново. За
// переименованием папка едет следом (moveFrames).

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	frameCount   = 10   // сколько кадров собираем сами
	frameWidth   = 960  // ширина картинки: на экран хватает, весит немного
	frameFrom    = 0.08 // с какой доли фильма начинаем брать
	frameTo      = 0.92 // и до какой
	frameDarkMin = 18   // средняя яркость темнее — считаем кадр пустым
	frameRetries = 3    // столько раз отступаем от тёмного места
)

func (s *server) framesDir(id string) string {
	return filepath.Join(s.cfg.cache, "frames", id)
}

// frameNames — файлы кадров: свои сначала, дальше собранные агентом.
func (s *server) frameNames(id string) []string {
	entries, err := os.ReadDir(s.framesDir(id))
	if err != nil {
		return nil
	}
	own, auto := []string{}, []string{}
	for _, e := range entries {
		switch name := e.Name(); {
		case e.IsDir() || !strings.HasSuffix(name, ".jpg"):
		case strings.HasPrefix(name, "own-"):
			own = append(own, name)
		case strings.HasPrefix(name, "auto-"):
			auto = append(auto, name)
		}
	}
	sort.Strings(own)
	sort.Strings(auto)
	return append(own, auto...)
}

func (s *server) hasAutoFrames(id string) bool {
	for _, n := range s.frameNames(id) {
		if strings.HasPrefix(n, "auto-") {
			return true
		}
	}
	return false
}

// moveFrames — кадры переезжают за переименованием: ключ у них от пути.
func (s *server) moveFrames(old, neu string) {
	from, to := s.framesDir(shortID(old)), s.framesDir(shortID(neu))
	if _, err := os.Stat(from); err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(to), 0o700)
	_ = os.RemoveAll(to)
	_ = os.Rename(from, to)
}

func (s *server) dropFrames(id string) { _ = os.RemoveAll(s.framesDir(id)) }

// --- сборка ---

// grabFrame вынимает один кадр и проверяет, что он не чёрный: у фильма в
// случайном месте легко попасть в затемнение между сценами.
func grabFrame(ctx context.Context, src, dst string, at, step float64) error {
	tmp := dst + ".tmp.jpg"
	defer os.Remove(tmp)
	var lastErr error = errors.New("ffmpeg не вынул кадр")
	for try := 0; try < frameRetries; try++ {
		pos := at + float64(try)*step
		cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-y", "-ss", strconv.FormatFloat(pos, 'f', 1, 64),
			"-i", src, "-frames:v", "1",
			"-vf", fmt.Sprintf("scale=w='min(%d,iw)':h=-2", frameWidth), "-q:v", "3", tmp)
		if err := cmd.Run(); err != nil || !fileReady(tmp) {
			lastErr = errors.New("ffmpeg не вынул кадр")
			continue
		}
		if brightness(tmp) < frameDarkMin {
			lastErr = errors.New("кадр слишком тёмный")
			continue
		}
		return os.Rename(tmp, dst)
	}
	return lastErr
}

// brightness — средняя яркость картинки, 0..255.
func brightness(path string) float64 {
	img, err := decodeFit(path, 64, 64)
	if err != nil {
		return 255 // прочитать не смогли — пусть кадр считается годным
	}
	sum, n := 0.0, 0
	for i := 0; i+3 < len(img.Pix); i += 4 {
		sum += 0.299*float64(img.Pix[i]) + 0.587*float64(img.Pix[i+1]) + 0.114*float64(img.Pix[i+2])
		n++
	}
	if n == 0 {
		return 255
	}
	return sum / float64(n)
}

func (s *server) duration(path string) float64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	if info := agentStore.probe(path, st); info != nil {
		return info.Duration
	}
	return 0
}

// videosUnder — серии сериала (или ролики папки) в порядке обхода.
func videosUnder(dir string) []string {
	subs, vids, _ := dirContents(dir)
	out := append([]string{}, vids...)
	for _, sub := range subs {
		_, inner, _ := dirContents(sub)
		out = append(out, inner...)
	}
	return out
}

// framePlan — из каких файлов и с каких секунд брать кадры.
func (s *server) framePlan(path string, isDir bool) []struct {
	file string
	at   float64
} {
	type shot = struct {
		file string
		at   float64
	}
	out := []shot{}
	if !isDir {
		dur := s.duration(path)
		if dur < 60 {
			return out
		}
		lo, hi := dur*frameFrom, dur*frameTo
		step := (hi - lo) / float64(frameCount)
		for i := range frameCount {
			// точки по всему фильму, но не ровно по сетке: иначе у сериала все
			// кадры приходятся на одинаковые места серии
			out = append(out, shot{path, lo + step*(float64(i)+0.15+0.7*rand.Float64())})
		}
		return out
	}
	files := videosUnder(path)
	if len(files) == 0 {
		return out
	}
	rand.Shuffle(len(files), func(i, j int) { files[i], files[j] = files[j], files[i] })
	for i := range frameCount {
		f := files[i%len(files)]
		dur := s.duration(f)
		if dur < 60 {
			continue
		}
		lo, hi := dur*0.15, dur*0.85
		out = append(out, shot{f, lo + (hi-lo)*rand.Float64()})
	}
	return out
}

// makeFrames собирает кадры для одного элемента каталога.
func (s *server) makeFrames(ctx context.Context, path string, isDir bool, t *task) (int, error) {
	id := shortID(path)
	dir := s.framesDir(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, err
	}
	for _, n := range s.frameNames(id) {
		if strings.HasPrefix(n, "auto-") {
			_ = os.Remove(filepath.Join(dir, n))
		}
	}
	plan := s.framePlan(path, isDir)
	if len(plan) == 0 {
		return 0, errors.New("нечего разбирать: файл короче минуты или в папке нет видео")
	}
	made := 0
	for i, shot := range plan {
		if ctx.Err() != nil {
			return made, ctx.Err()
		}
		if t != nil {
			v := t.snapshot()
			t.set(fmt.Sprintf("%s: кадр %d из %d", filepath.Base(path), i+1, len(plan)), v.Done, v.Total)
		}
		dst := filepath.Join(dir, fmt.Sprintf("auto-%02d.jpg", made+1))
		if err := grabFrame(ctx, shot.file, dst, shot.at, s.duration(shot.file)*0.03); err != nil {
			continue
		}
		made++
	}
	if made == 0 {
		return 0, errors.New("ни один кадр не вышел")
	}
	return made, nil
}

// --- выдача ---

// GET /api/frames?id= — кадры элемента каталога.
func (s *server) frames(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	path, ok := agentStore.pathOf(id)
	if !ok || !s.inRoots(path) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "не найдено"})
		return
	}
	items := []map[string]any{}
	for _, n := range s.frameNames(id) {
		items = append(items, map[string]any{
			"name": n,
			"own":  strings.HasPrefix(n, "own-"),
			"url":  s.cfg.base + "/frame/" + id + "/" + n + "?k=" + s.sign(id),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// GET /frame/{id}/{name}?k= — картинка кадра.
func (s *server) frame(w http.ResponseWriter, r *http.Request) {
	id, name := r.PathValue("id"), r.PathValue("name")
	if !s.validKey(id, r.URL.Query().Get("k")) || strings.ContainsAny(name, `/\`) || !strings.HasSuffix(name, ".jpg") {
		http.NotFound(w, r)
		return
	}
	p := filepath.Join(s.framesDir(id), name)
	if _, err := os.Stat(p); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, p)
}

// --- админка ---

// POST /api/admin/frames/scan {scope: all|films|item, id?} — собрать кадры.
func (s *server) adminFramesScan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope string `json:"scope"`
		ID    string `json:"id"`
		Redo  bool   `json:"redo"` // пересобрать даже там, где кадры уже есть
	}
	if !decodeBody(w, r, &req) {
		return
	}
	type target struct {
		path  string
		isDir bool
		name  string
	}
	targets := []target{}
	add := func(t catTile) {
		p, ok := agentStore.pathOf(t.ID)
		if !ok {
			return
		}
		st, err := os.Stat(p)
		if err != nil {
			return
		}
		if !req.Redo && s.hasAutoFrames(shortID(p)) {
			return
		}
		targets = append(targets, target{p, st.IsDir(), t.Name})
	}
	title := ""
	switch req.Scope {
	case "item":
		p, ok := s.adminPath(w, req.ID)
		if !ok {
			return
		}
		st, err := os.Stat(p)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "не найдено"})
			return
		}
		targets = append(targets, target{p, st.IsDir(), filepath.Base(p)})
		title = filepath.Base(p)
	case "films":
		cat := s.buildCatalog()
		for _, t := range cat.Films {
			add(t)
		}
		title = "фильмы"
	case "", "all":
		cat := s.buildCatalog()
		for _, t := range cat.Films {
			add(t)
		}
		for _, t := range cat.Series {
			add(t)
		}
		title = "вся медиатека"
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scope: all, films или item"})
		return
	}
	if len(targets) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"nothing": true,
			"error": "кадры уже собраны — пересобрать можно у отдельного фильма"})
		return
	}
	s.startTask(w, "frames", "Кадры: "+title, func(ctx context.Context, t *task) error {
		t.set("начинаем", 0, len(targets))
		for _, tgt := range targets {
			if ctx.Err() != nil {
				return nil
			}
			n, err := s.makeFrames(ctx, tgt.path, tgt.isDir, t)
			switch {
			case ctx.Err() != nil:
				return nil
			case err != nil:
				t.say("%s — не вышло: %v", tgt.name, err)
			default:
				t.say("%s — кадров: %d", tgt.name, n)
			}
			t.step(tgt.name)
		}
		return nil
	})
}

// POST /api/admin/frames/add {file, at} — кадр с текущего места серии.
func (s *server) adminFrameAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File string  `json:"file"`
		At   float64 `json:"at"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	src, ok := s.adminPath(w, req.File)
	if !ok {
		return
	}
	o := s.ownerOf(src)
	owner, ok := agentStore.pathOf(o.key)
	if !ok {
		owner = src
	}
	id := shortID(owner)
	dir := s.framesDir(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	dst := filepath.Join(dir, fmt.Sprintf("own-%d.jpg", time.Now().UnixNano()/1e6))
	if err := grabFrame(r.Context(), src, dst, max(0, req.At), 0); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "кадр не вышел: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": filepath.Base(dst)})
}

// POST /api/admin/frames/delete {id, name} — убрать кадр; без name — все.
func (s *server) adminFrameDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if _, ok := s.adminPath(w, req.ID); !ok {
		return
	}
	if req.Name == "" {
		s.dropFrames(req.ID)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if strings.ContainsAny(req.Name, `/\`) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "имя кадра"})
		return
	}
	_ = os.Remove(filepath.Join(s.framesDir(req.ID), req.Name))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// POST /api/admin/frames/cover {id, name} — кадр как обложка плитки.
func (s *server) adminFrameCover(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	path, ok := s.adminPath(w, req.ID)
	if !ok {
		return
	}
	src := filepath.Join(s.framesDir(req.ID), filepath.Base(req.Name))
	dst := coverPath(path)
	if err := renderJPEG(src, dst, coverSide, coverSide, 86); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "кадр не читается: " + err.Error()})
		return
	}
	dropOtherPosters(dst)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
