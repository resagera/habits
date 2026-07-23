// Package notify — отправка сообщений пользователям через Telegram Bot API.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const sendTimeout = 10 * time.Second

type Bot struct {
	Token  string
	Logger *slog.Logger
	// APIBase позволяет подменить Telegram API в тестах; пусто — прод.
	APIBase string

	mu       sync.Mutex
	username string
}

// Username возвращает @username бота (getMe, кэшируется) — нужен для
// ссылок-приглашений t.me/<bot>?startapp=…. Пусто при ошибке или dev.
func (b *Bot) Username(ctx context.Context) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.username != "" || b.Token == "" {
		return b.username
	}
	base := b.APIBase
	if base == "" {
		base = "https://api.telegram.org"
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/bot%s/getMe", base, b.Token), nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.Logger.Error("notify: getMe", "error", err)
		return ""
	}
	defer resp.Body.Close()
	var out struct {
		Result struct {
			Username string `json:"username"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ""
	}
	b.username = out.Result.Username
	return b.username
}

// SendMessage шлёт текст в личку (chat_id = telegram user id).
// При пустом токене (dev) — только логирует.
func (b *Bot) SendMessage(ctx context.Context, chatID int64, text string) error {
	if b.Token == "" {
		b.Logger.Info("notify: dev send", "chat_id", chatID, "text", text)
		return nil
	}
	base := b.APIBase
	if base == "" {
		base = "https://api.telegram.org"
	}
	body, _ := json.Marshal(map[string]any{"chat_id": chatID, "text": text})
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/bot%s/sendMessage", base, b.Token), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("telegram: %s: %s", resp.Status, msg)
	}
	return nil
}

// ChatInfo — что удалось узнать о пользователе через getChat.
type ChatInfo struct {
	Username  string
	FirstName string
	LastName  string
}

// GetChat запрашивает данные пользователя по числовому id (Bot API getChat).
// Работает только если бот уже видел пользователя; @логины частных аккаунтов
// Bot API не резолвит. ok=false — ничего не узнали (или dev без токена).
func (b *Bot) GetChat(ctx context.Context, chatID int64) (ChatInfo, bool) {
	if b.Token == "" {
		return ChatInfo{}, false
	}
	base := b.APIBase
	if base == "" {
		base = "https://api.telegram.org"
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/bot%s/getChat?chat_id=%d", base, b.Token, chatID), nil)
	if err != nil {
		return ChatInfo{}, false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ChatInfo{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ChatInfo{}, false
	}
	var out struct {
		Result struct {
			Username  string `json:"username"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ChatInfo{}, false
	}
	return ChatInfo(out.Result), true
}

// Broadcast шлёт сообщение нескольким получателям, ошибки только логирует.
func (b *Bot) Broadcast(ctx context.Context, chatIDs []int64, text string) {
	for _, id := range chatIDs {
		if err := b.SendMessage(ctx, id, text); err != nil {
			b.Logger.Error("notify: broadcast", "chat_id", id, "error", err)
		}
	}
}
