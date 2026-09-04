package main

import (
	"bufio"
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Очередь перекодирования.
//
// Живого перекодирования «на лету» здесь сознательно нет. Приставка играет как
// есть почти всё (по её же отчёту — 94% файлов), а редкое неиграбельное дешевле
// один раз перекодировать в фоне, чем городить поток с перезапуском при каждой
// перемотке. Поставили задачу с пульта — дальше пользуйтесь пультом как обычно,
// прогресс виден отдельно.
//
// Задачи идут ПО ОДНОЙ: перекодирование забирает все ядра, и две параллельные
// задачи не ускорят ничего, зато отберут процессор у того, что сейчас играет.

type jobStatus string

const (
	jobQueued  jobStatus = "queued"
	jobRunning jobStatus = "running"
	jobDone    jobStatus = "done"
	jobFailed  jobStatus = "failed"
	jobStopped jobStatus = "stopped"
)

type job struct {
	ID       string    `json:"id"`
	MediaID  string    `json:"media_id"`
	Name     string    `json:"name"`
	Profile  string    `json:"profile"` // remux | audio | video
	Status   jobStatus `json:"status"`
	Percent  int       `json:"percent"`
	Speed    string    `json:"speed"`
	Error    string    `json:"error"`
	Duration float64   `json:"duration"`
	Started  time.Time `json:"started"`
	Ended    time.Time `json:"ended"`

	cancel context.CancelFunc
}

type queue struct {
	mu     sync.Mutex
	dir    string // куда складывать результат
	jobs   map[string]*job
	order  []string
	wake   chan struct{}
	nextID int
}

func newQueue(cacheDir string) *queue {
	dir := filepath.Join(cacheDir, "transcoded")
	_ = os.MkdirAll(dir, 0o700)
	q := &queue{dir: dir, jobs: map[string]*job{}, wake: make(chan struct{}, 1)}
	go q.run()
	return q
}

// output — куда ляжет результат. Имя от идентификатора файла, а не от
// названия: названия повторяются в разных сериалах, а id уникален.
func (q *queue) output(mediaID string) string {
	return filepath.Join(q.dir, mediaID+".mp4")
}

// ready — перекодированная копия уже лежит на диске.
func (q *queue) ready(mediaID string) bool {
	st, err := os.Stat(q.output(mediaID))
	return err == nil && st.Size() > 0
}

func (q *queue) list() []*job {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]*job, 0, len(q.order))
	for _, id := range q.order {
		j := *q.jobs[id] // копия: наружу отдаём снимок, без указателя на живое
		j.cancel = nil
		out = append(out, &j)
	}
	sort.SliceStable(out, func(a, b int) bool {
		rank := map[jobStatus]int{jobRunning: 0, jobQueued: 1, jobFailed: 2, jobStopped: 3, jobDone: 4}
		return rank[out[a].Status] < rank[out[b].Status]
	})
	return out
}

// add ставит задачу в очередь. Повторную постановку того же файла молча
// приводит к уже существующей: нажать «перекодировать» дважды — обычное дело.
func (q *queue) add(mediaID, name, profile string, duration float64) *job {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, id := range q.order {
		j := q.jobs[id]
		if j.MediaID == mediaID && (j.Status == jobQueued || j.Status == jobRunning) {
			return j
		}
	}
	q.nextID++
	j := &job{
		ID: strconv.Itoa(q.nextID), MediaID: mediaID, Name: name,
		Profile: profile, Status: jobQueued, Duration: duration,
	}
	q.jobs[j.ID] = j
	q.order = append(q.order, j.ID)
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return j
}

func (q *queue) stop(jobID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[jobID]
	if !ok {
		return false
	}
	switch j.Status {
	case jobQueued:
		j.Status = jobStopped
	case jobRunning:
		if j.cancel != nil {
			j.cancel()
		}
	default:
		return false
	}
	return true
}

func (q *queue) run() {
	for {
		j := q.take()
		if j == nil {
			<-q.wake
			continue
		}
		q.execute(j)
	}
}

func (q *queue) take() *job {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, id := range q.order {
		if q.jobs[id].Status == jobQueued {
			return q.jobs[id]
		}
	}
	return nil
}

// args — что именно делаем. Профиль выбирается по вердикту разбора:
// сменить контейнер, пережать звук или пережать всё.
func args(profile, in, out string) []string {
	base := []string{"-nostdin", "-y", "-i", in}
	switch profile {
	case "remux":
		// только смена контейнера: потоки копируются, процессор не при делах
		base = append(base, "-c", "copy")
	case "audio":
		// самый частый случай: видео как есть, AC3/EAC3 → AAC
		base = append(base, "-c:v", "copy", "-c:a", "aac", "-b:a", "192k", "-ac", "2")
	default:
		base = append(base, "-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
			"-c:a", "aac", "-b:a", "192k", "-ac", "2")
	}
	// faststart двигает индекс в начало файла: без него браузер не начнёт
	// играть, пока не скачает всё, и перемотка не работает.
	// «-f mp4» обязателен: пишем во временный файл с расширением .part, а по
	// нему ffmpeg контейнер не угадывает и падает с «Invalid argument».
	return append(base, "-movflags", "+faststart", "-f", "mp4",
		"-progress", "pipe:1", "-nostats", out)
}

func (q *queue) execute(j *job) {
	path, ok := agentStore.pathOf(j.MediaID)
	if !ok {
		q.finish(j, jobFailed, "файл потерялся")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	q.mu.Lock()
	j.Status, j.Started, j.cancel = jobRunning, time.Now(), cancel
	q.mu.Unlock()

	tmp := q.output(j.MediaID) + ".part"
	cmd := exec.CommandContext(ctx, "ffmpeg", args(j.Profile, path, tmp)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		q.finish(j, jobFailed, err.Error())
		return
	}
	var errTail strings.Builder
	cmd.Stderr = &tailWriter{buf: &errTail, limit: 2000}
	if err := cmd.Start(); err != nil {
		q.finish(j, jobFailed, err.Error())
		return
	}

	go q.readProgress(j, stdout)
	err = cmd.Wait()
	// признак «остановили руками» снимаем ДО своего же cancel: иначе он
	// срабатывает всегда, и удачная задача выглядит прерванной
	stoppedByUser := ctx.Err() != nil
	cancel()
	_ = stdout.Close()

	if stoppedByUser {
		_ = os.Remove(tmp)
		q.finish(j, jobStopped, "остановлено")
		return
	}
	if err != nil {
		_ = os.Remove(tmp)
		q.finish(j, jobFailed, lastLine(errTail.String()))
		return
	}
	if err := os.Rename(tmp, q.output(j.MediaID)); err != nil {
		q.finish(j, jobFailed, err.Error())
		return
	}
	q.mu.Lock()
	j.Percent = 100
	q.mu.Unlock()
	q.finish(j, jobDone, "")
	log.Printf("перекодировано: %s", j.Name)
}

// readProgress читает машинный вывод ffmpeg: «out_time_ms=…» и «speed=…».
func (q *queue) readProgress(j *job, r interface{ Read([]byte) (int, error) }) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok {
			continue
		}
		q.mu.Lock()
		switch key {
		case "out_time_ms":
			if us, err := strconv.ParseInt(value, 10, 64); err == nil && j.Duration > 0 {
				// out_time_ms у ffmpeg на самом деле микросекунды
				p := int(float64(us) / 1e6 / j.Duration * 100)
				if p >= 0 && p <= 100 {
					j.Percent = p
				}
			}
		case "speed":
			j.Speed = strings.TrimSpace(value)
		}
		q.mu.Unlock()
	}
}

func (q *queue) finish(j *job, status jobStatus, errText string) {
	q.mu.Lock()
	j.Status, j.Error, j.Ended, j.cancel = status, errText, time.Now(), nil
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}: // взять следующую
	default:
	}
}

// tailWriter держит только хвост вывода: ffmpeg на ошибке пишет километры, а
// показать пользователю надо последнюю строку.
type tailWriter struct {
	buf   *strings.Builder
	limit int
}

func (t *tailWriter) Write(p []byte) (int, error) {
	t.buf.Write(p)
	if t.buf.Len() > t.limit*2 {
		s := t.buf.String()
		t.buf.Reset()
		t.buf.WriteString(s[len(s)-t.limit:])
	}
	return len(p), nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return "ffmpeg не справился"
}
