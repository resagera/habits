package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

var shareTokenRe = regexp.MustCompile(`^[0-9a-f]{24}$`)

type checkerTemplatesHandlers struct {
	store *store.Store
	bot   *notify.Bot
}

type templateRequest struct {
	Name      string                 `json:"name"`
	Items     []store.ImportItem     `json:"items"`
	Subgroups []store.ImportSubgroup `json:"subgroups"`
}

func (req *templateRequest) validate() string {
	req.Name = strings.TrimSpace(req.Name)
	if n := utf8.RuneCountInString(req.Name); n < 1 || n > 200 {
		return "name must be 1-200 characters"
	}
	return normalizeTemplateTree(&req.Items, &req.Subgroups)
}

// normalizeTemplateTree тримит/валидирует дерево шаблона на месте (пункты ≤500,
// имена подгрупп 1–200, всего пунктов ≤2000, подгрупп ≤500, глубина ≤ MaxCheckerDepth).
func normalizeTemplateTree(items *[]store.ImportItem, subs *[]store.ImportSubgroup) string {
	total, subCount := 0, 0
	var cleanItems func(list *[]store.ImportItem) string
	cleanItems = func(list *[]store.ImportItem) string {
		out := (*list)[:0]
		for _, it := range *list {
			it.Name = strings.TrimSpace(it.Name)
			if it.Name == "" {
				continue
			}
			if utf8.RuneCountInString(it.Name) > 500 {
				return "item must be at most 500 characters"
			}
			total++
			out = append(out, it)
		}
		*list = out
		return ""
	}
	var cleanSubs func(list *[]store.ImportSubgroup, level int) string
	cleanSubs = func(list *[]store.ImportSubgroup, level int) string {
		if len(*list) == 0 {
			return ""
		}
		if level > store.MaxCheckerDepth {
			return fmt.Sprintf("maximum nesting depth is %d levels", store.MaxCheckerDepth)
		}
		out := (*list)[:0]
		for _, sub := range *list {
			sub.Name = strings.TrimSpace(sub.Name)
			if !validLen(sub.Name, 200) {
				return "subgroup name must be 1-200 characters"
			}
			subCount++
			if subCount > 500 {
				return "too many subgroups"
			}
			if msg := cleanItems(&sub.Items); msg != "" {
				return msg
			}
			if msg := cleanSubs(&sub.Subgroups, level+1); msg != "" {
				return msg
			}
			out = append(out, sub)
		}
		*list = out
		return ""
	}
	if msg := cleanItems(items); msg != "" {
		return msg
	}
	if msg := cleanSubs(subs, 2); msg != "" {
		return msg
	}
	if total > 2000 {
		return "too many items"
	}
	return ""
}

func (h *checkerTemplatesHandlers) list(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	templates, err := h.store.ListCheckTemplates(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if templates == nil {
		templates = []store.CheckTemplate{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": templates})
}

func (h *checkerTemplatesHandlers) save(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var id int64
	if raw := r.PathValue("id"); raw != "" {
		var err error
		if id, err = strconv.ParseInt(raw, 10, 64); err != nil {
			badRequest(w, "invalid template id")
			return
		}
	}
	var req templateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if msg := req.validate(); msg != "" {
		badRequest(w, msg)
		return
	}
	t, err := h.store.SaveCheckTemplate(r.Context(), user.ID, id, req.Name, req.Items, req.Subgroups)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "template not found")
	case err != nil:
		internalError(w)
	case id == 0:
		writeJSON(w, http.StatusCreated, map[string]any{"template": t})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"template": t})
	}
}

func (h *checkerTemplatesHandlers) delete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid template id")
		return
	}
	switch err := h.store.DeleteCheckTemplate(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "template not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// POST /checker/templates/{id}/start — развернуть шаблон в новую группу.
func (h *checkerTemplatesHandlers) start(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid template id")
		return
	}
	group, err := h.store.StartCheckTemplate(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "template not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"group": group})
	}
}

// POST /checker/templates/{id}/share-token — токен и ссылка-приглашение.
func (h *checkerTemplatesHandlers) shareToken(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid template id")
		return
	}
	token, err := h.store.EnsureShareToken(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "template not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	link := ""
	if username := h.bot.Username(r.Context()); username != "" {
		link = fmt.Sprintf("https://t.me/%s?startapp=chk_%s", username, token)
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token, "link": link})
}

// POST /checker/templates/redeem — принять шаблон по токену-приглашению.
func (h *checkerTemplatesHandlers) redeem(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Token = strings.TrimPrefix(strings.TrimSpace(req.Token), "chk_")
	if !shareTokenRe.MatchString(req.Token) {
		badRequest(w, "invalid token")
		return
	}
	t, err := h.store.RedeemShareToken(r.Context(), user.ID, req.Token)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "invite not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"template": t})
	}
}

// POST /checker/templates/{id}/send — отправить шаблон пользователю
// приложения (точное совпадение id или @username, чтобы не перебирать базу).
func (h *checkerTemplatesHandlers) send(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid template id")
		return
	}
	var req struct {
		To string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.To) == "" {
		badRequest(w, "to (user id or @username) is required")
		return
	}
	recipient, err := h.store.FindUserExact(r.Context(), strings.TrimSpace(req.To))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	if recipient.ID == user.ID {
		badRequest(w, "cannot send to yourself")
		return
	}
	queued, _, err := deliverShare(r.Context(), h.store, h.bot, user, recipient.ID, "checker_template", id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "template not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent_to": recipient, "queued": queued})
}
