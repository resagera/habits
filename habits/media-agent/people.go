package main

// Актёры списком: все, кто встречается в описаниях медиатеки и планов, с
// правками из админки — имя, описание, своё фото. Правки лежат в
// people.json, своё фото — people/<pid>.custom.jpg рядом со скачанными.
//
// Ключ человека — Q-id Wikidata, а у старых описаний без идентификаторов —
// «name:Имя». Имя, которое где-то уже встретилось вместе с Q-id, считается
// тем же человеком: так «Том Харди» из старого списка актёров и Q208026 из
// нового состава — одна плашка с общими счётчиками.

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type personEdit struct {
	Name   string `json:"name,omitempty"`
	About  string `json:"about,omitempty"`
	Photo  bool   `json:"photo,omitempty"`
	Edited int64  `json:"edited,omitempty"`
}

type peopleStore struct {
	mu     sync.Mutex
	path   string
	People map[string]personEdit `json:"people"`
}

func newPeopleStore(cacheDir string) *peopleStore {
	p := &peopleStore{path: filepath.Join(cacheDir, "people.json"), People: map[string]personEdit{}}
	if data, err := os.ReadFile(p.path); err == nil {
		_ = json.Unmarshal(data, p)
	}
	if p.People == nil {
		p.People = map[string]personEdit{}
	}
	return p
}

func (p *peopleStore) get(key string) (personEdit, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.People[normPersonKey(key)]
	return e, ok
}

func (p *peopleStore) set(key string, fn func(*personEdit)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	k := normPersonKey(key)
	e := p.People[k]
	fn(&e)
	e.Edited = time.Now().Unix()
	p.People[k] = e
	if data, err := json.Marshal(p); err == nil {
		_ = writeAtomic(p.path, data)
	}
}

func nameKey(name string) string { return "name:" + strings.TrimSpace(name) }

// personEdits — правки человека: по Q-id, а нет их — сделанные, пока Q-id
// у него ещё не было (по имени). Иначе ночной сбор состава «стирал» бы их.
func (s *server) personEdits(key, name string) (personEdit, bool) {
	if e, ok := s.people.get(key); ok {
		return e, true
	}
	if reQID.MatchString(key) && name != "" {
		return s.people.get(nameKey(name))
	}
	return personEdit{}, false
}

// normPersonKey — ключ для хранения: у имени регистр не важен.
func normPersonKey(key string) string {
	if n, ok := strings.CutPrefix(key, "name:"); ok {
		return "name:" + strings.ToLower(strings.TrimSpace(n))
	}
	return key
}

func validPersonKey(key string) bool {
	if reQID.MatchString(key) {
		return true
	}
	n, ok := strings.CutPrefix(key, "name:")
	return ok && strings.TrimSpace(n) != "" && len(n) <= 200
}

// personFileID — имя файлов человека: Q-id или «n-<12 знаков sha1 имени>».
func personFileID(key string) string {
	if reQID.MatchString(key) {
		return key
	}
	n, _ := strings.CutPrefix(normPersonKey(key), "name:")
	sum := sha1.Sum([]byte(n))
	return "n-" + hex.EncodeToString(sum[:])[:12]
}

var rePersonFile = regexp.MustCompile(`^(Q[0-9]{1,12}|n-[0-9a-f]{12})$`)

func (s *server) personCustomPath(pid string) string {
	return filepath.Join(s.cfg.cache, "people", pid+".custom.jpg")
}

// personPhotoURL — ссылка на фото, если оно есть или может быть скачано.
// Своё фото — с меткой времени в ссылке: скачанное браузер помнит месяц, и без
// метки замена не была бы видна.
func (s *server) personPhotoURL(qid, name string) string {
	// своё фото — по Q-id, а нет — сделанное, пока человек был только именем
	var pids []string
	if qid != "" {
		pids = append(pids, qid)
	}
	if name != "" {
		pids = append(pids, personFileID(nameKey(name)))
	}
	for _, pid := range pids {
		if st, err := os.Stat(s.personCustomPath(pid)); err == nil && st.Size() > 0 {
			return s.cfg.base + "/person/" + pid + "?v=" + strconv.FormatInt(st.ModTime().UnixNano()/1e6, 10)
		}
	}
	if qid != "" {
		return s.cfg.base + "/person/" + qid
	}
	return ""
}

// --- список ---

type personRow struct {
	Key   string `json:"key"`
	QID   string `json:"qid,omitempty"`
	Name  string `json:"name"`
	About string `json:"about,omitempty"`
	Photo string `json:"photo,omitempty"`
	Have  int    `json:"have"`
	Plans int    `json:"plans"`
}

// peopleInfos — описания медиатеки и активных планов (второе значение — план ли).
func (s *server) peopleInfos() (infos []*itemInfo, isPlan []bool) {
	films, series, anime := s.rawVideoTiles()
	for _, t := range append(append(films, series...), anime...) {
		if p, ok := agentStore.pathOf(t.ID); ok {
			if inf, ok := readInfo(p); ok {
				infos, isPlan = append(infos, inf), append(isPlan, false)
			}
		}
	}
	for _, pl := range s.plans.all() {
		if pl.Archived == 0 && pl.Info != nil {
			infos, isPlan = append(infos, pl.Info), append(isPlan, true)
		}
	}
	return
}

func (s *server) peopleIndex() []personRow {
	infos, isPlan := s.peopleInfos()
	// имя → Q-id по всем составам с идентификаторами
	byName := map[string]string{}
	names := map[string]string{} // ключ → имя для показа
	for _, inf := range infos {
		for _, c := range inf.Cast {
			if c.QID != "" {
				if _, ok := byName[strings.ToLower(c.Name)]; !ok {
					byName[strings.ToLower(c.Name)] = c.QID
				}
				if names[c.QID] == "" {
					names[c.QID] = c.Name
				}
			}
		}
	}
	rows := map[string]*personRow{}
	for i, inf := range infos {
		seen := map[string]bool{}
		touch := func(key, name string) {
			if seen[key] {
				return
			}
			seen[key] = true
			r := rows[key]
			if r == nil {
				r = &personRow{Key: key, Name: name}
				if reQID.MatchString(key) {
					r.QID = key
					if names[key] != "" {
						r.Name = names[key]
					}
				}
				rows[key] = r
			}
			if isPlan[i] {
				r.Plans++
			} else {
				r.Have++
			}
		}
		people := append([]castMember(nil), inf.Cast...)
		for _, a := range inf.Actors {
			people = append(people, castMember{Name: strings.TrimSpace(a)})
		}
		for _, c := range people {
			if c.Name == "" && c.QID == "" {
				continue
			}
			switch q := byName[strings.ToLower(c.Name)]; {
			case c.QID != "":
				touch(c.QID, c.Name)
			case q != "":
				touch(q, c.Name)
			default:
				// одно и то же имя в разном регистре — один человек
				key := nameKey(c.Name)
				for k := range rows {
					if normPersonKey(k) == normPersonKey(key) {
						key = k
					}
				}
				touch(key, c.Name)
			}
		}
	}
	out := make([]personRow, 0, len(rows))
	for _, r := range rows {
		name := r.Name // имя из описаний: по нему ищутся правки «до Q-id»
		if !reQID.MatchString(r.Key) {
			name = strings.TrimPrefix(r.Key, "name:")
		}
		if e, ok := s.personEdits(r.Key, name); ok {
			if e.Name != "" {
				r.Name = e.Name
			}
			r.About = e.About
		}
		r.Photo = s.personPhotoURL(r.QID, name)
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Have != out[j].Have {
			return out[i].Have > out[j].Have
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// GET /api/admin/people
func (s *server) adminPeople(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"people": s.peopleIndex()})
}

// PUT /api/admin/people?key= {name, about}
func (s *server) adminPersonEdit(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	var req struct {
		Name  *string `json:"name"`
		About *string `json:"about"`
	}
	if !validPersonKey(key) || json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный запрос"})
		return
	}
	s.people.set(key, func(e *personEdit) {
		if req.Name != nil {
			e.Name = strings.TrimSpace(*req.Name)
		}
		if req.About != nil {
			e.About = strings.TrimSpace(*req.About)
		}
	})
	e, _ := s.people.get(key)
	writeJSON(w, http.StatusOK, map[string]any{"person": e})
}

// adminPersonImage — своё фото (target=person в /api/admin/image).
func (s *server) adminPersonImage(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("id")
	if !validPersonKey(key) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный ключ"})
		return
	}
	src, cleanup, err := s.imageSource(r)
	defer cleanup()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	dst := s.personCustomPath(personFileID(key))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := renderJPEG(src, dst, personPhotoW, personPhotoW*4/3, 88); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "картинка не читается (годятся JPG и PNG): " + err.Error()})
		return
	}
	s.people.set(key, func(e *personEdit) { e.Photo = true })
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) adminPersonImageDelete(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("id")
	if !validPersonKey(key) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный ключ"})
		return
	}
	_ = os.Remove(s.personCustomPath(personFileID(key)))
	s.people.set(key, func(e *personEdit) { e.Photo = false })
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// castPhotos — свои фото людей из состава описания (для «Инфо»): у людей без
// Q-id страница не посчитает имя файла, а у своих фото нужна метка в ссылке.
func (s *server) castPhotos(inf *itemInfo) map[string]string {
	out := map[string]string{}
	if inf == nil {
		return out
	}
	for _, n := range inf.Actors {
		if u := s.personPhotoURL("", n); u != "" {
			out[n] = u
		}
	}
	for _, c := range inf.Cast {
		// у человека с Q-id ссылку страница строит сама — нужна только своя
		if u := s.personPhotoURL(c.QID, c.Name); u != "" && (c.QID == "" || strings.Contains(u, "?v=")) {
			out[c.Name] = u
		}
	}
	return out
}
