package main

// Доп материалы сериала — «SPs», «Specials», «Extras», «Доп материалы»: папка
// внутри сезона (у аниме так лежат коротышки — по одной на серию) или рядом с
// сезонами (туда кладут «как снимался сериал»). Отдельной библиотекой их не
// сделать: это тот же сериал, и смотреть их хочется либо как ещё один сезон,
// либо вперемешку с сериями.
//
// Как они лежат — решает раздача, поэтому агент ничего не просит помечать
// руками: нашлась папка с таким именем — значит, допы. Настроек две, и у них
// разное время жизни:
//
//   - «смотреть вместе со спешиалами» живёт в сеансе приставки и приезжает
//     параметром extras=1: включено по умолчанию, но забывается при выходе;
//   - «после каждой серии» — свойство сериала, и хранится здесь, в extras.json.
//     Ключ записи — ПУТЬ, а не id: id это хеш пути, и переименование в админке
//     сбросило бы настройку (так же сделано в избранном).
//
// Выключенная настройка — это «допы отдельно, как ещё один сезон»: так удобнее
// для «как снимался сериал», а «после каждой серии» — для аниме, где коротышка
// привязана к своей серии.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Имена папок с допами. Список, а не «всё, что не сезон»: папка с незнакомым
// именем — скорее сезон под чужим названием («Фильмы», «OVA»), и утащить её в
// допы хуже, чем не узнать.
var extrasDirNames = map[string]bool{
	"sp": true, "sps": true, "special": true, "specials": true,
	"extra": true, "extras": true, "bonus": true, "bonuses": true,
	"доп": true, "допы": true, "доп материалы": true, "дополнения": true,
	"дополнительные материалы": true, "дополнительно": true,
}

// epNumber — номер серии из имени файла, но только там, где он назван явно:
// «S01E02», «1x02», «E12», « - 12 - ». Просто «первое число» не годится — в
// именах полно «1080p», «x265» и годов.
var epNumberRx = []*regexp.Regexp{
	regexp.MustCompile(`(?i)s\s*\d{1,2}\s*[\s._-]*e\s*(\d{1,3})`),
	regexp.MustCompile(`(?i)(?:^|[^\d])\d{1,2}x(\d{2,3})(?:[^\d]|$)`),
	regexp.MustCompile(`(?i)(?:^|[\s._\-\[(])e\s*(\d{1,3})(?:[^\d]|$)`),
	regexp.MustCompile(`(?:^|[\s._-])-\s*(\d{1,3})\s*-(?:[\s._-]|$)`),
}

func extrasName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.NewReplacer("_", " ", ".", " ", "-", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// isExtrasDir — папка с допами (по имени).
func isExtrasDir(name string) bool {
	return extrasDirNames[extrasName(name)]
}

func epNumber(name string) (int, bool) {
	for _, rx := range epNumberRx {
		if m := rx.FindStringSubmatch(name); m != nil {
			if n, err := strconv.Atoi(m[len(m)-1]); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

// seasonPart — кнопка на экране сериала: сезон или его допы.
type seasonPart struct {
	path  string
	name  string
	extra bool
}

// extrasTitle — подпись кнопки с допами. Внутри сезона — с его именем, чтобы
// у «Аптекарши» было видно, к какому сезону коротышки; рядом с сезонами общее
// «Доп материалы», но своё название папки («Как снимался сериал») сохраняем.
func extrasTitle(dir, season string) string {
	if season != "" {
		return season + " · доп"
	}
	if extrasDirNames[extrasName(dir)] {
		return "Доп материалы"
	}
	return dir
}

// seriesParts — из чего состоит сериал: сезоны в обычном порядке, у каждого
// сразу за ним его допы, в конце — допы, лежащие рядом с сезонами. Пусто, если
// это вообще не сериал с сезонами (фильм в папке, мини-сериал) — тогда
// каталог разбирает папку по-старому.
func seriesParts(seriesDir string) []seasonPart {
	sub, vids, _ := dirContents(seriesDir)
	if !hasSeasonDirs(sub) {
		return nil
	}
	var out, beside []seasonPart
	if len(vids) > 0 {
		// серии прямо в папке сериала рядом с сезонами — отдельным «сезоном»
		out = append(out, seasonPart{path: seriesDir, name: "Без сезона"})
	}
	for _, sd := range sub {
		base := filepath.Base(sd)
		inner, svids, _ := dirContents(sd)
		if isExtrasDir(base) {
			if len(svids) > 0 {
				beside = append(beside, seasonPart{path: sd, name: extrasTitle(base, ""), extra: true})
			}
			continue
		}
		if len(svids) > 0 {
			out = append(out, seasonPart{path: sd, name: base})
		}
		for _, ed := range inner {
			if !isExtrasDir(filepath.Base(ed)) {
				continue
			}
			if _, ev, _ := dirContents(ed); len(ev) > 0 {
				out = append(out, seasonPart{path: ed, name: extrasTitle(filepath.Base(ed), base), extra: true})
			}
		}
	}
	return append(out, beside...)
}

// extrasOf — папка с допами этого сезона, если она у него есть.
func extrasOf(seasonDir string) string {
	inner, _, _ := dirContents(seasonDir)
	for _, d := range inner {
		if !isExtrasDir(filepath.Base(d)) {
			continue
		}
		if _, v, _ := dirContents(d); len(v) > 0 {
			return d
		}
	}
	return ""
}

// seasonChain — порядок, в котором сериал играется сам, без участия человека:
// сезоны подряд, а допы либо не играются вовсе (галочка снята), либо после
// всех сезонов. При «после каждой серии» их здесь нет — они уже внутри
// сезонов, см. mixExtras.
func seasonChain(parts []seasonPart, withExtras, inline bool) []seasonPart {
	out := make([]seasonPart, 0, len(parts))
	for _, p := range parts {
		if !p.extra {
			out = append(out, p)
		}
	}
	if !withExtras || inline {
		return out
	}
	for _, p := range parts {
		if p.extra {
			out = append(out, p)
		}
	}
	return out
}

// mixExtras — допы встают сразу за своими сериями.
//
// Пара ищется по порядку, когда серий и допов одинаково (так у «Аптекарши»:
// 24 коротышки на 24 серии, и это самый частый случай), иначе по явному номеру
// в имени. Номер — только в виде «S01E02», «E12» и подобных: у аниме имена
// вида «Hitorigoto 01 [BDRip 1080p x265 FLAC]», и «первое число» поймало бы
// 1080 или 265. Что не нашло свою серию, едет в конец сезона: потерять допы
// хуже, чем поставить их не туда.
func mixExtras(eps, extras []entry) []entry {
	if len(extras) == 0 {
		return eps
	}
	films := make([]entry, 0, len(eps))
	for _, e := range eps {
		if !e.IsDir {
			films = append(films, e)
		}
	}
	after := map[int][]entry{} // индекс серии → допы за ней
	var tail []entry
	if len(films) == len(extras) && len(films) > 0 {
		for i, x := range extras {
			after[i] = append(after[i], x)
		}
	} else {
		byNum := map[int]int{}
		for i, e := range films {
			if n, ok := epNumber(e.Name); ok {
				if _, seen := byNum[n]; !seen {
					byNum[n] = i
				}
			}
		}
		for _, x := range extras {
			if n, ok := epNumber(x.Name); ok {
				if i, found := byNum[n]; found {
					after[i] = append(after[i], x)
					continue
				}
			}
			tail = append(tail, x)
		}
	}
	out := make([]entry, 0, len(eps)+len(extras))
	for _, e := range eps {
		if e.IsDir {
			out = append(out, e)
		}
	}
	for i, e := range films {
		out = append(out, e)
		out = append(out, after[i]...)
	}
	return append(out, tail...)
}

// --- настройка «после каждой серии» ---

type extrasStore struct {
	mu     sync.Mutex
	path   string
	inline map[string]bool // путь сериала → допы вперемешку с сериями
}

func newExtrasStore(cacheDir string) *extrasStore {
	e := &extrasStore{path: filepath.Join(cacheDir, "extras.json"), inline: map[string]bool{}}
	if data, err := os.ReadFile(e.path); err == nil {
		var list []string
		if json.Unmarshal(data, &list) == nil {
			for _, p := range list {
				e.inline[p] = true
			}
		}
	}
	return e
}

func (es *extrasStore) saveLocked() {
	list := make([]string, 0, len(es.inline))
	for p := range es.inline {
		list = append(list, p)
	}
	sort.Strings(list)
	if data, err := json.MarshalIndent(list, "", " "); err == nil {
		_ = writeAtomic(es.path, data)
	}
}

func (es *extrasStore) isInline(seriesDir string) bool {
	if seriesDir == "" {
		return false
	}
	es.mu.Lock()
	defer es.mu.Unlock()
	return es.inline[seriesDir]
}

func (es *extrasStore) setInline(seriesDir string, on bool) {
	es.mu.Lock()
	defer es.mu.Unlock()
	if on {
		es.inline[seriesDir] = true
	} else {
		delete(es.inline, seriesDir)
	}
	es.saveLocked()
}

// move — сериал переименовали в админке: настройка едет за ним.
func (es *extrasStore) move(old, neu string, isDir bool) {
	if !isDir {
		return
	}
	es.mu.Lock()
	defer es.mu.Unlock()
	changed := false
	for p := range es.inline {
		if p != old && !strings.HasPrefix(p, old+string(filepath.Separator)) {
			continue
		}
		delete(es.inline, p)
		es.inline[neu+strings.TrimPrefix(p, old)] = true
		changed = true
	}
	if changed {
		es.saveLocked()
	}
}

// POST /api/extras {id, inline} — переключатель «спешлы после каждой серии» у
// одного сериала. Без пина: это не правка библиотеки, а то же самое, что
// избранное, — свойство сериала, которое меняют с пульта.
func (s *server) setExtrasMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string `json:"id"`
		Inline bool   `json:"inline"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужен id"})
		return
	}
	path, ok := agentStore.pathOf(req.ID)
	if !ok || !s.inRoots(path) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "сериал не найден"})
		return
	}
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "это не сериал"})
		return
	}
	s.extras.setInline(path, req.Inline)
	writeJSON(w, http.StatusOK, map[string]bool{"inline": req.Inline})
}

// seriesDirOf — папка сериала, в которой лежит этот файл или папка. Считать
// «папка сезона = папка файла, сериал = уровнем выше» нельзя: у допов внутри
// сезона это на уровень глубже, поэтому поднимаемся, пока не окажемся прямо
// под корнем библиотеки.
func (s *server) seriesDirOf(dir string) string {
	for cur := dir; cur != "" && s.inRoots(cur) && !s.isRoot(cur); cur = filepath.Dir(cur) {
		if s.isRoot(filepath.Dir(cur)) {
			return cur
		}
		if filepath.Dir(cur) == cur {
			break
		}
	}
	return ""
}

// seasonItems — серии папки для плеера: при включённой галочке и режиме
// «после каждой серии» к ним подмешиваются допы этого сезона. Одно место для
// обеих ручек — и /api/browse, и /api/next-season: иначе список на экране и
// список, который играется дальше сам, разошлись бы.
func (s *server) seasonItems(dir string, withExtras bool) ([]entry, error) {
	items, err := agentStore.browse(dir, s.q.ready)
	if err != nil {
		return nil, err
	}
	if !withExtras || isExtrasDir(filepath.Base(dir)) {
		return items, nil
	}
	series := s.seriesDirOf(dir)
	if series == "" || series == dir || !s.extras.isInline(series) {
		return items, nil
	}
	ed := extrasOf(dir)
	if ed == "" {
		return items, nil
	}
	extra, err := agentStore.browse(ed, s.q.ready)
	if err != nil {
		return items, nil
	}
	files := make([]entry, 0, len(extra))
	for _, e := range extra {
		if !e.IsDir {
			files = append(files, e)
		}
	}
	return mixExtras(items, files), nil
}
