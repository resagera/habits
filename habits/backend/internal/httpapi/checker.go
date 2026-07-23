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

type checkerHandlers struct {
	store *store.Store
	bot   *notify.Bot
}

var groupShareTokenRe = regexp.MustCompile(`^[0-9a-f]{24}$`)

func (h *checkerHandlers) listGroups(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	groups, err := h.store.ListCheckGroups(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if groups == nil {
		groups = []store.CheckGroup{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func (h *checkerHandlers) createGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Name     string `json:"name"`
		ParentID *int64 `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if !validLen(req.Name, 200) {
		badRequest(w, "name must be 1-200 characters")
		return
	}
	group, err := h.store.CreateCheckGroup(r.Context(), user.ID, req.Name, req.ParentID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "parent group not found")
	case errors.Is(err, store.ErrTooDeep):
		writeError(w, http.StatusConflict, "too_deep",
			fmt.Sprintf("maximum nesting depth is %d levels", store.MaxCheckerDepth))
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"group": group})
	}
}

// POST /checker/groups/import — создать группу из импортированного дерева.
func (h *checkerHandlers) importGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var in store.ImportGroup
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if !validLen(in.Name, 200) {
		badRequest(w, "group name must be 1-200 characters")
		return
	}
	// нормализация и лимиты (подгруппы любой глубины)
	total, subCount := 0, 0
	cleanItems := func(items []store.ImportItem) ([]store.ImportItem, bool) {
		out := items[:0]
		for _, it := range items {
			it.Name = strings.TrimSpace(it.Name)
			if it.Name == "" {
				continue
			}
			if utf8.RuneCountInString(it.Name) > 500 {
				return nil, false
			}
			total++
			out = append(out, it)
		}
		return out, true
	}
	// рекурсивная очистка подгрупп; errMsg != "" — прервать с этой ошибкой.
	// level — уровень подгруппы (группа верхнего уровня = 1, её подгруппы = 2…).
	var cleanSubs func(subs []store.ImportSubgroup, level int) ([]store.ImportSubgroup, string)
	cleanSubs = func(subs []store.ImportSubgroup, level int) ([]store.ImportSubgroup, string) {
		if len(subs) == 0 {
			return subs, ""
		}
		if level > store.MaxCheckerDepth {
			return nil, fmt.Sprintf("maximum nesting depth is %d levels", store.MaxCheckerDepth)
		}
		out := subs[:0]
		for _, sub := range subs {
			sub.Name = strings.TrimSpace(sub.Name)
			if !validLen(sub.Name, 200) {
				return nil, "subgroup name must be 1-200 characters"
			}
			subCount++
			if subCount > 500 {
				return nil, "too many subgroups"
			}
			var ok bool
			if sub.Items, ok = cleanItems(sub.Items); !ok {
				return nil, "item must be at most 500 characters"
			}
			var msg string
			if sub.Subgroups, msg = cleanSubs(sub.Subgroups, level+1); msg != "" {
				return nil, msg
			}
			out = append(out, sub)
		}
		return out, ""
	}
	ok := false
	if in.Items, ok = cleanItems(in.Items); !ok {
		badRequest(w, "item must be at most 500 characters")
		return
	}
	var msg string
	// корневая группа — уровень 1, её подгруппы начинаются со 2-го
	if in.Subgroups, msg = cleanSubs(in.Subgroups, 2); msg != "" {
		badRequest(w, msg)
		return
	}
	if total > 2000 {
		badRequest(w, "too many items")
		return
	}
	group, err := h.store.ImportCheckGroup(r.Context(), user.ID, in)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"group": group})
}

// POST /checker/groups/{id}/share-token — токен и ссылка-приглашение на группу.
func (h *checkerHandlers) shareGroupToken(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
		return
	}
	token, err := h.store.EnsureGroupShareToken(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "group not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	link := ""
	if username := h.bot.Username(r.Context()); username != "" {
		link = fmt.Sprintf("https://t.me/%s?startapp=chg_%s", username, token)
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token, "link": link})
}

// POST /checker/groups/{id}/send — отправить копию группы пользователю приложения.
func (h *checkerHandlers) sendGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
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
	queued, _, err := deliverShare(r.Context(), h.store, h.bot, user, recipient.ID, "checker_group", id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "group not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent_to": recipient, "queued": queued})
}

// POST /checker/groups/redeem — принять группу по токену-приглашению.
func (h *checkerHandlers) redeemGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Token = strings.TrimPrefix(strings.TrimSpace(req.Token), "chg_")
	if !groupShareTokenRe.MatchString(req.Token) {
		badRequest(w, "invalid token")
		return
	}
	group, err := h.store.RedeemGroupShareToken(r.Context(), user.ID, req.Token)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "invite not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"group": group})
	}
}

func (h *checkerHandlers) renameGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
		return
	}
	var req struct {
		Name     *string `json:"name"`
		HideDone *bool   `json:"hide_done"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Name == nil && req.HideDone == nil {
		badRequest(w, "nothing to update")
		return
	}
	if req.Name != nil && !validLen(*req.Name, 200) {
		badRequest(w, "name must be 1-200 characters")
		return
	}
	group, err := h.store.UpdateCheckGroup(r.Context(), user.ID, id, req.Name, req.HideDone)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "group not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"group": group})
	}
}

func (h *checkerHandlers) deleteGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
		return
	}
	switch err := h.store.DeleteCheckGroup(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "group not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *checkerHandlers) createItem(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	groupID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if !validLen(req.Name, 500) {
		badRequest(w, "name must be 1-500 characters")
		return
	}
	item, err := h.store.CreateCheckItem(r.Context(), user.ID, groupID, req.Name)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "group not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"item": item})
	}
}

func (h *checkerHandlers) updateItem(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid item id")
		return
	}
	var req struct {
		Name     *string `json:"name"`
		Done     *bool   `json:"done"`
		Position *int32  `json:"position"`
		GroupID  *int64  `json:"group_id"` // перенос пункта в другую группу
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Name == nil && req.Done == nil && req.Position == nil && req.GroupID == nil {
		badRequest(w, "nothing to update")
		return
	}
	if req.Name != nil && !validLen(*req.Name, 500) {
		badRequest(w, "name must be 1-500 characters")
		return
	}
	var item store.CheckItem
	var err2 error
	// сначала перенос в другую группу (если задан), затем правка полей
	if req.GroupID != nil {
		item, err2 = h.store.MoveCheckItem(r.Context(), user.ID, id, *req.GroupID)
		if err2 != nil {
			itemUpdateError(w, err2)
			return
		}
	}
	if req.Name != nil || req.Done != nil || req.Position != nil {
		item, err2 = h.store.UpdateCheckItem(r.Context(), user.ID, id, store.CheckItemPatch{
			Name:     req.Name,
			Done:     req.Done,
			Position: req.Position,
		})
		if err2 != nil {
			itemUpdateError(w, err2)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": item})
}

func itemUpdateError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "item or group not found")
		return
	}
	internalError(w)
}

func (h *checkerHandlers) deleteItem(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid item id")
		return
	}
	switch err := h.store.DeleteCheckItem(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "item not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func validLen(s string, max int) bool {
	n := utf8.RuneCountInString(s)
	return n >= 1 && n <= max
}
