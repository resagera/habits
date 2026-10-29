package main

// Оформление: фон страницы, обложки, настройки плеера из админки.
//
// Фон — либо одна картинка в кэше агента (app/background.jpg), уменьшенная под
// экран, либо целый альбом «Фото» (или папка внутри него): тогда агент отдаёт
// странице горсть случайных снимков, и обои меняются сами — по таймеру и при
// каждой перезагрузке.
//
// Размытие, затемнение, масштаб и насыщенность делает страница (CSS): картинку
// можно подстроить ползунком, не пересобирая её. Источник одиночной картинки —
// файл, присланный из браузера (на ноутбуке), или снимок из раздела «Фото»: с
// пульта файл не выбрать, а альбомы — вот они.
//
// Обложка — та же картинка, но для плитки: у папки она ложится скрытым
// .cover.jpg внутрь (переезжает и удаляется вместе с папкой, а в альбоме фото
// не станет лишним снимком), у файла — рядом с ним под тем же именем, как
// найденный постер.

import (
	"encoding/json"
	"errors"
	"io"
	"math/rand"
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
	// фон храним крупнее экранной копии снимка: на 4K-телевизоре картинка
	// в 1920 точек растягивается вдвое и выглядит мылом — это и есть тот
	// самый «сильно увеличивается»
	bgWidth  = 2560
	bgHeight = 1440
	// сколько случайных снимков альбома отдаём странице за раз: список едет
	// в каждом ответе настроек, а крутить их всё равно будут десятками
	bgPickCount = 60
)

// looks — то, что страница берёт при запуске: фон и поведение плеера.
type looks struct {
	BgBlur int    `json:"bg_blur"` // px, 0 — без размытия
	BgDim  int    `json:"bg_dim"`  // %, насколько притемнить фон под плитками
	Arrows string `json:"arrows"`  // во весь экран ↑↓: seek (±5 мин) | volume
	BgVer  int64  `json:"bg_ver"`  // время смены фона — в ссылке, против кэша
	// Альбом вместо одной картинки: путь папки раздела «Фото». Пусто — фон
	// одиночный, как раньше.
	BgAlbum  string `json:"bg_album,omitempty"`
	BgEvery  int    `json:"bg_every"`  // минут между сменами обоев, 0 — только при перезагрузке
	BgFit    string `json:"bg_fit"`    // cover (заполнить) | contain (целиком)
	BgZoom   int    `json:"bg_zoom"`   // %, 100…200 поверх выбранного вписывания
	BgSat    int    `json:"bg_sat"`    // насыщенность, %, 50…200
	BgPan    bool   `json:"bg_pan"`    // медленный наезд
	BgPoster *bool  `json:"bg_poster"` // постер сериала вместо фона на его экране
}

// posterBackdrop — указатель, чтобы отличить «выключено» от «в старом файле
// настроек такого поля вовсе не было».
func (v looks) posterBackdrop() bool { return v.BgPoster == nil || *v.BgPoster }

type looksStore struct {
	mu   sync.Mutex
	path string
	v    looks
}

func newLooksStore(cacheDir string) *looksStore {
	l := &looksStore{path: filepath.Join(cacheDir, "looks.json"),
		v: looks{BgBlur: 6, BgDim: 40, Arrows: "seek", BgFit: "cover", BgZoom: 100, BgSat: 100}}
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

// albumBackgrounds — горсть случайных снимков альбома в экранном размере.
// Берём уже готовые копии раздела «Фото» (тот же /photo/{id}?s=view): они
// кэшированы и ровно того размера, что нужен под обои.
//
// Случайность здесь, а не на странице: так каждая перезагрузка приносит новый
// набор, даже если в альбоме тысяча снимков, а в ответ уезжает полсотни ссылок,
// а не мегабайт.
func (s *server) albumBackgrounds(dir string) []string {
	if dir == "" || !s.inRoots(dir) {
		return nil
	}
	var files []string
	var walk func(d string, depth int)
	walk = func(d string, depth int) {
		dirs, items := photoDir(d)
		for _, p := range items {
			// ролики пропускаем: кадр из них пришлось бы вынимать ffmpeg-ом
			// на каждый новый, а обоев и так хватает
			if !isVideoFile(p) {
				files = append(files, p)
			}
		}
		if depth >= photoDepth {
			return
		}
		for _, sub := range dirs {
			walk(sub, depth+1)
		}
	}
	walk(dir, 1)
	rand.Shuffle(len(files), func(i, j int) { files[i], files[j] = files[j], files[i] })
	if len(files) > bgPickCount {
		files = files[:bgPickCount]
	}
	out := make([]string, 0, len(files))
	for _, p := range files {
		out = append(out, s.photoURL(p, "view"))
	}
	return out
}

// GET /api/settings — оформление для страницы (без PIN: его видит каждый экран).
func (s *server) settings(w http.ResponseWriter, r *http.Request) {
	v := s.looks.get()
	bg := ""
	if fileReady(s.backgroundPath()) {
		bg = s.cfg.base + "/background?v=" + strconv.FormatInt(v.BgVer, 36)
	}
	list := s.albumBackgrounds(v.BgAlbum)
	if len(list) > 0 {
		bg = list[0] // страница постарше про список не знает — пусть будет хоть один
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"background": bg, "backgrounds": list,
		"bg_blur": v.BgBlur, "bg_dim": v.BgDim, "arrows": v.Arrows,
		"bg_every": v.BgEvery, "bg_fit": v.BgFit, "bg_zoom": v.BgZoom, "bg_sat": v.BgSat,
		"bg_pan": v.BgPan, "bg_poster": v.posterBackdrop(),
		"bg_album": v.BgAlbum != "", "bg_album_name": albumName(v.BgAlbum), "bg_album_count": len(list),
	})
}

func albumName(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Base(dir)
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

// POST /api/admin/settings {bg_blur?, bg_dim?, arrows?, bg_every?, bg_fit?,
// bg_zoom?, bg_sat?, bg_pan?, bg_poster?, bg_album_id?}
func (s *server) adminSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BgBlur   *int    `json:"bg_blur"`
		BgDim    *int    `json:"bg_dim"`
		Arrows   *string `json:"arrows"`
		BgEvery  *int    `json:"bg_every"`
		BgFit    *string `json:"bg_fit"`
		BgZoom   *int    `json:"bg_zoom"`
		BgSat    *int    `json:"bg_sat"`
		BgPan    *bool   `json:"bg_pan"`
		BgPoster *bool   `json:"bg_poster"`
		AlbumID  *string `json:"bg_album_id"` // пустая строка — вернуться к одной картинке
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный запрос"})
		return
	}
	album := ""
	if req.AlbumID != nil && *req.AlbumID != "" {
		p, ok := agentStore.pathOf(*req.AlbumID)
		if !ok || !s.inRoots(p) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "папка не найдена"})
			return
		}
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "это не папка"})
			return
		}
		if len(s.albumBackgrounds(p)) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "в этой папке нет снимков"})
			return
		}
		album = p
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
		if req.BgEvery != nil {
			v.BgEvery = clamp(*req.BgEvery, 0, 1440)
		}
		if req.BgFit != nil && (*req.BgFit == "cover" || *req.BgFit == "contain") {
			v.BgFit = *req.BgFit
		}
		if req.BgZoom != nil {
			v.BgZoom = clamp(*req.BgZoom, 100, 200)
		}
		if req.BgSat != nil {
			v.BgSat = clamp(*req.BgSat, 0, 200)
		}
		if req.BgPan != nil {
			v.BgPan = *req.BgPan
		}
		if req.BgPoster != nil {
			on := *req.BgPoster
			v.BgPoster = &on
		}
		if req.AlbumID != nil {
			v.BgAlbum = album
			v.BgVer = time.Now().Unix()
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
		d, ok := s.coverDest(w, r.URL.Query().Get("id"))
		if !ok {
			return
		}
		dst = d
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
	maxW, maxH := bgWidth, bgHeight
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
		// альбом убираем вместе с картинкой: «✖ Убрать» значит «без фона»
		s.looks.update(func(v *looks) { v.BgAlbum, v.BgVer = "", time.Now().Unix() })
	case "cover":
		d, ok := s.coverDest(w, r.URL.Query().Get("id"))
		if !ok {
			return
		}
		_ = os.Remove(d)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target: background или cover"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// coverDest — куда класть обложку по id из админки. У папки-подборки своего
// места на диске нет, её обложка живёт в кэше агента.
func (s *server) coverDest(w http.ResponseWriter, id string) (string, bool) {
	if c := s.colls.find(id); c != nil {
		return s.collCoverPath(c.ID), true
	}
	p, ok := s.adminPath(w, id)
	if !ok {
		return "", false
	}
	return coverPath(p), true
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
