package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

type linksHandlers struct {
	store *store.Store
	bot   *notify.Bot
}

// токен-приглашение — 24 hex-символа (12 случайных байт), как в Checker/Articles.
var linksShareTokenRe = regexp.MustCompile(`^[0-9a-f]{24}$`)

func (h *linksHandlers) tree(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	folders, links, err := h.store.LinksTree(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if folders == nil {
		folders = []store.LinkFolder{}
	}
	if links == nil {
		links = []store.Link{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": folders, "links": links})
}

func (h *linksHandlers) createFolder(w http.ResponseWriter, r *http.Request) {
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
	folder, err := h.store.CreateLinkFolder(r.Context(), user.ID, req.Name, req.ParentID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "parent folder not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"folder": folder})
	}
}

// folderPatchBody различает отсутствие поля parent_id и явный null.
type folderPatchBody struct {
	Name     *string `json:"name"`
	Position *int32  `json:"position"`
	ParentID *int64  `json:"parent_id"`

	parentSet bool
}

func (b *folderPatchBody) UnmarshalJSON(data []byte) error {
	type plain folderPatchBody
	if err := json.Unmarshal(data, (*plain)(b)); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	_, b.parentSet = raw["parent_id"]
	return nil
}

func (h *linksHandlers) updateFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid folder id")
		return
	}
	var req folderPatchBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Name == nil && req.Position == nil && !req.parentSet {
		badRequest(w, "nothing to update")
		return
	}
	if req.Name != nil && !validLen(*req.Name, 200) {
		badRequest(w, "name must be 1-200 characters")
		return
	}
	if req.parentSet && req.ParentID != nil && *req.ParentID == id {
		badRequest(w, "folder cannot be its own parent")
		return
	}
	patch := store.LinkFolderPatch{Name: req.Name, Position: req.Position}
	if req.parentSet {
		patch.ParentID = &req.ParentID
	}
	folder, err := h.store.UpdateLinkFolder(r.Context(), user.ID, id, patch)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "folder not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"folder": folder})
	}
}

func (h *linksHandlers) deleteFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid folder id")
		return
	}
	switch err := h.store.DeleteLinkFolder(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "folder not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *linksHandlers) createLink(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Name     string   `json:"name"`
		URL      string   `json:"url"`
		Tags     []string `json:"tags"`
		Pinned   bool     `json:"pinned"`
		FolderID *int64   `json:"folder_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if !validLen(req.Name, 500) {
		badRequest(w, "name must be 1-500 characters")
		return
	}
	if !validLen(req.URL, 2000) {
		badRequest(w, "url must be 1-2000 characters")
		return
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}
	link, err := h.store.CreateLink(r.Context(), user.ID, store.Link{
		FolderID: req.FolderID,
		Name:     req.Name,
		URL:      req.URL,
		Tags:     req.Tags,
		Pinned:   req.Pinned,
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "folder not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"link": link})
	}
}

type linkPatchBody struct {
	Name     *string   `json:"name"`
	URL      *string   `json:"url"`
	Tags     *[]string `json:"tags"`
	Pinned   *bool     `json:"pinned"`
	Position *int32    `json:"position"`
	FolderID *int64    `json:"folder_id"`

	folderSet bool
}

func (b *linkPatchBody) UnmarshalJSON(data []byte) error {
	type plain linkPatchBody
	if err := json.Unmarshal(data, (*plain)(b)); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	_, b.folderSet = raw["folder_id"]
	return nil
}

func (h *linksHandlers) updateLink(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid link id")
		return
	}
	var req linkPatchBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Name == nil && req.URL == nil && req.Tags == nil && req.Pinned == nil &&
		req.Position == nil && !req.folderSet {
		badRequest(w, "nothing to update")
		return
	}
	if req.Name != nil && !validLen(*req.Name, 500) {
		badRequest(w, "name must be 1-500 characters")
		return
	}
	if req.URL != nil && !validLen(*req.URL, 2000) {
		badRequest(w, "url must be 1-2000 characters")
		return
	}
	patch := store.LinkPatch{
		Name:     req.Name,
		URL:      req.URL,
		Tags:     req.Tags,
		Pinned:   req.Pinned,
		Position: req.Position,
	}
	if req.folderSet {
		patch.FolderID = &req.FolderID
	}
	link, err := h.store.UpdateLink(r.Context(), user.ID, id, patch)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "link or folder not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"link": link})
	}
}

func (h *linksHandlers) deleteLink(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid link id")
		return
	}
	switch err := h.store.DeleteLink(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "link not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// sendFolder — «отправить папку пользователю»: получатель получает копию
// поддерева (папка + вложенные + ссылки) через общий механизм deliverShare.
// Работает только с серверным хранилищем — у папки должен быть серверный id.
func (h *linksHandlers) sendFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid folder id")
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
	queued, _, err := deliverShare(r.Context(), h.store, h.bot, user, recipient.ID, "links_folder", id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "folder not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent_to": recipient, "queued": queued})
}

// shareFolderToken — токен и ссылка-приглашение на папку (lnf_<token>).
func (h *linksHandlers) shareFolderToken(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid folder id")
		return
	}
	token, err := h.store.EnsureLinkFolderShareToken(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "folder not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	link := ""
	if username := h.bot.Username(r.Context()); username != "" {
		link = fmt.Sprintf("https://t.me/%s?startapp=lnf_%s", username, token)
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token, "link": link})
}

// redeemFolder — принять папку по токену-приглашению (копия поддерева).
func (h *linksHandlers) redeemFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Token = strings.TrimPrefix(strings.TrimSpace(req.Token), "lnf_")
	if !linksShareTokenRe.MatchString(req.Token) {
		badRequest(w, "invalid token")
		return
	}
	name, err := h.store.RedeemLinkFolderShareToken(r.Context(), user.ID, req.Token)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "invite not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folder": map[string]string{"name": name}})
}

// shareLinkToken — токен и ссылка-приглашение на одну ссылку (lnk_<token>).
func (h *linksHandlers) shareLinkToken(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid link id")
		return
	}
	token, err := h.store.EnsureLinkShareToken(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "link not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	link := ""
	if username := h.bot.Username(r.Context()); username != "" {
		link = fmt.Sprintf("https://t.me/%s?startapp=lnk_%s", username, token)
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token, "link": link})
}

// redeemLink — принять одну ссылку по токену-приглашению.
func (h *linksHandlers) redeemLink(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Token = strings.TrimPrefix(strings.TrimSpace(req.Token), "lnk_")
	if !linksShareTokenRe.MatchString(req.Token) {
		badRequest(w, "invalid token")
		return
	}
	name, err := h.store.RedeemLinkShareToken(r.Context(), user.ID, req.Token)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "invite not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"link": map[string]string{"name": name}})
}

// sendLink — «отправить ссылку пользователю»: получатель получает копию
// одной ссылки через общий deliverShare (kind link).
func (h *linksHandlers) sendLink(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid link id")
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
	queued, _, err := deliverShare(r.Context(), h.store, h.bot, user, recipient.ID, "link", id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "link not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent_to": recipient, "queued": queued})
}

// click увеличивает счётчик переходов по ссылке (для топ-10).
func (h *linksHandlers) click(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid link id")
		return
	}
	clicks, err := h.store.ClickLink(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "link not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]int32{"clicks": clicks})
	}
}

// replaceAll — полный импорт (перенос локального хранилища на сервер).
func (h *linksHandlers) replaceAll(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Folders []struct {
			ID       int64  `json:"id"`
			ParentID *int64 `json:"parent_id"`
			Name     string `json:"name"`
			Position int32  `json:"position"`
		} `json:"folders"`
		Links []struct {
			FolderID *int64   `json:"folder_id"`
			Name     string   `json:"name"`
			URL      string   `json:"url"`
			Tags     []string `json:"tags"`
			Pinned   bool     `json:"pinned"`
			Position int32    `json:"position"`
			Clicks   int32    `json:"clicks"`
		} `json:"links"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if len(req.Folders) > 5000 || len(req.Links) > 50000 {
		badRequest(w, "too many items")
		return
	}
	folders := make([]store.BulkFolder, 0, len(req.Folders))
	for _, f := range req.Folders {
		if !validLen(f.Name, 200) {
			badRequest(w, "folder name must be 1-200 characters")
			return
		}
		folders = append(folders, store.BulkFolder{
			TmpID: f.ID, ParentTmpID: f.ParentID, Name: f.Name, Position: f.Position,
		})
	}
	links := make([]store.BulkLink, 0, len(req.Links))
	for _, l := range req.Links {
		if !validLen(l.Name, 500) || !validLen(l.URL, 2000) {
			badRequest(w, "link name must be 1-500 and url 1-2000 characters")
			return
		}
		tags := l.Tags
		if tags == nil {
			tags = []string{}
		}
		links = append(links, store.BulkLink{
			FolderTmpID: l.FolderID, Name: l.Name, URL: l.URL,
			Tags: tags, Pinned: l.Pinned, Position: l.Position, Clicks: l.Clicks,
		})
	}
	if err := h.store.ReplaceLinks(r.Context(), user.ID, folders, links); err != nil {
		internalError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /links/storage — где пользователь хранит ссылки ('', 'local', 'server').
// Настройка живёт на сервере: localStorage в Telegram-webview может очищаться.
func (h *linksHandlers) getStorage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	v, err := h.store.GetLinksStorage(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"storage": v})
}

func (h *linksHandlers) setStorage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Storage string `json:"storage"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		(req.Storage != "local" && req.Storage != "server") {
		badRequest(w, "storage must be 'local' or 'server'")
		return
	}
	if err := h.store.SetLinksStorage(r.Context(), user.ID, req.Storage); err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"storage": req.Storage})
}
