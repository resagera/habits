package main

// Субтитры рядом с видео: «Фильм.srt», «Фильм.ru.srt», «Фильм.eng.vtt».
//
// Браузер понимает только WebVTT, а с раздач приходит SRT, часто в cp1251,
// и ASS. Агент отдаёт любой из них как VTT в UTF-8 — переводит на лету, файл
// на диске не трогает. Какие файлы к какой серии — tracks.go; встроенные в
// mkv субтитры там же выгружаются рядом отдельными файлами.

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type subRef struct {
	Label string `json:"label"`
	Lang  string `json:"lang,omitempty"`
	URL   string `json:"url"`
}

var subExt = sideSubExt

// subLangs — подпись к частым кодам языков в имени файла.
var subLangs = map[string]string{"ru": "Русские", "rus": "Русские", "en": "Английские", "eng": "Английские",
	"uk": "Украинские", "ukr": "Украинские", "hy": "Армянские", "arm": "Армянские", "de": "Немецкие",
	"fr": "Французские", "es": "Испанские", "ja": "Японские", "jpn": "Японские"}

var reSrtTime = regexp.MustCompile(`(\d{1,2}):(\d{2}):(\d{2})[,.](\d{3})`)

// srtToVTT — SRT в WebVTT: заголовок, точка вместо запятой в метках времени,
// часы двумя цифрами. Номера реплик VTT разрешает как идентификаторы.
func srtToVTT(text string) string {
	text = strings.ReplaceAll(strings.TrimPrefix(text, "\ufeff"), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if strings.HasPrefix(text, "WEBVTT") {
		return text
	}
	text = reSrtTime.ReplaceAllStringFunc(text, func(m string) string {
		p := reSrtTime.FindStringSubmatch(m)
		h := p[1]
		if len(h) == 1 {
			h = "0" + h
		}
		return h + ":" + p[2] + ":" + p[3] + "." + p[4]
	})
	return "WEBVTT\n\n" + strings.TrimLeft(text, "\n")
}

// GET /subs/{id}?k= — субтитры в WebVTT.
func (s *server) subs(w http.ResponseWriter, r *http.Request) {
	path, _ := s.fileFor(w, r)
	if path == "" {
		return
	}
	if !subExt[strings.ToLower(filepath.Ext(path))] {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	buf, _ := io.ReadAll(io.LimitReader(f, 4<<20))
	text, _ := decodeText(buf)
	if ext := strings.ToLower(filepath.Ext(path)); ext == ".ass" || ext == ".ssa" {
		// ASS со стилями браузер не знает — ffmpeg переводит в VTT (без оформления)
		text = assToVTT(r.Context(), text)
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = io.WriteString(w, srtToVTT(text))
}

// assToVTT — субтитры ASS/SSA в WebVTT через ffmpeg. Не вышло — пустой VTT:
// лучше без субтитров, чем ошибка плеера.
func assToVTT(ctx context.Context, text string) string {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-f", "ass", "-i", "pipe:0", "-f", "webvtt", "pipe:1")
	cmd.Stdin = strings.NewReader(text)
	out, err := cmd.Output()
	if err != nil {
		return "WEBVTT\n"
	}
	return string(out)
}
