package main

// Переезд папки медиатеки на другой диск — задачей, с процентами.
//
// Зачем отдельный механизм, когда в админке есть «перенести». Тот работает с
// ОДНИМ файлом и прямо на запросе: для трека в альбоме этого хватает. Сериал —
// это десятки гигабайт, по USB 2.0 они едут часами, и на HTTP-запросе такое
// держать нельзя. Поэтому переезд устроен как закачка по ссылке: очередь на
// диске, по одной папке за раз, каждая — отдельной задачей с полосой
// выполнения.
//
// Порядок шагов такой, чтобы ни на одном нельзя было остаться без файлов:
//
//  1. копия в служебную папку на диске-ПРИЁМНИКЕ (вне корней библиотек, иначе
//     в каталоге мелькали бы полусобранные сериалы);
//  2. сверка: каждый файл источника есть в копии и ровно того же размера;
//  3. rename копии в библиотеку — тот же диск, поэтому мгновенно и неделимо:
//     сериал появляется целиком, а не по серии;
//  4. память агента переезжает на новый путь — id у нас хеш пути, и без этого
//     шага сериал встанет с начала, без постера и заставок;
//  5. и только теперь удаляется исходник.
//
// Сорвалось до шага 3 — исходник на месте, а копия остаётся лежать: следующий
// прогон её догонит, а не начнёт заново. Сорвалось на 4 — файлы уже на новом
// месте, и повторять нечего: память допишется при следующем открытии папки,
// пусть и забыв позицию просмотра.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// migrateReserve — запас на приёмнике сверх размера папки: забивать диск под
// ноль нельзя, туда же будут приезжать новые сериалы. Переменная, а не
// константа, только ради тестов: в /tmp столько свободного места не бывает.
var migrateReserve int64 = 5 << 30

const (
	// предел на одну постановку: 200 папок — это уже вся медиатека
	migrateQueueMax = 200
	// как часто смотрим, не пора ли взяться за очередь
	migrateKickEvery = 15 * time.Second
	// служебная папка для копий. С точки — чтобы не попадаться на глаза в
	// «Файлах», и рядом с корнями, а не внутри: внутри её увидел бы каталог.
	migrateTempName = ".habits-migrate"
	// как часто обновляем проценты: чаще — лишняя работа под замком
	migrateTick = 500 * time.Millisecond
	// буфер копирования. Больше обычного: по USB 2.0 крупные куски заметно
	// быстрее мелких, а памяти на мини-сервере не жалко.
	migrateBuf = 4 << 20
)

// migrateItem — одна папка в очереди. Пути храним целиком, а не id: id — хеш
// пути, а путь видно глазами в самом файле очереди, когда разбираешься, что
// не уехало.
type migrateItem struct {
	ID    string `json:"id"`
	Src   string `json:"src"`
	Dst   string `json:"dst"`
	Name  string `json:"name"`
	Lib   string `json:"lib"` // название библиотеки-приёмника, для подписи
	Size  int64  `json:"size"`
	Files int    `json:"files"`
	Added int64  `json:"added"`
}

// --- очередь ---

type migrateStore struct {
	mu    sync.Mutex
	path  string
	items []migrateItem
	next  int
}

func newMigrateStore(cacheDir string) *migrateStore {
	m := &migrateStore{path: filepath.Join(cacheDir, "migrate-queue.json")}
	if data, err := os.ReadFile(m.path); err == nil {
		_ = json.Unmarshal(data, &m.items)
	}
	// номера продолжаем с хвоста, чтобы после перезапуска id не повторялись
	for _, it := range m.items {
		if n, err := strconv.Atoi(strings.TrimPrefix(it.ID, "m")); err == nil && n > m.next {
			m.next = n
		}
	}
	return m
}

func (ms *migrateStore) saveLocked() {
	if data, err := json.MarshalIndent(ms.items, "", " "); err == nil {
		_ = writeAtomic(ms.path, data)
	}
}

// add ставит папки в хвост. Ту же папку второй раз не берём: нажать
// «Перенести всё» дважды легко, а второй проход нашёл бы исходник уже
// удалённым и записал бы в отчёт ошибку.
func (ms *migrateStore) add(items []migrateItem) (added int, queue []migrateItem) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	for _, it := range items {
		if len(ms.items) >= migrateQueueMax {
			break
		}
		if ms.hasLocked(it.Src) {
			continue
		}
		ms.next++
		it.ID = "m" + strconv.Itoa(ms.next)
		it.Added = time.Now().Unix()
		ms.items = append(ms.items, it)
		added++
	}
	if added > 0 {
		ms.saveLocked()
	}
	return added, append([]migrateItem{}, ms.items...)
}

func (ms *migrateStore) hasLocked(src string) bool {
	for _, have := range ms.items {
		if have.Src == src {
			return true
		}
	}
	return false
}

func (ms *migrateStore) list() []migrateItem {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return append([]migrateItem{}, ms.items...)
}

// take снимает голову очереди и говорит, сколько осталось после неё.
func (ms *migrateStore) take() (migrateItem, int, bool) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if len(ms.items) == 0 {
		return migrateItem{}, 0, false
	}
	it := ms.items[0]
	ms.items = ms.items[1:]
	ms.saveLocked()
	return it, len(ms.items), true
}

// drop снимает из очереди одну папку или все.
func (ms *migrateStore) drop(id string) int {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	before := len(ms.items)
	if id == "all" {
		ms.items = nil
	} else {
		kept := ms.items[:0]
		for _, it := range ms.items {
			if it.ID != id {
				kept = append(kept, it)
			}
		}
		ms.items = kept
	}
	if gone := before - len(ms.items); gone > 0 {
		ms.saveLocked()
		return gone
	}
	return 0
}

// --- обход и копирование ---

// fileEntry — файл источника так, как его надо воспроизвести на приёмнике.
// Время правки переносим: по нему агент решает, не перечитать ли разбор, и
// без него вся папка пошла бы через ffprobe заново.
type fileEntry struct {
	rel  string
	size int64
	mod  time.Time
}

// walkFiles — файлы папки с размерами. Ссылки пропускаем: разворачивать их
// при переезде значит затащить на диск то, что лежало совсем в другом месте.
func walkFiles(root string) ([]fileEntry, int64, error) {
	var out []fileEntry
	var total int64
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == trashDirName || info.Name() == migrateTempName {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, fileEntry{rel: rel, size: info.Size(), mod: info.ModTime()})
		total += info.Size()
		return nil
	})
	sort.Slice(out, func(a, b int) bool { return out[a].rel < out[b].rel })
	return out, total, err
}

// copyOne копирует файл, отмечая в задаче пройденные байты. Останов проверяем
// на каждом куске: ждать конца сериала после нажатия «Остановить» никто не
// станет.
func copyOne(ctx context.Context, src, dst string, f fileEntry, mark func(int64)) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	buf := make([]byte, migrateBuf)
	for {
		if err := ctx.Err(); err != nil {
			out.Close()
			os.Remove(tmp)
			return err
		}
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				os.Remove(tmp)
				return werr
			}
			mark(int64(n))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			os.Remove(tmp)
			return rerr
		}
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	// время правки — после переименования: rename его не меняет, а вот запись
	// в файл только что обновила
	_ = os.Chtimes(dst, f.mod, f.mod)
	return nil
}

// copyTree копирует папку файл за файлом. Уже лежащее на приёмнике того же
// размера пропускается — так прерванный переезд продолжается с места, а не
// с начала.
func copyTree(ctx context.Context, src, dst string, files []fileEntry, total int64,
	report func(done int64, rel string)) error {
	var done int64
	last := time.Time{}
	for _, f := range files {
		from, to := filepath.Join(src, f.rel), filepath.Join(dst, f.rel)
		if st, err := os.Stat(to); err == nil && st.Size() == f.size {
			done += f.size
			report(done, f.rel)
			continue
		}
		mark := func(n int64) {
			done += n
			if time.Since(last) >= migrateTick {
				last = time.Now()
				report(done, f.rel)
			}
		}
		if err := copyOne(ctx, from, to, f, mark); err != nil {
			return fmt.Errorf("%s: %w", f.rel, err)
		}
		report(done, f.rel)
	}
	return nil
}

// verifyTree — сверка: каждый файл источника есть на той стороне и ровно того
// же размера. Лишние файлы на приёмнике не беда (там мог остаться хвост
// прошлого прогона), а вот недостающий или короткий — повод всё отменить.
func verifyTree(src, dst string, files []fileEntry) []string {
	var bad []string
	for _, f := range files {
		st, err := os.Stat(filepath.Join(dst, f.rel))
		switch {
		case err != nil:
			bad = append(bad, f.rel+": не доехал")
		case st.Size() != f.size:
			bad = append(bad, fmt.Sprintf("%s: размер %d вместо %d", f.rel, st.Size(), f.size))
		}
		if len(bad) >= 10 {
			bad = append(bad, "…")
			break
		}
	}
	return bad
}

// --- сам переезд ---

// migrateTempDir — служебная папка для копий на диске-приёмнике. Рядом с
// корнем библиотеки, а не внутри: внутри её увидел бы каталог. Та же файловая
// система, поэтому rename готовой копии в библиотеку мгновенный.
func (s *server) migrateTempDir(destRoot string) string {
	dir := filepath.Join(filepath.Dir(strings.TrimRight(destRoot, string(filepath.Separator))), migrateTempName)
	// корень библиотеки может лежать и прямо в точке монтирования — тогда
	// «рядом» окажется вне диска, и rename перестанет быть мгновенным
	if s.inRoots(dir) || !sameDisk(dir, destRoot) {
		return filepath.Join(destRoot, migrateTempName)
	}
	return dir
}

// sameDisk — лежат ли оба пути на одной файловой системе. От этого зависит,
// будет ли rename готовой копии в библиотеку мгновенным или вернёт EXDEV.
func sameDisk(a, b string) bool {
	dev := nearestDevice(a)
	return dev != 0 && dev == nearestDevice(b)
}

// nearestDevice — раздел ближайшего существующего родителя. Служебной папки
// под копию ещё нет, а знать её диск надо заранее.
func nearestDevice(p string) uint64 {
	for {
		if dev := deviceOf(p); dev != 0 {
			return dev
		}
		parent := filepath.Dir(p)
		if parent == p {
			return 0
		}
		p = parent
	}
}

// moveFolder — переезд одной папки. Шаги и их порядок описаны в начале файла.
func (s *server) moveFolder(ctx context.Context, t *task, it migrateItem) error {
	st, err := os.Stat(it.Src)
	if err != nil || !st.IsDir() {
		return errors.New("источника больше нет: " + it.Src)
	}
	if exists(it.Dst) {
		return errors.New("на приёмнике уже есть «" + it.Name + "» — разберитесь, какая копия нужна")
	}
	destRoot := filepath.Dir(it.Dst)
	if dst, err := os.Stat(destRoot); err != nil || !dst.IsDir() {
		return errors.New("библиотека-приёмник недоступна: " + destRoot)
	}

	t.set("смотрю, что переносить", 0, 0)
	files, total, err := walkFiles(it.Src)
	if err != nil {
		return fmt.Errorf("обход %s: %w", it.Src, err)
	}
	if len(files) == 0 {
		return errors.New("в папке нет файлов")
	}
	if free, _ := diskSpace(destRoot); free > 0 && free < total+migrateReserve {
		return fmt.Errorf("на приёмнике %s, а нужно %s с запасом", human(free), human(total+migrateReserve))
	}

	tmpRoot := s.migrateTempDir(destRoot)
	tmp := filepath.Join(tmpRoot, it.Name)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return fmt.Errorf("служебная папка %s: %w", tmp, err)
	}
	t.say("файлов %d, %s → %s", len(files), human(total), it.Lib)

	// 1. копия. Проценты считаем по байтам: файлы разного размера, и по их
	// числу полоса дёргалась бы рывками.
	started := time.Now()
	// проценты считаем по БАЙТАМ, а не по числу файлов: серии бывают и по
	// 300 МБ, и по 3 ГБ, и полоса дёргалась бы рывками. В задаче лежат как
	// есть, а в человеческий вид их приводит страница.
	err = copyTree(ctx, it.Src, tmp, files, total, func(done int64, rel string) {
		t.set("копирую "+filepath.Base(rel), int(done), int(total))
	})
	if err != nil {
		if ctx.Err() != nil {
			t.say("остановлено — копия осталась в %s, исходник не тронут", tmpRoot)
			return ctx.Err()
		}
		return err
	}
	t.say("скопировано за %s (%s/с)", shortDur(time.Since(started)), human(speed(total, time.Since(started))))

	// 2. сверка
	t.set("сверяю размеры", int(total), int(total))
	if bad := verifyTree(it.Src, tmp, files); len(bad) > 0 {
		for _, b := range bad {
			t.say("не сошлось: %s", b)
		}
		return errors.New("сверка не прошла — исходник оставлен на месте")
	}
	t.say("сверка: все %d того же размера", len(files))

	// 3. копия становится библиотечной папкой. Тот же диск — значит, это один
	// неделимый шаг: не бывает состояния «половина сериала видна».
	if err := os.Rename(tmp, it.Dst); err != nil {
		return fmt.Errorf("перенос в библиотеку: %w", err)
	}
	// постер и описание рядом с папкой едут следом
	moved := s.moveSidecars(it.Src, it.Dst)

	// 4. память агента. Делаем ДО удаления исходника: если здесь что-то
	// пойдёт не так, файлы уже на месте и терять нечего.
	s.moveMemory(it.Src, it.Dst, true)
	t.say("память перенесена на новый путь")

	// 5. исходник
	if err := os.RemoveAll(it.Src); err != nil {
		t.say("файлы переехали, но исходник не стёрся: %v", err)
	} else {
		t.say("исходник удалён: %s", it.Src)
	}
	for _, f := range moved {
		_ = os.Remove(f)
	}
	s.pruneTemp(tmpRoot)
	log.Printf("переезд: %s → %s (%d файлов, %s)", it.Src, it.Dst, len(files), human(total))
	t.set(fmt.Sprintf("переехало %s в %d файлах", human(total), len(files)), int(total), int(total))
	return nil
}

// moveSidecars копирует постер и описание, лежащие РЯДОМ с папкой (а не
// внутри), и возвращает исходники — их удалит вызывающий, когда убедится, что
// переезд удался.
func (s *server) moveSidecars(src, dst string) []string {
	srcBase, dstBase := posterBase(src, true), posterBase(dst, true)
	var done []string
	for _, f := range sidecars(src, true) {
		to := dstBase + strings.TrimPrefix(f, srcBase)
		if exists(to) {
			continue
		}
		if copyFile(f, to) == nil {
			// путь постера кэш помнит отдельно: он лежит рядом с папкой, а не
			// внутри, и переносом поддерева не задевается
			agentStore.move(f, to, false)
			done = append(done, f)
		}
	}
	return done
}

// pruneTemp убирает служебную папку, когда в ней ничего не осталось: пустая
// скрытая папка на диске никому не мешает, но и держать её незачем.
func (s *server) pruneTemp(dir string) {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
}

func speed(n int64, d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64(float64(n) / d.Seconds())
}

func shortDur(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Hour {
		return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
}

// --- очередь в работе ---

// migrateWorker поднимает очередь, когда агент освободился: папки могли лечь в
// неё, пока шла другая задача, или остаться с прошлого запуска.
func (s *server) migrateWorker() {
	for {
		time.Sleep(migrateKickEvery)
		if len(s.migrateq.list()) == 0 || s.tasks.busy() {
			continue
		}
		// Ошибка здесь значит только одно: пока спали, задачу занял кто-то
		// другой. Поговорим об этом на следующем круге.
		_, _ = s.migrateKick()
	}
}

// migrateKick берёт из очереди головную папку и делает её задачей. Каждая
// папка — своя задача: так в админке видно и что едет сейчас, и чем кончились
// предыдущие.
func (s *server) migrateKick() (*task, error) {
	it, left, ok := s.migrateq.take()
	if !ok {
		return nil, errors.New("очередь пуста")
	}
	title := "📦 " + it.Name + " → " + it.Lib
	if left > 0 {
		title += fmt.Sprintf(" (в очереди ещё %d)", left)
	}
	t, err := s.tasks.start("migrate", title, func(ctx context.Context, t *task) error {
		return s.moveFolder(ctx, t, it)
	})
	if err != nil {
		// не взялись — папка возвращается в голову очереди
		s.migrateq.mu.Lock()
		s.migrateq.items = append([]migrateItem{it}, s.migrateq.items...)
		s.migrateq.saveLocked()
		s.migrateq.mu.Unlock()
		return nil, err
	}
	return t, nil
}

// --- API ---

func (s *server) libraryByID(id string) library {
	for _, lib := range s.cfg.roots {
		if lib.ID == id {
			return lib
		}
	}
	return library{}
}

// migrateLib — библиотека так, как её видит страница переезда: с диском и
// свободным местом, чтобы было видно, откуда и куда есть смысл ехать.
type migrateLib struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  string `json:"kind"`
	Disk  string `json:"disk"`
	Free  int64  `json:"free"`
	Avail bool   `json:"available"`
}

// migrateCandidate — папка, которую можно перенести.
type migrateCandidate struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Files int    `json:"files"`
	Clash bool   `json:"clash"` // на приёмнике уже есть папка с этим именем
	Queue bool   `json:"queued"`
}

// GET /api/admin/migrate?src=<id>&dst=<id> — что куда можно перевезти.
// Без src отдаём только список библиотек: обход папок источника стоит
// времени, и делать его на каждое открытие админки незачем.
func (s *server) adminMigrateState(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"queue": s.migrateq.list(), "busy": s.tasks.busy()}
	libs := []migrateLib{}
	for _, lib := range s.cfg.roots {
		if !isVideoLib(lib.Kind) {
			continue
		}
		free, _ := diskSpace(lib.Path)
		libs = append(libs, migrateLib{ID: lib.ID, Title: lib.Title, Kind: lib.Kind,
			Disk: diskLabel(lib.Path), Free: free, Avail: lib.available()})
	}
	out["libs"] = libs

	src := s.libraryByID(r.URL.Query().Get("src"))
	if src.Path == "" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	dst := s.libraryByID(r.URL.Query().Get("dst"))
	queued := map[string]bool{}
	for _, it := range s.migrateq.list() {
		queued[it.Src] = true
	}
	items := []migrateCandidate{}
	entries, _ := os.ReadDir(src.Path)
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(src.Path, e.Name())
		files, size, _ := walkFiles(p)
		c := migrateCandidate{Name: e.Name(), Size: size, Files: len(files), Queue: queued[p]}
		if dst.Path != "" {
			c.Clash = exists(filepath.Join(dst.Path, e.Name()))
		}
		items = append(items, c)
	}
	sort.Slice(items, func(a, b int) bool { return items[a].Size > items[b].Size })
	out["items"] = items
	writeJSON(w, http.StatusOK, out)
}

// diskLabel — как назвать диск в админке: точка монтирования без пути внутри
// неё. «/mnt/mybook/rudb/media/serials» → «/mnt/mybook».
func diskLabel(p string) string {
	dev := nearestDevice(p)
	best := p
	for {
		parent := filepath.Dir(best)
		if parent == best || nearestDevice(parent) != dev {
			return best
		}
		best = parent
	}
}

// POST /api/admin/migrate {src, dst, names} — поставить папки в очередь.
// Пустой names значит «все, что лежат в источнике».
func (s *server) adminMigrateStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Src   string   `json:"src"`
		Dst   string   `json:"dst"`
		Names []string `json:"names"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	src, dst := s.libraryByID(req.Src), s.libraryByID(req.Dst)
	switch {
	case src.Path == "" || dst.Path == "":
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нет такой библиотеки"})
		return
	case src.ID == dst.ID:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "откуда и куда — одна и та же библиотека"})
		return
	case !src.available() || !dst.available():
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "одна из библиотек сейчас недоступна"})
		return
	case src.Kind != dst.Kind:
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "библиотеки разного вида: «" + src.Kind + "» и «" + dst.Kind + "»"})
		return
	}

	want := map[string]bool{}
	for _, n := range req.Names {
		want[n] = true
	}
	var items []migrateItem
	var skipped []string
	entries, err := os.ReadDir(src.Path)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if len(want) > 0 && !want[e.Name()] {
			continue
		}
		from := filepath.Join(src.Path, e.Name())
		to := filepath.Join(dst.Path, e.Name())
		if exists(to) {
			skipped = append(skipped, e.Name()+": на приёмнике уже есть")
			continue
		}
		files, size, _ := walkFiles(from)
		if len(files) == 0 {
			skipped = append(skipped, e.Name()+": нет файлов")
			continue
		}
		items = append(items, migrateItem{Src: from, Dst: to, Name: e.Name(),
			Lib: dst.Title, Size: size, Files: len(files)})
	}
	added, _ := s.migrateq.add(items)
	out := map[string]any{"added": added, "skipped": skipped}
	// Занят — папки просто ждут: их поднимет migrateWorker. Отказом это не
	// считаем, иначе пришлось бы объяснять, почему «ошибка», а всё в порядке.
	if added > 0 && !s.tasks.busy() {
		if _, err := s.migrateKick(); err != nil {
			out["busy"] = err.Error()
		}
	}
	// очередь читаем ПОСЛЕ запуска: голову мы только что могли взять в работу,
	// и показывать её ещё и ждущей — врать
	out["queue"] = s.migrateq.list()
	log.Printf("админка: в очередь переезда %d папок из %s в %s", added, src.Path, dst.Path)
	writeJSON(w, http.StatusOK, out)
}

// POST /api/admin/migrate/drop {id} — снять из очереди одну папку или все
// («all»). Идущую задачу это не останавливает: для неё есть своя кнопка.
func (s *server) adminMigrateDrop(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"dropped": s.migrateq.drop(req.ID), "queue": s.migrateq.list()})
}
