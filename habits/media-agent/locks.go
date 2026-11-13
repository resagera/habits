package main

// Папки под пином: закрытая папка не видна в медиатеке, пока не введут её код.
//
// Зачем на агенте, а не на странице. Спрятать плитку в браузере — это замок,
// нарисованный на двери: id объекта предсказуем (хеш пути), и прямая ссылка
// отдала бы и список, и сами файлы. Поэтому закрытое не показывает и не отдаёт
// САМ агент — и в каталоге, и в обходе папок, и при выдаче файла.
//
// Пин введён — значит, папка видна. Отдельного списка «что спрятано» нигде
// нет: ввод кода и есть способ найти папку, иначе прятать было бы незачем.
// Поэтому `match` ищет папку ПО коду, а не наоборот.
//
// Пин хранится хешем с солью. Не потому, что файл кэша кто-то украдёт, а
// потому, что он попадает в еженедельную копию разметки, которая уезжает в
// Telegram: коды оттуда читаться не должны.
//
// Ключ записи — ПУТЬ папки, как в избранном: id это хеш пути, и
// переименование в админке сбросило бы замок (`locks.move` в `moveMemory`).

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// Короткий код набирают пультом, длинный там не набрать.
	lockPinMin = 4
	lockPinMax = 12
	// Сколько держим папку открытой без обращений. «До выхода из раздела» —
	// это явный `POST /api/lock`, а простой на случай, когда от телевизора
	// просто отошли.
	unlockIdle = 30 * time.Minute
	// Перебор четырёхзначного кода — это десять тысяч попыток, то есть
	// минуты. Считаем промахи и запираем ввод, как у админского пина.
	unlockMaxFails = 5
	unlockLockFor  = 5 * time.Minute
)

// --- хранилище замков ---

type folderLock struct {
	Path string `json:"path"`
	Salt string `json:"salt"`
	Hash string `json:"hash"` // sha256(соль + пин)
	Set  int64  `json:"set"`
}

type lockStore struct {
	mu    sync.Mutex
	path  string
	items map[string]folderLock // путь папки → замок
}

func newLockStore(cacheDir string) *lockStore {
	l := &lockStore{path: filepath.Join(cacheDir, "locks.json"), items: map[string]folderLock{}}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, &l.items)
		if l.items == nil {
			l.items = map[string]folderLock{}
		}
	}
	return l
}

func (ls *lockStore) saveLocked() {
	if data, err := json.MarshalIndent(ls.items, "", " "); err == nil {
		_ = writeAtomic(ls.path, data)
	}
}

func pinHash(salt, pin string) string {
	sum := sha256.Sum256([]byte(salt + pin))
	return hex.EncodeToString(sum[:])
}

func validPin(pin string) error {
	pin = strings.TrimSpace(pin)
	if len(pin) < lockPinMin || len(pin) > lockPinMax {
		return fmt.Errorf("код должен быть от %d до %d знаков", lockPinMin, lockPinMax)
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return fmt.Errorf("код набирают пультом — только цифры")
		}
	}
	return nil
}

func (ls *lockStore) set(path, pin string) error {
	if err := validPin(pin); err != nil {
		return err
	}
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	salt := hex.EncodeToString(buf)
	ls.mu.Lock()
	defer ls.mu.Unlock()
	ls.items[path] = folderLock{Path: path, Salt: salt, Hash: pinHash(salt, pin), Set: time.Now().Unix()}
	ls.saveLocked()
	return nil
}

func (ls *lockStore) drop(path string) bool {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if _, ok := ls.items[path]; !ok {
		return false
	}
	delete(ls.items, path)
	ls.saveLocked()
	return true
}

func (ls *lockStore) has(path string) bool {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	_, ok := ls.items[path]
	return ok
}

// at — когда папку закрыли. Отдельным методом, потому что наружу уходит
// только это: соль и хеш кода не покидают хранилища.
func (ls *lockStore) at(path string) int64 {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return ls.items[path].Set
}

// paths — пути всех закрытых папок. Наружу этот список не уходит: он нужен
// админке (там свой пин) и проверке видимости.
func (ls *lockStore) paths() []string {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	out := make([]string, 0, len(ls.items))
	for p := range ls.items {
		out = append(out, p)
	}
	return out
}

// guard — ближайшая закрытая папка НАД путём (или он сам). Пустая строка
// значит «ничем не закрыт». Берём самую глубокую: закрыли и альбом, и папку
// внутри него — спрашивать надо код той, в которую лезут.
func (ls *lockStore) guard(p string) string {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	sep := string(filepath.Separator)
	best := ""
	for path := range ls.items {
		if p == path || strings.HasPrefix(p, path+sep) {
			if len(path) > len(best) {
				best = path
			}
		}
	}
	return best
}

// match — какой папке принадлежит этот код. Сравнение всех записей до конца,
// без выхода на первом совпадении: так время ответа не зависит от того,
// угадали ли первые цифры.
func (ls *lockStore) match(pin string) string {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	found := ""
	for path, l := range ls.items {
		if subtle.ConstantTimeCompare([]byte(pinHash(l.Salt, pin)), []byte(l.Hash)) == 1 {
			found = path
		}
	}
	return found
}

// move — замок едет за папкой при переименовании и переезде на другой диск.
// Ключ здесь путь, и без этого закрытая папка после переименования открылась
// бы сама, молча.
func (ls *lockStore) move(old, neu string, isDir bool) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	sep := string(filepath.Separator)
	changed := false
	for p, l := range ls.items {
		np := ""
		switch {
		case p == old:
			np = neu
		case isDir && strings.HasPrefix(p, old+sep):
			np = neu + p[len(old):]
		default:
			continue
		}
		delete(ls.items, p)
		l.Path = np
		ls.items[np] = l
		changed = true
	}
	if changed {
		ls.saveLocked()
	}
}

// forget убирает замки папок, которых больше нет на диске. Зовётся при
// удалении: иначе запись пережила бы папку и помешала создать новую с тем же
// именем — та открылась бы уже под старым кодом.
func (ls *lockStore) forget(p string, isDir bool) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	sep := string(filepath.Separator)
	changed := false
	for path := range ls.items {
		if path == p || (isDir && strings.HasPrefix(path, p+sep)) {
			delete(ls.items, path)
			changed = true
		}
	}
	if changed {
		ls.saveLocked()
	}
}

// --- кто что открыл ---

// unlockStore помнит, какие папки открыты у каждого зрителя. Ключ — случайный
// билет, который страница держит у себя: иначе пин, введённый на телефоне,
// открыл бы папку и на телевизоре в соседней комнате.
type unlockStore struct {
	mu    sync.Mutex
	items map[string]*unlockSession
}

type unlockSession struct {
	paths map[string]bool
	seen  time.Time
}

func newUnlockStore() *unlockStore {
	u := &unlockStore{items: map[string]*unlockSession{}}
	go u.sweeper()
	return u
}

func (us *unlockStore) sweeper() {
	for range time.Tick(time.Minute) {
		us.mu.Lock()
		for token, sess := range us.items {
			if time.Since(sess.seen) > unlockIdle {
				delete(us.items, token)
			}
		}
		us.mu.Unlock()
	}
}

// open выдаёт билет (или дополняет уже выданный) и открывает на нём папку.
func (us *unlockStore) open(token, path string) string {
	us.mu.Lock()
	defer us.mu.Unlock()
	sess := us.items[token]
	if token == "" || sess == nil {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			return ""
		}
		token = hex.EncodeToString(buf)
		sess = &unlockSession{paths: map[string]bool{}}
		us.items[token] = sess
	}
	sess.paths[path] = true
	sess.seen = time.Now()
	return token
}

// allowed — открыта ли папка на этом билете. Заодно продлевает срок: пока
// смотрят, закрываться незачем.
func (us *unlockStore) allowed(token, path string) bool {
	if token == "" {
		return false
	}
	us.mu.Lock()
	defer us.mu.Unlock()
	sess := us.items[token]
	if sess == nil {
		return false
	}
	if time.Since(sess.seen) > unlockIdle {
		delete(us.items, token)
		return false
	}
	if !sess.paths[path] {
		return false
	}
	sess.seen = time.Now()
	return true
}

// close — «вышли из раздела»: всё, что было открыто, закрывается сразу, не
// дожидаясь получаса простоя.
func (us *unlockStore) close(token string) {
	us.mu.Lock()
	defer us.mu.Unlock()
	delete(us.items, token)
}

func (us *unlockStore) openCount(token string) int {
	us.mu.Lock()
	defer us.mu.Unlock()
	if sess := us.items[token]; sess != nil {
		return len(sess.paths)
	}
	return 0
}

// --- проверка видимости ---

// Имя куки с билетом. Кука, а не только заголовок, — чтобы не переписывать
// каждый `<img src>` и `<video src>` на странице: у них заголовок не
// поставить, а параметр в ссылке пришлось бы дописывать в десятке мест и
// однажды забыть. Браузер отправляет её сам, и ровно тому агенту, который её
// выдал.
const unlockCookie = "htv_unlock"

// ticket — билет зрителя: заголовок (запросы страницы), параметр `u`
// (на случай ссылки, скопированной руками) или кука.
func ticket(r *http.Request) string {
	if t := r.Header.Get("X-Unlock"); t != "" {
		return t
	}
	if t := r.URL.Query().Get("u"); t != "" {
		return t
	}
	if c, err := r.Cookie(unlockCookie); err == nil {
		return c.Value
	}
	return ""
}

// setTicket кладёт билет в куку сеанса. Срока жизни у неё нет: когда
// закрывать, решает агент (получас простоя или явный «выход из раздела»), а
// браузер просто носит её, пока открыт.
func (s *server) setTicket(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: unlockCookie, Value: token,
		Path: s.cfg.base + "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

// visible — можно ли показывать этот путь зрителю с таким билетом.
func (s *server) visible(p string, r *http.Request) bool {
	guard := s.locks.guard(p)
	return guard == "" || s.unlocks.allowed(ticket(r), guard)
}

// --- API просмотра ---

var unlockGuard struct {
	mu    sync.Mutex
	fails int
	until time.Time
}

// POST /api/unlock {pin, ticket} — открыть папку по её коду. Какую именно,
// решает сам код: списка закрытых папок страница не получает никогда.
func (s *server) unlock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Pin    string `json:"pin"`
		Ticket string `json:"ticket"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	unlockGuard.mu.Lock()
	if wait := time.Until(unlockGuard.until); wait > 0 {
		unlockGuard.mu.Unlock()
		writeJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": fmt.Sprintf("Слишком много неверных кодов — подождите %d мин", int(wait.Minutes())+1)})
		return
	}
	path := s.locks.match(strings.TrimSpace(req.Pin))
	if path == "" {
		unlockGuard.fails++
		if unlockGuard.fails >= unlockMaxFails {
			unlockGuard.fails = 0
			unlockGuard.until = time.Now().Add(unlockLockFor)
			log.Printf("папки под пином: %d неверных кодов подряд — пауза %s", unlockMaxFails, unlockLockFor)
		}
		unlockGuard.mu.Unlock()
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Нет папки с таким кодом"})
		return
	}
	unlockGuard.fails = 0
	unlockGuard.mu.Unlock()

	if _, err := os.Stat(path); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Папки больше нет на диске"})
		return
	}
	token := s.unlocks.open(firstNonEmpty(req.Ticket, ticket(r)), path)
	if token == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не вышло открыть"})
		return
	}
	s.setTicket(w, token)
	log.Printf("папки под пином: открыта «%s»", filepath.Base(path))
	writeJSON(w, http.StatusOK, map[string]any{"ticket": token, "id": s.remember(path),
		"name": filepath.Base(path), "open": s.unlocks.openCount(token)})
}

// POST /api/lock {ticket} — закрыть всё открытое, не дожидаясь простоя.
func (s *server) lock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ticket string `json:"ticket"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	s.unlocks.close(firstNonEmpty(req.Ticket, ticket(r)))
	s.setTicket(w, "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- API админки ---

type lockView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Library string `json:"library"`
	Set     int64  `json:"set"`
	Gone    bool   `json:"gone,omitempty"` // папки уже нет на диске
}

// GET /api/admin/locks — какие папки закрыты. Сами коды не отдаются никогда:
// их нельзя подсмотреть, только задать заново.
func (s *server) adminLocks(w http.ResponseWriter, r *http.Request) {
	out := []lockView{}
	for _, p := range s.locks.paths() {
		v := lockView{ID: s.remember(p), Name: filepath.Base(p), Path: p,
			Library: s.libraryOf(p).Title, Set: s.locks.at(p)}
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			v.Gone = true
		}
		out = append(out, v)
	}
	sortByName(out)
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "min": lockPinMin, "max": lockPinMax})
}

func sortByName(list []lockView) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && strings.ToLower(list[j].Name) < strings.ToLower(list[j-1].Name); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// POST /api/admin/locks {id, pin} — закрыть папку кодом или сменить код.
func (s *server) adminLockSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID  string `json:"id"`
		Pin string `json:"pin"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	p, ok := s.adminPath(w, req.ID)
	if !ok {
		return
	}
	st, err := os.Stat(p)
	if err != nil || !st.IsDir() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "код ставится на папку, а не на файл"})
		return
	}
	if s.isRoot(p) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "целый раздел под код не закрывается — только папка внутри него"})
		return
	}
	if kind := s.libraryOf(p).Kind; kind != "music" && kind != "photo" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "под код прячутся папки музыки, фото и видео"})
		return
	}
	// Код чужой папки на этой же не даст её открыть: `match` вернул бы одну
	// папку из двух, и какую — зависело бы от порядка в карте.
	if other := s.locks.match(strings.TrimSpace(req.Pin)); other != "" && other != p {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "такой код уже стоит на другой папке — придумайте другой"})
		return
	}
	if err := s.locks.set(p, strings.TrimSpace(req.Pin)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	log.Printf("админка: папка «%s» закрыта кодом", filepath.Base(p))
	writeJSON(w, http.StatusOK, map[string]any{"id": req.ID, "name": filepath.Base(p)})
}

// POST /api/admin/locks/drop {id} — снять код. Берём путь из самого замка, а
// не с диска: снять код с папки, которой уже нет, тоже надо уметь.
func (s *server) adminLockDrop(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	for _, p := range s.locks.paths() {
		if shortID(p) == req.ID {
			s.locks.drop(p)
			log.Printf("админка: с папки «%s» снят код", filepath.Base(p))
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "такая папка не закрыта"})
}

func firstNonEmpty(list ...string) string {
	for _, v := range list {
		if v != "" {
			return v
		}
	}
	return ""
}
