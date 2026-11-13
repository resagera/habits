package main

// Актёры: состав с фотографиями в карточке «Инфо» и фильмография по своей
// медиатеке — в каких ещё наших фильмах и сериалах этот человек играет.
//
// Состав берётся из Wikidata (P161 «в ролях») вместе с идентификаторами
// людей: по ним и находится фильмография — имя «Том Харди» могло бы совпасть
// у двух разных людей, а Q208026 — нет. Фото — P18 человека, картинка с
// Викисклада; агент берёт её один раз и хранит в кэше (people/<Q>.jpg).
//
// У описаний, собранных до этого выпуска, состава с идентификаторами нет —
// он дописывается в фоне при открытии «Инфо» и ночью в тихие часы
// (ensureCast). Правленное руками описание не трогается, кроме самого
// состава.

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	castMax       = 20  // сколько актёров берём
	personPhotoW  = 300 // ширина фото
	personMissTTL = 7 * 24 * time.Hour
)

type castMember struct {
	Name string `json:"name"`
	QID  string `json:"qid,omitempty"`
}

var reQID = regexp.MustCompile(`^Q[0-9]{1,12}$`)

// castFrom — состав из записи Wikidata (подписи уже собраны в e.labels).
func (e *wdEntity) castFrom() []castMember {
	var out []castMember
	for _, c := range e.Claims["P161"] {
		if len(out) >= castMax {
			break
		}
		id := snakID(c.MainSnak)
		if l := e.labels[id]; l != "" && reQID.MatchString(id) {
			out = append(out, castMember{Name: l, QID: id})
		}
	}
	return out
}

// qidOf — запись Wikidata описания: сохранённая, по IMDb или по странице
// Википедии.
func qidOf(inf *itemInfo) string {
	if inf.QID != "" {
		return inf.QID
	}
	if inf.IMDb != "" {
		if q, err := wikidataByIMDb(inf.IMDb); err == nil {
			return q
		}
	}
	if u, err := url.Parse(inf.Wiki); err == nil && strings.HasSuffix(u.Host, ".wikipedia.org") {
		lang := strings.Split(u.Host, ".")[0]
		title, _ := url.PathUnescape(strings.TrimPrefix(u.Path, "/wiki/"))
		if q, err := wikidataByWiki(lang, strings.ReplaceAll(title, "_", " ")); err == nil {
			return q
		}
	}
	return ""
}

var castLocks sync.Map

// ensureCast дописывает в описание состав с идентификаторами, если его ещё не
// искали. Возвращает, изменилось ли что-то.
func (s *server) ensureCast(p string) bool {
	lock, _ := castLocks.LoadOrStore(p, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	inf, ok := readInfo(p)
	if !ok || inf.CastAt != 0 {
		return false
	}
	infoSem <- struct{}{}
	qid := qidOf(inf)
	var cast []castMember
	if qid != "" {
		if e, err := wikidataEntity(qid); err == nil {
			cast = e.castFrom()
		}
	}
	<-infoSem
	inf.QID, inf.Cast, inf.CastAt = qid, cast, time.Now().Unix()
	if err := writeInfo(p, inf); err != nil {
		log.Printf("состав для %s не сохранился: %v", filepath.Base(p), err)
		return false
	}
	return len(cast) > 0
}

// castSoon — найти состав в фоне: карточка открывается сразу, а актёры
// появятся при следующем открытии.
func (s *server) castSoon(p string) {
	if _, busy := castLocks.Load(p); busy {
		return
	}
	go s.ensureCast(p)
}

// --- фото ---

var personSem = make(chan struct{}, 3)

func (s *server) personPhotoPath(qid string) string {
	return filepath.Join(s.cfg.cache, "people", qid+".jpg")
}

// personImage — имя файла картинки человека на Викискладе (P18).
func personImage(qid string) (string, error) {
	var res struct {
		Entities map[string]*wdEntity `json:"entities"`
	}
	if err := getJSON(wikidataAPI+"action=wbgetentities&props=claims&ids="+url.QueryEscape(qid), &res); err != nil {
		return "", err
	}
	e := res.Entities[qid]
	if e == nil || len(e.Claims["P18"]) == 0 {
		return "", errors.New("фото нет")
	}
	var file string
	if json.Unmarshal(e.Claims["P18"][0].MainSnak.DataValue.Value, &file) != nil || file == "" {
		return "", errors.New("фото нет")
	}
	return file, nil
}

// GET /person/{qid} — фото актёра. Нет — 404, и страница рисует инициалы.
func (s *server) personPhoto(w http.ResponseWriter, r *http.Request) {
	qid := r.PathValue("qid")
	if !reQID.MatchString(qid) {
		http.NotFound(w, r)
		return
	}
	dst := s.personPhotoPath(qid)
	miss := dst + ".miss"
	if !fileReady(dst) {
		if st, err := os.Stat(miss); err == nil && time.Since(st.ModTime()) < personMissTTL {
			http.NotFound(w, r)
			return
		}
		personSem <- struct{}{}
		err := s.fetchPersonPhoto(qid, dst)
		<-personSem
		if err != nil {
			_ = os.MkdirAll(filepath.Dir(miss), 0o700)
			_ = os.WriteFile(miss, nil, 0o600)
			http.NotFound(w, r)
			return
		}
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=2592000")
	http.ServeFile(w, r, dst)
}

func (s *server) fetchPersonPhoto(qid, dst string) error {
	if fileReady(dst) {
		return nil // успел другой запрос
	}
	file, err := personImage(qid)
	if err != nil {
		return err
	}
	data, ext, err := download("https://commons.wikimedia.org/wiki/Special:FilePath/" +
		url.PathEscape(file) + "?width=" + itoa(personPhotoW))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".src" + ext
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	defer os.Remove(tmp)
	return renderJPEG(tmp, dst, personPhotoW, personPhotoW*4/3, 85)
}

// --- фильмография по своей медиатеке ---

// GET /api/person?qid=&name= — в каких наших фильмах и сериалах человек
// играет. Ищем по идентификатору, а у описаний без него — по имени.
func (s *server) person(w http.ResponseWriter, r *http.Request) {
	qid, name := r.URL.Query().Get("qid"), strings.TrimSpace(r.URL.Query().Get("name"))
	if qid != "" && !reQID.MatchString(qid) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный qid"})
		return
	}
	if qid == "" && name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужен qid или name"})
		return
	}
	films, series, anime := s.rawVideoTiles()
	ids := []string{}
	for _, t := range append(append(films, series...), anime...) {
		p, ok := agentStore.pathOf(t.ID)
		if !ok || !s.visible(p, r) {
			continue
		}
		inf, ok := readInfo(p)
		if !ok {
			continue
		}
		if hit, who := castHit(inf, qid, name); hit {
			ids = append(ids, t.ID)
			if name == "" {
				name = who
			}
		}
	}
	out := map[string]any{"name": name, "qid": qid, "ids": ids}
	if qid != "" {
		out["photo"] = s.cfg.base + "/person/" + qid
	}
	writeJSON(w, http.StatusOK, out)
}

// castHit — играет ли человек в этом описании (и как его там зовут).
func castHit(inf *itemInfo, qid, name string) (bool, string) {
	for _, c := range inf.Cast {
		if (qid != "" && c.QID == qid) || (qid == "" && strings.EqualFold(c.Name, name)) {
			return true, c.Name
		}
	}
	// описание без состава с идентификаторами — по имени из списка актёров
	if name != "" && (len(inf.Cast) == 0 || qid == "") {
		for _, a := range inf.Actors {
			if strings.EqualFold(strings.TrimSpace(a), name) {
				return true, a
			}
		}
	}
	return false, ""
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
