package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
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

// TokenStore — поиск владельца по хэшу токена доступа (веб-версия,
// расширение браузера). Реализуется *store.Store без импорта пакета auth.
type TokenStore interface {
	AccessTokenOwner(ctx context.Context, hash string) (userID, tokenID int64, username, firstName string, err error)
	TouchAccessToken(ctx context.Context, tokenID int64, device string) error
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
	Tokens    TokenStore
	AdminIDs  map[int64]bool
	Logger    *slog.Logger

	seen      sync.Map // user id -> seenEntry
	tokenSeen sync.Map // token id -> time.Time последней записи last_used_at

	bruteOnce sync.Once
	brute     *bruteGuard
}

// guard — лимитер попыток входа по токену (создаётся при первом обращении).
func (m *Middleware) guard() *bruteGuard {
	m.bruteOnce.Do(func() { m.brute = newBruteGuard() })
	return m.brute
}

// IsAdmin — «этот человек администратор». Управляет видимостью personal-
// страниц (доступ к собственным данным) независимо от способа входа.
func (m *Middleware) IsAdmin(userID int64) bool {
	return m.AdminIDs[userID]
}

// IsAdminSession — «у этой сессии есть админ-полномочия»: только вход из
// Telegram. Токен-сессии (веб, расширение) админ-прав не получают, даже если
// владелец — админ. Использовать там, где решается доступ к чужим данным и
// управлению (/admin/*, tech_notes релизов, флаг is_admin в /me).
func (m *Middleware) IsAdminSession(ctx context.Context) bool {
	u := UserFromContext(ctx)
	return !u.TokenSession && m.AdminIDs[u.ID]
}

// InvalidateUser сбрасывает кэш (например, после бана — чтобы 403 наступил сразу).
func (m *Middleware) InvalidateUser(userID int64) {
	m.seen.Delete(userID)
}

func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := m.authenticate(r)
		if !ok {
			// перебор токенов: IP временно заблокирован
			if m.guard().Blocked(clientIP(r), time.Now()) {
				writeAuthError(w, http.StatusTooManyRequests, "too_many_attempts",
					"слишком много неудачных попыток входа — попробуйте позже")
				return
			}
			writeAuthError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid Telegram init data")
			return
		}

		// Границы токен-сессии: ни админ-API, ни выпуск новых токенов —
		// утёкший токен не даёт ни эскалации прав, ни самопродления.
		if user.TokenSession && isTokenForbiddenPath(r.URL.Path) {
			writeAuthError(w, http.StatusForbidden, "forbidden",
				"недоступно для токена доступа — откройте приложение в Telegram")
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

// isTokenForbiddenPath — пути, закрытые для входа по токену доступа.
func isTokenForbiddenPath(path string) bool {
	return strings.HasPrefix(path, "/api/v1/admin/") ||
		strings.HasPrefix(path, "/api/v1/settings/tokens")
}

func writeAuthError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(`{"error":{"code":"` + code + `","message":"` + message + `"}}`))
}

func (m *Middleware) authenticate(r *http.Request) (TgUser, bool) {
	header := r.Header.Get("Authorization")
	// Токен доступа: веб-версия и расширение браузера (вне Telegram).
	// Проверяется и в dev-режиме — чтобы механику можно было тестировать.
	if token, found := strings.CutPrefix(header, "Bearer "); found && token != "" {
		return m.authenticateToken(r, token)
	}
	if m.DevBypass {
		return TgUser{ID: m.DevUserID, Username: "dev", FirstName: "Dev"}, true
	}
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

// tokenTouchInterval — как часто обновляем last_used_at токена.
const tokenTouchInterval = 5 * time.Minute

// authenticateToken проверяет токен доступа по sha256-хэшу. Данные
// пользователя берём из БД: иначе TouchUser затёр бы username/first_name.
func (m *Middleware) authenticateToken(r *http.Request, token string) (TgUser, bool) {
	if m.Tokens == nil || len(token) > 200 {
		return TgUser{}, false
	}
	ip, now := clientIP(r), time.Now()
	// заблокированный IP до БД не доходит
	if m.guard().Blocked(ip, now) {
		return TgUser{}, false
	}
	sum := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(sum[:])
	userID, tokenID, username, firstName, err := m.Tokens.AccessTokenOwner(r.Context(), hash)
	if err != nil || userID == 0 {
		if m.guard().Fail(ip, now) && m.Logger != nil {
			m.Logger.Warn("auth: too many token attempts, ip blocked", "ip", ip)
		}
		return TgUser{}, false
	}
	m.guard().Success(ip)
	// last_used_at пишем не чаще раза в 5 минут на токен
	if v, ok := m.tokenSeen.Load(tokenID); !ok || time.Since(v.(time.Time)) > tokenTouchInterval {
		if err := m.Tokens.TouchAccessToken(r.Context(), tokenID, shortDevice(r.UserAgent())); err == nil {
			m.tokenSeen.Store(tokenID, time.Now())
		}
	}
	return TgUser{
		ID: userID, Username: username, FirstName: firstName,
		TokenSession: true, TokenID: tokenID,
	}, true
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
