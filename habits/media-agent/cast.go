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
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	fillCast(inf)
	if err := writeInfo(p, inf); err != nil {
		log.Printf("состав для %s не сохранился: %v", filepath.Base(p), err)
		return false
	}
	return len(inf.Cast) > 0
}

// wikidataEntityFn — запись Wikidata; в тестах подменяется.
var wikidataEntityFn = wikidataEntity

// fillCast дописывает в описание состав с идентификаторами (сеть).
func fillCast(inf *itemInfo) {
	infoSem <- struct{}{}
	qid := qidOf(inf)
	var cast []castMember
	if qid != "" {
		if e, err := wikidataEntityFn(qid); err == nil {
			cast = e.castFrom()
		}
	}
	<-infoSem
	inf.QID, inf.Cast, inf.CastAt = qid, cast, time.Now().Unix()
}

// ensurePlanCast — то же для плана: описание у него не в файле, а в plans.json.
func (s *server) ensurePlanCast(id string) bool {
	pl, ok := s.plans.find(id)
	if !ok || pl.Info == nil || pl.Info.CastAt != 0 {
		return false
	}
	inf := *pl.Info
	fillCast(&inf)
	s.plans.update(id, func(p *plan) {
		// новое описание, а не правка на месте: копии плана из all() и find()
		// держат тот же указатель и читаются без блокировки
		if p.Info != nil && p.Info.CastAt == 0 {
			ni := *p.Info
			ni.QID, ni.Cast, ni.CastAt = inf.QID, inf.Cast, inf.CastAt
			p.Info = &ni
		}
	})
	return len(inf.Cast) > 0
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
	if !rePersonFile.MatchString(qid) {
		http.NotFound(w, r)
		return
	}
	// своё фото из админки главнее скачанного
	if own := s.personCustomPath(qid); fileReady(own) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, own)
		return
	}
	if !reQID.MatchString(qid) {
		http.NotFound(w, r)
		return
	}
	dst := s.personPhotoPath(qid)
	if !s.ensurePersonPhoto(qid) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=2592000")
	http.ServeFile(w, r, dst)
}

// personFetchFn — скачать фото; в тестах подменяется.
var personFetchFn = (*server).fetchPersonPhoto

// ensurePersonPhoto — фото человека в кэше: есть или скачалось. Неудача
// помнится неделю (.miss).
func (s *server) ensurePersonPhoto(qid string) bool {
	dst := s.personPhotoPath(qid)
	if fileReady(dst) {
		return true
	}
	miss := dst + ".miss"
	if st, err := os.Stat(miss); err == nil && time.Since(st.ModTime()) < personMissTTL {
		return false
	}
	personSem <- struct{}{}
	err := personFetchFn(s, qid, dst)
	<-personSem
	if err != nil {
		_ = os.MkdirAll(filepath.Dir(miss), 0o700)
		_ = os.WriteFile(miss, nil, 0o600)
		return false
	}
	return true
}

// photoMissing — есть ли в составе люди, чьё фото ещё не пробовали взять.
func (s *server) photoMissing(cast []castMember) bool {
	for _, c := range cast {
		if c.QID == "" {
			continue
		}
		dst := s.personPhotoPath(c.QID)
		if fileReady(dst) {
			continue
		}
		if st, err := os.Stat(dst + ".miss"); err == nil && time.Since(st.ModTime()) < personMissTTL {
			continue
		}
		return true
	}
	return false
}

// --- задача «Актёры по всей медиатеке» ---

// castTargets — плитки и планы (как «plan:<id>»), где нет состава с
// идентификаторами или не взяты фото.
func (s *server) castTargets() []string {
	var out []string
	films, series, anime := s.rawVideoTiles()
	for _, t := range append(append(films, series...), anime...) {
		p, ok := agentStore.pathOf(t.ID)
		if !ok {
			continue
		}
		if inf, ok := readInfo(p); ok && (inf.CastAt == 0 || s.photoMissing(inf.Cast)) {
			out = append(out, p)
		}
	}
	for _, pl := range s.plans.all() {
		if pl.Info != nil && (pl.Info.CastAt == 0 || s.photoMissing(pl.Info.Cast)) {
			out = append(out, "plan:"+pl.ID)
		}
	}
	return out
}

// quietCast — один элемент работы: состав, затем фото всех из него.
func (s *server) quietCast(t *task, item string) error {
	var cast []castMember
	if id, ok := strings.CutPrefix(item, "plan:"); ok {
		s.ensurePlanCast(id)
		pl, ok := s.plans.find(id)
		if !ok || pl.Info == nil {
			return nil // план удалили
		}
		cast = pl.Info.Cast
	} else {
		s.ensureCast(item)
		inf, ok := readInfo(item)
		if !ok {
			return errors.New("описания нет")
		}
		cast = inf.Cast
	}
	for _, c := range cast {
		if c.QID != "" {
			s.ensurePersonPhoto(c.QID)
		}
	}
	return nil
}

// POST /api/admin/cast/run — собрать актёров сейчас, не дожидаясь ночи.
func (s *server) adminCastRun(w http.ResponseWriter, r *http.Request) {
	items := s.castTargets()
	if len(items) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "делать нечего: у всех описаний состав и фото уже есть"})
		return
	}
	for _, j := range s.quiet.list() {
		if j.Kind == "cast" {
			s.quiet.drop(j.ID) // прежняя работа устарела: список свежее
		}
	}
	job := s.quiet.add(quietJob{Kind: "cast", Now: true, Items: items,
		Title: fmt.Sprintf("Актёры: %d описаний", len(items))})
	s.startTask(w, "quiet-cast", "👥 "+job.Title, func(ctx context.Context, t *task) error {
		return s.runQuietJob(ctx, t, job.ID)
	})
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
	key := qid
	if key == "" {
		key = nameKey(name)
	}
	out := map[string]any{"name": name, "qid": qid, "ids": ids, "key": key}
	if e, ok := s.personEdits(key, name); ok {
		if e.Name != "" {
			out["name"] = e.Name
		}
		if e.About != "" {
			out["about"] = e.About
		}
	}
	if u := s.personPhotoURL(qid, name); u != "" {
		out["photo"] = u
	}
	if qid != "" {
		films := s.filmography(qid)
		s.markFilms(films, r)
		out["films"] = films
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
