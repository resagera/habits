package main

// Оформление: фон страницы, обложки, настройки плеера из админки.
//
// Фон — одна картинка в кэше агента (app/background.jpg), уменьшенная под экран.
// Размытие и затемнение делает страница (CSS): картинку можно поменять
// ползунком, не пересобирая её. Источник — файл, присланный из браузера (на
// ноутбуке), или снимок из раздела «Фото»: с пульта файл не выбрать, а
// альбомы — вот они.
//
// Обложка — та же картинка, но для плитки: у папки она ложится скрытым
// .cover.jpg внутрь (переезжает и удаляется вместе с папкой, а в альбоме фото
// не станет лишним снимком), у файла — рядом с ним под тем же именем, как
// найденный постер.

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	coverFile     = ".cover.jpg"
	coverSide     = 1000
	uploadMaxSize = 30 << 20
)

// looks — то, что страница берёт при запуске: фон и поведение плеера.
type looks struct {
	BgBlur int    `json:"bg_blur"` // px, 0 — без размытия
	BgDim  int    `json:"bg_dim"`  // %, насколько притемнить фон под плитками
	Arrows string `json:"arrows"`  // во весь экран ↑↓: seek (±5 мин) | volume
	BgVer  int64  `json:"bg_ver"`  // время смены фона — в ссылке, против кэша
}

type looksStore struct {
	mu   sync.Mutex
	path string
	v    looks
}

func newLooksStore(cacheDir string) *looksStore {
	l := &looksStore{path: filepath.Join(cacheDir, "looks.json"), v: looks{BgBlur: 6, BgDim: 40, Arrows: "seek"}}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, &l.v)
	}
	return l
}

func (l *looksStore) get() looks {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.v
}

func (l *looksStore) update(fn func(v *looks)) looks {
	l.mu.Lock()
	defer l.mu.Unlock()
	fn(&l.v)
	if data, err := json.Marshal(l.v); err == nil {
		_ = writeAtomic(l.path, data)
	}
	return l.v
}

func (s *server) backgroundPath() string { return filepath.Join(s.cfg.cache, "app", "background.jpg") }

// GET /api/settings — оформление для страницы (без PIN: его видит каждый экран).
func (s *server) settings(w http.ResponseWriter, r *http.Request) {
	v := s.looks.get()
	bg := ""
	if fileReady(s.backgroundPath()) {
		bg = s.cfg.base + "/background?v=" + strconv.FormatInt(v.BgVer, 36)
	}
	writeJSON(w, http.StatusOK, map[string]any{"background": bg, "bg_blur": v.BgBlur, "bg_dim": v.BgDim, "arrows": v.Arrows})
}

// GET /background — картинка фона. Ссылка меняется вместе с фоном (&v=).
func (s *server) background(w http.ResponseWriter, r *http.Request) {
	if !fileReady(s.backgroundPath()) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=2592000, immutable")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, s.backgroundPath())
}

// POST /api/admin/settings {bg_blur?, bg_dim?, arrows?}
func (s *server) adminSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BgBlur *int    `json:"bg_blur"`
		BgDim  *int    `json:"bg_dim"`
		Arrows *string `json:"arrows"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный запрос"})
		return
	}
	v := s.looks.update(func(v *looks) {
		if req.BgBlur != nil {
			v.BgBlur = clamp(*req.BgBlur, 0, 40)
		}
		if req.BgDim != nil {
			v.BgDim = clamp(*req.BgDim, 0, 90)
		}
		if req.Arrows != nil && (*req.Arrows == "seek" || *req.Arrows == "volume") {
			v.Arrows = *req.Arrows
		}
	})
	writeJSON(w, http.StatusOK, v)
}

func clamp(n, lo, hi int) int { return min(max(n, lo), hi) }

// imageSource — откуда взять картинку: из тела запроса (файл из браузера) или
// снимок библиотеки {photo_id}. У ролика из альбома берётся его кадр.
func (s *server) imageSource(r *http.Request) (src string, cleanup func(), err error) {
	cleanup = func() {}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var req struct {
			PhotoID string `json:"photo_id"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req) != nil || req.PhotoID == "" {
			return "", cleanup, errors.New("нужен photo_id")
		}
		p, ok := agentStore.pathOf(req.PhotoID)
		if !ok || !s.inRoots(p) {
			return "", cleanup, errors.New("снимок не найден")
		}
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			return "", cleanup, errors.New("снимок не найден")
		}
		if isVideoFile(p) {
			frame, err := s.photoVariant(p, st, "view")
			return frame, cleanup, err
		}
		if !decodableExt[strings.ToLower(filepath.Ext(p))] {
			return "", cleanup, errors.New("годятся JPG и PNG")
		}
		return p, cleanup, nil
	}
	// файл из браузера: во временный файл, разбирать будем с диска
	tmp, err := os.CreateTemp(s.cfg.cache, ".upload-*")
	if err != nil {
		return "", cleanup, err
	}
	cleanup = func() { _ = os.Remove(tmp.Name()) }
	n, err := io.Copy(tmp, io.LimitReader(r.Body, uploadMaxSize+1))
	tmp.Close()
	switch {
	case err != nil:
		return "", cleanup, err
	case n > uploadMaxSize:
		return "", cleanup, errors.New("картинка больше 30 МБ")
	case n == 0:
		return "", cleanup, errors.New("пустой файл")
	}
	// тип решает содержимое, а имя временного файла — только для EXIF: JPEG
	// читает поворот, PNG нет
	name := tmp.Name()
	if ct := r.Header.Get("Content-Type"); strings.Contains(ct, "jpeg") {
		_ = os.Rename(name, name+".jpg")
		name += ".jpg"
		cleanup = func() { _ = os.Remove(name) }
	}
	return name, cleanup, nil
}

// POST /api/admin/image?target=background|cover&id= — фон страницы или обложка
// плитки. Тело — картинка (image/*) или {"photo_id": …} в JSON.
func (s *server) adminImage(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	var dst string
	switch target {
	case "background":
		dst = s.backgroundPath()
	case "cover":
		p, ok := s.adminPath(w, r.URL.Query().Get("id"))
		if !ok {
			return
		}
		dst = coverPath(p)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target: background или cover"})
		return
	}
	src, cleanup, err := s.imageSource(r)
	defer cleanup()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	maxW, maxH := viewWidth, viewHeight
	if target == "cover" {
		maxW, maxH = coverSide, coverSide
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := renderJPEG(src, dst, maxW, maxH, 86); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "картинка не читается (годятся JPG и PNG): " + err.Error()})
		return
	}
	if target == "background" {
		s.looks.update(func(v *looks) { v.BgVer = time.Now().Unix() })
	} else {
		dropOtherPosters(dst)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// DELETE /api/admin/image?target=&id= — убрать фон или обложку.
func (s *server) adminImageDelete(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Query().Get("target") {
	case "background":
		_ = os.Remove(s.backgroundPath())
		s.looks.update(func(v *looks) { v.BgVer = time.Now().Unix() })
	case "cover":
		p, ok := s.adminPath(w, r.URL.Query().Get("id"))
		if !ok {
			return
		}
		_ = os.Remove(coverPath(p))
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target: background или cover"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// coverPath — куда класть обложку: папке — скрытый файл внутрь, файлу — рядом.
func coverPath(p string) string {
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		return filepath.Join(p, coverFile)
	}
	return posterBase(p, false) + ".jpg"
}

// dropOtherPosters: у файла обложка — «Имя.jpg»; найденный раньше «Имя.png» или
// «Имя.webp» перебивал бы её, поэтому лишние убираем.
func dropOtherPosters(dst string) {
	if filepath.Base(dst) == coverFile {
		return
	}
	base := strings.TrimSuffix(dst, ".jpg")
	for _, ext := range posterExts {
		if ext != ".jpg" {
			_ = os.Remove(base + ext)
		}
	}
}
