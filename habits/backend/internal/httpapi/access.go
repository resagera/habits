package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/store"
)

type accessHandlers struct {
	store  *store.Store
	authMW *auth.Middleware
}

// guardedPages: первый сегмент /api/v1/<seg> → код страницы из реестра.
var guardedPages = map[string]string{
	"tracker":   "tracker",
	"checker":   "checker",
	"tasks":     "tasks",
	"diary":     "diary",
	"metrics":   "metrics",
	"reminders": "reminders",
	"converter": "converter",
	"links":     "links",
	"articles":  "articles",
	"servers":   "servers",
	"passwords": "passwords",
	"files":     "files",
	"terminal":  "terminal",
	"contacts":  "contacts",
	"projects":  "projects",
	"food":      "food",
	"automation": "automation",
}

// pageGuard закрывает API страниц с персональным доступом. Админ проходит везде.
func (h *accessHandlers) pageGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seg := strings.TrimPrefix(r.URL.Path, "/api/v1/")
		if i := strings.IndexByte(seg, '/'); i >= 0 {
			seg = seg[:i]
		}
		page, guarded := guardedPages[seg]
		if !guarded {
			next.ServeHTTP(w, r)
			return
		}
		user := auth.UserFromContext(r.Context())
		if h.authMW.IsAdmin(user.ID) {
			next.ServeHTTP(w, r)
			return
		}
		allowed, err := h.store.PageAllowed(r.Context(), user.ID, page)
		if err != nil {
			internalError(w)
			return
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "forbidden", "page access denied")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// visiblePages — карта видимых пользователю страниц (для /me/pages и поиска).
func (h *accessHandlers) visiblePages(r *http.Request) (map[string]bool, map[string]bool, error) {
	user := auth.UserFromContext(r.Context())
	vis, err := h.store.PageVisibilities(r.Context())
	if err != nil {
		return nil, nil, err
	}
	admin := h.authMW.IsAdmin(user.ID)
	grantedPages, grantedFeatures := map[string]bool{}, map[string]bool{}
	if !admin {
		grantedPages, grantedFeatures, err = h.store.UserPageSet(r.Context(), user.ID)
		if err != nil {
			return nil, nil, err
		}
	}
	pages := make(map[string]bool, len(store.PagesRegistry))
	for _, p := range store.PagesRegistry {
		pages[p.Code] = admin || vis[p.Code] == "all" || grantedPages[p.Code]
	}
	features := make(map[string]bool, len(store.FeaturesRegistry))
	for _, f := range store.FeaturesRegistry {
		features[f.Code] = admin || grantedFeatures[f.Code]
	}
	return pages, features, nil
}

// GET /me/pages — какие страницы и опции доступны текущему пользователю.
func (h *accessHandlers) mePages(w http.ResponseWriter, r *http.Request) {
	pages, features, err := h.visiblePages(r)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": pages, "features": features})
}

// GET /search?q= — глобальный поиск по данным своих страниц.
func (h *accessHandlers) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) < 2 {
		badRequest(w, "q must be at least 2 characters")
		return
	}
	if len(q) > 100 {
		badRequest(w, "q is too long")
		return
	}
	pages, _, err := h.visiblePages(r)
	if err != nil {
		internalError(w)
		return
	}
	user := auth.UserFromContext(r.Context())
	hits, err := h.store.GlobalSearch(r.Context(), user.ID, q, pages)
	if err != nil {
		internalError(w)
		return
	}
	if hits == nil {
		hits = []store.SearchHit{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
}

// GET /share/recipients — с кем пользователь уже делился (для подсказок).
func (h *accessHandlers) recentRecipients(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	users, err := h.store.RecentRecipients(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if users == nil {
		users = []store.AccessUser{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// --- админка доступов ---

type adminPage struct {
	store.PageInfo
	Visibility string             `json:"visibility"`
	Users      []store.AccessUser `json:"users"`
	Features   []adminFeature     `json:"features"`
}

type adminFeature struct {
	store.FeatureInfo
	Users []store.AccessUser `json:"users"`
}

// GET /admin/pages — полный список страниц с видимостью, доступами и опциями.
func (h *accessHandlers) adminPages(w http.ResponseWriter, r *http.Request) {
	vis, err := h.store.PageVisibilities(r.Context())
	if err != nil {
		internalError(w)
		return
	}
	result := make([]adminPage, 0, len(store.PagesRegistry))
	for _, p := range store.PagesRegistry {
		page := adminPage{PageInfo: p, Visibility: vis[p.Code], Users: []store.AccessUser{}, Features: []adminFeature{}}
		if page.Visibility == "personal" {
			users, err := h.store.ListGrants(r.Context(), "page_access", p.Code)
			if err != nil {
				internalError(w)
				return
			}
			if users != nil {
				page.Users = users
			}
		}
		for _, f := range store.FeaturesRegistry {
			if f.Page != p.Code {
				continue
			}
			users, err := h.store.ListGrants(r.Context(), "feature_access", f.Code)
			if err != nil {
				internalError(w)
				return
			}
			feature := adminFeature{FeatureInfo: f, Users: []store.AccessUser{}}
			if users != nil {
				feature.Users = users
			}
			page.Features = append(page.Features, feature)
		}
		result = append(result, page)
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": result})
}

// PUT /admin/pages/{page} — смена видимости.
func (h *accessHandlers) setVisibility(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Visibility string `json:"visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		(req.Visibility != "all" && req.Visibility != "personal") {
		badRequest(w, "visibility must be 'all' or 'personal'")
		return
	}
	switch err := h.store.SetPageVisibility(r.Context(), r.PathValue("page"), req.Visibility); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "unknown page")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]string{"page": r.PathValue("page"), "visibility": req.Visibility})
	}
}

func validFeature(code string) bool {
	for _, f := range store.FeaturesRegistry {
		if f.Code == code {
			return true
		}
	}
	return false
}

// POST /admin/pages/{page}/access и /admin/features/{feature}/access.
func (h *accessHandlers) addGrant(table, key string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		k := r.PathValue(key)
		if key == "page" && !validPageCode(k) || key == "feature" && !validFeature(k) {
			writeError(w, http.StatusNotFound, "not_found", "unknown "+key)
			return
		}
		var req struct {
			UserID int64 `json:"user_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == 0 {
			badRequest(w, "user_id is required")
			return
		}
		switch err := h.store.AddGrant(r.Context(), table, k, req.UserID); {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "user not found")
		case err != nil:
			internalError(w)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

func (h *accessHandlers) removeGrant(table, key string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
		if err != nil {
			badRequest(w, "invalid user id")
			return
		}
		switch err := h.store.RemoveGrant(r.Context(), table, r.PathValue(key), userID); {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "grant not found")
		case err != nil:
			internalError(w)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// GET /admin/users/search?q= — поиск по id/логину/имени.
func (h *accessHandlers) searchUsers(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		badRequest(w, "q is required")
		return
	}
	users, err := h.store.SearchUsers(r.Context(), strings.TrimPrefix(q, "@"), 10)
	if err != nil {
		internalError(w)
		return
	}
	if users == nil {
		users = []store.AccessUser{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func validPageCode(code string) bool {
	for _, p := range store.PagesRegistry {
		if p.Code == code {
			return true
		}
	}
	return false
}
