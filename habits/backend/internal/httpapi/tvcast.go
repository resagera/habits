package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/store"
)

// «Отправить на ТВ»: фото или ролик из галереи телефона — сразу на экран
// приставки, без библиотеки.
//
// Телефон может быть и не в домашней сети, а страница пульта в Telegram
// открыта по https — до агента в локальной сети ей не достучаться. Поэтому
// файл на пару часов ложится на прод под случайным именем, приставке по шине
// уходит {t:"cast", token}, и она забирает файл сама (с Range — ролик можно
// мотать). Через tvCastTTL файл стирается.

const (
	tvCastMax = 500 << 20
	tvCastTTL = 3 * time.Hour
)

type tvCast struct {
	dir  string
	once sync.Once
}

func (c *tvCast) path(token string) string { return filepath.Join(c.dir, token) }

// sweep стирает просроченное; запускается один раз, при первой отправке.
func (c *tvCast) startSweeper() {
	c.once.Do(func() {
		go func() {
			for {
				entries, _ := os.ReadDir(c.dir)
				for _, e := range entries {
					if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > tvCastTTL {
						_ = os.Remove(filepath.Join(c.dir, e.Name()))
					}
				}
				time.Sleep(10 * time.Minute)
			}
		}()
	})
}

func castKind(contentType string) string {
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return "image"
	case strings.HasPrefix(contentType, "video/"):
		return "video"
	}
	return ""
}

// POST /api/v1/tv/cast?room=&name= — тело — сам файл (Content-Type image/* или video/*).
func (h *tvHandlers) castUpload(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	key := store.NormalizeTVRoom(r.URL.Query().Get("room"))
	kind := castKind(r.Header.Get("Content-Type"))
	if key == "" || kind == "" {
		badRequest(w, "нужны room и фото или видео")
		return
	}
	owner, err := h.store.TVRoomOwner(r.Context(), key)
	if errors.Is(err, store.ErrNotFound) || (err == nil && owner != user.ID) {
		writeError(w, http.StatusNotFound, "not_found", "приставка не найдена")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	if tv, _ := h.hub.Present(key); tv == 0 {
		writeError(w, http.StatusConflict, "offline", "приставка не на связи — откройте плеер на телевизоре")
		return
	}
	if err := os.MkdirAll(h.cast.dir, 0o700); err != nil {
		internalError(w)
		return
	}
	h.cast.startSweeper()
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	token := hex.EncodeToString(b)
	f, err := os.Create(h.cast.path(token))
	if err != nil {
		internalError(w)
		return
	}
	n, err := io.Copy(f, http.MaxBytesReader(w, r.Body, tvCastMax))
	f.Close()
	if err != nil {
		_ = os.Remove(h.cast.path(token))
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "файл больше 500 МБ или загрузка оборвалась")
		return
	}
	// тип нужен при отдаче: сохраняем рядом
	_ = os.WriteFile(h.cast.path(token)+".type", []byte(r.Header.Get("Content-Type")), 0o600)
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if len([]rune(name)) > 80 {
		name = string([]rune(name)[:80])
	}
	msg, _ := json.Marshal(map[string]any{"t": "cast", "token": token, "kind": kind, "name": name})
	h.hub.BroadcastAll(key, msg)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "size": n, "kind": kind})
}

// GET /api/v1/tv/cast/{token} — сам файл, вне авторизации: ссылка случайная и
// живёт tvCastTTL. Range — через http.ServeContent.
func (h *tvHandlers) castFile(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if len(token) != 40 {
		http.NotFound(w, r)
		return
	}
	if _, err := hex.DecodeString(token); err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(h.cast.path(token))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || time.Since(st.ModTime()) > tvCastTTL {
		http.NotFound(w, r)
		return
	}
	if ct, err := os.ReadFile(h.cast.path(token) + ".type"); err == nil {
		w.Header().Set("Content-Type", string(ct))
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	// приставка открыта с http://192.168.x.x — чужой origin
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// POST /api/v1/tv/cast/stop?room= — убрать показ с экрана.
func (h *tvHandlers) castStop(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	key := store.NormalizeTVRoom(r.URL.Query().Get("room"))
	if owner, err := h.store.TVRoomOwner(r.Context(), key); err != nil || owner != user.ID {
		writeError(w, http.StatusNotFound, "not_found", "приставка не найдена")
		return
	}
	msg, _ := json.Marshal(map[string]string{"t": "cast-stop"})
	h.hub.BroadcastAll(key, msg)
	w.WriteHeader(http.StatusNoContent)
}
