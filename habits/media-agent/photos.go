package main

// Фото: папки с фотографиями, просмотр на весь экран.
//
// Снимки с фотоаппарата весят 6–8 МБ и имеют 24 Мп. Отдавать их приставке как
// есть нельзя: полоса из семидесяти превью — это полгигабайта по вайфаю и
// гигабайты распакованных картинок в памяти WebView. Поэтому агент готовит две
// копии каждого снимка: превью для полосы и плиток и экранную (не больше
// 1920×1080) для просмотра. Оригинал экрану ТВ ничего не добавляет.
//
// Копии делаются по требованию и кладутся в кэш, ключ — путь, размер и время
// правки: заменили файл под тем же именем — копии соберутся заново. Открыли
// папку — агент в фоне готовит копии всех её снимков, чтобы листание дальше
// шло без ожидания.
//
// Поворот: камера пишет снимок «как лежала матрица» и отмечает в EXIF, как его
// повернуть. Браузер с оригиналом это делает сам, но в копию EXIF не
// переносится — поворот применяется при сборке копии.
//
// Ролики с телефона в альбоме — такие же элементы полосы: превью и «экранная
// копия» для них — кадр, который вынимает ffmpeg, а смотрят сам файл.
// Поворот вертикального ролика ffmpeg применяет сам (autorotate).

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var photoExt = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true}

// decodable — из чего собираем копии. WebP стандартная библиотека не читает,
// а GIF после сборки потерял бы анимацию: такие отдаются как есть, браузер
// приставки понимает оба.
var decodableExt = map[string]bool{".jpg": true, ".jpeg": true, ".png": true}

const (
	photoDepth = 6 // глубже вложенные папки не ищем: хватит любой разумной раскладки
	thumbSide  = 480
	viewWidth  = 1920
	viewHeight = 1080
)

// photoDir — подпапки и снимки (с роликами) одной папки в «человеческом» порядке.
func photoDir(dir string) (dirs, photos []string) {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	for _, it := range items {
		name := it.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(dir, name)
		ext := strings.ToLower(filepath.Ext(name))
		switch {
		case it.IsDir():
			dirs = append(dirs, full)
		case photoExt[ext] || videoExt[ext]:
			photos = append(photos, full)
		}
	}
	byName := func(list []string) {
		sort.SliceStable(list, func(a, b int) bool {
			return naturalLess(filepath.Base(list[a]), filepath.Base(list[b]))
		})
	}
	byName(dirs)
	byName(photos)
	return
}

// photoTree — сколько снимков в папке вместе с вложенными и какой из них
// первый: он станет обложкой плитки.
func photoTree(dir string, depth int) (count int, first string) {
	dirs, photos := photoDir(dir)
	count = len(photos)
	if len(photos) > 0 {
		first = photos[0]
	}
	if depth >= photoDepth {
		return
	}
	for _, d := range dirs {
		n, f := photoTree(d, depth+1)
		count += n
		if first == "" {
			first = f
		}
	}
	return
}

// photoTiles — плитки раздела «Фото»: папки первого уровня, в которых есть
// снимки (хоть во вложенных). Снимки прямо в корне — одной плиткой с
// названием библиотеки.
func (s *server) photoTiles(lib library) []catTile {
	var out []catTile
	dirs, photos := photoDir(lib.Path)
	if len(photos) > 0 {
		out = append(out, catTile{ID: s.remember(lib.Path), Name: lib.Title, Kind: "photos",
			Count: len(photos), Poster: s.albumCover(lib.Path, photos[0])})
	}
	for _, d := range dirs {
		n, first := photoTree(d, 1)
		if n == 0 {
			continue
		}
		out = append(out, catTile{ID: s.remember(d), Name: filepath.Base(d), Kind: "photos",
			Count: n, Poster: s.albumCover(d, first)})
	}
	return out
}

// albumCover — обложка альбома: выбранная в админке, иначе первый снимок.
func (s *server) albumCover(dir, first string) string {
	if u := s.ownPosterURL(dir, s.remember(dir), "photos"); u != "" {
		return u
	}
	return s.photoURL(first, "thumb")
}

// photoURL — ссылка на копию снимка. Время правки в ссылке нужно кэшу
// браузера: заменили файл — ссылка другая, старая картинка не всплывёт.
func (s *server) photoURL(path, variant string) string {
	id := s.remember(path)
	v := ""
	if st, err := os.Stat(path); err == nil {
		v = strconv.FormatInt(st.ModTime().Unix(), 36)
	}
	return s.cfg.base + "/photo/" + id + "?k=" + s.sign(id) + "&s=" + variant + "&v=" + v
}

type photoOut struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Thumb string `json:"thumb"`
	View  string `json:"view"`
	// ролик: View — его кадр, а смотрят URL
	Video bool   `json:"video,omitempty"`
	URL   string `json:"url,omitempty"`
}

func isVideoFile(p string) bool { return videoExt[strings.ToLower(filepath.Ext(p))] }

type photoFolderOut struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
	Cover string `json:"cover,omitempty"`
}

// GET /api/photos?id= — снимки папки и её подпапки со снимками.
func (s *server) photos(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	dir, ok := agentStore.pathOf(id)
	if !ok || !s.inRoots(dir) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "папка не найдена"})
		return
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "папка не найдена"})
		return
	}
	dirs, files := photoDir(dir)
	photos := make([]photoOut, 0, len(files))
	for _, p := range files {
		o := photoOut{ID: s.remember(p), Name: filepath.Base(p),
			Thumb: s.photoURL(p, "thumb"), View: s.photoURL(p, "view")}
		if isVideoFile(p) {
			o.Video, o.URL = true, s.mediaURL(o.ID)
		}
		photos = append(photos, o)
	}
	folders := []photoFolderOut{}
	for _, d := range dirs {
		n, first := photoTree(d, 1)
		if n == 0 {
			continue
		}
		folders = append(folders, photoFolderOut{ID: s.remember(d), Name: filepath.Base(d),
			Count: n, Cover: s.photoURL(first, "thumb")})
	}
	name := filepath.Base(dir)
	for _, lib := range s.cfg.roots {
		if lib.Path == dir {
			name = lib.Title
		}
	}
	go s.warmPhotos(dir, files)
	writeJSON(w, http.StatusOK, map[string]any{
		"folder": map[string]string{"id": id, "name": name},
		"photos": photos, "dirs": folders})
}

// GET /photo/{id}?k=&s=thumb|view — копия снимка. Не вышло собрать копию
// (незнакомый формат, битый файл) — отдаём оригинал: пусть браузер попробует.
func (s *server) photo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.validKey(id, r.URL.Query().Get("k")) {
		http.Error(w, "нет доступа", http.StatusForbidden)
		return
	}
	path, ok := agentStore.pathOf(id)
	if !ok || !s.inRoots(path) {
		http.NotFound(w, r)
		return
	}
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	variant := r.URL.Query().Get("s")
	if variant != "thumb" {
		variant = "view"
	}
	out, err := s.photoVariant(path, st, variant)
	if err != nil {
		log.Printf("копия снимка %s: %v — отдаю оригинал", path, err)
		out = path
	}
	// ссылка меняется вместе с файлом (&v=), поэтому кэшировать можно надолго
	w.Header().Set("Cache-Control", "public, max-age=2592000, immutable")
	if ct := mimeByExt(out); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeFile(w, r, out)
}

func mimeByExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	}
	return ""
}

var (
	// Сборка копии — полсекунды процессора и ~100 МБ памяти на 24 Мп снимок.
	// Двух разом хватает, чтобы полоса превью наполнялась быстро, и при этом
	// не мешать раздаче видео на слабом i3.
	photoSem   = make(chan struct{}, 2)
	photoLocks sync.Map // ключ копии → *sync.Mutex: одну копию собирают один раз
	photoWarm  sync.Map // папка → время последнего прогрева
)

// photoVariant — путь к готовой копии, собирая её при необходимости. Обе копии
// собираются за один разбор файла: он и есть самое дорогое.
func (s *server) photoVariant(path string, st os.FileInfo, variant string) (string, error) {
	video := isVideoFile(path)
	if !video && !decodableExt[strings.ToLower(filepath.Ext(path))] {
		return path, nil
	}
	key := shortID(path + "|" + strconv.FormatInt(st.Size(), 10) + "|" +
		strconv.FormatInt(st.ModTime().UnixNano(), 10))
	dir := filepath.Join(s.cfg.cache, "photos")
	thumb := filepath.Join(dir, key+"-thumb.jpg")
	view := filepath.Join(dir, key+"-view.jpg")
	want := view
	if variant == "thumb" {
		want = thumb
	}
	if fileReady(want) {
		return want, nil
	}
	lock, _ := photoLocks.LoadOrStore(key, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	if fileReady(want) {
		return want, nil
	}
	photoSem <- struct{}{}
	defer func() { <-photoSem }()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	build := makePhotoCopies
	if video {
		build = makeVideoCopies
	}
	if err := build(path, thumb, view); err != nil {
		return "", err
	}
	return want, nil
}

func fileReady(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Size() > 0
}

// warmPhotos готовит копии всех снимков папки по одному, пока их смотрят.
// Один проход раз в 10 минут на папку: повторные заходы копии и так найдут.
func (s *server) warmPhotos(dir string, files []string) {
	if last, ok := photoWarm.Load(dir); ok && time.Since(last.(time.Time)) < 10*time.Minute {
		return
	}
	photoWarm.Store(dir, time.Now())
	started, made := time.Now(), 0
	for _, p := range files {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		// превью первым делом: его ждёт полоса, экранная копия — следом
		if _, err := s.photoVariant(p, st, "thumb"); err == nil {
			made++
		}
	}
	if made > 0 {
		log.Printf("фото: копии %d снимков в %s готовы за %s", made, filepath.Base(dir),
			time.Since(started).Round(time.Second))
	}
}

// --- сборка копий ---

// decodeFit — картинка, вписанная в рамку и повёрнутая по EXIF.
func decodeFit(src string, maxW, maxH int) (*image.RGBA, error) {
	f, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	orientation := 1
	if ext := strings.ToLower(filepath.Ext(src)); ext == ".jpg" || ext == ".jpeg" {
		orientation = exifOrientation(f)
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
	}
	img, _, err := image.Decode(bufio.NewReaderSize(f, 1<<20))
	if err != nil {
		return nil, err
	}
	return orient(fitImage(img, maxW, maxH, orientation), orientation), nil
}

// renderJPEG — картинка в рамке maxW×maxH как JPEG: фон страницы, обложка.
func renderJPEG(src, dst string, maxW, maxH, quality int) error {
	img, err := decodeFit(src, maxW, maxH)
	if err != nil {
		return err
	}
	return writeJPEG(dst, img, quality)
}

// makeVideoCopies — кадр ролика как «снимок»: ffmpeg берёт кадр на первой
// секунде (у совсем коротких — самый первый) уже вписанным в экран, превью
// уменьшается из него.
func makeVideoCopies(src, thumbPath, viewPath string) error {
	tmp := viewPath + ".tmp.jpg"
	defer os.Remove(tmp)
	for _, at := range []string{"1", "0"} {
		cmd := exec.Command("ffmpeg", "-v", "error", "-y", "-ss", at, "-i", src, "-frames:v", "1",
			// min(): маленький ролик не растягиваем до экрана, только большие ужимаем
			"-vf", fmt.Sprintf("scale=w='min(%d,iw)':h='min(%d,ih)':force_original_aspect_ratio=decrease", viewWidth, viewHeight),
			"-q:v", "3", tmp)
		if cmd.Run() == nil && fileReady(tmp) {
			break
		}
	}
	if !fileReady(tmp) {
		return errors.New("ffmpeg не вынул кадр")
	}
	if err := os.Rename(tmp, viewPath); err != nil {
		return err
	}
	return renderJPEG(viewPath, thumbPath, thumbSide, thumbSide, 82)
}

func makePhotoCopies(src, thumbPath, viewPath string) error {
	view, err := decodeFit(src, viewWidth, viewHeight)
	if err != nil {
		return err
	}
	b := view.Bounds()
	tw, th := fitBox(b.Dx(), b.Dy(), thumbSide, thumbSide)
	thumb := &image.RGBA{Pix: boxResize(view.Pix, b.Dx(), b.Dy(), view.Stride, 4, tw, th),
		Stride: tw * 4, Rect: image.Rect(0, 0, tw, th)}
	if err := writeJPEG(viewPath, view, 88); err != nil {
		return err
	}
	return writeJPEG(thumbPath, thumb, 82)
}

func writeJPEG(path string, img image.Image, quality int) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 256<<10)
	err = jpeg.Encode(w, img, &jpeg.Options{Quality: quality})
	if err == nil {
		err = w.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

// fitBox — размер, вписанный в рамку с сохранением пропорций. Увеличивать не
// увеличиваем: мелкая картинка остаётся своего размера.
func fitBox(w, h, maxW, maxH int) (int, int) {
	if w <= maxW && h <= maxH {
		return w, h
	}
	if w*maxH > h*maxW {
		return maxW, max(1, h*maxW/w)
	}
	return max(1, w*maxH/h), maxH
}

// fitImage уменьшает снимок до рамки. Рамка задана для снимка УЖЕ повёрнутого:
// у вертикального кадра, записанного лёжа, ширина и высота меняются местами.
func fitImage(img image.Image, maxW, maxH, orientation int) *image.RGBA {
	b := img.Bounds()
	if orientation >= 5 {
		maxW, maxH = maxH, maxW
	}
	w, h := fitBox(b.Dx(), b.Dy(), maxW, maxH)
	if yc, ok := img.(*image.YCbCr); ok && b.Min == (image.Point{}) {
		return resizeYCbCr(yc, w, h)
	}
	rgba, ok := img.(*image.RGBA)
	if !ok || b.Min != (image.Point{}) {
		rgba = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(rgba, rgba.Rect, img, b.Min, draw.Src)
	}
	return &image.RGBA{Pix: boxResize(rgba.Pix, b.Dx(), b.Dy(), rgba.Stride, 4, w, h),
		Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
}

// resizeYCbCr уменьшает JPEG прямо в его плоскостях: яркость и цвет по
// отдельности, и только маленький результат переводится в RGB. Перевод всех
// 24 Мп в RGB до уменьшения стоил бы вдвое дороже.
func resizeYCbCr(img *image.YCbCr, w, h int) *image.RGBA {
	sw, sh := img.Rect.Dx(), img.Rect.Dy()
	cw, ch := sw, sh
	switch img.SubsampleRatio {
	case image.YCbCrSubsampleRatio422:
		cw = (sw + 1) / 2
	case image.YCbCrSubsampleRatio420:
		cw, ch = (sw+1)/2, (sh+1)/2
	case image.YCbCrSubsampleRatio440:
		ch = (sh + 1) / 2
	case image.YCbCrSubsampleRatio411:
		cw = (sw + 3) / 4
	case image.YCbCrSubsampleRatio410:
		cw, ch = (sw+3)/4, (sh+1)/2
	}
	y := boxResize(img.Y, sw, sh, img.YStride, 1, w, h)
	cb := boxResize(img.Cb, cw, ch, img.CStride, 1, w, h)
	cr := boxResize(img.Cr, cw, ch, img.CStride, 1, w, h)
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for i, j := 0, 0; i < len(y); i, j = i+1, j+4 {
		r, g, b := color.YCbCrToRGB(y[i], cb[i], cr[i])
		out.Pix[j], out.Pix[j+1], out.Pix[j+2], out.Pix[j+3] = r, g, b, 255
	}
	return out
}

// boxResize — уменьшение усреднением: каждый пиксель результата — среднее
// прямоугольника исходника под ним. При уменьшении в разы это и быстро, и
// без «лесенок», в отличие от выборки ближайшего пикселя.
func boxResize(src []byte, sw, sh, stride, nch, dw, dh int) []byte {
	dst := make([]byte, dw*dh*nch)
	xs := make([]int, dw+1)
	for i := range xs {
		xs[i] = i * sw / dw
	}
	if nch == 1 {
		return boxResizePlane(src, sw, sh, stride, dw, dh, xs, dst)
	}
	sum := make([]uint32, nch)
	for dy := 0; dy < dh; dy++ {
		y0, y1 := dy*sh/dh, (dy+1)*sh/dh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for dx := 0; dx < dw; dx++ {
			x0, x1 := xs[dx], xs[dx+1]
			if x1 <= x0 {
				x1 = x0 + 1
			}
			for c := range sum {
				sum[c] = 0
			}
			for yy := y0; yy < y1; yy++ {
				row := src[yy*stride+x0*nch : yy*stride+x1*nch]
				for k := 0; k < len(row); k += nch {
					for c := 0; c < nch; c++ {
						sum[c] += uint32(row[k+c])
					}
				}
			}
			n := uint32((x1 - x0) * (y1 - y0))
			o := (dy*dw + dx) * nch
			for c := 0; c < nch; c++ {
				dst[o+c] = byte((sum[c] + n/2) / n)
			}
		}
	}
	return dst
}

// boxResizePlane — то же для одной плоскости (яркость, цвет JPEG): без
// внутреннего цикла по каналам заметно быстрее, а плоскостей тут три из трёх.
func boxResizePlane(src []byte, sw, sh, stride, dw, dh int, xs []int, dst []byte) []byte {
	for dy := 0; dy < dh; dy++ {
		y0, y1 := dy*sh/dh, (dy+1)*sh/dh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for dx := 0; dx < dw; dx++ {
			x0, x1 := xs[dx], xs[dx+1]
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var sum uint32
			for yy := y0; yy < y1; yy++ {
				for _, v := range src[yy*stride+x0 : yy*stride+x1] {
					sum += uint32(v)
				}
			}
			n := uint32((x1 - x0) * (y1 - y0))
			dst[dy*dw+dx] = byte((sum + n/2) / n)
		}
	}
	return dst
}

// orient поворачивает и отражает картинку по метке EXIF (1–8).
func orient(img *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return img
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	ow, oh := w, h
	if o >= 5 {
		ow, oh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for Y := 0; Y < oh; Y++ {
		for X := 0; X < ow; X++ {
			var x, y int
			switch o {
			case 2:
				x, y = w-1-X, Y
			case 3:
				x, y = w-1-X, h-1-Y
			case 4:
				x, y = X, h-1-Y
			case 5:
				x, y = Y, X
			case 6:
				x, y = Y, h-1-X
			case 7:
				x, y = w-1-Y, h-1-X
			case 8:
				x, y = w-1-Y, X
			}
			copy(out.Pix[(Y*ow+X)*4:(Y*ow+X)*4+4], img.Pix[(y*w+x)*4:(y*w+x)*4+4])
		}
	}
	return out
}

// exifOrientation читает из JPEG метку поворота (тег 0x0112). Нет метки или
// файл не такой, как ждали, — 1, «как есть».
func exifOrientation(r io.Reader) int {
	br := bufio.NewReader(r)
	var soi [2]byte
	if _, err := io.ReadFull(br, soi[:]); err != nil || soi != [2]byte{0xFF, 0xD8} {
		return 1
	}
	for {
		var m [4]byte
		if _, err := io.ReadFull(br, m[:]); err != nil || m[0] != 0xFF {
			return 1
		}
		marker, size := m[1], int(binary.BigEndian.Uint16(m[2:]))-2
		if marker == 0xDA || size < 0 { // дальше сжатые данные — метки не будет
			return 1
		}
		if marker != 0xE1 {
			if _, err := br.Discard(size); err != nil {
				return 1
			}
			continue
		}
		seg := make([]byte, size)
		if _, err := io.ReadFull(br, seg); err != nil {
			return 1
		}
		if o, err := tiffOrientation(seg); err == nil {
			return o
		}
	}
}

func tiffOrientation(seg []byte) (int, error) {
	if len(seg) < 14 || string(seg[:6]) != "Exif\x00\x00" {
		return 0, errors.New("не EXIF")
	}
	t := seg[6:]
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0, errors.New("порядок байт")
	}
	off := int(bo.Uint32(t[4:8]))
	if off+2 > len(t) {
		return 0, errors.New("IFD за пределами")
	}
	n := int(bo.Uint16(t[off:]))
	for i := 0; i < n; i++ {
		e := off + 2 + i*12
		if e+12 > len(t) {
			break
		}
		if bo.Uint16(t[e:]) == 0x0112 {
			if o := int(bo.Uint16(t[e+8:])); o >= 1 && o <= 8 {
				return o, nil
			}
		}
	}
	return 1, nil
}
