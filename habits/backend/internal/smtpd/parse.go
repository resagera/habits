package smtpd

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"golang.org/x/text/encoding/ianaindex"
)

// Разбор письма. Стандартная библиотека покрывает почти всё; недостаёт только
// перекодировки — Go работает с UTF-8, а русские магазины до сих пор шлют
// windows-1251 и koi8-r. Её берёт на себя x/text (он уже в зависимостях
// транзитом, поэтому новых модулей не появляется).

// Parsed — то, что вытащили из письма.
type Parsed struct {
	Subject     string
	FromName    string
	FromAddr    string
	MessageID   string
	Date        *time.Time
	Text        string
	HTML        string
	Attachments []ParsedAttachment
	// признаки для оценки: письма без Message-ID и Date почти всегда шлёт скрипт
	HasMessageID bool
	HasDate      bool
}

// ParsedAttachment — вложение целиком в памяти: письмо и так ограничено 15 МБ.
type ParsedAttachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// декодер заголовков: =?utf-8?B?...?= и такие же в cp1251
var headerDecoder = mime.WordDecoder{CharsetReader: charsetReader}

func charsetReader(label string, input io.Reader) (io.Reader, error) {
	enc, err := ianaindex.MIME.Encoding(label)
	if err != nil || enc == nil {
		return input, nil // неизвестная кодировка — отдаём как есть, не теряя письмо
	}
	return enc.NewDecoder().Reader(input), nil
}

func decodeHeader(v string) string {
	out, err := headerDecoder.DecodeHeader(v)
	if err != nil {
		return v
	}
	return out
}

// Parse разбирает сырое письмо. Ошибка разбора не должна терять письмо:
// исходник всё равно лежит на диске, поэтому при сбое возвращаем то, что
// удалось вытащить.
func Parse(raw []byte) Parsed {
	var p Parsed
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		p.Text = string(raw)
		return p
	}
	h := msg.Header
	p.Subject = decodeHeader(h.Get("Subject"))
	p.MessageID = strings.Trim(h.Get("Message-ID"), "<> ")
	p.HasMessageID = p.MessageID != ""
	if d, err := mail.ParseDate(h.Get("Date")); err == nil {
		p.Date = &d
		p.HasDate = true
	}
	if addr, err := mail.ParseAddress(h.Get("From")); err == nil {
		p.FromName = decodeHeader(addr.Name)
		p.FromAddr = strings.ToLower(addr.Address)
	} else {
		p.FromName = decodeHeader(h.Get("From"))
	}

	ct := h.Get("Content-Type")
	if ct == "" {
		ct = "text/plain"
	}
	body, _ := io.ReadAll(msg.Body)
	walkPart(&p, ct, h.Get("Content-Transfer-Encoding"), "", body, 0)

	if p.Text == "" && p.HTML != "" {
		p.Text = htmlToText(p.HTML)
	}
	return p
}

// walkPart раскладывает часть письма: multipart рекурсивно, текст в поля,
// остальное во вложения.
func walkPart(p *Parsed, contentType, encoding, disposition string, body []byte, depth int) {
	if depth > 10 {
		return // защита от письма-матрёшки
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = "text/plain"
		params = map[string]string{}
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return
		}
		mr := multipart.NewReader(bytes.NewReader(body), boundary)
		for {
			part, err := mr.NextPart()
			if err != nil {
				return
			}
			data, err := io.ReadAll(part)
			if err != nil {
				continue
			}
			walkPart(p, part.Header.Get("Content-Type"),
				part.Header.Get("Content-Transfer-Encoding"),
				part.Header.Get("Content-Disposition"), data, depth+1)
		}
	}

	decoded := decodeBody(body, encoding)
	filename := ""
	if disposition != "" {
		if _, dp, err := mime.ParseMediaType(disposition); err == nil {
			filename = decodeHeader(dp["filename"])
		}
	}
	if filename == "" {
		filename = decodeHeader(params["name"])
	}
	isAttachment := filename != "" || strings.HasPrefix(disposition, "attachment")

	switch {
	case !isAttachment && mediaType == "text/plain":
		p.Text += toUTF8(decoded, params["charset"])
	case !isAttachment && mediaType == "text/html":
		p.HTML += toUTF8(decoded, params["charset"])
	case len(decoded) > 0:
		p.Attachments = append(p.Attachments, ParsedAttachment{
			Filename: filename, ContentType: mediaType, Data: decoded,
		})
	}
}

func decodeBody(body []byte, encoding string) []byte {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		// письма часто содержат переносы внутри base64, поэтому чистим пробелы
		clean := bytes.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, body)
		out, err := base64.StdEncoding.DecodeString(string(clean))
		if err != nil {
			return body
		}
		return out
	case "quoted-printable":
		out, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body)))
		if err != nil {
			return body
		}
		return out
	default:
		return body
	}
}

func toUTF8(data []byte, charset string) string {
	charset = strings.ToLower(strings.TrimSpace(charset))
	if charset == "" || charset == "utf-8" || charset == "utf8" || charset == "us-ascii" {
		return string(data)
	}
	enc, err := ianaindex.MIME.Encoding(charset)
	if err != nil || enc == nil {
		return string(data)
	}
	out, err := io.ReadAll(enc.NewDecoder().Reader(bytes.NewReader(data)))
	if err != nil {
		return string(data)
	}
	return string(out)
}

var (
	reScriptStyle = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	reBreak       = regexp.MustCompile(`(?i)<(br|/p|/div|/tr|/h[1-6])[^>]*>`)
	reTag         = regexp.MustCompile(`(?s)<[^>]*>`)
	reSpaces      = regexp.MustCompile(`[ \t]{2,}`)
	reNewlines    = regexp.MustCompile(`\n{3,}`)
)

// htmlToText — грубое превращение письма в текст: нужно и для поиска, и для
// будущего разбора чеков, а тащить парсер HTML ради этого незачем.
func htmlToText(h string) string {
	s := reScriptStyle.ReplaceAllString(h, " ")
	s = reBreak.ReplaceAllString(s, "\n")
	s = reTag.ReplaceAllString(s, " ")
	s = htmlUnescape(s)
	s = strings.ReplaceAll(s, "\r", "")
	s = reSpaces.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	s = strings.Join(lines, "\n")
	return strings.TrimSpace(reNewlines.ReplaceAllString(s, "\n\n"))
}

var htmlEntities = strings.NewReplacer(
	"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`,
	"&#39;", "'", "&apos;", "'", "&mdash;", "—", "&ndash;", "–", "&laquo;", "«",
	"&raquo;", "»", "&rsquo;", "’", "&hellip;", "…", "&#8212;", "—",
)

func htmlUnescape(s string) string {
	return htmlEntities.Replace(s)
}

// clip обрезает строку до n рун — тела писем в базе не должны быть безразмерными.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
