// Package tgphotos — приём картинок в чате бота: пользователь отправляет
// боту фото, оно сохраняется в его галерею фонов (Settings → Фон).
// Единственный потребитель getUpdates этого бота — конфликтов нет.
package tgphotos

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"crypto/rand"
	"encoding/hex"

	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

const (
	pollTimeout  = 25 // long-poll, секунды
	maxFileBytes = 5 << 20
)

type Worker struct {
	Store   *store.Store
	Bot     *notify.Bot
	DataDir string
	Logger  *slog.Logger
	// APIBase для тестов; пусто — прод.
	APIBase string
}

type update struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		From *struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
		Text  string `json:"text"`
		Photo []struct {
			FileID   string `json:"file_id"`
			FileSize int64  `json:"file_size"`
		} `json:"photo"`
		Document *struct {
			FileID   string `json:"file_id"`
			MimeType string `json:"mime_type"`
			FileSize int64  `json:"file_size"`
		} `json:"document"`
	} `json:"message"`
}

func (w *Worker) base() string {
	if w.APIBase != "" {
		return w.APIBase
	}
	return "https://api.telegram.org"
}

func (w *Worker) Run(ctx context.Context) {
	if w.Bot.Token == "" {
		w.Logger.Warn("tgphotos: BOT_TOKEN is empty, worker disabled")
		return
	}
	var offset int64
	for ctx.Err() == nil {
		updates, err := w.getUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			w.Logger.Error("tgphotos: getUpdates", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			w.handle(ctx, u)
		}
	}
}

func (w *Worker) getUpdates(ctx context.Context, offset int64) ([]update, error) {
	q := url.Values{}
	q.Set("timeout", fmt.Sprint(pollTimeout))
	q.Set("allowed_updates", `["message"]`)
	if offset > 0 {
		q.Set("offset", fmt.Sprint(offset))
	}
	ctx, cancel := context.WithTimeout(ctx, (pollTimeout+10)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/bot%s/getUpdates?%s", w.base(), w.Bot.Token, q.Encode()), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		OK          bool     `json:"ok"`
		Result      []update `json:"result"`
		Description string   `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("telegram: %s", out.Description)
	}
	return out.Result, nil
}

func (w *Worker) handle(ctx context.Context, u update) {
	m := u.Message
	if m == nil || m.From == nil || m.Chat.Type != "private" {
		return
	}
	userID := m.From.ID

	fileID, size := "", int64(0)
	switch {
	case len(m.Photo) > 0:
		// последний элемент — самое большое разрешение
		best := m.Photo[len(m.Photo)-1]
		fileID, size = best.FileID, best.FileSize
	case m.Document != nil && isImageMime(m.Document.MimeType):
		fileID, size = m.Document.FileID, m.Document.FileSize
	case m.Text == "/start":
		w.reply(ctx, userID, "Привет! Это бот приложения Habits 👋\n\nОткройте мини-приложение через кнопку меню. А если отправите сюда картинку — она появится в галерее фонов (Settings → Фон).")
		return
	default:
		if m.Text != "" {
			w.reply(ctx, userID, "Отправьте картинку — я добавлю её в фоны приложения 🖼")
		}
		return
	}

	exists, err := w.Store.UserExists(ctx, userID)
	if err != nil {
		w.Logger.Error("tgphotos: user check", "error", err)
		return
	}
	if !exists {
		w.reply(ctx, userID, "Сначала откройте приложение Habits хотя бы раз — потом пришлите картинку ещё раз 🙂")
		return
	}
	if size > maxFileBytes {
		w.reply(ctx, userID, "Картинка больше 5 МБ — пришлите поменьше 🙏")
		return
	}

	filename, err := w.download(ctx, fileID)
	if err != nil {
		w.Logger.Error("tgphotos: download", "user", userID, "error", err)
		w.reply(ctx, userID, "Не получилось сохранить картинку, попробуйте ещё раз")
		return
	}
	if _, err := w.Store.AddBackgroundImage(ctx, userID, filename); err != nil {
		os.Remove(filepath.Join(w.DataDir, "backgrounds", filename))
		w.Logger.Error("tgphotos: save", "user", userID, "error", err)
		return
	}
	w.Logger.Info("tgphotos: background saved", "user", userID, "file", filename)
	w.reply(ctx, userID, "Готово! Картинка добавлена в галерею фонов — выберите её в Settings → Фон 🖼")
}

func isImageMime(m string) bool {
	switch m {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
		return true
	}
	return false
}

// download скачивает файл через getFile и кладёт в галерею фонов.
func (w *Worker) download(ctx context.Context, fileID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/bot%s/getFile?file_id=%s", w.base(), w.Bot.Token, url.QueryEscape(fileID)), nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var meta struct {
		OK     bool `json:"ok"`
		Result struct {
			FilePath string `json:"file_path"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil || !meta.OK {
		return "", fmt.Errorf("getFile failed")
	}

	fileReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/file/bot%s/%s", w.base(), w.Bot.Token, meta.Result.FilePath), nil)
	if err != nil {
		return "", err
	}
	fileResp, err := http.DefaultClient.Do(fileReq)
	if err != nil {
		return "", err
	}
	defer fileResp.Body.Close()
	if fileResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("file download: %s", fileResp.Status)
	}

	body := io.LimitReader(fileResp.Body, maxFileBytes+1)
	head := make([]byte, 512)
	n, err := io.ReadFull(body, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", err
	}
	var ext string
	switch http.DetectContentType(head[:n]) {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	default:
		return "", fmt.Errorf("not an image")
	}

	buf := make([]byte, 16)
	rand.Read(buf)
	filename := hex.EncodeToString(buf) + ext
	dir := filepath.Join(w.DataDir, "backgrounds")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst, err := os.Create(filepath.Join(dir, filename))
	if err != nil {
		return "", err
	}
	defer dst.Close()
	if _, err := dst.Write(head[:n]); err != nil {
		os.Remove(dst.Name())
		return "", err
	}
	written, err := io.Copy(dst, body)
	if err != nil || written+int64(n) > maxFileBytes {
		os.Remove(dst.Name())
		return "", fmt.Errorf("file too large or copy failed")
	}
	return filename, nil
}

func (w *Worker) reply(ctx context.Context, chatID int64, text string) {
	if err := w.Bot.SendMessage(ctx, chatID, text); err != nil {
		w.Logger.Error("tgphotos: reply", "chat", chatID, "error", err)
	}
}
