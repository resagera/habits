package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

var articleTokenRe = regexp.MustCompile(`^[0-9a-f]{24}$`)

type articlesHandlers struct {
	store   *store.Store
	bot     *notify.Bot
	dataDir string
}

func (h *articlesHandlers) tree(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	folders, articles, err := h.store.ArticlesTree(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if folders == nil {
		folders = []store.ArticleFolder{}
	}
	if articles == nil {
		articles = []store.Article{}
	}
	shared, err := h.store.SharedTrees(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if shared == nil {
		shared = []store.SharedTree{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": folders, "articles": articles, "shared": shared})
}

func (h *articlesHandlers) get(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid article id")
		return
	}
	a, err := h.store.GetArticleShared(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "article not found")
	case err != nil:
		internalError(w)
	default:
		pos, _ := h.store.ReadPosition(r.Context(), user.ID, id)
		writeJSON(w, http.StatusOK, map[string]any{"article": a, "read_pos": pos})
	}
}

// PUT /articles/{id}/read-pos {pos} — сохранить позицию чтения (0..1).
func (h *articlesHandlers) setReadPos(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid article id")
		return
	}
	var req struct {
		Pos float32 `json:"pos"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Pos < 0 || req.Pos > 1 {
		badRequest(w, "pos must be within [0, 1]")
		return
	}
	switch err := h.store.SetReadPosition(r.Context(), user.ID, id, req.Pos); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "article not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// GET /articles/{id}/history — список ревизий (новые сверху).
func (h *articlesHandlers) history(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid article id")
		return
	}
	revs, err := h.store.ListArticleRevisions(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "article not found")
	case err != nil:
		internalError(w)
	default:
		if revs == nil {
			revs = []store.ArticleRevision{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"revisions": revs})
	}
}

// GET /articles/revisions/{id} — содержимое ревизии.
func (h *articlesHandlers) revision(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid revision id")
		return
	}
	content, savedAt, err := h.store.GetArticleRevision(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "revision not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"content": content, "saved_at": savedAt})
	}
}

type articleRequest struct {
	Title    *string `json:"title"`
	Content  *string `json:"content"`
	FolderID *int64  `json:"folder_id"`
	// отличаем "не менять папку" от "переместить в корень"
	SetFolder bool `json:"set_folder"`
}

func validTitle(t string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(t))
	return n >= 1 && n <= 300
}

func (h *articlesHandlers) create(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req articleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Title == nil || !validTitle(*req.Title) {
		badRequest(w, "title must be 1-300 characters")
		return
	}
	content := ""
	if req.Content != nil {
		content = *req.Content
	}
	if len(content) > 1<<20 {
		badRequest(w, "content is too large (max 1 MB)")
		return
	}
	a, err := h.store.CreateArticle(r.Context(), user.ID, strings.TrimSpace(*req.Title), content, req.FolderID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "folder not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"article": a})
	}
}

func (h *articlesHandlers) update(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid article id")
		return
	}
	var req articleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Title != nil && !validTitle(*req.Title) {
		badRequest(w, "title must be 1-300 characters")
		return
	}
	if req.Title != nil {
		t := strings.TrimSpace(*req.Title)
		req.Title = &t
	}
	if req.Content != nil && len(*req.Content) > 1<<20 {
		badRequest(w, "content is too large (max 1 MB)")
		return
	}
	var folderID **int64
	if req.SetFolder {
		folderID = &req.FolderID
	}
	a, err := h.store.UpdateArticle(r.Context(), user.ID, id, req.Title, req.Content, folderID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "article or folder not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"article": a})
	}
}

func (h *articlesHandlers) delete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid article id")
		return
	}
	switch err := h.store.DeleteArticle(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "article not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- папки ---

func (h *articlesHandlers) createFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Name     string `json:"name"`
		ParentID *int64 `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validTitle(req.Name) {
		badRequest(w, "name must be 1-200 characters")
		return
	}
	f, err := h.store.CreateArticleFolder(r.Context(), user.ID, strings.TrimSpace(req.Name), req.ParentID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "parent folder not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"folder": f})
	}
}

func (h *articlesHandlers) updateFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid folder id")
		return
	}
	var req struct {
		Name      *string `json:"name"`
		ParentID  *int64  `json:"parent_id"`
		SetParent bool    `json:"set_parent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Name != nil && !validTitle(*req.Name) {
		badRequest(w, "name must be 1-200 characters")
		return
	}
	var parentID **int64
	if req.SetParent {
		parentID = &req.ParentID
	}
	f, err := h.store.UpdateArticleFolder(r.Context(), user.ID, id, req.Name, parentID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "folder not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"folder": f})
	}
}

func (h *articlesHandlers) deleteFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid folder id")
		return
	}
	switch err := h.store.DeleteArticleFolder(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "folder not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- шаринг ---

// POST /articles/{id}/share-token — токен и ссылка-приглашение.
func (h *articlesHandlers) shareToken(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid article id")
		return
	}
	token, err := h.store.EnsureArticleToken(r.Context(), user.ID, id, "share_token")
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "article not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	link := ""
	if username := h.bot.Username(r.Context()); username != "" {
		link = fmt.Sprintf("https://t.me/%s?startapp=art_%s", username, token)
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token, "link": link})
}

// POST /articles/{id}/download-token — публичная ссылка на скачивание .md.
func (h *articlesHandlers) downloadToken(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid article id")
		return
	}
	token, err := h.store.EnsureArticleToken(r.Context(), user.ID, id, "download_token")
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "article not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token, "path": "dl/articles/" + token})
}

// POST /articles/{id}/read-token — публичная ссылка на чтение (страница
// в браузере, без авторизации).
func (h *articlesHandlers) readToken(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid article id")
		return
	}
	token, err := h.store.EnsureArticleToken(r.Context(), user.ID, id, "read_token")
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "article not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token, "path": "read/" + token})
}

// GET /api/v1/articles/public/{token} — статья для публичной страницы
// чтения (вне auth-middleware).
func (h *articlesHandlers) publicRead(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if !articleTokenRe.MatchString(token) {
		writeError(w, http.StatusNotFound, "not_found", "article not found")
		return
	}
	a, err := h.store.ArticleByReadToken(r.Context(), token)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "article not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"title": a.Title, "content": a.Content, "updated_at": a.UpdatedAt,
	})
}

// GET /dl/articles/{token} — публичное скачивание (вне auth-middleware).
func (h *articlesHandlers) publicDownload(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if !articleTokenRe.MatchString(token) {
		http.NotFound(w, r)
		return
	}
	a, err := h.store.ArticleByDownloadToken(r.Context(), token)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	filename := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, a.Title) + ".md"
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	fmt.Fprintf(w, "# %s\n\n%s", a.Title, a.Content)
}

// POST /articles/images — загрузка картинки для вставки в Markdown.
// Файл сохраняется под случайным именем и раздаётся публично
// (/uploads/articles/), как фоновые картинки.
func (h *articlesHandlers) uploadImage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		badRequest(w, "multipart field 'file' is required (max 5 MB)")
		return
	}
	defer file.Close()

	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		internalError(w)
		return
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
		badRequest(w, "file must be a jpeg/png/webp/gif image")
		return
	}

	buf := make([]byte, 16)
	rand.Read(buf)
	filename := hex.EncodeToString(buf) + ext

	dir := filepath.Join(h.dataDir, "articles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		internalError(w)
		return
	}
	dst, err := os.Create(filepath.Join(dir, filename))
	if err != nil {
		internalError(w)
		return
	}
	defer dst.Close()
	if _, err := dst.Write(head[:n]); err != nil {
		internalError(w)
		return
	}
	if _, err := io.Copy(dst, file); err != nil {
		os.Remove(dst.Name())
		badRequest(w, "upload failed or file too large (max 5 MB)")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"url": "uploads/articles/" + filename})
}

// POST /articles/redeem — принять статью по токену-приглашению.
func (h *articlesHandlers) redeem(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Token = strings.TrimPrefix(strings.TrimSpace(req.Token), "art_")
	if !articleTokenRe.MatchString(req.Token) {
		badRequest(w, "invalid token")
		return
	}
	a, err := h.store.RedeemArticleToken(r.Context(), user.ID, req.Token)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "invite not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"article": a})
	}
}

// POST /articles/{id}/send — отправить статью пользователю приложения.
func (h *articlesHandlers) send(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid article id")
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
	queued, _, err := deliverShare(r.Context(), h.store, h.bot, user, recipient.ID, "article", id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "article not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent_to": recipient, "queued": queued})
}

// GET /articles/search?q= — поиск по содержимому (свои + доступные).
func (h *articlesHandlers) searchContent(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) < 2 || len(q) > 100 {
		badRequest(w, "q must be 2-100 characters")
		return
	}
	hits, err := h.store.SearchArticlesContent(r.Context(), user.ID, q, 20)
	if err != nil {
		internalError(w)
		return
	}
	if hits == nil {
		hits = []store.ArticleSearchHit{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
}

// POST /articles/folders/{id}/share {to} — открыть доступ к категории.
func (h *articlesHandlers) shareFolder(w http.ResponseWriter, r *http.Request) {
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
		badRequest(w, "cannot share to yourself")
		return
	}
	name, err := h.store.ShareArticleFolder(r.Context(), user.ID, id, recipient.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "folder not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	from := user.FirstName
	if user.Username != "" {
		from += " @" + user.Username
	}
	go h.bot.SendMessage(context.Background(), recipient.ID,
		fmt.Sprintf("📄 %s открыл вам доступ к категории статей «%s» — она появилась на вкладке Articles в Habits.",
			strings.TrimSpace(from), name))
	_ = h.store.TouchShareRecipient(r.Context(), user.ID, recipient.ID)
	writeJSON(w, http.StatusOK, map[string]any{"shared_to": recipient})
}

// GET /articles/folders/{id}/shares — кому открыт доступ (владелец).
func (h *articlesHandlers) listFolderShares(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid folder id")
		return
	}
	users, err := h.store.ListFolderShares(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "folder not found")
	case err != nil:
		internalError(w)
	default:
		if users == nil {
			users = []store.AccessUser{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": users})
	}
}

// DELETE /articles/folders/{id}/shares/{userID} — владелец отзывает доступ.
func (h *articlesHandlers) revokeFolderShare(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err1 := strconv.ParseInt(r.PathValue("id"), 10, 64)
	target, err2 := strconv.ParseInt(r.PathValue("userID"), 10, 64)
	if err1 != nil || err2 != nil {
		badRequest(w, "invalid id")
		return
	}
	switch err := h.store.RevokeFolderShare(r.Context(), user.ID, id, target, true); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "share not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// DELETE /articles/shared/{id} — получатель убирает доступную категорию у себя.
func (h *articlesHandlers) leaveShared(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid folder id")
		return
	}
	switch err := h.store.RevokeFolderShare(r.Context(), 0, id, user.ID, false); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "share not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
