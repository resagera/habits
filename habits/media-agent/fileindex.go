package main

// Индекс файлов: что лежит в библиотеках и что о каждом файле известно —
// размер, разрешение снимка, качество звука, число страниц PDF.
//
// Собирается ЗАРАНЕЕ обходом диска, а не в момент открытия папки: открыть
// папку на сотню снимков и ждать, пока агент прочтёт заголовок каждого с
// USB-диска, — ровно то, чего не хочется на пульте. Обход идёт фоновой
// задачей (видно в админке, «Задачи»): через пару минут после старта, потом
// раз в indexEvery и вскоре после загрузки файлов. Сверка — по «размер +
// время правки»: неизменившийся файл второй раз не разбирается, поэтому
// повторный обход — это просто чтение каталогов.
//
// Ключ — полный путь, как у всей памяти агента. Переименование и переезд
// через агента переносят запись (moveMemory). Файл, переложенный мимо агента
// (руками, через --out и копирование), обход узнаёт: пропал по одному пути и
// появился по другому с тем же размером и временем правки — значит, переехал,
// и за ним едет вся память (заголовок, позиция просмотра…).
//
// Главная осторожность — отвал диска. Корень, которого нет на месте, обход
// пропускает целиком, а при ошибке чтения посреди обхода ничего не удаляет:
// иначе один отвал USB стёр бы индекс библиотеки и с ним переезды.
//
// В копию разметки индекс не кладём (backupSkip): он восстанавливается обходом.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	indexEvery     = 6 * time.Hour
	indexFirst     = 2 * time.Minute
	indexSoonDelay = 30 * time.Second
	indexBusyRetry = 5 * time.Minute
)

// виды файлов — по ним страница выбирает значок и просмотрщик
var (
	textExt = map[string]bool{".txt": true, ".md": true, ".nfo": true, ".srt": true, ".ass": true,
		".ssa": true, ".vtt": true, ".log": true, ".json": true, ".csv": true, ".xml": true,
		".ini": true, ".cue": true, ".m3u": true, ".m3u8": true, ".yml": true, ".yaml": true,
		".sub": true, ".lrc": true, ".conf": true}
	archiveExt = map[string]bool{".zip": true, ".rar": true, ".7z": true, ".tar": true, ".gz": true,
		".tgz": true, ".bz2": true, ".xz": true}
	docExt = map[string]bool{".doc": true, ".docx": true, ".odt": true, ".rtf": true, ".xls": true,
		".xlsx": true, ".ods": true, ".ppt": true, ".pptx": true, ".epub": true, ".fb2": true, ".djvu": true}
	imageExtraExt = map[string]bool{".bmp": true, ".heic": true, ".heif": true, ".tif": true, ".tiff": true}
)

func fileKind(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case videoExt[ext]:
		return "video"
	case audioExt[ext]:
		return "audio"
	case photoExt[ext] || imageExtraExt[ext]:
		return "photo"
	case ext == ".pdf":
		return "pdf"
	case textExt[ext]:
		return "text"
	case archiveExt[ext]:
		return "archive"
	case docExt[ext]:
		return "doc"
	}
	return "other"
}

// fileRec — что известно о файле. Поля пустые, если к виду не относятся.
type fileRec struct {
	Size  int64   `json:"size"`
	MTime int64   `json:"mtime"`
	Kind  string  `json:"kind"`
	W     int     `json:"w,omitempty"`
	H     int     `json:"h,omitempty"`
	Dur   float64 `json:"dur,omitempty"`
	VC    string  `json:"vc,omitempty"`   // кодек картинки
	AC    string  `json:"ac,omitempty"`   // кодек звука
	ABR   int64   `json:"abr,omitempty"`  // бит/с звука (у видео — первой дорожки)
	Rate  int     `json:"rate,omitempty"` // Гц
	Bits  int     `json:"bits,omitempty"` // разрядность звука
	Ch    int     `json:"ch,omitempty"`
	Pages int     `json:"pages,omitempty"`
	Taken int64   `json:"taken,omitempty"` // дата съёмки из EXIF, unix
}

// dirAgg — сводка по папке со всем, что внутри.
type dirAgg struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

type indexStatus struct {
	LastWalk int64 `json:"last_walk"`
	Took     int64 `json:"took_ms"`
	Files    int   `json:"files"`
	Changed  int   `json:"changed"`
	Moved    int   `json:"moved"`
	Removed  int   `json:"removed"`
}

type fileIndex struct {
	mu     sync.RWMutex
	path   string
	files  map[string]fileRec
	dirs   map[string]dirAgg
	side   map[string][]string // видео → файлы его дорожек (tracks.go)
	status indexStatus
	dirty  bool
	soon   chan struct{}
}

func newFileIndex(cacheDir string) *fileIndex {
	x := &fileIndex{path: filepath.Join(cacheDir, "fileindex.json"),
		files: map[string]fileRec{}, dirs: map[string]dirAgg{}, soon: make(chan struct{}, 1)}
	if data, err := os.ReadFile(x.path); err == nil {
		var saved struct {
			Files  map[string]fileRec `json:"files"`
			Status indexStatus        `json:"status"`
		}
		if json.Unmarshal(data, &saved) == nil && saved.Files != nil {
			x.files, x.status = saved.Files, saved.Status
			x.rebuildDirs()
		}
	}
	// переезды через агента меняют индекс между обходами — сохраняем следом
	go func() {
		for range time.Tick(30 * time.Second) {
			x.save()
		}
	}()
	return x
}

func (x *fileIndex) save() {
	x.mu.Lock()
	if !x.dirty {
		x.mu.Unlock()
		return
	}
	data, err := json.Marshal(struct {
		Files  map[string]fileRec `json:"files"`
		Status indexStatus        `json:"status"`
	}{x.files, x.status})
	x.dirty = false
	x.mu.Unlock()
	if err == nil {
		_ = writeAtomic(x.path, data)
	}
}

func (x *fileIndex) get(path string) (fileRec, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	r, ok := x.files[path]
	return r, ok
}

func (x *fileIndex) dir(path string) (dirAgg, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	d, ok := x.dirs[path]
	return d, ok
}

func (x *fileIndex) put(path string, r fileRec) {
	x.mu.Lock()
	x.files[path] = r
	x.dirty = true
	x.mu.Unlock()
}

// rebuildDirs пересчитывает сводки папок из списка файлов. Дёшево: тысячи
// записей, и только в памяти. Вызывать под замком на запись или до того, как
// индекс стал общим.
func (x *fileIndex) rebuildDirs() {
	dirs := map[string]dirAgg{}
	for p, r := range x.files {
		for d := filepath.Dir(p); ; d = filepath.Dir(d) {
			a := dirs[d]
			a.Files++
			a.Bytes += r.Size
			dirs[d] = a
			if up := filepath.Dir(d); up == d {
				break
			}
		}
	}
	x.dirs = dirs
	x.rebuildSide()
}

// move — переезд через агента (moveMemory): записи едут без повторного
// разбора. Если по новому пути уже есть запись (её успел сделать обход),
// она главнее.
func (x *fileIndex) move(old, neu string, isDir bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	type change struct{ from, to string }
	var list []change
	for p := range x.files {
		if np, ok := movedCollPath(p, old, neu, isDir); ok {
			list = append(list, change{p, np})
		}
	}
	for _, c := range list {
		r := x.files[c.from]
		delete(x.files, c.from)
		if _, ok := x.files[c.to]; !ok {
			x.files[c.to] = r
		}
	}
	if len(list) > 0 {
		x.rebuildDirs()
		x.dirty = true
	}
}

// under — пути индекса внутри корня.
func (x *fileIndex) under(root string) []string {
	x.mu.RLock()
	defer x.mu.RUnlock()
	var out []string
	pre := root + string(filepath.Separator)
	for p := range x.files {
		if strings.HasPrefix(p, pre) {
			out = append(out, p)
		}
	}
	return out
}

// requestSoon — «загляни скоро»: после загрузки файлов. Несколько просьб
// подряд сливаются в один обход.
func (x *fileIndex) requestSoon() {
	select {
	case x.soon <- struct{}{}:
	default:
	}
}

// --- разбор одного файла ---

func (s *server) analyzeFile(path string, st os.FileInfo) fileRec {
	r := fileRec{Size: st.Size(), MTime: st.ModTime().Unix(), Kind: fileKind(path)}
	switch r.Kind {
	case "video":
		// разбор видео уже есть в общем кэше (тот же, что у списка серий)
		if info := agentStore.probe(path, st); info != nil {
			r.W, r.H, r.Dur, r.VC, r.AC = info.Width, info.Height, info.Duration, info.VCodec, info.ACodec
			if len(info.Audio) > 0 {
				r.ABR, r.Ch = info.Audio[0].BitRate, info.Audio[0].Channels
			}
		}
	case "audio":
		probeAudio(path, &r)
	case "photo":
		probePhoto(path, &r)
	case "pdf":
		r.Pages = pdfPages(path)
	}
	return r
}

// probeAudio — качество звука: кодек, битрейт, частота, разрядность. Свой
// вызов ffprobe, а не общий разбор: частота и разрядность нужны только здесь.
func probeAudio(path string, r *fileRec) {
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "a:0", "-print_format", "json",
		"-show_entries", "stream=codec_name,sample_rate,bits_per_raw_sample,bits_per_sample,channels,bit_rate:format=duration,bit_rate",
		path).Output()
	if err != nil {
		return
	}
	var raw struct {
		Format struct {
			Duration string `json:"duration"`
			BitRate  string `json:"bit_rate"`
		} `json:"format"`
		Streams []struct {
			Codec   string `json:"codec_name"`
			Rate    string `json:"sample_rate"`
			RawBits string `json:"bits_per_raw_sample"`
			Bits    int    `json:"bits_per_sample"`
			Ch      int    `json:"channels"`
			BitRate string `json:"bit_rate"`
		} `json:"streams"`
	}
	if json.Unmarshal(out, &raw) != nil {
		return
	}
	r.Dur, _ = strconv.ParseFloat(raw.Format.Duration, 64)
	if len(raw.Streams) == 0 {
		return
	}
	a := raw.Streams[0]
	r.AC, r.Ch = a.Codec, a.Ch
	r.Rate, _ = strconv.Atoi(a.Rate)
	r.Bits, _ = strconv.Atoi(a.RawBits)
	if r.Bits == 0 {
		r.Bits = a.Bits
	}
	r.ABR, _ = strconv.ParseInt(a.BitRate, 10, 64)
	if r.ABR == 0 {
		// у flac битрейта дорожки нет — берём средний по файлу
		r.ABR, _ = strconv.ParseInt(raw.Format.BitRate, 10, 64)
	}
}

// probePhoto — размер снимка по заголовку, без чтения картинки целиком, и
// дата съёмки из EXIF.
func probePhoto(path string, r *fileRec) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	head := make([]byte, 256<<10)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	switch strings.ToLower(filepath.Ext(path)) {
	case ".webp":
		r.W, r.H = webpSize(head)
		return
	case ".heic", ".heif", ".tif", ".tiff", ".bmp":
		return // заголовки не читаем: таких единицы, ffprobe ради них не зовём
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(head)); err == nil {
		r.W, r.H = cfg.Width, cfg.Height
	} else if _, err := f.Seek(0, io.SeekStart); err == nil {
		// заголовок оказался дальше первых 256 КБ (огромная миниатюра в EXIF)
		if cfg, _, err := image.DecodeConfig(f); err == nil {
			r.W, r.H = cfg.Width, cfg.Height
		}
	}
	if t, orient := exifInfo(head); !t.IsZero() || orient != 0 {
		if !t.IsZero() {
			r.Taken = t.Unix()
		}
		// снимок с телефона «лёжа»: в пикселях он широкий, а смотрят его стоя
		if orient >= 5 && orient <= 8 {
			r.W, r.H = r.H, r.W
		}
	}
}

// webpSize — размер из заголовка WebP (простой VP8, без потерь VP8L и
// расширенный VP8X).
func webpSize(b []byte) (int, int) {
	if len(b) < 30 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return 0, 0
	}
	switch string(b[12:16]) {
	case "VP8 ":
		if b[23] == 0x9d && b[24] == 0x01 && b[25] == 0x2a {
			return int(binary.LittleEndian.Uint16(b[26:28]) & 0x3fff), int(binary.LittleEndian.Uint16(b[28:30]) & 0x3fff)
		}
	case "VP8L":
		if b[20] == 0x2f {
			bits := binary.LittleEndian.Uint32(b[21:25])
			return int(bits&0x3fff) + 1, int((bits>>14)&0x3fff) + 1
		}
	case "VP8X":
		w := int(b[24]) | int(b[25])<<8 | int(b[26])<<16
		h := int(b[27]) | int(b[28])<<8 | int(b[29])<<16
		return w + 1, h + 1
	}
	return 0, 0
}

// exifInfo — дата съёмки (DateTimeOriginal, иначе DateTime) и поворот из
// EXIF в JPEG. Своего разбора хватает: нужны два тега, а тянуть библиотеку
// ради них агенту без зависимостей незачем.
func exifInfo(b []byte) (time.Time, int) {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return time.Time{}, 0
	}
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return time.Time{}, 0
		}
		marker := b[i+1]
		size := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
		if marker == 0xDA || size < 2 { // дальше пошла картинка
			return time.Time{}, 0
		}
		seg := i + 4
		end := i + 2 + size
		if end > len(b) {
			end = len(b)
		}
		if marker == 0xE1 && end-seg > 6 && string(b[seg:seg+6]) == "Exif\x00\x00" {
			return parseTIFF(b[seg+6 : end])
		}
		i += 2 + size
	}
	return time.Time{}, 0
}

func parseTIFF(t []byte) (taken time.Time, orient int) {
	if len(t) < 8 {
		return
	}
	var bo binary.ByteOrder
	switch string(t[0:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return
	}
	readStr := func(count, off uint32) string {
		if int(off)+int(count) > len(t) || count == 0 {
			return ""
		}
		return strings.TrimRight(string(t[off:off+count]), "\x00 ")
	}
	var dateTime, original string
	var walkIFD func(off uint32, depth int)
	walkIFD = func(off uint32, depth int) {
		if depth > 2 || int(off)+2 > len(t) {
			return
		}
		n := int(bo.Uint16(t[off : off+2]))
		for k := 0; k < n; k++ {
			e := int(off) + 2 + k*12
			if e+12 > len(t) {
				return
			}
			tag := bo.Uint16(t[e : e+2])
			count := bo.Uint32(t[e+4 : e+8])
			val := bo.Uint32(t[e+8 : e+12])
			switch tag {
			case 0x0112: // Orientation, SHORT — значение прямо в поле
				orient = int(bo.Uint16(t[e+8 : e+10]))
			case 0x0132:
				dateTime = readStr(count, val)
			case 0x9003:
				original = readStr(count, val)
			case 0x8769: // ExifIFD
				walkIFD(val, depth+1)
			}
		}
	}
	walkIFD(bo.Uint32(t[4:8]), 0)
	for _, v := range []string{original, dateTime} {
		if tm, err := time.ParseInLocation("2006:01:02 15:04:05", v, time.Local); err == nil {
			return tm, orient
		}
	}
	return
}

// pdfPages — число страниц через pdfinfo (poppler). Нет poppler — ноль, и
// просмотрщик спросит число страниц сам, когда PDF откроют.
func pdfPages(path string) int {
	out, err := exec.Command("pdfinfo", path).Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(line, "Pages:"); ok {
			n, _ := strconv.Atoi(strings.TrimSpace(v))
			return n
		}
	}
	return 0
}

// --- обход ---

var errDiskGone = errors.New("диск не на месте")

// walkIndex — один обход всех библиотек.
func (s *server) walkIndex(ctx context.Context, t *task) error {
	start := time.Now()
	st := indexStatus{}
	for li, lib := range s.cfg.roots {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		t.set(lib.Title, li, len(s.cfg.roots))
		if !lib.available() {
			t.say("%s: диска нет на месте — пропускаю, записи не трогаю", lib.Title)
			continue
		}
		res, err := s.walkLibrary(ctx, t, lib)
		st.Files += res.Files
		st.Changed += res.Changed
		st.Moved += res.Moved
		st.Removed += res.Removed
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case err != nil:
			t.say("%s: %v — удалённое не вычёркиваю", lib.Title, err)
		default:
			line := fmt.Sprintf("%s: файлов %d", lib.Title, res.Files)
			if res.Changed > 0 {
				line += fmt.Sprintf(", разобрано %d", res.Changed)
			}
			if res.Moved > 0 {
				line += fmt.Sprintf(", переехало мимо агента %d", res.Moved)
			}
			if res.Removed > 0 {
				line += fmt.Sprintf(", пропало %d", res.Removed)
			}
			t.say("%s", line)
		}
	}
	st.LastWalk, st.Took = time.Now().Unix(), time.Since(start).Milliseconds()
	s.index.mu.Lock()
	s.index.status = st
	s.index.rebuildDirs()
	s.index.dirty = true
	s.index.mu.Unlock()
	s.index.save()
	agentStore.save()
	s.reportBroken(t)
	return nil
}

func (s *server) walkLibrary(ctx context.Context, t *task, lib library) (indexStatus, error) {
	var res indexStatus
	seen := map[string]bool{}
	var fresh []string
	readErr := false
	err := filepath.WalkDir(lib.Path, func(p string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if p == lib.Path {
				return err
			}
			readErr = true
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if p != lib.Path && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir // корзина, недокопированный переезд, обложки
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		// другой корень внутри этого обойдёт себя сам
		if s.libraryOf(p).Path != lib.Path {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			readErr = true
			return nil
		}
		seen[p] = true
		res.Files++
		old, had := s.index.get(p)
		if had && old.Size == info.Size() && old.MTime == info.ModTime().Unix() {
			return nil
		}
		if !had {
			fresh = append(fresh, p)
		}
		t.set(lib.Title+": "+d.Name(), res.Files, 0)
		s.index.put(p, s.analyzeFile(p, info))
		res.Changed++
		return nil
	})
	if err != nil {
		return res, err
	}
	if readErr {
		return res, errors.New("часть папок не прочиталась")
	}
	var gone []string
	for _, p := range s.index.under(lib.Path) {
		if !seen[p] && s.libraryOf(p).Path == lib.Path {
			gone = append(gone, p)
		}
	}
	if len(seen) == 0 && len(gone) > 0 {
		// пустой корень при полном индексе — это отвал диска или пустая точка
		// монтирования, а не «всё удалили»
		return res, errDiskGone
	}
	res.Moved = s.adoptMoves(gone, fresh)
	for _, p := range gone {
		if _, still := s.index.get(p); still {
			s.index.mu.Lock()
			delete(s.index.files, p)
			s.index.dirty = true
			s.index.mu.Unlock()
			res.Removed++
		}
	}
	return res, nil
}

// adoptMoves — переезды мимо агента: пропавший файл и появившийся с тем же
// размером и временем правки — один и тот же файл. Пара берётся, только
// когда она однозначна: два одинаковых пропавших и один появившийся — не
// угадываем.
func (s *server) adoptMoves(gone, fresh []string) int {
	if len(gone) == 0 || len(fresh) == 0 {
		return 0
	}
	key := func(r fileRec) string { return strconv.FormatInt(r.Size, 10) + "|" + strconv.FormatInt(r.MTime, 10) }
	goneBy := map[string][]string{}
	for _, p := range gone {
		if r, ok := s.index.get(p); ok && r.Size > 0 {
			goneBy[key(r)] = append(goneBy[key(r)], p)
		}
	}
	freshBy := map[string][]string{}
	for _, p := range fresh {
		if r, ok := s.index.get(p); ok && r.Size > 0 {
			freshBy[key(r)] = append(freshBy[key(r)], p)
		}
	}
	moved := 0
	for k, olds := range goneBy {
		news := freshBy[k]
		if len(olds) != 1 || len(news) != 1 {
			continue
		}
		log.Printf("индекс: %s переехал мимо агента в %s", olds[0], news[0])
		s.moveMemory(olds[0], news[0], false)
		moved++
	}
	return moved
}

// indexWorker — расписание обходов. Обход — обычная фоновая задача, а их
// агент делает по одной: занят — заглянем позже.
func (s *server) indexWorker() {
	timer := time.NewTimer(indexFirst)
	for {
		select {
		case <-timer.C:
		case <-s.index.soon:
			// дать загрузке закончиться: файлы часто едут пачкой
			time.Sleep(indexSoonDelay)
			timer.Stop()
		}
		if _, err := s.tasks.start("index", "Обход файлов", s.walkIndex); err != nil {
			timer.Reset(indexBusyRetry)
			continue
		}
		timer.Reset(indexEvery)
	}
}

// --- что отдаём наружу ---

// GET /api/admin/index — когда был последний обход и что он нашёл.
func (s *server) adminIndex(w http.ResponseWriter, r *http.Request) {
	s.index.mu.RLock()
	st, n := s.index.status, len(s.index.files)
	s.index.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"status": st, "files": n})
}

// POST /api/admin/index — обойти сейчас.
func (s *server) adminIndexRun(w http.ResponseWriter, r *http.Request) {
	s.startTask(w, "index", "Обход файлов", s.walkIndex)
}
