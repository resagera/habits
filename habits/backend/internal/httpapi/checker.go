package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
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
	// ленивая очистка корзины по сроку хранения (на каждом входе в Checker)
	if days, err := h.store.GetCheckerTrashDays(r.Context(), user.ID); err == nil && days > 0 {
		_ = h.store.PurgeExpiredCheckerTrash(r.Context(), user.ID, time.Now().AddDate(0, 0, -days))
	}
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

// POST /checker/groups/{id}/share-access {to} — открыть совместный доступ к списку.
func (h *checkerHandlers) shareAccess(w http.ResponseWriter, r *http.Request) {
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
		badRequest(w, "cannot share with yourself")
		return
	}
	queued, _, err := deliverShare(r.Context(), h.store, h.bot, user, recipient.ID, "checker_share", id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "group not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shared_with": recipient, "queued": queued})
}

// GET /checker/groups/{id}/history — история изменений списка (владелец/участник).
func (h *checkerHandlers) history(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
		return
	}
	entries, err := h.store.ListCheckerHistory(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "group not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"history": entries})
	}
}

// POST /checker/items/{id}/reminder {remind_at} — дедлайн/напоминание у пункта.
func (h *checkerHandlers) setItemReminder(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid item id")
		return
	}
	var req struct {
		RemindAt *time.Time `json:"remind_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	switch err := h.store.SetItemReminder(r.Context(), user.ID, id, req.RemindAt); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "item not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"remind_at": req.RemindAt})
	}
}

// POST /checker/groups/{id}/reminder {remind_at} — напоминание о списке (владелец).
func (h *checkerHandlers) setGroupReminder(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
		return
	}
	var req struct {
		RemindAt *time.Time `json:"remind_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	switch err := h.store.SetGroupReminder(r.Context(), user.ID, id, req.RemindAt); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "group not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"remind_at": req.RemindAt})
	}
}

// PATCH /checker/groups/{id}/recurring — расписание сброса (владелец).
func (h *checkerHandlers) setRecurring(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
		return
	}
	var req struct {
		Period string `json:"period"`
		Minute int    `json:"minute"`
		Dow    int    `json:"dow"`
		Dom    int    `json:"dom"`
		TzOff  int    `json:"tz_off"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	switch req.Period {
	case "none", "daily", "weekly", "monthly":
	default:
		badRequest(w, "period must be none/daily/weekly/monthly")
		return
	}
	if req.Minute < 0 || req.Minute > 1439 || req.Dow < 0 || req.Dow > 6 ||
		req.Dom < 1 || req.Dom > 31 || req.TzOff < -840 || req.TzOff > 840 {
		badRequest(w, "invalid schedule values")
		return
	}
	group, err := h.store.SetCheckerRecurrence(r.Context(), user.ID, id, req.Period, req.Minute, req.Dow, req.Dom, req.TzOff)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "group not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"group": group})
	}
}

// POST /checker/groups/{id}/reset — ручной сброс списка (владелец/участник).
func (h *checkerHandlers) resetNow(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
		return
	}
	switch err := h.store.ManualResetChecker(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "group not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// GET /checker/groups/{id}/snapshots — дни со снимками (календарь).
func (h *checkerHandlers) listSnapshots(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
		return
	}
	days, err := h.store.ListCheckerSnapshots(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "group not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"days": days})
	}
}

// GET /checker/groups/{id}/snapshots/{day} — снимок конкретного дня.
func (h *checkerHandlers) getSnapshot(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
		return
	}
	data, err := h.store.GetCheckerSnapshot(r.Context(), user.ID, id, r.PathValue("day"))
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "snapshot not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"data": data})
	}
}

// GET /checker/groups/{id}/shares — участники совместного доступа (владелец).
func (h *checkerHandlers) listShares(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
		return
	}
	users, err := h.store.ListCheckerShares(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "group not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	if users == nil {
		users = []store.AccessUser{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// DELETE /checker/groups/{id}/shares/{userID} — владелец отзывает доступ.
func (h *checkerHandlers) revokeShare(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
		return
	}
	target, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
	if err != nil {
		badRequest(w, "invalid user id")
		return
	}
	switch err := h.store.RevokeCheckerShare(r.Context(), user.ID, id, target); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "share not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// DELETE /checker/shared/{id} — участник убирает у себя доступ к списку.
func (h *checkerHandlers) leaveShared(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
		return
	}
	switch err := h.store.RevokeCheckerShare(r.Context(), user.ID, id, user.ID); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "share not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
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

// updateGroup меняет имя, «скрывать выполненное» и/или промежуточный статус.
func (h *checkerHandlers) updateGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
		return
	}
	var req struct {
		Name         *string `json:"name"`
		HideDone     *bool   `json:"hide_done"`
		ProgressMode *bool   `json:"progress_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Name == nil && req.HideDone == nil && req.ProgressMode == nil {
		badRequest(w, "nothing to update")
		return
	}
	if req.Name != nil && !validLen(*req.Name, 200) {
		badRequest(w, "name must be 1-200 characters")
		return
	}
	group, err := h.store.UpdateCheckGroup(r.Context(), user.ID, id, req.Name, req.HideDone, req.ProgressMode)
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
	name, err := h.store.SoftDeleteCheckGroup(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "group not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"name": name})
	}
}

// GET /checker/trash — содержимое корзины + срок хранения.
func (h *checkerHandlers) listTrash(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	days, err := h.store.GetCheckerTrashDays(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if days > 0 {
		_ = h.store.PurgeExpiredCheckerTrash(r.Context(), user.ID, time.Now().AddDate(0, 0, -days))
	}
	trashed, err := h.store.ListCheckerTrash(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"trashed": trashed, "retention_days": days})
}

// POST /checker/groups/{id}/restore — восстановить группу из корзины.
func (h *checkerHandlers) restoreGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
		return
	}
	switch err := h.store.RestoreCheckGroup(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "group not in trash")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// DELETE /checker/trash/{id} — удалить группу из корзины навсегда.
func (h *checkerHandlers) purgeGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
		return
	}
	switch err := h.store.PurgeCheckGroup(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "group not in trash")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// DELETE /checker/trash — очистить корзину.
func (h *checkerHandlers) emptyTrash(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if err := h.store.EmptyCheckerTrash(r.Context(), user.ID); err != nil {
		internalError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PUT /checker/trash-days — срок хранения корзины (1..365 дней).
func (h *checkerHandlers) setTrashDays(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Days int `json:"days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Days < 1 || req.Days > 365 {
		badRequest(w, "days must be 1..365")
		return
	}
	if err := h.store.SetCheckerTrashDays(r.Context(), user.ID, req.Days); err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"retention_days": req.Days})
}

// moveGroup меняет родителя группы (parent_id: id или null — в верхний уровень).
func (h *checkerHandlers) moveGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
		return
	}
	var req struct {
		ParentID *int64 `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	group, err := h.store.MoveCheckGroup(r.Context(), user.ID, id, req.ParentID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "group or parent not found")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "cannot move a group into its own subtree")
	case errors.Is(err, store.ErrTooDeep):
		writeError(w, http.StatusConflict, "too_deep",
			fmt.Sprintf("maximum nesting depth is %d levels", store.MaxCheckerDepth))
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"group": group})
	}
}

// reorderGroups задаёт порядок групп-соседей одного родителя.
func (h *checkerHandlers) reorderGroups(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		ParentID   *int64  `json:"parent_id"`
		OrderedIDs []int64 `json:"ordered_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if len(req.OrderedIDs) == 0 {
		badRequest(w, "ordered_ids is required")
		return
	}
	switch err := h.store.ReorderCheckGroups(r.Context(), user.ID, req.ParentID, req.OrderedIDs); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "group not found or wrong parent")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// duplicateGroup копирует группу со всем поддеревом рядом с оригиналом.
func (h *checkerHandlers) duplicateGroup(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
		return
	}
	group, err := h.store.DuplicateCheckGroup(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "group not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"group": group})
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

// bulkItems — массовые действия над прямыми пунктами группы:
// check_all / uncheck_all / delete_done. Возвращает обновлённые пункты.
func (h *checkerHandlers) bulkItems(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	groupID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid group id")
		return
	}
	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	var items []store.CheckItem
	switch req.Action {
	case "check_all":
		items, err = h.store.BulkSetItemsDone(r.Context(), user.ID, groupID, true)
	case "uncheck_all":
		items, err = h.store.BulkSetItemsDone(r.Context(), user.ID, groupID, false)
	case "delete_done":
		items, err = h.store.DeleteDoneItems(r.Context(), user.ID, groupID)
	default:
		badRequest(w, "action must be check_all, uncheck_all or delete_done")
		return
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "group not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
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
		Note     *string `json:"note"`
		Label    *string `json:"label"`
		GroupID  *int64  `json:"group_id"` // перенос пункта в другую группу
		// «взят в работу» — необязательная отметка между несделанным и сделанным
		InProgress *bool `json:"in_progress"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Name == nil && req.Done == nil && req.Position == nil && req.Note == nil &&
		req.Label == nil && req.GroupID == nil && req.InProgress == nil {
		badRequest(w, "nothing to update")
		return
	}
	if req.Name != nil && !validLen(*req.Name, 500) {
		badRequest(w, "name must be 1-500 characters")
		return
	}
	if req.Note != nil && utf8.RuneCountInString(*req.Note) > 4000 {
		badRequest(w, "note must be at most 4000 characters")
		return
	}
	if req.Label != nil && utf8.RuneCountInString(*req.Label) > 16 {
		badRequest(w, "label must be at most 16 characters")
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
	if req.Name != nil || req.Done != nil || req.Position != nil || req.Note != nil ||
		req.Label != nil || req.InProgress != nil {
		item, err2 = h.store.UpdateCheckItem(r.Context(), user.ID, id, store.CheckItemPatch{
			Name:       req.Name,
			Done:       req.Done,
			Position:   req.Position,
			Note:       req.Note,
			Label:      req.Label,
			InProgress: req.InProgress,
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
