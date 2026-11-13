package main

// Дорожки: озвучки и субтитры отдельными файлами рядом с видео и встроенные
// в него.
//
// Браузер не умеет переключать звуковые дорожки внутри файла, а AC3/DTS не
// играет вовсе. Поэтому другая озвучка звучит из отдельного <audio>, а видео
// в это время молчит; страница держит их вместе (player.html, «озвучка»).
// Что именно звучит:
//   - отдельный файл, который браузер берёт сам (mp3, m4a, aac, flac, opus,
//     ogg), — отдаётся как есть, с перемоткой по Range;
//   - всё остальное (mka, ac3, dts, вторая дорожка внутри mkv) — агент на
//     лету перегоняет в AAC стерео (/trackaudio/{id}?a=N&t=секунды). Места на
//     диске это не занимает, ждать не надо: ffmpeg для звука в сотни раз
//     быстрее реального времени. Перемотка — новый запрос с другим t.
//
// Отдельные файлы ищутся по индексу (fileindex.go), а не чтением папок при
// открытии каталога. Свой — у которого имя начинается с имени серии:
// «S01E01.mka», «S01E01.rus.LostFilm.ac3», «S01E01.ru.srt». Лежать он может
// рядом или в подпапках до трёх уровней — так раздают с торрентов: «Rus
// Sound/LostFilm/S01E01.mka», «Subs/S01E01.srt». Подпись берётся из имени и
// папок: «LostFilm», «Русская».

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	sideAudioExt = map[string]bool{".mka": true, ".ac3": true, ".eac3": true, ".dts": true, ".mp3": true,
		".aac": true, ".m4a": true, ".flac": true, ".opus": true, ".ogg": true, ".wav": true, ".mp2": true}
	sideSubExt = map[string]bool{".srt": true, ".vtt": true, ".ass": true, ".ssa": true}
	// что <audio> берёт сам, без перегонки
	nativeAudioExt = map[string]bool{".mp3": true, ".m4a": true, ".aac": true, ".flac": true,
		".opus": true, ".ogg": true, ".wav": true}
)

const sideDepth = 3 // на сколько папок вверх от файла дорожки искать её серию

// audioLangs — подписи озвучек по коду языка (у субтитров — subLangs).
var audioLangs = map[string]string{"ru": "Русская", "rus": "Русская", "en": "Английская", "eng": "Английская",
	"uk": "Украинская", "ukr": "Украинская", "hy": "Армянская", "arm": "Армянская", "de": "Немецкая",
	"ger": "Немецкая", "fr": "Французская", "fre": "Французская", "es": "Испанская", "spa": "Испанская",
	"ja": "Японская", "jpn": "Японская", "it": "Итальянская", "ita": "Итальянская", "ko": "Корейская",
	"kor": "Корейская", "zh": "Китайская", "chi": "Китайская"}

// reGenericDir — папки, которые ничего не говорят об озвучке, кроме языка:
// «Rus Sound», «Audio», «Subs», «Звук».
var reGenericDir = regexp.MustCompile(`(?i)^\s*(rus|eng|ukr|ru|en|ua)?[\s._-]*(sounds?|audio|dub|tracks?|subs?|subtitles?|звук|озвучк[аи]|дорожки|субтитры)?[\s._-]*(rus|eng|ukr|ru|en|ua)?\s*$`)

func isSideExt(ext string) bool { return sideAudioExt[ext] || sideSubExt[ext] }

func stemOf(p string) string {
	b := filepath.Base(p)
	return strings.TrimSuffix(b, filepath.Ext(b))
}

// sideMatches — относится ли файл дорожки к серии: имя то же или начинается
// с имени серии и точки («S01E01.rus»).
func sideMatches(sideStem, videoStem string) bool {
	ls, lv := strings.ToLower(sideStem), strings.ToLower(videoStem)
	return ls == lv || strings.HasPrefix(ls, lv+".")
}

// rebuildSide — какие файлы дорожек к какой серии. Только по записям индекса,
// диск не читается. Вызывать под замком на запись (как rebuildDirs).
func (x *fileIndex) rebuildSide() {
	videos := map[string][]string{} // папка → видео в ней
	for p := range x.files {
		if videoExt[strings.ToLower(filepath.Ext(p))] {
			d := filepath.Dir(p)
			videos[d] = append(videos[d], p)
		}
	}
	side := map[string][]string{}
	for p := range x.files {
		if !isSideExt(strings.ToLower(filepath.Ext(p))) {
			continue
		}
		stem := stemOf(p)
		d := filepath.Dir(p)
		for depth := 0; depth <= sideDepth; depth++ {
			found := false
			for _, v := range videos[d] {
				if sideMatches(stem, stemOf(v)) {
					side[v] = append(side[v], p)
					found = true
				}
			}
			up := filepath.Dir(d)
			if found || up == d {
				break
			}
			d = up
		}
	}
	for v := range side {
		sort.Strings(side[v])
	}
	x.side = side
}

// sidecars — файлы дорожек серии и знает ли о папке индекс вообще. Не знает
// (обход ещё не был) — список собирается чтением одной папки, как раньше.
func (x *fileIndex) sidecars(video string) ([]string, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	if _, ok := x.files[video]; !ok {
		return nil, false
	}
	return x.side[video], true
}

// sideLabel — подпись и язык файла дорожки по его имени и папкам между ним и
// серией. Язык — код из имени или папки «Rus Sound».
func sideLabel(video, side string) (names []string, lang string) {
	rel, err := filepath.Rel(filepath.Dir(video), filepath.Dir(side))
	if err == nil && rel != "." {
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			if m := reGenericDir.FindStringSubmatch(part); m != nil {
				for _, c := range []string{m[1], m[3]} {
					if c != "" && lang == "" {
						lang = strings.ToLower(c)
					}
				}
				continue
			}
			names = append(names, part)
		}
		// из папок нужна самая близкая к файлу: «LostFilm», а не «Звук HD»
		if len(names) > 1 {
			names = names[len(names)-1:]
		}
	}
	vs := stemOf(video)
	mid := strings.Trim(stemOf(side)[min(len(vs), len(stemOf(side))):], ". ")
	for _, tok := range strings.Split(mid, ".") {
		tok = strings.TrimSpace(strings.Trim(tok, "[]() "))
		if tok == "" {
			continue
		}
		lt := strings.ToLower(tok)
		if _, ok := audioLangs[lt]; ok {
			lang = lt
			continue
		}
		if _, ok := subLangs[lt]; ok {
			lang = lt
			continue
		}
		names = append(names, tok)
	}
	if lang == "ua" {
		lang = "uk"
	}
	return names, lang
}

// trackRef — звуковая дорожка для страницы. Main — та, что внутри видео
// первой: её играет само видео. URL с Live нужно дополнять «&t=секунды».
type trackRef struct {
	Label string `json:"label"`
	Lang  string `json:"lang,omitempty"`
	URL   string `json:"url,omitempty"`
	Live  bool   `json:"live,omitempty"`
	Main  bool   `json:"main,omitempty"`
	File  bool   `json:"file,omitempty"` // отдельным файлом
}

func (s *server) liveURL(id string, n int) string {
	return s.cfg.base + "/trackaudio/" + id + "?k=" + s.sign(id) + "&a=" + strconv.Itoa(n)
}

// embeddedLabel — подпись встроенной дорожки: название, иначе язык.
func embeddedLabel(a audioTrack, n int) string {
	if a.Title != "" {
		return a.Title
	}
	if l, ok := audioLangs[strings.ToLower(a.Lang)]; ok {
		return l
	}
	if a.Lang != "" && a.Lang != "und" {
		return strings.ToUpper(a.Lang)
	}
	if n == 0 {
		return "Основная"
	}
	return fmt.Sprintf("Дорожка %d", n+1)
}

// tracksFor — озвучки и субтитры серии. info — разбор видео, если известен.
func (s *server) tracksFor(video string, info *mediaInfo, sides []string) ([]trackRef, []subRef) {
	var audio []trackRef
	if info != nil {
		for n, a := range info.Audio {
			ref := trackRef{Label: embeddedLabel(a, n), Lang: strings.ToLower(a.Lang)}
			if n == 0 {
				ref.Main = true
			} else {
				id := shortID(video)
				ref.URL, ref.Live = s.liveURL(id, n), true
			}
			audio = append(audio, ref)
		}
	}
	if len(audio) == 0 {
		audio = append(audio, trackRef{Label: "Основная", Main: true})
	}
	var subs []subRef
	nSide := 0
	for _, p := range sides {
		ext := strings.ToLower(filepath.Ext(p))
		names, lang := sideLabel(video, p)
		id := shortID(p)
		agentStore.rememberPath(id, p)
		if sideSubExt[ext] {
			ref := subRef{Label: strings.Join(names, " · "), Lang: lang}
			if ref.Label == "" {
				ref.Label = "Субтитры"
				if l, ok := subLangs[lang]; ok {
					ref.Label = l
				}
			}
			ref.URL = s.cfg.base + "/subs/" + id + "?k=" + s.sign(id)
			subs = append(subs, ref)
			continue
		}
		nSide++
		ref := trackRef{Label: strings.Join(names, " · "), Lang: lang, File: true}
		if ref.Label == "" {
			if l, ok := audioLangs[lang]; ok {
				ref.Label = l
			} else {
				ref.Label = fmt.Sprintf("Озвучка %d", nSide)
			}
		}
		if nativeAudioExt[ext] {
			ref.URL = s.mediaURL(id)
		} else {
			ref.URL, ref.Live = s.liveURL(id, 0), true
		}
		audio = append(audio, ref)
	}
	// одинаковые подписи различаем номером — иначе в списке не понять, где какая
	seen := map[string]int{}
	for i := range audio {
		seen[audio[i].Label]++
		if k := seen[audio[i].Label]; k > 1 {
			audio[i].Label += fmt.Sprintf(" (%d)", k)
		}
	}
	if len(audio) < 2 {
		audio = nil // выбирать не из чего — кнопки не будет
	}
	return audio, subs
}

// cachedInfo — разбор из кэша без вызова ffprobe: при показе списка ждать
// разбора нельзя, а обход файлов его уже сделал.
func (s *store) cachedInfo(path string) *mediaInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.probes[shortID(path)].Info
}

// attachTracks дописывает видеофайлам списка озвучки и субтитры.
func (s *server) attachTracks(items []outItem) {
	dirs := map[string][]string{} // запасной путь: имена папки, если индекс о ней не знает
	for i := range items {
		if items[i].IsDir || !videoExt[strings.ToLower(filepath.Ext(items[i].Name))] {
			continue
		}
		p, ok := agentStore.pathOf(items[i].ID)
		if !ok {
			continue
		}
		sides, known := s.index.sidecars(p)
		if !known {
			dir := filepath.Dir(p)
			names, ok := dirs[dir]
			if !ok {
				if list, err := os.ReadDir(dir); err == nil {
					for _, it := range list {
						if !it.IsDir() && isSideExt(strings.ToLower(filepath.Ext(it.Name()))) {
							names = append(names, it.Name())
						}
					}
				}
				dirs[dir] = names
			}
			vs := stemOf(p)
			for _, n := range names {
				if sideMatches(stemOf(n), vs) {
					sides = append(sides, filepath.Join(dir, n))
				}
			}
		}
		info := items[i].Info
		if info == nil {
			info = agentStore.cachedInfo(p)
		}
		items[i].Audio, items[i].Subs = s.tracksFor(p, info, sides)
	}
}

// dropSidecars — список серий без того, что к ним прилагается: озвучки
// mp3/m4a рядом с серией не серии, а папки «Rus Sound», «Subs» без единого
// видео — не сезоны. В режиме «Файлы» всё это видно как есть.
func dropSidecars(dir string, items []entry) []entry {
	var stems []string
	for _, it := range items {
		if !it.IsDir && videoExt[strings.ToLower(filepath.Ext(it.Name))] {
			stems = append(stems, stemOf(it.Name))
		}
	}
	out := items[:0:0]
	for _, it := range items {
		if it.IsDir {
			if !hasVideoWithin(filepath.Join(dir, it.Name), 2) {
				continue
			}
		} else if ext := strings.ToLower(filepath.Ext(it.Name)); audioExt[ext] {
			side := false
			for _, st := range stems {
				if sideMatches(stemOf(it.Name), st) {
					side = true
					break
				}
			}
			if side {
				continue
			}
		}
		out = append(out, it)
	}
	return out
}

// hasVideoWithin — есть ли видео в папке или её подпапках до depth уровней.
func hasVideoWithin(dir string, depth int) bool {
	list, err := os.ReadDir(dir)
	if err != nil {
		return true // не прочиталась — пусть видно будет, что она есть
	}
	for _, it := range list {
		if strings.HasPrefix(it.Name(), ".") {
			continue
		}
		if it.IsDir() {
			if depth > 0 && hasVideoWithin(filepath.Join(dir, it.Name()), depth-1) {
				return true
			}
		} else if videoExt[strings.ToLower(filepath.Ext(it.Name()))] {
			return true
		}
	}
	return false
}

// --- звук на лету ---

// liveSlots — сколько перегонок звука одновременно. Смотрит одна приставка;
// запас — на смену дорожки, пока старый запрос ещё не закрылся.
var liveSlots = make(chan struct{}, 3)

// GET /trackaudio/{id}?k=&a=N&t=секунды — дорожка N файла в AAC стерео с
// момента t. Ответ — поток ADTS без длины: <audio> играет его, как радио.
func (s *server) trackAudio(w http.ResponseWriter, r *http.Request) {
	path, _ := s.fileFor(w, r)
	if path == "" {
		return
	}
	ext := strings.ToLower(filepath.Ext(path))
	if !videoExt[ext] && !sideAudioExt[ext] {
		http.NotFound(w, r)
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("a"))
	t, _ := strconv.ParseFloat(r.URL.Query().Get("t"), 64)
	if n < 0 || n > 64 || t < 0 || t > 86400 {
		http.Error(w, "плохие a или t", http.StatusBadRequest)
		return
	}
	select {
	case liveSlots <- struct{}{}:
		defer func() { <-liveSlots }()
	default:
		http.Error(w, "занято другими дорожками", http.StatusServiceUnavailable)
		return
	}
	args := []string{"-nostdin", "-v", "error"}
	if t > 0 {
		args = append(args, "-ss", strconv.FormatFloat(t, 'f', 2, 64))
	}
	args = append(args, "-i", path, "-map", "0:a:"+strconv.Itoa(n), "-vn", "-sn", "-dn",
		"-ac", "2", "-c:a", "aac", "-b:a", "192k", "-f", "adts", "pipe:1")
	cmd := exec.CommandContext(r.Context(), "ffmpeg", args...)
	w.Header().Set("Content-Type", "audio/aac")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	cmd.Stdout = flushWriter{w}
	if err := cmd.Run(); err != nil && r.Context().Err() == nil {
		log.Printf("дорожка %d из %s: %v", n, filepath.Base(path), err)
	}
}

// flushWriter отдаёт звук сразу, а не копит буфер: первые секунды нужны
// странице немедленно.
type flushWriter struct{ w http.ResponseWriter }

func (f flushWriter) Write(b []byte) (int, error) {
	n, err := f.w.Write(b)
	if fl, ok := f.w.(http.Flusher); ok {
		fl.Flush()
	}
	return n, err
}

// --- дорожки внутри файлов: найти и выгрузить рядом ---

type streamInfo struct {
	Type  string // audio | sub
	N     int    // номер среди дорожек своего вида (0:a:N)
	Codec string
	Lang  string
	Title string
}

var (
	streamsMu    sync.Mutex
	streamsCache = map[string][]streamInfo{} // путь|размер|время → дорожки
)

// probeStreams — звуковые дорожки и субтитры файла.
func probeStreams(path string) ([]streamInfo, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%s|%d|%d", path, st.Size(), st.ModTime().Unix())
	streamsMu.Lock()
	got, ok := streamsCache[key]
	streamsMu.Unlock()
	if ok {
		return got, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries",
		"stream=codec_type,codec_name:stream_tags=language,title,handler_name", "-of", "json", path).Output()
	if err != nil {
		return nil, errors.New("ffprobe не прочитал файл")
	}
	var raw struct {
		Streams []struct {
			CodecType string            `json:"codec_type"`
			CodecName string            `json:"codec_name"`
			Tags      map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if json.Unmarshal(out, &raw) != nil {
		return nil, errors.New("ffprobe ответил непонятно")
	}
	var list []streamInfo
	na, ns := 0, 0
	for _, s := range raw.Streams {
		tag := func(k string) string {
			for tk, v := range s.Tags {
				if strings.EqualFold(tk, k) {
					return strings.TrimSpace(v)
				}
			}
			return ""
		}
		switch s.CodecType {
		case "audio":
			list = append(list, streamInfo{"audio", na, s.CodecName, tag("language"), trackTitle(tag("title"), tag("handler_name"))})
			na++
		case "subtitle":
			list = append(list, streamInfo{"sub", ns, s.CodecName, tag("language"), trackTitle(tag("title"), tag("handler_name"))})
			ns++
		}
	}
	streamsMu.Lock()
	streamsCache[key] = list
	streamsMu.Unlock()
	return list, nil
}

func (si streamInfo) key() string {
	return si.Type + "|" + strings.ToLower(si.Lang) + "|" + strings.ToLower(si.Title)
}

func (si streamInfo) label() string {
	l := si.Title
	if l == "" {
		if si.Type == "audio" {
			l = audioLangs[strings.ToLower(si.Lang)]
		} else {
			l = subLangs[strings.ToLower(si.Lang)]
		}
	}
	if l == "" && si.Lang != "" && si.Lang != "und" {
		l = strings.ToUpper(si.Lang)
	}
	if l == "" {
		l = fmt.Sprintf("дорожка %d", si.N+1)
	}
	return l
}

// textSub — субтитры текстом (их можно положить файлом); картинкой (PGS,
// DVD) — нельзя без распознавания.
var textSub = map[string]string{"subrip": ".srt", "mov_text": ".srt", "webvtt": ".srt", "text": ".srt",
	"ass": ".ass", "ssa": ".ass"}

var reBadName = regexp.MustCompile(`[\\/:*?"<>|.\x00-\x1f]+`)

// sideName — имя файла для выгруженной дорожки: «S01E01.rus.LostFilm.mka».
// Разбор подписи (sideLabel) прочитает его обратно в «LostFilm», язык rus.
func (si streamInfo) sideName(video string) string {
	parts := []string{stemOf(video)}
	if l := strings.ToLower(strings.TrimSpace(si.Lang)); l != "" && l != "und" && !reBadName.MatchString(l) {
		parts = append(parts, l)
	}
	if t := strings.Trim(reSpaces.ReplaceAllString(reBadName.ReplaceAllString(si.Title, " "), " "), " "); t != "" {
		if r := []rune(t); len(r) > 60 {
			t = string(r[:60])
		}
		parts = append(parts, t)
	}
	if len(parts) == 1 {
		parts = append(parts, fmt.Sprintf("track%d", si.N+1))
	}
	ext := ".mka"
	if si.Type == "sub" {
		ext = textSub[si.Codec]
	}
	return strings.Join(parts, ".") + ext
}

// titleVideos — все видео в папке сериала на любой глубине (или сам файл фильма).
func titleVideos(root string) []string {
	st, err := os.Stat(root)
	if err != nil {
		return nil
	}
	if !st.IsDir() {
		if videoExt[strings.ToLower(filepath.Ext(root))] {
			return []string{root}
		}
		return nil
	}
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if p != root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() && videoExt[strings.ToLower(filepath.Ext(p))] {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

type trackSummary struct {
	Key    string `json:"key"`
	Type   string `json:"type"`
	Label  string `json:"label"`
	Lang   string `json:"lang,omitempty"`
	Codec  string `json:"codec"`
	Files  int    `json:"files"`  // в скольких сериях есть
	Main   int    `json:"main"`   // в скольких она первая (её играет видео)
	Done   int    `json:"done"`   // уже лежит рядом файлом
	NoFile bool   `json:"nofile"` // картинкой — файлом не положить
}

// GET /api/admin/tracks/titles — что можно выбрать: папки сериалов и фильмы
// из видеобиблиотек.
func (s *server) adminTrackTitles(w http.ResponseWriter, r *http.Request) {
	type title struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Lib   string `json:"lib"`
		IsDir bool   `json:"is_dir"`
	}
	out := []title{}
	for _, lib := range s.cfg.roots {
		if !isVideoLib(lib.Kind) || !lib.available() {
			continue
		}
		list, err := os.ReadDir(lib.Path)
		if err != nil {
			continue
		}
		for _, it := range list {
			name := it.Name()
			if strings.HasPrefix(name, ".") || (!it.IsDir() && !videoExt[strings.ToLower(filepath.Ext(name))]) {
				continue
			}
			p := filepath.Join(lib.Path, name)
			id := shortID(p)
			agentStore.rememberPath(id, p)
			out = append(out, title{id, name, lib.Title, it.IsDir()})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	writeJSON(w, http.StatusOK, map[string]any{"titles": out})
}

// pathsByIDs — пути выбранного в библиотеках; чужое и пропавшее молча мимо.
func (s *server) pathsByIDs(ids []string) []string {
	var out []string
	for _, id := range ids {
		if p, ok := agentStore.pathOf(id); ok && s.inRoots(p) {
			out = append(out, p)
		}
	}
	return out
}

// POST /api/admin/tracks/scan {ids} — какие дорожки внутри серий выбранного.
func (s *server) adminTrackScan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req) != nil || len(req.IDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "выберите сериалы"})
		return
	}
	var videos []string
	for _, root := range s.pathsByIDs(req.IDs) {
		videos = append(videos, titleVideos(root)...)
	}
	if len(videos) > 2000 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "слишком много серий сразу — выберите меньше"})
		return
	}
	sums := map[string]*trackSummary{}
	var order []string
	bad := 0
	for _, v := range videos {
		list, err := probeStreams(v)
		if err != nil {
			bad++
			continue
		}
		for _, si := range list {
			k := si.key()
			sm, ok := sums[k]
			if !ok {
				sm = &trackSummary{Key: k, Type: si.Type, Label: si.label(), Lang: si.Lang, Codec: si.Codec}
				if si.Type == "sub" && textSub[si.Codec] == "" {
					sm.NoFile = true
				}
				sums[k] = sm
				order = append(order, k)
			}
			sm.Files++
			if si.Type == "audio" && si.N == 0 {
				sm.Main++
			}
			if !sm.NoFile {
				if _, err := os.Stat(filepath.Join(filepath.Dir(v), si.sideName(v))); err == nil {
					sm.Done++
				}
			}
		}
	}
	out := make([]*trackSummary, 0, len(order))
	for _, k := range order {
		out = append(out, sums[k])
	}
	writeJSON(w, http.StatusOK, map[string]any{"videos": len(videos), "unreadable": bad, "tracks": out})
}

// POST /api/admin/tracks/extract {ids, keys} — фоновая задача: выбранные
// дорожки каждой серии — отдельными файлами рядом, без перекодирования.
func (s *server) adminTrackExtract(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs  []string `json:"ids"`
		Keys []string `json:"keys"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req) != nil || len(req.IDs) == 0 || len(req.Keys) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "выберите сериалы и дорожки"})
		return
	}
	roots := s.pathsByIDs(req.IDs)
	if len(roots) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "сериалы не найдены — обновите список"})
		return
	}
	want := map[string]bool{}
	for _, k := range req.Keys {
		want[k] = true
	}
	title := "Дорожки в файлы: " + filepath.Base(roots[0])
	if len(roots) > 1 {
		title += fmt.Sprintf(" и ещё %d", len(roots)-1)
	}
	s.startTask(w, "tracks", title, func(ctx context.Context, t *task) error {
		return s.extractTracks(ctx, t, roots, want)
	})
}

func (s *server) extractTracks(ctx context.Context, t *task, roots []string, want map[string]bool) error {
	var videos []string
	for _, root := range roots {
		videos = append(videos, titleVideos(root)...)
	}
	made, skipped, failed := 0, 0, 0
	for i, v := range videos {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		t.set(filepath.Base(v), i, len(videos))
		list, err := probeStreams(v)
		if err != nil {
			t.say("%s: %v", filepath.Base(v), err)
			failed++
			continue
		}
		for _, si := range list {
			if !want[si.key()] || (si.Type == "sub" && textSub[si.Codec] == "") {
				continue
			}
			name := si.sideName(v)
			dst := filepath.Join(filepath.Dir(v), name)
			if _, err := os.Stat(dst); err == nil {
				skipped++
				continue
			}
			if err := extractOne(ctx, v, si, dst); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				t.say("%s: %v", name, err)
				failed++
				continue
			}
			if st, err := os.Stat(dst); err == nil {
				s.index.put(dst, s.analyzeFile(dst, st))
			}
			made++
		}
	}
	s.index.mu.Lock()
	s.index.rebuildDirs()
	s.index.mu.Unlock()
	t.say("выгружено файлов: %d, уже были: %d, не вышло: %d", made, skipped, failed)
	if made == 0 && failed > 0 {
		return errors.New("ни одна дорожка не выгрузилась")
	}
	return nil
}

// extractOne — одна дорожка в файл: копия потока, без перекодирования
// (субтитры mov_text/webvtt — в SRT). Пишет во временное скрытое имя:
// оборванная выгрузка не притворится готовой дорожкой.
func extractOne(ctx context.Context, video string, si streamInfo, dst string) error {
	tmp := filepath.Join(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp")
	defer os.Remove(tmp)
	args := []string{"-nostdin", "-v", "error", "-y", "-i", video}
	if si.Type == "audio" {
		args = append(args, "-map", "0:a:"+strconv.Itoa(si.N), "-vn", "-sn", "-dn", "-c", "copy", "-f", "matroska", tmp)
	} else {
		codec, format := "srt", "srt"
		if textSub[si.Codec] == ".ass" {
			codec, format = "copy", "ass"
		}
		args = append(args, "-map", "0:s:"+strconv.Itoa(si.N), "-vn", "-an", "-dn", "-c:s", codec, "-f", format, tmp)
	}
	out, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 200 {
			msg = msg[len(msg)-200:]
		}
		return fmt.Errorf("ffmpeg: %v %s", err, msg)
	}
	return os.Rename(tmp, dst)
}
