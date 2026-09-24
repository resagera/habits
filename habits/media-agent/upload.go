package main

// Загрузка файлов из браузера частями: ZIP с музыкой, фон, обложка.
//
// Частями по 768 КБ — потому что перед агентом стоит nginx, а у него предел
// тела запроса по умолчанию 1 МБ: одним запросом не прошла бы даже
// фотография с телефона. Заодно страница показывает прогресс, а оборвавшаяся
// загрузка не портит библиотеку — недокачанное лежит во временном файле кэша
// и убирается через сутки.
//
//	POST /api/admin/upload {name, size}        → {id}
//	PUT  /api/admin/upload/{id}?offset=N        тело — очередной кусок
//	POST /api/admin/upload/{id}/finish?target=…&id=
//	  music-zip | photo-zip — новый альбом в библиотеку музыки или фото
//	  into-zip | into      — архив или один файл в существующую папку (&id=)
//	  background | cover   — фон страницы и обложки

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	uploadChunkMax = 1 << 20 // кусок с запасом под предел nginx
	uploadTotalMax = 8 << 30
)

func (s *server) uploadDir() string { return filepath.Join(s.cfg.cache, "uploads") }

func (s *server) uploadRoutes() {
	s.mux.HandleFunc("POST /api/admin/upload", s.admin(s.uploadStart))
	s.mux.HandleFunc("PUT /api/admin/upload/{id}", s.admin(s.uploadChunk))
	s.mux.HandleFunc("POST /api/admin/upload/{id}/finish", s.admin(s.uploadFinish))
}

func validUploadID(id string) bool {
	if len(id) != 24 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func (s *server) uploadStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil || req.Size <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "нужны name и size"})
		return
	}
	if req.Size > uploadTotalMax {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "файл больше 8 ГБ"})
		return
	}
	// место: сам файл и столько же на распаковку
	if free, _ := diskSpace(s.cfg.cache); free > 0 && free < req.Size*2 {
		writeJSON(w, http.StatusInsufficientStorage, map[string]string{"error": "на диске агента мало места: свободно " + human(free)})
		return
	}
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	if err := os.MkdirAll(s.uploadDir(), 0o700); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	meta, _ := json.Marshal(req)
	_ = os.WriteFile(filepath.Join(s.uploadDir(), id+".json"), meta, 0o600)
	if err := os.WriteFile(filepath.Join(s.uploadDir(), id+".part"), nil, 0o600); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "chunk": 768 << 10})
}

func (s *server) uploadChunk(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validUploadID(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "загрузка не найдена"})
		return
	}
	part := filepath.Join(s.uploadDir(), id+".part")
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "загрузка не найдена"})
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	// кусок пришёл повторно (сеть моргнула после записи) — уже есть, не пишем
	if offset != st.Size() {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "не то место", "size": st.Size()})
		return
	}
	n, err := io.Copy(f, http.MaxBytesReader(w, r.Body, uploadChunkMax))
	if err != nil {
		_ = f.Truncate(st.Size())
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "кусок не записался: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"size": st.Size() + n})
}

func (s *server) uploadFinish(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validUploadID(id) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "загрузка не найдена"})
		return
	}
	part := filepath.Join(s.uploadDir(), id+".part")
	metaPath := filepath.Join(s.uploadDir(), id+".json")
	defer os.Remove(part)
	defer os.Remove(metaPath)
	var meta struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	data, _ := os.ReadFile(metaPath)
	_ = json.Unmarshal(data, &meta)
	st, err := os.Stat(part)
	if err != nil || st.Size() != meta.Size {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "файл дошёл не целиком — повторите загрузку"})
		return
	}
	switch target := r.URL.Query().Get("target"); target {
	case "music-zip", "photo-zip":
		kind, what := "music", "треков"
		if target == "photo-zip" {
			kind, what = "photo", "снимков и роликов"
		}
		lib := s.libraryByKind(kind, r.URL.Query().Get("lib"))
		if lib.Path == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "в настройках агента нет такой библиотеки"})
			return
		}
		album, n, err := extractAlbumZip(part, lib.Path, strings.TrimSuffix(meta.Name, filepath.Ext(meta.Name)), kind, false)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		log.Printf("админка: «%s» — %d %s из %s в %s", album, n, what, meta.Name, lib.Title)
		writeJSON(w, http.StatusOK, map[string]any{"album": album, "tracks": n, "kind": kind,
			"library": lib.Title, "path": filepath.Join(lib.Path, album)})
	case "into", "into-zip":
		dir, ok := s.adminPath(w, r.URL.Query().Get("id"))
		if !ok {
			return
		}
		st, err := os.Stat(dir)
		if err != nil || !st.IsDir() {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "дозагружать можно только в папку"})
			return
		}
		kind := s.libraryOf(dir).Kind
		if target == "into-zip" {
			_, n, err := extractAlbumZip(part, dir, "", kind, true)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			log.Printf("админка: в «%s» добавлено %d файлов из %s", filepath.Base(dir), n, meta.Name)
			writeJSON(w, http.StatusOK, map[string]any{"added": n, "album": filepath.Base(dir)})
			return
		}
		name, err := cleanName(filepath.Base(meta.Name))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "негодное имя файла"})
			return
		}
		if main, keep := wanted(kind, strings.ToLower(filepath.Ext(name))); !main && !keep {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "«" + name + "» не для этой библиотеки"})
			return
		}
		dst := freeName(filepath.Join(dir, name))
		if err := os.Rename(part, dst); err != nil {
			// другой диск — копируем
			if err := copyFile(part, dst); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
		log.Printf("админка: в «%s» добавлен %s", filepath.Base(dir), filepath.Base(dst))
		writeJSON(w, http.StatusOK, map[string]any{"added": 1, "name": filepath.Base(dst), "album": filepath.Base(dir)})
	case "background", "cover":
		dst := s.backgroundPath()
		maxW, maxH := viewWidth, viewHeight
		if target == "cover" {
			p, ok := s.adminPath(w, r.URL.Query().Get("id"))
			if !ok {
				return
			}
			dst, maxW, maxH = coverPath(p), coverSide, coverSide
		}
		// EXIF-поворот читается только у файла с расширением JPEG
		src := part
		if ext := strings.ToLower(filepath.Ext(meta.Name)); ext == ".jpg" || ext == ".jpeg" {
			src = part + ".jpg"
			_ = os.Rename(part, src)
			defer os.Remove(src)
		}
		_ = os.MkdirAll(filepath.Dir(dst), 0o755)
		if err := renderJPEG(src, dst, maxW, maxH, 86); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "картинка не читается (годятся JPG и PNG): " + err.Error()})
			return
		}
		if target == "background" {
			s.looks.update(func(v *looks) { v.BgVer = time.Now().Unix() })
		} else {
			dropOtherPosters(dst)
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target: music-zip, photo-zip, background или cover"})
	}
}

// libraryByKind — куда класть альбом: названная библиотека или первая
// подходящая по виду.
func (s *server) libraryByKind(kind, id string) library {
	for _, lib := range s.cfg.roots {
		if lib.Kind == kind && lib.available() && (id == "" || lib.ID == id) {
			return lib
		}
	}
	return library{}
}

// cleanUploads убирает недокачанное старше суток.
func (s *server) cleanUploads() {
	entries, _ := os.ReadDir(s.uploadDir())
	for _, e := range entries {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
			_ = os.Remove(filepath.Join(s.uploadDir(), e.Name()))
		}
	}
}

// --- распаковка музыки ---

var keepInAlbum = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".lrc": true, ".cue": true}

// wanted — что оставляем из архива: главное по виду библиотеки и попутное
// (обложки, тексты). Подпапки внутри альбома сохраняются как есть — у музыки
// это CD1/CD2, у фото «День 1», «День 2».
func wanted(kind, ext string) (main, keep bool) {
	if kind == "photo" {
		return photoExt[ext] || videoExt[ext], keepInAlbum[ext]
	}
	return audioExt[ext], keepInAlbum[ext]
}

// extractAlbumZip распаковывает архив альбомом в корень библиотеки. Одна папка
// внутри архива — это и есть альбом; иначе альбом называется по архиву.
// Распаковка идёт во временную скрытую папку и переезжает на место одним
// rename: каталог не увидит альбом наполовину. Имя занято — «Альбом (2)».
//
// into=true — дозагрузка в существующую папку: лишняя папка-обёртка из архива
// снимается, файлы ложатся прямо в неё (подпапки внутри сохраняются), а
// занятые имена получают «(2)» — чужое не затирается.
func extractAlbumZip(zipPath, root, fallbackName, kind string, into bool) (string, int, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", 0, errors.New("это не ZIP-архив или он повреждён")
	}
	defer zr.Close()
	type member struct {
		f     *zip.File
		parts []string
	}
	var files []member
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := zipName(f)
		parts := []string{}
		for _, p := range strings.Split(strings.ReplaceAll(name, "\\", "/"), "/") {
			if p != "" && p != "." {
				parts = append(parts, p)
			}
		}
		if len(parts) == 0 || containsStr(parts, "..") || containsStr(parts, "__MACOSX") ||
			strings.HasPrefix(parts[len(parts)-1], ".") {
			continue
		}
		main, keep := wanted(kind, strings.ToLower(path.Ext(parts[len(parts)-1])))
		if !main && !keep {
			continue
		}
		files = append(files, member{f, parts})
	}
	tracks := 0
	for _, m := range files {
		if main, _ := wanted(kind, strings.ToLower(path.Ext(m.parts[len(m.parts)-1]))); main {
			tracks++
		}
	}
	if tracks == 0 {
		if kind == "photo" {
			return "", 0, errors.New("в архиве нет снимков и роликов (jpg, png, mp4, mov…)")
		}
		return "", 0, errors.New("в архиве нет треков (mp3, m4a, flac, ogg, opus, wav…)")
	}
	top := ""
	single := true
	for _, m := range files {
		if len(m.parts) < 2 || (top != "" && m.parts[0] != top) {
			single = false
			break
		}
		top = m.parts[0]
	}
	album := fallbackName
	if single {
		album = top
		for i := range files {
			files[i].parts = files[i].parts[1:]
		}
	}
	if into {
		// файлы ложатся в саму папку, по одному: наполовину распакованный
		// архив тут не страшен, а занятые имена не затираются
		added := 0
		for _, m := range files {
			dst := freeName(filepath.Join(append([]string{root}, m.parts...)...))
			if !strings.HasPrefix(dst, root+string(filepath.Separator)) {
				continue
			}
			if err := unzipOne(m.f, dst); err != nil {
				return "", added, fmt.Errorf("не распаковался %s: %v", strings.Join(m.parts, "/"), err)
			}
			added++
		}
		return filepath.Base(root), added, nil
	}
	album, err = cleanName(album)
	if err != nil {
		album = "Загружено " + time.Now().Format("2006-01-02 15-04")
	}
	final := filepath.Join(root, album)
	for i := 2; exists(final); i++ {
		final = filepath.Join(root, fmt.Sprintf("%s (%d)", album, i))
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	tmp := filepath.Join(root, ".upload-"+hex.EncodeToString(b))
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return "", 0, err
	}
	for _, m := range files {
		dst := filepath.Join(append([]string{tmp}, m.parts...)...)
		if !strings.HasPrefix(dst, tmp+string(filepath.Separator)) {
			continue
		}
		if err := unzipOne(m.f, dst); err != nil {
			_ = os.RemoveAll(tmp)
			return "", 0, fmt.Errorf("не распаковался %s: %v", strings.Join(m.parts, "/"), err)
		}
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.RemoveAll(tmp)
		return "", 0, err
	}
	return filepath.Base(final), tracks, nil
}

func unzipOne(f *zip.File, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, io.LimitReader(rc, int64(f.UncompressedSize64)+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// freeName — свободное имя рядом: «Трек.mp3» занят → «Трек (2).mp3».
func freeName(p string) string {
	if !exists(p) {
		return p
	}
	ext := filepath.Ext(p)
	base := strings.TrimSuffix(p, ext)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if !exists(candidate) {
			return candidate
		}
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dst)
	}
	return err
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// zipName — имя файла в архиве по-человечески. Архиваторы Windows пишут
// кириллицу в CP866 без флага UTF-8, и Go отдаёт её как есть — «ђ‹мЎ®¬»
// вместо «Альбом». Если имя не UTF-8 — декодируем как CP866.
func zipName(f *zip.File) string {
	if f.NonUTF8 || !utf8.ValidString(f.Name) {
		return cp866(f.Name)
	}
	return f.Name
}

func cp866(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c < 0x80:
			b.WriteByte(c)
		case c <= 0xAF: // А..Я, а..п
			b.WriteRune(rune(0x0410 + int(c-0x80)))
		case c >= 0xE0 && c <= 0xEF: // р..я
			b.WriteRune(rune(0x0440 + int(c-0xE0)))
		case c == 0xF0:
			b.WriteRune('Ё')
		case c == 0xF1:
			b.WriteRune('ё')
		default: // псевдографика — в именах файлов не бывает
			b.WriteByte('_')
		}
	}
	return b.String()
}
