package main

// Просмотр файлов, которые не играются: текст и PDF.
//
// PDF рисует агент (pdftoppm из poppler) постранично в JPEG: во вебвью
// Android TV встроенного просмотра PDF нет, а тащить на приставку pdf.js —
// мегабайты кода ради редкого файла. Страница-картинка листается стрелками
// пульта так же, как снимки. Готовые страницы лежат в кэше `pdf/` и в копию
// разметки не едут.
//
// Текст отдаётся началом файла (textMax): нужен, чтобы прочитать .nfo или
// заметку, а не открыть лог на гигабайт. Не UTF-8 — значит, почти наверняка
// cp1251: так лежат старые .nfo и субтитры с русских раздач.

import (
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	textMax  = 512 << 10
	pdfWidth = 1600 // по ширине: страницу читают, а не любуются ею
)

var pdfLocks sync.Map // путь страницы → *sync.Mutex: одну страницу рисуем один раз

// fileFor — путь файла по id с проверкой подписи и замков. Пусто — уже
// ответили отказом.
func (s *server) fileFor(w http.ResponseWriter, r *http.Request) (string, os.FileInfo) {
	id := r.PathValue("id")
	if !s.validKey(id, r.URL.Query().Get("k")) {
		http.Error(w, "нет доступа", http.StatusForbidden)
		return "", nil
	}
	path, ok := agentStore.pathOf(id)
	if !ok || !s.inRoots(path) || !s.visible(path, r) {
		http.NotFound(w, r)
		return "", nil
	}
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return "", nil
	}
	return path, st
}

// GET /text/{id}?k= — начало текстового файла.
func (s *server) viewText(w http.ResponseWriter, r *http.Request) {
	path, st := s.fileFor(w, r)
	if path == "" {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	buf, _ := io.ReadAll(io.LimitReader(f, textMax))
	text, enc := decodeText(buf)
	writeJSON(w, http.StatusOK, map[string]any{"text": text, "enc": enc,
		"truncated": st.Size() > int64(len(buf)), "size": st.Size()})
}

// decodeText — UTF-8 (с BOM или без), UTF-16 по BOM, иначе cp1251.
func decodeText(b []byte) (string, string) {
	switch {
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		b = b[3:]
	case len(b) >= 2 && (b[0] == 0xFF && b[1] == 0xFE || b[0] == 0xFE && b[1] == 0xFF):
		return decodeUTF16(b[2:], b[0] == 0xFF), "utf-16"
	}
	// обрезанный посреди буквы конец не делает файл «не UTF-8»
	cut := b
	for i := 0; i < 3 && len(cut) > 0 && !utf8.Valid(cut); i++ {
		cut = cut[:len(cut)-1]
	}
	if utf8.Valid(cut) {
		return string(cut), "utf-8"
	}
	var sb strings.Builder
	sb.Grow(len(b) * 2)
	for _, c := range b {
		if c < 0x80 {
			sb.WriteByte(c)
		} else {
			sb.WriteRune(cp1251[c-0x80])
		}
	}
	return sb.String(), "cp1251"
}

func decodeUTF16(b []byte, little bool) string {
	var sb strings.Builder
	for i := 0; i+1 < len(b); i += 2 {
		u := uint16(b[i])<<8 | uint16(b[i+1])
		if little {
			u = uint16(b[i+1])<<8 | uint16(b[i])
		}
		if u >= 0xD800 && u < 0xE000 {
			if i+3 < len(b) {
				lo := uint16(b[i+2])<<8 | uint16(b[i+3])
				if little {
					lo = uint16(b[i+3])<<8 | uint16(b[i+2])
				}
				sb.WriteRune(rune((uint32(u)-0xD800)<<10 + (uint32(lo) - 0xDC00) + 0x10000))
				i += 2
			}
			continue
		}
		sb.WriteRune(rune(u))
	}
	return sb.String()
}

// cp1251 — верхняя половина Windows-1251.
var cp1251 = [128]rune{
	'Ђ', 'Ѓ', '‚', 'ѓ', '„', '…', '†', '‡', '€', '‰', 'Љ', '‹', 'Њ', 'Ќ', 'Ћ', 'Џ',
	'ђ', '‘', '’', '“', '”', '•', '–', '—', '�', '™', 'љ', '›', 'њ', 'ќ', 'ћ', 'џ',
	' ', 'Ў', 'ў', 'Ј', '¤', 'Ґ', '¦', '§', 'Ё', '©', 'Є', '«', '¬', '­', '®', 'Ї',
	'°', '±', 'І', 'і', 'ґ', 'µ', '¶', '·', 'ё', '№', 'є', '»', 'ј', 'Ѕ', 'ѕ', 'ї',
	'А', 'Б', 'В', 'Г', 'Д', 'Е', 'Ж', 'З', 'И', 'Й', 'К', 'Л', 'М', 'Н', 'О', 'П',
	'Р', 'С', 'Т', 'У', 'Ф', 'Х', 'Ц', 'Ч', 'Ш', 'Щ', 'Ъ', 'Ы', 'Ь', 'Э', 'Ю', 'Я',
	'а', 'б', 'в', 'г', 'д', 'е', 'ж', 'з', 'и', 'й', 'к', 'л', 'м', 'н', 'о', 'п',
	'р', 'с', 'т', 'у', 'ф', 'х', 'ц', 'ч', 'ш', 'щ', 'ъ', 'ы', 'ь', 'э', 'ю', 'я',
}

// GET /api/pdf/{id}?k= — сколько страниц. Индекс обычно уже знает, но PDF
// мог появиться после обхода.
func (s *server) pdfInfo(w http.ResponseWriter, r *http.Request) {
	path, st := s.fileFor(w, r)
	if path == "" {
		return
	}
	pages := 0
	if rec, ok := s.index.get(path); ok && rec.MTime == st.ModTime().Unix() && rec.Size == st.Size() {
		pages = rec.Pages
	}
	if pages == 0 {
		pages = pdfPages(path)
	}
	if pages == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "PDF не открылся (нет poppler или файл испорчен)"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"pages": pages})
}

// GET /pdf/{id}/{page}?k= — страница картинкой.
func (s *server) pdfPage(w http.ResponseWriter, r *http.Request) {
	path, st := s.fileFor(w, r)
	if path == "" {
		return
	}
	page, err := strconv.Atoi(r.PathValue("page"))
	if err != nil || page < 1 || page > 100000 {
		http.Error(w, "нет такой страницы", http.StatusBadRequest)
		return
	}
	// в имени папки время правки: заменили файл — старые страницы не годятся
	dir := filepath.Join(s.cfg.cache, "pdf", shortID(path)+"-"+strconv.FormatInt(st.ModTime().Unix(), 36))
	out := filepath.Join(dir, "p-"+strconv.Itoa(page)+".jpg")
	if !exists(out) {
		mu, _ := pdfLocks.LoadOrStore(out, &sync.Mutex{})
		mu.(*sync.Mutex).Lock()
		if !exists(out) {
			err = renderPDFPage(path, page, dir, out)
		}
		mu.(*sync.Mutex).Unlock()
		pdfLocks.Delete(out)
		if err != nil {
			log.Printf("pdf %s стр. %d: %v", path, page, err)
			http.Error(w, "страница не нарисовалась", http.StatusUnprocessableEntity)
			return
		}
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, out)
}

func renderPDFPage(src string, page int, dir, out string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	prefix := filepath.Join(dir, "tmp-"+strconv.Itoa(page))
	p := strconv.Itoa(page)
	cmd := exec.Command("pdftoppm", "-f", p, "-l", p, "-singlefile", "-jpeg", "-jpegopt", "quality=85",
		"-scale-to-x", strconv.Itoa(pdfWidth), "-scale-to-y", "-1", src, prefix)
	if b, err := cmd.CombinedOutput(); err != nil {
		return &pdfErr{strings.TrimSpace(string(b)), err}
	}
	return os.Rename(prefix+".jpg", out)
}

type pdfErr struct {
	msg string
	err error
}

func (e *pdfErr) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return e.err.Error()
}
