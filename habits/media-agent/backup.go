package main

// Резервная копия разметки и присмотр за местом на дисках.
//
// Разметка — это то, чего больше нигде нет: отметки заставки и титров,
// папки-подборки, избранное, ссылки, «Продолжить просмотр», обложки папок,
// выбранный фон и кадры для «Инфо». Всё это лежит в одном экземпляре в кэше
// агента, на том же системном диске, где кончается место. Умрёт диск — пропадут
// часы ручной и машинной работы, а сами фильмы при этом целы.
//
// Поэтому раз в неделю кладём архив рядом (по возможности на ДРУГОЙ диск) и,
// если он небольшой, отправляем копию себе в Telegram — это и есть хранение
// вне дома.
//
// Что НЕ кладём: кэш разбора (probe.json), копии фото, перекодированные файлы,
// APK — всё это собирается заново само. И ключ подписи ссылок: секрету не место
// в архиве, который уезжает наружу, а новый агент заведёт его сам (пропадут
// только ссылки, открытые в браузере прямо сейчас).

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	backupKeep    = 8        // сколько архивов держим на диске
	backupMaxSend = 20 << 20 // крупнее этого в Telegram не отправляем
	maintEvery    = time.Hour
	spaceRepeat   = 24 * time.Hour // как часто напоминать про одно и то же место
)

// backupSkip — что в архив не кладём: либо соберётся заново, либо секрет.
var backupSkip = map[string]bool{
	"photos": true, "transcoded": true, "uploads": true, "backups": true,
	"probe.json": true, "secret": true, "habits-tv.apk": true,
}

// --- память между прогонами ---

type spaceState struct {
	WarnedAt int64 `json:"warned_at"`
	Free     int64 `json:"free"`
}

type maintState struct {
	LastBackup int64                  `json:"last_backup"`
	LastSize   int64                  `json:"last_size"`
	LastName   string                 `json:"last_name"`
	LastSent   bool                   `json:"last_sent"`
	LastError  string                 `json:"last_error,omitempty"`
	Space      map[string]*spaceState `json:"space"`
}

type maintStore struct {
	mu   sync.Mutex
	path string
	v    maintState
}

func newMaintStore(cacheDir string) *maintStore {
	m := &maintStore{path: filepath.Join(cacheDir, "maint.json"),
		v: maintState{Space: map[string]*spaceState{}}}
	if data, err := os.ReadFile(m.path); err == nil {
		_ = json.Unmarshal(data, &m.v)
	}
	if m.v.Space == nil {
		m.v.Space = map[string]*spaceState{}
	}
	return m
}

func (ms *maintStore) get() maintState {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.v
}

func (ms *maintStore) update(fn func(v *maintState)) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	fn(&ms.v)
	if data, err := json.MarshalIndent(ms.v, "", " "); err == nil {
		_ = writeAtomic(ms.path, data)
	}
}

// --- сборка архива ---

func (s *server) backupDir() string {
	if s.cfg.backupDir != "" {
		return s.cfg.backupDir
	}
	return filepath.Join(s.cfg.cache, "backups")
}

// makeBackup собирает архив и возвращает путь к нему.
func (s *server) makeBackup() (string, int64, error) {
	dir := s.backupDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", 0, err
	}
	name := "habits-media-" + time.Now().Format("20060102-1504") + ".zip"
	path := filepath.Join(dir, name)
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(tmp)

	zw := zip.NewWriter(f)
	if err := s.writeBackup(zw); err != nil {
		zw.Close()
		f.Close()
		return "", 0, err
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return "", 0, err
	}
	st, err := f.Stat()
	f.Close()
	if err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", 0, err
	}
	s.dropOldBackups(dir)
	return path, st.Size(), nil
}

func (s *server) writeBackup(zw *zip.Writer) error {
	readme, _ := zw.Create("ЧТО-ЭТО.txt")
	fmt.Fprintf(readme, backupReadme, time.Now().Format("02.01.2006 15:04"), s.cfg.cache)

	root := s.cfg.cache
	return filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // пропавший на ходу файл не повод ронять копию
		}
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == "." {
			return nil
		}
		if backupSkip[info.Name()] || strings.HasSuffix(info.Name(), ".part") ||
			strings.HasSuffix(info.Name(), ".tmp") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		src, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer src.Close()
		_, err = io.Copy(w, src)
		return err
	})
}

const backupReadme = `Резервная копия разметки медиаагента Habits
Собрана %s из %s

Внутри — то, чего больше нигде нет:
  marks.json        отметки заставки и титров (сериал / сезон / серия)
  collections.json  папки-подборки и их состав
  favorites.json    избранное
  links.json        ссылки «где ещё посмотреть»
  progress.json     «Продолжить просмотр»
  looks.json        оформление: фон, размытие, масштаб
  playlists.json    музыкальные плейлисты
  stats.json        статистика просмотра
  trash.json        корзина админки
  room              имя комнаты для пульта
  collections/      обложки папок-подборок
  app/background.jpg выбранный фон
  frames/           кадры для карточек «Инфо»

Чего внутри НЕТ (соберётся заново само):
  probe.json, photos/, transcoded/, uploads/, APK приложения
  secret — ключ подписи ссылок: секрету не место в архиве, который уезжает
  наружу. Новый агент заведёт свой; перестанут работать только ссылки,
  открытые в браузере прямо сейчас.

Как вернуть: остановить агент, распаковать содержимое в кэш
(systemctl --user stop habits-media-agent), запустить обратно.
`

func (s *server) dropOldBackups(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "habits-media-") && strings.HasSuffix(e.Name(), ".zip") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // имя со временем — порядок и есть хронология
	for i := 0; i < len(names)-backupKeep; i++ {
		_ = os.Remove(filepath.Join(dir, names[i]))
	}
}

// --- отправка в Telegram ---

// sendBackup отдаёт архив боту через прод: токен бота живёт там, а не на
// мини-сервере.
func (s *server) sendBackup(path string, size int64) error {
	u := s.notifyURL()
	if u == "" || s.cfg.room == "" {
		return fmt.Errorf("шина не настроена")
	}
	if size > backupMaxSend {
		return fmt.Errorf("архив %s — больше предела %s", human(size), human(backupMaxSend))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("room", s.cfg.room)
	_ = mw.WriteField("caption", "Резервная копия разметки медиатеки, "+human(size)+
		". Внутри отметки заставки, папки, избранное, ссылки, «Продолжить» и кадры.")
	part, err := mw.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	resp, err := notifyHTTP.Post(u+"/file", mw.FormDataContentType(), bytes.NewReader(body.Bytes()))
	if err != nil {
		return fmt.Errorf("прод недоступен: %v", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK:
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("к этой приставке ещё не подключали пульт — отправлять некому")
	}
	return fmt.Errorf("прод ответил %d", resp.StatusCode)
}

// runBackup — собрать копию и, если влезает, отправить её себе.
func (s *server) runBackup() (maintState, error) {
	path, size, err := s.makeBackup()
	if err != nil {
		s.maint.update(func(v *maintState) { v.LastError = err.Error() })
		log.Printf("резервная копия не собралась: %v", err)
		return s.maint.get(), err
	}
	sent := false
	note := ""
	if err := s.sendBackup(path, size); err != nil {
		note = err.Error()
		log.Printf("резервная копия не ушла в Telegram: %v", err)
	} else {
		sent = true
	}
	log.Printf("резервная копия: %s (%s), в Telegram: %v", path, human(size), sent)
	s.maint.update(func(v *maintState) {
		v.LastBackup, v.LastSize, v.LastName, v.LastSent, v.LastError =
			time.Now().Unix(), size, filepath.Base(path), sent, note
	})
	return s.maint.get(), nil
}

// --- место на дисках ---

// deviceOf — на каком разделе лежит путь: у библиотек и кэша он часто один, и
// предупреждать о нём дважды незачем.
func deviceOf(path string) uint64 {
	var st syscall.Stat_t
	if syscall.Stat(path, &st) != nil {
		return 0
	}
	return uint64(st.Dev)
}

type spaceSpot struct {
	title string
	path  string
	free  int64
	total int64
}

// spaceSpots — по строке на РАЗДЕЛ, а не на библиотеку: у фильмов, музыки и
// кэша он часто один, и предупреждать о нём трижды незачем. Имена при этом
// собираем все: «Фильмы — свободно 8 ГБ» вместо «Сериалы», когда на том же
// диске лежит половина медиатеки, только путает.
func (s *server) spaceSpots() []spaceSpot {
	at := map[uint64]int{}
	out := []spaceSpot{}
	add := func(title, path string) {
		dev := deviceOf(path)
		if i, ok := at[dev]; ok {
			if !strings.Contains(out[i].title, title) {
				out[i].title += ", " + title
			}
			return
		}
		free, total := diskSpace(path)
		if total == 0 {
			return
		}
		at[dev] = len(out)
		out = append(out, spaceSpot{title: title, path: path, free: free, total: total})
	}
	for _, lib := range s.cfg.roots {
		if lib.available() {
			add(lib.Title, lib.Path)
		}
	}
	add("кэш агента", s.cfg.cache)
	return out
}

// spaceView — то же, что видит присмотр за местом, но для админки.
func (s *server) spaceView() []map[string]any {
	limit := int64(s.cfg.lowSpaceGB) << 30
	out := []map[string]any{}
	for _, sp := range s.spaceSpots() {
		out = append(out, map[string]any{"title": sp.title, "path": sp.path,
			"free": sp.free, "total": sp.total, "low": limit > 0 && sp.free < limit})
	}
	return out
}

// checkSpace предупреждает, когда место кончается, и говорит, когда оно
// вернулось. Напоминаем не чаще раза в сутки — и сразу, если с прошлого раза
// свободного стало заметно меньше: быстрое падение важнее расписания.
func (s *server) checkSpace() {
	limit := int64(s.cfg.lowSpaceGB) << 30
	if limit <= 0 {
		return
	}
	for _, sp := range s.spaceSpots() {
		key := fmt.Sprintf("%d", deviceOf(sp.path))
		st := s.maint.get().Space[key]
		switch {
		case sp.free < limit:
			now := time.Now().Unix()
			if st != nil && now-st.WarnedAt < int64(spaceRepeat/time.Second) && sp.free > st.Free*3/4 {
				continue
			}
			s.notifyAsync(fmt.Sprintf("⚠ Мало места: %s — свободно %s из %s (%s)",
				sp.title, human(sp.free), human(sp.total), sp.path))
			s.maint.update(func(v *maintState) { v.Space[key] = &spaceState{WarnedAt: now, Free: sp.free} })
		case st != nil && sp.free > limit*6/5:
			// с запасом выше порога — чтобы сообщения не мигали на границе
			s.notifyAsync(fmt.Sprintf("Место снова в порядке: %s — свободно %s", sp.title, human(sp.free)))
			s.maint.update(func(v *maintState) { delete(v.Space, key) })
		}
	}
}

// maintWorker — раз в час смотрит на место и собирает копию, если неделя прошла.
// Первый прогон через две минуты после старта: агент успевает подняться, а если
// копии нет вовсе, она появится сразу.
func (s *server) maintWorker() {
	time.Sleep(2 * time.Minute)
	for {
		s.checkSpace()
		last := s.maint.get().LastBackup
		if time.Since(time.Unix(last, 0)) >= time.Duration(s.cfg.backupDays)*24*time.Hour {
			_, _ = s.runBackup()
		}
		time.Sleep(maintEvery)
	}
}

// POST /api/admin/backup — собрать копию сейчас.
func (s *server) adminBackup(w http.ResponseWriter, r *http.Request) {
	v, err := s.runBackup()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backup": v, "dir": s.backupDir()})
}
