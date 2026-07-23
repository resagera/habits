package auth

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ctxKey struct{}

// UserFromContext возвращает аутентифицированного Telegram-пользователя.
func UserFromContext(ctx context.Context) TgUser {
	u, _ := ctx.Value(ctxKey{}).(TgUser)
	return u
}

type UserStore interface {
	// TouchUser обновляет last_seen и связку IP+устройство, возвращает бан-статус.
	TouchUser(ctx context.Context, id int64, username, firstName, ip, device string) (banned bool, err error)
}

// touchInterval — как часто пишем last_seen/устройство в БД (на пользователя).
const touchInterval = 5 * time.Minute

type seenEntry struct {
	lastWrite time.Time
	banned    bool
}

type Middleware struct {
	BotToken  string
	DevBypass bool
	DevUserID int64
	MaxAge    time.Duration
	Users     UserStore
	AdminIDs  map[int64]bool

	seen sync.Map // user id -> seenEntry
}

func (m *Middleware) IsAdmin(userID int64) bool {
	return m.AdminIDs[userID]
}

// InvalidateUser сбрасывает кэш (например, после бана — чтобы 403 наступил сразу).
func (m *Middleware) InvalidateUser(userID int64) {
	m.seen.Delete(userID)
}

func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := m.authenticate(r)
		if !ok {
			writeAuthError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid Telegram init data")
			return
		}

		var entry seenEntry
		if v, cached := m.seen.Load(user.ID); cached {
			entry = v.(seenEntry)
		}
		if entry.lastWrite.IsZero() || time.Since(entry.lastWrite) > touchInterval {
			banned, err := m.Users.TouchUser(r.Context(), user.ID, user.Username, user.FirstName,
				clientIP(r), shortDevice(r.UserAgent()))
			if err != nil {
				writeAuthError(w, http.StatusInternalServerError, "internal", "failed to register user")
				return
			}
			entry = seenEntry{lastWrite: time.Now(), banned: banned}
			m.seen.Store(user.ID, entry)
		}
		if entry.banned {
			writeAuthError(w, http.StatusForbidden, "banned", "account is banned")
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, user)))
	})
}

func writeAuthError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(`{"error":{"code":"` + code + `","message":"` + message + `"}}`))
}

func (m *Middleware) authenticate(r *http.Request) (TgUser, bool) {
	if m.DevBypass {
		return TgUser{ID: m.DevUserID, Username: "dev", FirstName: "Dev"}, true
	}
	header := r.Header.Get("Authorization")
	initData, found := strings.CutPrefix(header, "tma ")
	if !found || initData == "" {
		return TgUser{}, false
	}
	maxAge := m.MaxAge
	if maxAge == 0 {
		maxAge = 24 * time.Hour
	}
	user, err := ValidateInitData(initData, m.BotToken, maxAge, time.Now())
	if err != nil {
		return TgUser{}, false
	}
	return user, true
}

// clientIP берёт адрес из заголовков прокси (Caddy проставляет X-Forwarded-For).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, found := strings.Cut(xff, ","); found || first != "" {
			return strings.TrimSpace(first)
		}
	}
	if realIP := r.Header.Get("X-Real-Ip"); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// shortDevice сводит User-Agent к короткой стабильной форме
// («iPhone · Telegram»), чтобы связки IP+устройство не плодились
// на каждое обновление версии браузера.
func shortDevice(ua string) string {
	if ua == "" {
		return "неизвестно"
	}
	var os string
	switch {
	case strings.Contains(ua, "iPhone"):
		os = "iPhone"
	case strings.Contains(ua, "iPad"):
		os = "iPad"
	case strings.Contains(ua, "Android"):
		os = "Android"
	case strings.Contains(ua, "Windows"):
		os = "Windows"
	case strings.Contains(ua, "Macintosh") || strings.Contains(ua, "Mac OS X"):
		os = "macOS"
	case strings.Contains(ua, "Linux"):
		os = "Linux"
	default:
		os = "другое"
	}
	var app string
	switch {
	case strings.Contains(ua, "Telegram"):
		app = "Telegram"
	case strings.Contains(ua, "Firefox/"):
		app = "Firefox"
	case strings.Contains(ua, "Edg/"):
		app = "Edge"
	case strings.Contains(ua, "OPR/"):
		app = "Opera"
	case strings.Contains(ua, "Chrome/"):
		app = "Chrome"
	case strings.Contains(ua, "Safari/"):
		app = "Safari"
	default:
		app = "браузер"
	}
	return os + " · " + app
}
