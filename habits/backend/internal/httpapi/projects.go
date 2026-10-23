package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

// Страница Projects: проекты-сборники из блоков. Ref-блоки (чек-лист, статья,
// задача, категория задач) резолвятся при каждой загрузке — данные живые.
type projectsHandlers struct {
	store   *store.Store
	bot     *notify.Bot
	dataDir string
}

var projectStatuses = map[string]bool{
	"draft": true, "planned": true, "active": true, "paused": true,
	"done": true, "cancelled": true, "archived": true,
}

var blockKindLabels = map[string]string{
	"text":          "текст",
	"images":        "картинки",
	"file":          "файл",
	"location":      "геолокация",
	"checker_group": "чек-лист",
	"article":       "статья",
	"task":          "задача",
	"task_category": "категория задач",
}

// --- список / CRUD проектов ---

// GET /projects — категории + проекты (свои и расшаренные) + типы для подсказок.
func (h *projectsHandlers) list(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	cats, err := h.store.ListProjectCategories(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	projects, err := h.store.ListProjects(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	types, err := h.store.ProjectTypes(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if cats == nil {
		cats = []store.ProjectCategory{}
	}
	if projects == nil {
		projects = []store.Project{}
	}
	if types == nil {
		types = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"categories": cats, "projects": projects, "types": types,
	})
}

type projectRequest struct {
	CategoryID  *int64   `json:"category_id"`
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	Icon        *string  `json:"icon"`
	Color       *string  `json:"color"`
	Cover       *string  `json:"cover"`
	Ptype       *string  `json:"ptype"`
	Status      *string  `json:"status"`
	Tags        []string `json:"tags"`
	StartDate   *string  `json:"start_date"`
	DueDate     *string  `json:"due_date"`
	Tz          *string  `json:"tz"`
	Position    *int32   `json:"position"`
}

func (pr *projectRequest) validate() string {
	if pr.Name != nil {
		*pr.Name = strings.TrimSpace(*pr.Name)
		if l := utf8.RuneCountInString(*pr.Name); l < 1 || l > 200 {
			return "name must be 1-200 characters"
		}
	}
	if pr.Description != nil && utf8.RuneCountInString(*pr.Description) > 2000 {
		return "description must be at most 2000 characters"
	}
	if pr.Icon != nil && utf8.RuneCountInString(*pr.Icon) > 8 {
		return "icon must be a short emoji"
	}
	if pr.Color != nil && !colorRe.MatchString(*pr.Color) {
		return "color must be #rrggbb"
	}
	if pr.Ptype != nil {
		*pr.Ptype = strings.TrimSpace(*pr.Ptype)
		if utf8.RuneCountInString(*pr.Ptype) > 100 {
			return "type must be at most 100 characters"
		}
	}
	if pr.Status != nil && !projectStatuses[*pr.Status] {
		return "unknown status"
	}
	if len(pr.Tags) > 30 {
		return "at most 30 tags"
	}
	clean := pr.Tags[:0]
	for _, t := range pr.Tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if utf8.RuneCountInString(t) > 50 {
			return "tag must be at most 50 characters"
		}
		clean = append(clean, t)
	}
	pr.Tags = clean
	for _, d := range []*string{pr.StartDate, pr.DueDate} {
		if d != nil && *d != "" && !taskDateRe.MatchString(*d) {
			return "dates must be YYYY-MM-DD"
		}
	}
	if pr.Tz != nil && utf8.RuneCountInString(*pr.Tz) > 64 {
		return "tz must be at most 64 characters"
	}
	return ""
}

// POST /projects
func (h *projectsHandlers) create(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req projectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Name == nil {
		badRequest(w, "name is required")
		return
	}
	if msg := req.validate(); msg != "" {
		badRequest(w, msg)
		return
	}
	n := store.NewProject{
		CategoryID: req.CategoryID, Name: *req.Name,
		Color: "#607d8b", Status: "draft",
	}
	if req.Description != nil {
		n.Description = *req.Description
	}
	if req.Icon != nil {
		n.Icon = *req.Icon
	}
	if req.Color != nil {
		n.Color = *req.Color
	}
	if req.Ptype != nil {
		n.Ptype = *req.Ptype
	}
	if req.Status != nil {
		n.Status = *req.Status
	}
	if req.Tags != nil {
		n.Tags = req.Tags
	}
	if req.StartDate != nil && *req.StartDate != "" {
		n.StartDate = req.StartDate
	}
	if req.DueDate != nil && *req.DueDate != "" {
		n.DueDate = req.DueDate
	}
	if req.Tz != nil {
		n.Tz = *req.Tz
	}
	p, err := h.store.CreateProject(r.Context(), user.ID, n)
	if err != nil {
		if store.IsForeignKeyViolation(err) {
			badRequest(w, "category not found")
			return
		}
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"project": p})
}

// PATCH /projects/{id} — метаданные, только владелец.
func (h *projectsHandlers) update(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid project id")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		badRequest(w, "invalid body")
		return
	}
	var raw map[string]json.RawMessage
	var req projectRequest
	if json.Unmarshal(body, &raw) != nil || json.Unmarshal(body, &req) != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if msg := req.validate(); msg != "" {
		badRequest(w, msg)
		return
	}
	has := func(k string) bool { _, ok := raw[k]; return ok }
	patch := store.ProjectPatch{
		Name: req.Name, Description: req.Description, Icon: req.Icon,
		Color: req.Color, Cover: req.Cover, Ptype: req.Ptype, Status: req.Status,
		Tz: req.Tz, Position: req.Position,
	}
	if has("tags") {
		patch.Tags = req.Tags
		if patch.Tags == nil {
			patch.Tags = []string{}
		}
	}
	if has("category_id") {
		patch.SetCategory = true
		patch.CategoryID = req.CategoryID
	}
	if has("start_date") {
		patch.SetStart = true
		if req.StartDate != nil && *req.StartDate != "" {
			patch.StartDate = req.StartDate
		}
	}
	if has("due_date") {
		patch.SetDue = true
		if req.DueDate != nil && *req.DueDate != "" {
			patch.DueDate = req.DueDate
		}
	}
	p, oldCover, err := h.store.UpdateProject(r.Context(), user.ID, id, patch)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	case err != nil:
		if store.IsForeignKeyViolation(err) {
			badRequest(w, "category not found")
			return
		}
		internalError(w)
		return
	}
	if oldCover != "" {
		h.removeFile(oldCover)
	}
	_ = h.store.AddProjectHistory(r.Context(), id, user.ID, "изменил параметры проекта")
	writeJSON(w, http.StatusOK, map[string]any{"project": p})
}

// DELETE /projects/{id} — только владелец, вместе с файлами блоков.
func (h *projectsHandlers) delete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid project id")
		return
	}
	files, err := h.store.DeleteProject(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "project not found")
	case err != nil:
		internalError(w)
	default:
		for _, f := range files {
			h.removeFile(f)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// GET /projects/{id} — проект + блоки (живые данные) + участники; отмечает просмотр.
func (h *projectsHandlers) get(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid project id")
		return
	}
	p, blocks, err := h.store.GetProject(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	members, err := h.store.ListProjectShares(r.Context(), user.ID, id)
	if err != nil {
		internalError(w)
		return
	}
	if blocks == nil {
		blocks = []store.ProjectBlock{}
	}
	if members == nil {
		members = []store.AccessUser{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": p, "blocks": blocks, "members": members})
}

// GET /projects/{id}/history
func (h *projectsHandlers) history(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid project id")
		return
	}
	entries, err := h.store.ListProjectHistory(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "project not found")
	case err != nil:
		internalError(w)
	default:
		if entries == nil {
			entries = []store.ProjectHistoryEntry{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"history": entries})
	}
}

// --- категории ---

// POST /projects/categories
func (h *projectsHandlers) createCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if l := utf8.RuneCountInString(req.Name); l < 1 || l > 200 {
		badRequest(w, "name must be 1-200 characters")
		return
	}
	c, err := h.store.CreateProjectCategory(r.Context(), user.ID, req.Name)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"category": c})
}

// PATCH /projects/categories/{id}
func (h *projectsHandlers) updateCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid category id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if l := utf8.RuneCountInString(req.Name); l < 1 || l > 200 {
		badRequest(w, "name must be 1-200 characters")
		return
	}
	switch err := h.store.UpdateProjectCategory(r.Context(), user.ID, id, req.Name); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// DELETE /projects/categories/{id} — проекты уходят в общий список (FK SET NULL).
func (h *projectsHandlers) deleteCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid category id")
		return
	}
	switch err := h.store.DeleteProjectCategory(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- блоки ---

type blockContent struct {
	// text
	Text *string `json:"text"`
	Rich *bool   `json:"rich"`
	// images
	Images []string `json:"images"`
	// file
	URL  *string `json:"url"`
	Name *string `json:"name"`
	Size *int64  `json:"size"`
	// location
	Lat   *float64 `json:"lat"`
	Lon   *float64 `json:"lon"`
	Label *string  `json:"label"`
	// ref
	RefID *int64 `json:"ref_id"`
}

// validateBlockContent нормализует content под kind и возвращает JSON или ошибку.
func (h *projectsHandlers) validateBlockContent(kind string, raw json.RawMessage) (json.RawMessage, string) {
	var c blockContent
	if raw != nil {
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, "invalid content"
		}
	}
	switch kind {
	case "text":
		text := ""
		if c.Text != nil {
			text = *c.Text
		}
		if utf8.RuneCountInString(text) > 65536 {
			return nil, "text must be at most 65536 characters"
		}
		rich := c.Rich != nil && *c.Rich
		out, _ := json.Marshal(map[string]any{"text": text, "rich": rich})
		return out, ""
	case "images":
		if len(c.Images) == 0 {
			return nil, "images are required"
		}
		if len(c.Images) > 30 {
			return nil, "at most 30 images per block"
		}
		for _, u := range c.Images {
			if !strings.HasPrefix(u, "uploads/projects/") || strings.Contains(u, "..") {
				return nil, "invalid image url"
			}
		}
		out, _ := json.Marshal(map[string]any{"images": c.Images})
		return out, ""
	case "file":
		if c.URL == nil || !strings.HasPrefix(*c.URL, "uploads/projects/") || strings.Contains(*c.URL, "..") {
			return nil, "invalid file url"
		}
		name, size := "файл", int64(0)
		if c.Name != nil && strings.TrimSpace(*c.Name) != "" {
			name = strings.TrimSpace(*c.Name)
		}
		if utf8.RuneCountInString(name) > 300 {
			return nil, "file name too long"
		}
		if c.Size != nil {
			size = *c.Size
		}
		out, _ := json.Marshal(map[string]any{"url": *c.URL, "name": name, "size": size})
		return out, ""
	case "location":
		if c.Lat == nil || c.Lon == nil || *c.Lat < -90 || *c.Lat > 90 || *c.Lon < -180 || *c.Lon > 180 {
			return nil, "lat/lon are required"
		}
		label := ""
		if c.Label != nil {
			label = strings.TrimSpace(*c.Label)
		}
		if utf8.RuneCountInString(label) > 300 {
			return nil, "label too long"
		}
		out, _ := json.Marshal(map[string]any{"lat": *c.Lat, "lon": *c.Lon, "label": label})
		return out, ""
	case "checker_group", "article", "task", "task_category":
		if c.RefID == nil || *c.RefID <= 0 {
			return nil, "ref_id is required"
		}
		out, _ := json.Marshal(map[string]any{"ref_id": *c.RefID})
		return out, ""
	}
	return nil, "unknown block kind"
}

// POST /projects/{id}/blocks — добавить блок. Для ref-блоков можно передать
// create_name вместо content.ref_id — сущность будет создана на своей странице.
func (h *projectsHandlers) createBlock(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid project id")
		return
	}
	var req struct {
		Kind       string          `json:"kind"`
		Content    json.RawMessage `json:"content"`
		Bg         string          `json:"bg"`
		Collapsed  bool            `json:"collapsed"`
		Position   *int32          `json:"position"`
		CreateName string          `json:"create_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Bg != "" && !colorRe.MatchString(req.Bg) {
		badRequest(w, "bg must be #rrggbb or empty")
		return
	}
	// «создать новый элемент» — сущность появляется на соответствующей странице
	if name := strings.TrimSpace(req.CreateName); name != "" {
		refID, msg := h.createRefEntity(r, user.ID, req.Kind, name)
		if msg != "" {
			badRequest(w, msg)
			return
		}
		req.Content, _ = json.Marshal(map[string]int64{"ref_id": refID})
	}
	content, msg := h.validateBlockContent(req.Kind, req.Content)
	if msg != "" {
		badRequest(w, msg)
		return
	}
	// существующий ref должен резолвиться у добавляющего
	switch req.Kind {
	case "checker_group", "article", "task", "task_category":
		var c blockContent
		_ = json.Unmarshal(content, &c)
		if _, ok := h.store.ResolveProjectRef(r.Context(), user.ID, req.Kind, *c.RefID); !ok {
			writeError(w, http.StatusNotFound, "not_found", "referenced item not found")
			return
		}
	}
	b, err := h.store.CreateProjectBlock(r.Context(), user.ID, id, req.Kind, content, req.Bg, req.Collapsed, req.Position)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "project not found")
	case errors.Is(err, store.ErrLimit):
		badRequest(w, "достигнут лимит блоков в проекте")
	case err != nil:
		internalError(w)
	default:
		_ = h.store.AddProjectHistory(r.Context(), id, user.ID, "добавил блок: "+blockKindLabels[req.Kind])
		writeJSON(w, http.StatusCreated, map[string]any{"block": b})
	}
}

// createRefEntity создаёт новую сущность для ref-блока на её родной странице.
func (h *projectsHandlers) createRefEntity(r *http.Request, userID int64, kind, name string) (int64, string) {
	if utf8.RuneCountInString(name) > 200 {
		return 0, "create_name too long"
	}
	switch kind {
	case "checker_group":
		g, err := h.store.CreateCheckGroup(r.Context(), userID, name, nil)
		if err != nil {
			return 0, "cannot create checker group"
		}
		return g.ID, ""
	case "article":
		a, err := h.store.CreateArticle(r.Context(), userID, name, "", nil)
		if err != nil {
			return 0, "cannot create article"
		}
		return a.ID, ""
	case "task":
		t, err := h.store.CreateTask(r.Context(), userID, store.NewTask{
			Title: name, Status: "Открыта", StatusKind: "open",
		})
		if err != nil {
			return 0, "cannot create task"
		}
		return t.ID, ""
	case "task_category":
		p, err := h.store.CreateTaskProject(r.Context(), userID, name, "#607d8b")
		if err != nil {
			return 0, "cannot create task category"
		}
		return p.ID, ""
	}
	return 0, "create_name is not supported for this kind"
}

// PATCH /projects/blocks/{id}
func (h *projectsHandlers) updateBlock(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid block id")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		badRequest(w, "invalid body")
		return
	}
	var raw map[string]json.RawMessage
	var req struct {
		Kind      string          `json:"kind"` // требуется при смене content
		Content   json.RawMessage `json:"content"`
		Bg        *string         `json:"bg"`
		Collapsed *bool           `json:"collapsed"`
		Position  *int32          `json:"position"`
	}
	if json.Unmarshal(body, &raw) != nil || json.Unmarshal(body, &req) != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Bg != nil && *req.Bg != "" && !colorRe.MatchString(*req.Bg) {
		badRequest(w, "bg must be #rrggbb or empty")
		return
	}
	patch := store.BlockPatch{Bg: req.Bg, Collapsed: req.Collapsed, Position: req.Position}
	if _, ok := raw["content"]; ok {
		content, msg := h.validateBlockContent(req.Kind, req.Content)
		if msg != "" {
			badRequest(w, msg)
			return
		}
		patch.Content = content
	}
	b, projectID, err := h.store.UpdateProjectBlock(r.Context(), user.ID, id, patch)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "block not found")
	case err != nil:
		internalError(w)
	default:
		if patch.Content != nil {
			_ = h.store.AddProjectHistory(r.Context(), projectID, user.ID, "изменил блок: "+blockKindLabels[b.Kind])
		}
		writeJSON(w, http.StatusOK, map[string]any{"block": b})
	}
}

// DELETE /projects/blocks/{id}
func (h *projectsHandlers) deleteBlock(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid block id")
		return
	}
	projectID, kind, files, err := h.store.DeleteProjectBlock(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "block not found")
	case err != nil:
		internalError(w)
	default:
		for _, f := range files {
			h.removeFile(f)
		}
		_ = h.store.AddProjectHistory(r.Context(), projectID, user.ID, "удалил блок: "+blockKindLabels[kind])
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- загрузка файлов ---

// POST /projects/{id}/upload — multipart 'file': картинки (jpeg/png/webp/gif)
// или произвольный файл (до 20 МБ). Возвращает {url, name, size, image}.
func (h *projectsHandlers) upload(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid project id")
		return
	}
	if ok, err := h.store.HasProjectAccess(r.Context(), user.ID, id); err != nil || !ok {
		if err != nil {
			internalError(w)
		} else {
			writeError(w, http.StatusNotFound, "not_found", "project not found")
		}
		return
	}
	limits, err := h.store.LimitsForUser(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	maxMB := limits.MaxFileMB
	if limits.MaxImageMB > maxMB {
		maxMB = limits.MaxImageMB
	}
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxMB)<<20+(1<<20))
	file, hdr, err := r.FormFile("file")
	if err != nil {
		badRequest(w, fmt.Sprintf("multipart field 'file' is required (max %d MB)", maxMB))
		return
	}
	defer file.Close()

	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		internalError(w)
		return
	}
	ext := ""
	image := false
	switch http.DetectContentType(head[:n]) {
	case "image/jpeg":
		ext, image = ".jpg", true
	case "image/png":
		ext, image = ".png", true
	case "image/webp":
		ext, image = ".webp", true
	case "image/gif":
		ext, image = ".gif", true
	default:
		// произвольный файл — расширение из имени (без точек-обходов)
		ext = strings.ToLower(filepath.Ext(hdr.Filename))
		if len(ext) > 10 || strings.ContainsAny(ext, "/\\") {
			ext = ""
		}
	}

	// лимит количества по типу пользователя
	imgCount, fileCount, err := h.store.CountProjectUploads(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if image && imgCount >= limits.MaxImages {
		badRequest(w, fmt.Sprintf("достигнут лимит картинок (%d) — удалите ненужные", limits.MaxImages))
		return
	}
	if !image && fileCount >= limits.MaxFiles {
		badRequest(w, fmt.Sprintf("достигнут лимит файлов (%d) — удалите ненужные", limits.MaxFiles))
		return
	}

	buf := make([]byte, 16)
	rand.Read(buf)
	filename := hex.EncodeToString(buf) + ext

	dir := filepath.Join(h.dataDir, "projects")
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
		badRequest(w, fmt.Sprintf("upload failed or file too large (max %d MB)", maxMB))
		return
	}
	info, _ := dst.Stat()
	size := int64(0)
	if info != nil {
		size = info.Size()
	}
	// лимит размера: у картинок и файлов — свой
	sizeLimit := int64(limits.MaxFileMB) << 20
	if image {
		sizeLimit = int64(limits.MaxImageMB) << 20
	}
	if size > sizeLimit {
		os.Remove(dst.Name())
		kind := "файла"
		mb := limits.MaxFileMB
		if image {
			kind = "картинки"
			mb = limits.MaxImageMB
		}
		badRequest(w, fmt.Sprintf("превышен лимит размера %s (%d МБ)", kind, mb))
		return
	}
	url := "uploads/projects/" + filename
	if err := h.store.RegisterProjectUpload(r.Context(), url, user.ID, image, size); err != nil {
		os.Remove(dst.Name())
		internalError(w)
		return
	}
	name := filepath.Base(hdr.Filename)
	if name == "." || name == "/" || name == "" {
		name = filename
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"url": url, "name": name, "size": size, "image": image,
	})
}

// removeFile удаляет файл по хранимому пути uploads/projects/<имя>
// вместе с учётной записью в project_uploads (лимиты).
func (h *projectsHandlers) removeFile(url string) {
	if !strings.HasPrefix(url, "uploads/projects/") {
		return
	}
	name := filepath.Base(strings.TrimPrefix(url, "uploads/projects/"))
	if name == "." || name == "/" || name == "" {
		return
	}
	os.Remove(filepath.Join(h.dataDir, "projects", name))
	_ = h.store.DeleteProjectUpload(context.Background(), url)
}

// --- шаринг ---

// POST /projects/{id}/share {to} — через deliverShare (kind "project").
func (h *projectsHandlers) share(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid project id")
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
	queued, _, err := deliverShare(r.Context(), h.store, h.bot, user, recipient.ID, "project", id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	_ = h.store.AddProjectHistory(r.Context(), id, user.ID, "открыл доступ: "+accessUserLabel(recipient))
	writeJSON(w, http.StatusOK, map[string]any{"shared_with": recipient, "queued": queued})
}

// GET /projects/{id}/shares
func (h *projectsHandlers) listShares(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid project id")
		return
	}
	users, err := h.store.ListProjectShares(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "project not found")
	case err != nil:
		internalError(w)
	default:
		if users == nil {
			users = []store.AccessUser{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": users})
	}
}

// DELETE /projects/{id}/shares/{userId} — отзыв владельцем или «покинуть».
func (h *projectsHandlers) revokeShare(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid project id")
		return
	}
	targetID, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil {
		badRequest(w, "invalid user id")
		return
	}
	switch err := h.store.RevokeProjectShare(r.Context(), user.ID, id, targetID); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "share not found")
	case err != nil:
		internalError(w)
	default:
		action := "убрал участника"
		if targetID == user.ID {
			action = "покинул проект"
		}
		_ = h.store.AddProjectHistory(r.Context(), id, user.ID, action)
		w.WriteHeader(http.StatusNoContent)
	}
}

// accessUserLabel — человекочитаемое имя пользователя для истории.
func accessUserLabel(u store.AccessUser) string {
	if u.FirstName != "" {
		return u.FirstName
	}
	if u.Username != "" {
		return "@" + u.Username
	}
	return "#" + strconv.FormatInt(u.ID, 10)
}
