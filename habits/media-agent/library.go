package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Библиотеки, обход папок и разбор файлов через ffprobe.
//
// Разбор идёт ПО ТРЕБОВАНИЮ — когда открыли папку, а не сплошным обходом при
// старте: файлов тут восемь тысяч, сплошной обход занял бы минуты и держал бы
// диск без всякой пользы (за вечер открывают две-три папки). Результат кладётся
// в кэш на диск, поэтому второй заход в папку мгновенный.

type library struct {
	ID    string `json:"id"`
	Path  string `json:"-"` // наружу пути не отдаём
	Title string `json:"title"`
	Kind  string `json:"kind"`
}

// mediaInfo — что мы знаем о файле. Verdict отвечает на единственный
// практический вопрос: заиграет ли это в браузере приставки как есть.
type mediaInfo struct {
	Container string   `json:"container"`
	VCodec    string   `json:"vcodec"`
	ACodec    string   `json:"acodec"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
	Duration  float64  `json:"duration"`
	Subs      []string `json:"subs"`
	Tracks    int      `json:"tracks"`
	Verdict   string   `json:"verdict"` // ok | remux | audio | video
	Why       string   `json:"why"`
}

type entry struct {
	ID    string     `json:"id"`
	Name  string     `json:"name"`
	IsDir bool       `json:"is_dir"`
	Size  int64      `json:"size"`
	Info  *mediaInfo `json:"info,omitempty"`
	// Ready — путь к перекодированной копии готов, играть надо её
	Ready bool `json:"ready"`
}

var (
	videoExt = map[string]bool{".mkv": true, ".mp4": true, ".avi": true, ".m4v": true,
		".mov": true, ".ts": true, ".webm": true, ".wmv": true, ".flv": true,
		".mpg": true, ".mpeg": true}
	audioExt = map[string]bool{".mp3": true, ".flac": true, ".m4a": true, ".aac": true,
		".ogg": true, ".opus": true, ".wav": true, ".wma": true}
)

func isMedia(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return videoExt[ext] || audioExt[ext]
}

// shortID — устойчивый идентификатор пути. Наружу уходит он, а не путь: так
// страница не знает раскладку дисков, а подобрать чужой файл перебором нельзя.
func shortID(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:9])
}

// --- кэш разбора ---

type cacheEntry struct {
	Key  string     `json:"key"` // путь|размер|время правки
	Info *mediaInfo `json:"info"`
}

type store struct {
	mu     sync.RWMutex
	dir    string
	probes map[string]cacheEntry // id → разбор
	paths  map[string]string     // id → путь
	dirty  bool
}

func newStore(dir string) *store {
	s := &store{dir: dir, probes: map[string]cacheEntry{}, paths: map[string]string{}}
	s.load()
	go s.flusher()
	return s
}

func (s *store) file() string { return filepath.Join(s.dir, "probe.json") }

func (s *store) load() {
	data, err := os.ReadFile(s.file())
	if err != nil {
		return
	}
	var saved struct {
		Probes map[string]cacheEntry `json:"probes"`
		Paths  map[string]string     `json:"paths"`
	}
	if json.Unmarshal(data, &saved) != nil {
		return // кэш испорчен — не беда, соберётся заново
	}
	if saved.Probes != nil {
		s.probes = saved.Probes
	}
	if saved.Paths != nil {
		s.paths = saved.Paths
	}
}

// flusher сохраняет кэш раз в 10 секунд, если было что менять: писать файл на
// каждый разобранный ролик — лишние сотни записей на диск за один обход папки.
func (s *store) flusher() {
	for range time.Tick(10 * time.Second) {
		s.save()
	}
}

func (s *store) save() {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return
	}
	data, err := json.Marshal(struct {
		Probes map[string]cacheEntry `json:"probes"`
		Paths  map[string]string     `json:"paths"`
	}{s.probes, s.paths})
	s.dirty = false
	s.mu.Unlock()
	if err != nil {
		return
	}
	tmp := s.file() + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, s.file())
	}
}

func (s *store) rememberPath(id, path string) {
	s.mu.Lock()
	if s.paths[id] != path {
		s.paths[id] = path
		s.dirty = true
	}
	s.mu.Unlock()
}

func (s *store) pathOf(id string) (string, bool) {
	s.mu.RLock()
	p, ok := s.paths[id]
	s.mu.RUnlock()
	return p, ok
}

// probe возвращает разбор файла, беря его из кэша, пока размер и время правки
// не изменились.
func (s *store) probe(path string, st os.FileInfo) *mediaInfo {
	id := shortID(path)
	key := path + "|" + strconv.FormatInt(st.Size(), 10) + "|" +
		strconv.FormatInt(st.ModTime().Unix(), 10)

	s.mu.RLock()
	got, ok := s.probes[id]
	s.mu.RUnlock()
	if ok && got.Key == key {
		return got.Info
	}

	info := ffprobe(path)
	s.mu.Lock()
	s.probes[id] = cacheEntry{Key: key, Info: info}
	s.paths[id] = path
	s.dirty = true
	s.mu.Unlock()
	return info
}

// --- ffprobe ---

func ffprobe(path string) *mediaInfo {
	out, err := exec.Command("ffprobe", "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", path).Output()
	if err != nil {
		return nil
	}
	var raw struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecName   string `json:"codec_name"`
			CodecType   string `json:"codec_type"`
			Width       int    `json:"width"`
			Height      int    `json:"height"`
			Disposition struct {
				AttachedPic int `json:"attached_pic"`
			} `json:"disposition"`
		} `json:"streams"`
	}
	if json.Unmarshal(out, &raw) != nil {
		return nil
	}
	info := &mediaInfo{Container: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")}
	info.Duration, _ = strconv.ParseFloat(raw.Format.Duration, 64)
	for _, s := range raw.Streams {
		switch s.CodecType {
		case "video":
			// обложка внутри mp3 — это тоже «видеопоток», но не видео
			if s.Disposition.AttachedPic == 1 || info.VCodec != "" {
				continue
			}
			info.VCodec, info.Width, info.Height = s.CodecName, s.Width, s.Height
		case "audio":
			if info.ACodec == "" {
				info.ACodec = s.CodecName
			}
			info.Tracks++
		case "subtitle":
			info.Subs = append(info.Subs, s.CodecName)
		}
	}
	info.Verdict, info.Why = verdict(info)
	return info
}

// Что приставка берёт как есть — по её же отчёту: HEVC (включая 10 бит), AV1,
// VP8/VP9, H.264; звук AAC/MP3/Opus/Vorbis/FLAC. Не берёт AC3 и EAC3 — это и
// есть главная причина перекодировать.
var (
	okVideo     = map[string]bool{"h264": true, "hevc": true, "vp8": true, "vp9": true, "av1": true}
	okAudio     = map[string]bool{"aac": true, "mp3": true, "opus": true, "vorbis": true, "flac": true}
	okContainer = map[string]bool{"mp4": true, "m4v": true, "mov": true, "webm": true,
		"mp3": true, "m4a": true, "flac": true, "ogg": true, "opus": true, "wav": true, "aac": true}
)

func verdict(i *mediaInfo) (string, string) {
	var why []string
	level := "ok"
	up := func(next string) {
		rank := map[string]int{"ok": 0, "remux": 1, "audio": 2, "video": 3}
		if rank[next] > rank[level] {
			level = next
		}
	}
	if i.VCodec != "" {
		if !okContainer[i.Container] {
			up("remux")
			why = append(why, "контейнер "+i.Container)
		}
		if !okVideo[i.VCodec] {
			up("video")
			why = append(why, "видео "+i.VCodec)
		}
	}
	if i.ACodec != "" && !okAudio[i.ACodec] {
		up("audio")
		why = append(why, "звук "+i.ACodec)
	}
	return level, strings.Join(why, ", ")
}

// --- обход папки ---

// browse читает одну папку: подпапки и медиафайлы, разбор — параллельно.
func (s *store) browse(dir string, transcoded func(string) bool) ([]entry, error) {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []entry
	var files []struct {
		idx  int
		path string
		st   os.FileInfo
	}
	for _, it := range items {
		name := it.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(dir, name)
		if it.IsDir() {
			out = append(out, entry{ID: shortID(full), Name: name, IsDir: true})
			s.rememberPath(shortID(full), full)
			continue
		}
		if !isMedia(name) {
			continue
		}
		st, err := it.Info()
		if err != nil {
			continue
		}
		id := shortID(full)
		out = append(out, entry{ID: id, Name: name, Size: st.Size(), Ready: transcoded(id)})
		s.rememberPath(id, full)
		files = append(files, struct {
			idx  int
			path string
			st   os.FileInfo
		}{len(out) - 1, full, st})
	}

	// ffprobe — процесс на файл, десяток разом диск переживёт, а ждать
	// последовательно двадцать файлов по 30 мс уже заметно
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, f := range files {
		wg.Add(1)
		go func(idx int, path string, st os.FileInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[idx].Info = s.probe(path, st)
		}(f.idx, f.path, f.st)
	}
	wg.Wait()

	sort.SliceStable(out, func(a, b int) bool {
		if out[a].IsDir != out[b].IsDir {
			return out[a].IsDir // папки сверху
		}
		return naturalLess(out[a].Name, out[b].Name)
	})
	return out, nil
}

// naturalLess сравнивает имена «по-человечески»: числа внутри сравниваются как
// числа. Иначе «Серия 10» встаёт перед «Серия 2», и порядок серий рассыпается —
// ровно то, ради чего папку и открывают.
func naturalLess(a, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	i, j := 0, 0
	for i < len(la) && j < len(lb) {
		ca, cb := la[i], lb[j]
		if isDigit(ca) && isDigit(cb) {
			si, sj := i, j
			for i < len(la) && isDigit(la[i]) {
				i++
			}
			for j < len(lb) && isDigit(lb[j]) {
				j++
			}
			na, _ := strconv.ParseInt(strings.TrimLeft(la[si:i], "0"), 10, 64)
			nb, _ := strconv.ParseInt(strings.TrimLeft(lb[sj:j], "0"), 10, 64)
			if na != nb {
				return na < nb
			}
			continue
		}
		if ca != cb {
			return ca < cb
		}
		i++
		j++
	}
	return len(la)-i < len(lb)-j
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
