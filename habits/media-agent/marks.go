package main

// Заставка и титры: где показывать «⏭ Пропустить».
//
// Отметки бывают трёх уровней: на весь сериал, на сезон и на отдельную серию.
// Играет самая точная: у серии — её собственная, иначе сезонная, иначе общая.
// Так у сериала с разными заставками по сезонам достаточно открыть по одной
// серии в каждом сезоне, а сериал с одинаковой заставкой отмечается один раз.
//
// Начало и конец заставки считаются от начала серии (заставка идёт в одно и то
// же время), титры — от КОНЦА: серии разной длины, а титры одинаковые.
// «Титры кончаются» тоже от конца: 0 — идут до самого конца серии, иначе после
// них есть сцена, и пропуск ведёт к ней, а не к следующей серии.
//
// Ключ записи — хеш пути (сериала, сезона или файла). Путь хранится рядом:
// по нему отметки переезжают вместе с переименованной папкой.

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type seriesMarks struct {
	IntroStart        float64 `json:"intro_start"`
	IntroEnd          float64 `json:"intro_end"`            // 0 — заставка не отмечена
	CreditsFromEnd    float64 `json:"credits_from_end"`     // 0 — титры не отмечены
	CreditsEndFromEnd float64 `json:"credits_end_from_end"` // 0 — титры до конца серии
	AutoSkip          bool    `json:"auto_skip"`            // пропускать заставку без вопроса
	SkipOff           bool    `json:"skip_off"`             // не предлагать пропуск у этого сериала
	Scope             string  `json:"scope,omitempty"`      // series | season | episode
	Path              string  `json:"path,omitempty"`       // чтобы пережить переименование
}

func (m seriesMarks) empty() bool {
	return m.IntroStart == 0 && m.IntroEnd == 0 && m.CreditsFromEnd == 0 &&
		m.CreditsEndFromEnd == 0 && !m.AutoSkip && !m.SkipOff
}

type marksStore struct {
	mu    sync.Mutex
	path  string
	items map[string]seriesMarks
}

func newMarksStore(cacheDir string) *marksStore {
	m := &marksStore{path: filepath.Join(cacheDir, "marks.json"), items: map[string]seriesMarks{}}
	if data, err := os.ReadFile(m.path); err == nil {
		_ = json.Unmarshal(data, &m.items)
		if m.items == nil {
			m.items = map[string]seriesMarks{}
		}
	}
	return m
}

func (m *marksStore) saveLocked() {
	if data, err := json.Marshal(m.items); err == nil {
		_ = writeAtomic(m.path, data)
	}
}

// moveMarks переносит отметки за переименованной папкой или файлом.
func (s *server) moveMarks(old, neu string, isDir bool) {
	s.marks.mu.Lock()
	defer s.marks.mu.Unlock()
	sep := string(filepath.Separator)
	changed := false
	for key, rec := range s.marks.items {
		p := rec.Path
		neuPath := ""
		switch {
		case p == "":
			continue
		case p == old:
			neuPath = neu
		case isDir && strings.HasPrefix(p, old+sep):
			neuPath = neu + p[len(old):]
		default:
			continue
		}
		delete(s.marks.items, key)
		rec.Path = neuPath
		s.marks.items[shortID(neuPath)] = rec
		changed = true
	}
	if changed {
		s.marks.saveLocked()
	}
}

// marksScopes — три уровня отметок для серии: сама серия, её сезон, сериал.
// Рядом с ключом храним путь, от которого он взят: по нему запись переезжает
// за переименованием. У фильма прямо в корне библиотеки «сериал» — сам файл,
// у фильма в папке и у сериала — папка.
type markLevel struct{ key, path string }

type marksScopes struct {
	series, season, episode markLevel // пустой key — уровня нет
}

func (s *server) scopesFor(path string) (marksScopes, owner) {
	o := s.ownerOf(path)
	seriesPath, _ := agentStore.pathOf(o.key)
	sc := marksScopes{series: markLevel{o.key, seriesPath}, episode: markLevel{shortID(path), path}}
	if season := filepath.Dir(path); o.kind == "series" && season != o.dir {
		sc.season = markLevel{shortID(season), season}
	}
	return sc, o
}

// merged — самая точная отметка: у серии главнее сезонной, сезонная главнее
// общей. Заставка и титры берутся по отдельности: сезон может уточнять только
// заставку, а титры остаются общими. Галочки «пропускать сам» и «не предлагать
// пропуск» — общие для всех уровней: включённая где угодно, включена везде.
func (m *marksStore) merged(sc marksScopes) seriesMarks {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := seriesMarks{}
	for _, lvl := range []markLevel{sc.series, sc.season, sc.episode} {
		if lvl.key == "" {
			continue
		}
		rec, ok := m.items[lvl.key]
		if !ok {
			continue
		}
		if rec.IntroEnd > 0 {
			out.IntroStart, out.IntroEnd = rec.IntroStart, rec.IntroEnd
		}
		if rec.CreditsFromEnd > 0 {
			out.CreditsFromEnd, out.CreditsEndFromEnd = rec.CreditsFromEnd, rec.CreditsEndFromEnd
		}
		// Выключатели складываем, а не перекрываем: они ставятся на сериал, а
		// запись серии, добавленная поиском по звуку, их не касается — иначе
		// «не предлагать пропуск» слетало бы на каждой размеченной серии.
		out.AutoSkip = out.AutoSkip || rec.AutoSkip
		out.SkipOff = out.SkipOff || rec.SkipOff
	}
	return out
}

func (m *marksStore) get(key string) seriesMarks {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.items[key]
}

// marksKey — чья это серия: ключи всех трёх уровней.
func (s *server) marksTarget(w http.ResponseWriter, fileID string) (string, marksScopes, owner, bool) {
	path, ok := agentStore.pathOf(fileID)
	if !ok || !s.inRoots(path) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "файл не найден"})
		return "", marksScopes{}, owner{}, false
	}
	sc, o := s.scopesFor(path)
	return path, sc, o, true
}

func (s *server) marksReply(w http.ResponseWriter, path string, sc marksScopes, o owner) {
	out := map[string]any{
		"key": o.key, "kind": o.kind, "marks": s.marks.merged(sc),
		"series": s.marks.get(sc.series.key), "episode": s.marks.get(sc.episode.key),
		"season_id": sc.season.key, "episode_id": sc.episode.key,
		"season_name": "", "episode_name": strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
	}
	if sc.season.key != "" {
		out["season"] = s.marks.get(sc.season.key)
		out["season_name"] = filepath.Base(filepath.Dir(path))
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/marks?file= — отметки для этой серии: что играет (merged) и что
// записано на каждом уровне.
func (s *server) getMarks(w http.ResponseWriter, r *http.Request) {
	path, sc, o, ok := s.marksTarget(w, r.URL.Query().Get("file"))
	if !ok {
		return
	}
	s.marksReply(w, path, sc, o)
}

// PUT /api/marks {file, scope, intro_start?, intro_end?, credits_from_end?,
// credits_end_from_end?, auto_skip?, reset?} — scope: series (по умолчанию),
// season или episode.
func (s *server) putMarks(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File              string   `json:"file"`
		Scope             string   `json:"scope"`
		IntroStart        *float64 `json:"intro_start"`
		IntroEnd          *float64 `json:"intro_end"`
		CreditsFromEnd    *float64 `json:"credits_from_end"`
		CreditsEndFromEnd *float64 `json:"credits_end_from_end"`
		AutoSkip          *bool    `json:"auto_skip"`
		SkipOff           *bool    `json:"skip_off"`
		Reset             bool     `json:"reset"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil || req.File == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужен file"})
		return
	}
	path, sc, o, ok := s.marksTarget(w, req.File)
	if !ok {
		return
	}
	lvl := sc.series
	switch req.Scope {
	case "", "series":
		req.Scope = "series"
	case "season":
		if sc.season.key == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "у этой серии нет сезона — отмечайте на сериал"})
			return
		}
		lvl = sc.season
	case "episode":
		lvl = sc.episode
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scope: series, season или episode"})
		return
	}
	pos := func(p *float64) float64 { return max(0, min(*p, 6*3600)) }
	// Уточняют один край, а второй записан уровнем выше: без него запись
	// неполная и пропала бы при проверке — доносим унаследованное значение.
	inherited := s.marks.merged(sc)
	s.marks.mu.Lock()
	m := s.marks.items[lvl.key]
	if req.Reset {
		m = seriesMarks{}
	}
	if req.IntroStart != nil && m.IntroEnd == 0 {
		m.IntroEnd = inherited.IntroEnd
	}
	if req.CreditsEndFromEnd != nil && m.CreditsFromEnd == 0 {
		m.CreditsFromEnd = inherited.CreditsFromEnd
	}
	if req.IntroStart != nil {
		m.IntroStart = pos(req.IntroStart)
	}
	if req.IntroEnd != nil {
		m.IntroEnd = pos(req.IntroEnd)
	}
	if req.CreditsFromEnd != nil {
		m.CreditsFromEnd = pos(req.CreditsFromEnd)
	}
	if req.CreditsEndFromEnd != nil {
		m.CreditsEndFromEnd = pos(req.CreditsEndFromEnd)
	}
	if req.AutoSkip != nil {
		m.AutoSkip = *req.AutoSkip
	}
	if req.SkipOff != nil {
		m.SkipOff = *req.SkipOff
	}
	// конец раньше начала — начало не отмечали или отметили не то
	if m.IntroEnd > 0 && m.IntroStart >= m.IntroEnd {
		m.IntroStart = 0
	}
	// «титры кончаются» позже их начала — значит, до конца серии
	if m.CreditsEndFromEnd >= m.CreditsFromEnd {
		m.CreditsEndFromEnd = 0
	}
	if m.empty() {
		delete(s.marks.items, lvl.key)
	} else {
		m.Scope, m.Path = req.Scope, lvl.path
		s.marks.items[lvl.key] = m
	}
	s.marks.saveLocked()
	s.marks.mu.Unlock()
	s.marksReply(w, path, sc, o)
}

// --- отметки из самого файла ---
//
// Общедоступной базы таймингов заставок для сериалов нет (для аниме есть
// AniSkip, но он ищет по идентификатору MyAnimeList). Зато тайминги часто
// лежат в самом файле: у mkv бывают главы — «Intro», «Opening», «Ending
// Credits». Их и читаем: одна кнопка вместо секундомера.

type chapter struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Title string  `json:"title"`
}

var (
	reIntroChapter   = regexp.MustCompile(`(?i)intro|opening|^op$|^op[\s.:-]|заставк|вступлен|начальн`)
	reCreditsChapter = regexp.MustCompile(`(?i)credit|ending|outro|^ed$|^ed[\s.:-]|титр|концовк|финальн`)
)

func ffchapters(path string) ([]chapter, float64) {
	out, err := exec.Command("ffprobe", "-v", "error", "-print_format", "json",
		"-show_chapters", "-show_format", path).Output()
	if err != nil {
		return nil, 0
	}
	var raw struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Chapters []struct {
			Start string            `json:"start_time"`
			End   string            `json:"end_time"`
			Tags  map[string]string `json:"tags"`
		} `json:"chapters"`
	}
	if json.Unmarshal(out, &raw) != nil {
		return nil, 0
	}
	dur, _ := strconv.ParseFloat(raw.Format.Duration, 64)
	chs := make([]chapter, 0, len(raw.Chapters))
	for _, c := range raw.Chapters {
		ch := chapter{Title: c.Tags["title"]}
		ch.Start, _ = strconv.ParseFloat(c.Start, 64)
		ch.End, _ = strconv.ParseFloat(c.End, 64)
		chs = append(chs, ch)
	}
	return chs, dur
}

// marksFromChapters — что из глав похоже на заставку и на титры. Заставка
// считается от начала, титры — от конца, как и везде в отметках.
func marksFromChapters(chs []chapter, dur float64) seriesMarks {
	out := seriesMarks{}
	for _, ch := range chs {
		switch {
		// заставка — только в первой четверти серии, иначе это «интро сезона»
		case reIntroChapter.MatchString(ch.Title) && out.IntroEnd == 0 &&
			ch.End > ch.Start+5 && (dur == 0 || ch.Start < dur/4):
			out.IntroStart, out.IntroEnd = ch.Start, ch.End
		// титры — в последней трети
		case reCreditsChapter.MatchString(ch.Title) && out.CreditsFromEnd == 0 &&
			dur > 0 && ch.Start > dur*2/3:
			out.CreditsFromEnd = dur - ch.Start
			if dur-ch.End > 2 { // после титров ещё что-то есть — сцена
				out.CreditsEndFromEnd = dur - ch.End
			}
		}
	}
	return out
}

// GET /api/marks/chapters?file= — главы файла и то, что из них вышло.
func (s *server) chapterMarks(w http.ResponseWriter, r *http.Request) {
	path, ok := agentStore.pathOf(r.URL.Query().Get("file"))
	if !ok || !s.inRoots(path) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "файл не найден"})
		return
	}
	chs, dur := ffchapters(path)
	if chs == nil {
		chs = []chapter{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"chapters": chs, "duration": dur, "found": marksFromChapters(chs, dur),
	})
}
