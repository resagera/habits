package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/store"
)

type appearanceHandlers struct {
	store   *store.Store
	dataDir string
}

// каталоги внутри DATA_DIR (раздаются как /uploads/<...>/)
const (
	galleryDir     = "gallery"
	backgroundsSub = "backgrounds"
)

// themeBackground — снимок фона, сохранённый вместе с темой.
//
// Файл КОПИРУЕТСЯ: иначе удаление картинки из своей коллекции ломало бы уже
// сохранённую тему. Ссылки (общая галерея, внешний адрес) копировать не нужно.
type themeBackground struct {
	Kind     string `json:"kind"` // none | file | url
	File     string `json:"file,omitempty"`
	Thumb    string `json:"thumb,omitempty"`
	URL      string `json:"url,omitempty"`
	Position string `json:"position,omitempty"`
	Blur     int32  `json:"blur"`
	Dim      int32  `json:"dim"`
	Scale    int    `json:"scale"`
	OffsetX  int    `json:"offset_x"`
	OffsetY  int    `json:"offset_y"`
	FocalX   int    `json:"focal_x"`
	FocalY   int    `json:"focal_y"`
}

func (h *appearanceHandlers) backgroundsDir() string {
	return filepath.Join(h.dataDir, backgroundsSub)
}

// copyFile — копия файла со случайным именем; возвращает новое имя.
func copyFile(dir, name, prefix string) (string, error) {
	src, err := os.Open(filepath.Join(dir, filepath.Base(name)))
	if err != nil {
		return "", err
	}
	defer src.Close()
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	dstName := prefix + hex.EncodeToString(buf) + filepath.Ext(name)
	dst, err := os.Create(filepath.Join(dir, dstName))
	if err != nil {
		return "", err
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		os.Remove(dst.Name())
		return "", err
	}
	return dstName, nil
}

// copyBetween — копия файла в другой каталог со случайным именем.
func copyBetween(srcDir, dstDir, name, prefix string) (string, error) {
	src, err := os.Open(filepath.Join(srcDir, filepath.Base(name)))
	if err != nil {
		return "", err
	}
	defer src.Close()
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	dstName := prefix + hex.EncodeToString(buf) + filepath.Ext(name)
	dst, err := os.Create(filepath.Join(dstDir, dstName))
	if err != nil {
		return "", err
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		os.Remove(dst.Name())
		return "", err
	}
	return dstName, nil
}

// captureBackground снимает текущий фон пользователя для сохранения в тему.
func (h *appearanceHandlers) captureBackground(ctx context.Context, userID int64) themeBackground {
	bg, images, err := h.store.GetBackground(ctx, userID)
	if err != nil {
		return themeBackground{Kind: "none"}
	}
	p, _ := h.store.GetBackgroundPlacement(ctx, userID)
	out := themeBackground{
		Kind: bg.Kind, Position: bg.Position, Blur: bg.Blur, Dim: bg.Dim,
		Scale: p.Scale, OffsetX: p.OffsetX, OffsetY: p.OffsetY,
		FocalX: p.FocalX, FocalY: p.FocalY,
	}
	switch bg.Kind {
	case "url":
		out.URL = bg.Value
	case "file":
		name, err := copyFile(h.backgroundsDir(), bg.Value, "theme_")
		if err != nil {
			out.Kind = "none"
			return out
		}
		out.File = name
		// превью копируем тоже — карточка темы показывает картинку
		for _, img := range images {
			if img.Filename == bg.Value && img.Thumb != "" {
				if t, err := copyFile(h.backgroundsDir(), img.Thumb, "theme_"); err == nil {
					out.Thumb = t
				}
			}
		}
	}
	return out
}

// GET /appearance — тема, черновик и сохранённые темы для текущего режима входа.
//
// Оформление в Telegram и в браузере/расширении раздельное (с v2.61): пишем и
// читаем разные колонки в зависимости от вида сессии.
func (h *appearanceHandlers) get(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	st, err := h.store.GetAppearanceState(r.Context(), user.ID, user.TokenSession)
	if err != nil {
		internalError(w)
		return
	}
	themes, err := h.store.ListUserThemes(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if themes == nil {
		themes = []store.UserTheme{}
	}
	placement, err := h.store.GetBackgroundPlacement(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"state": st, "themes": themes, "placement": placement,
	})
}

// PUT /appearance — выбор темы и черновик «своей темы».
func (h *appearanceHandlers) set(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req store.AppearanceState
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Mode != "auto" && req.Mode != "fixed" {
		badRequest(w, "mode must be auto|fixed")
		return
	}
	if len(req.Draft) > 8192 {
		badRequest(w, "draft is too large")
		return
	}
	for _, id := range []string{req.ThemeID, req.AutoLight, req.AutoDark} {
		if len(id) > 64 {
			badRequest(w, "theme id is too long")
			return
		}
	}
	if err := h.store.SetAppearanceState(r.Context(), user.ID, user.TokenSession, req); err != nil {
		internalError(w)
		return
	}
	h.get(w, r)
}

// POST /appearance/themes — сохранить тему (создать или перезаписать по имени).
func (h *appearanceHandlers) saveTheme(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req store.UserTheme
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len([]rune(req.Name)) > 60 {
		badRequest(w, "name is required (1-60 chars)")
		return
	}
	if req.Kind != "light" && req.Kind != "dark" {
		badRequest(w, "kind must be light|dark")
		return
	}
	if len(req.Tokens) > 8192 || len(req.Bg) > 4096 {
		badRequest(w, "theme is too large")
		return
	}
	if len(req.Tokens) == 0 {
		req.Tokens = json.RawMessage("{}")
	}
	// фон снимаем сами: клиенту незачем знать про копирование файлов, а тема
	// должна пережить удаление исходной картинки
	captured, err := json.Marshal(h.captureBackground(r.Context(), user.ID))
	if err != nil {
		internalError(w)
		return
	}
	req.Bg = captured
	out, err := h.store.SaveUserTheme(r.Context(), user.ID, req)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "theme not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"theme": out})
	}
}

func (h *appearanceHandlers) deleteTheme(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid theme id")
		return
	}
	// файлы, скопированные для темы, удаляем вместе с ней — иначе они
	// накапливались бы в каталоге навсегда
	var files []string
	if themes, err := h.store.ListUserThemes(r.Context(), user.ID); err == nil {
		for _, t := range themes {
			if t.ID != id {
				continue
			}
			var bg themeBackground
			if json.Unmarshal(t.Bg, &bg) == nil {
				files = append(files, bg.File, bg.Thumb)
			}
		}
	}
	switch err := h.store.DeleteUserTheme(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "theme not found")
	case err != nil:
		internalError(w)
	default:
		for _, f := range files {
			if f != "" {
				_ = os.Remove(filepath.Join(h.backgroundsDir(), filepath.Base(f)))
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// POST /appearance/themes/{id}/apply — включить сохранённую тему целиком.
//
// Тема — это весь вид: цвета И фон. Поэтому вместе с выбором темы
// восстанавливаем её картинку и размещение; файл у темы свой, так что он
// работает, даже если исходную картинку из коллекции удалили.
func (h *appearanceHandlers) applyTheme(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid theme id")
		return
	}
	themes, err := h.store.ListUserThemes(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	var theme *store.UserTheme
	for i := range themes {
		if themes[i].ID == id {
			theme = &themes[i]
		}
	}
	if theme == nil {
		writeError(w, http.StatusNotFound, "not_found", "theme not found")
		return
	}

	st, err := h.store.GetAppearanceState(r.Context(), user.ID, user.TokenSession)
	if err != nil {
		internalError(w)
		return
	}
	st.Mode = "fixed"
	st.ThemeID = "saved:" + strconv.FormatInt(id, 10)
	if err := h.store.SetAppearanceState(r.Context(), user.ID, user.TokenSession, st); err != nil {
		internalError(w)
		return
	}

	var bg themeBackground
	if json.Unmarshal(theme.Bg, &bg) == nil && bg.Kind != "" {
		value := bg.URL
		if bg.Kind == "file" {
			value = bg.File
		}
		if bg.Position == "" {
			bg.Position = "cover"
		}
		if err := h.store.SetBackground(r.Context(), user.ID, store.BackgroundSettings{
			Kind: bg.Kind, Value: value, Position: bg.Position, Blur: bg.Blur, Dim: bg.Dim,
		}); err != nil {
			internalError(w)
			return
		}
		if bg.Scale > 0 {
			_ = h.store.SetBackgroundPlacement(r.Context(), user.ID, store.BackgroundPlacement{
				Scale: bg.Scale, OffsetX: bg.OffsetX, OffsetY: bg.OffsetY,
				FocalX: bg.FocalX, FocalY: bg.FocalY,
			})
		}
	}
	h.get(w, r)
}

// POST /admin/gallery/images/from-background — положить в общую галерею
// картинку из своей коллекции: админ пополняет галерею тем, что уже загрузил,
// не выкачивая и не загружая файл заново.
func (h *appearanceHandlers) galleryFromBackground(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		ImageID    int64  `json:"image_id"`
		CategoryID *int64 `json:"category_id"`
		Title      string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ImageID == 0 {
		badRequest(w, "image_id is required")
		return
	}
	_, images, err := h.store.GetBackground(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	var src *store.BackgroundImage
	for i := range images {
		if images[i].ID == req.ImageID {
			src = &images[i]
		}
	}
	if src == nil {
		writeError(w, http.StatusNotFound, "not_found", "image not found")
		return
	}
	if err := os.MkdirAll(filepath.Join(h.dataDir, galleryDir), 0o755); err != nil {
		internalError(w)
		return
	}
	name, err := copyBetween(h.backgroundsDir(), filepath.Join(h.dataDir, galleryDir), src.Filename, "")
	if err != nil {
		internalError(w)
		return
	}
	thumb := ""
	if src.Thumb != "" {
		if t, err := copyBetween(h.backgroundsDir(), filepath.Join(h.dataDir, galleryDir), src.Thumb, "t_"); err == nil {
			thumb = t
		}
	}
	img, err := h.store.AddGalleryImage(r.Context(), user.ID, store.GalleryImage{
		CategoryID: req.CategoryID, Filename: name, Thumb: thumb,
		Title: strings.TrimSpace(req.Title),
	})
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"image": img})
}

// PUT /appearance/placement — масштаб, смещение и точка фокуса фона.
func (h *appearanceHandlers) setPlacement(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var p store.BackgroundPlacement
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if p.Scale < 10 || p.Scale > 400 {
		badRequest(w, "scale must be 10-400")
		return
	}
	if p.OffsetX < -100 || p.OffsetX > 100 || p.OffsetY < -100 || p.OffsetY > 100 {
		badRequest(w, "offset must be -100..100")
		return
	}
	if p.FocalX < 0 || p.FocalX > 100 || p.FocalY < 0 || p.FocalY > 100 {
		badRequest(w, "focal must be 0..100")
		return
	}
	if err := h.store.SetBackgroundPlacement(r.Context(), user.ID, p); err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// --- папки своих фонов ---

func (h *appearanceHandlers) listFolders(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	folders, err := h.store.ListBackgroundFolders(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if folders == nil {
		folders = []store.BackgroundFolder{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": folders})
}

func (h *appearanceHandlers) createFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Name     string `json:"name"`
		ParentID *int64 `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len([]rune(req.Name)) > 100 {
		badRequest(w, "name is required (1-100 chars)")
		return
	}
	f, err := h.store.CreateBackgroundFolder(r.Context(), user.ID, req.Name, req.ParentID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"folder": f})
}

// PATCH /appearance/folders/{id} — имя, свёрнутость и перенос.
func (h *appearanceHandlers) updateFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid folder id")
		return
	}
	var req struct {
		Name      *string `json:"name"`
		Collapsed *bool   `json:"collapsed"`
		ParentID  *int64  `json:"parent_id"`
		MoveRoot  bool    `json:"move_to_root"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	setParent := req.ParentID != nil || req.MoveRoot
	err = h.store.UpdateBackgroundFolder(r.Context(), user.ID, id, req.Name, req.Collapsed, req.ParentID, setParent)
	switch {
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "нельзя перенести папку внутрь самой себя")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "folder not found")
	case err != nil:
		internalError(w)
	default:
		h.listFolders(w, r)
	}
}

func (h *appearanceHandlers) deleteFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid folder id")
		return
	}
	switch err := h.store.DeleteBackgroundFolder(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "folder not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// POST /appearance/images/{id}/move — перенос картинки в папку (null — в корень).
func (h *appearanceHandlers) moveImage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid image id")
		return
	}
	var req struct {
		FolderID *int64 `json:"folder_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	switch err := h.store.MoveBackgroundImage(r.Context(), user.ID, id, req.FolderID); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "image not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// --- общая галерея ---

// GET /appearance/gallery — категории и картинки (видно всем).
func (h *appearanceHandlers) gallery(w http.ResponseWriter, r *http.Request) {
	cats, err := h.store.ListGalleryCategories(r.Context())
	if err != nil {
		internalError(w)
		return
	}
	images, err := h.store.ListGalleryImages(r.Context())
	if err != nil {
		internalError(w)
		return
	}
	if cats == nil {
		cats = []store.GalleryCategory{}
	}
	if images == nil {
		images = []store.GalleryImage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": cats, "images": images})
}

// POST /admin/gallery/categories
func (h *appearanceHandlers) createGalleryCategory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		ParentID *int64 `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len([]rune(req.Name)) > 100 {
		badRequest(w, "name is required (1-100 chars)")
		return
	}
	c, err := h.store.CreateGalleryCategory(r.Context(), req.Name, req.ParentID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"category": c})
}

func (h *appearanceHandlers) updateGalleryCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid category id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		badRequest(w, "name is required")
		return
	}
	switch err := h.store.RenameGalleryCategory(r.Context(), id, strings.TrimSpace(req.Name)); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (h *appearanceHandlers) deleteGalleryCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid category id")
		return
	}
	switch err := h.store.DeleteGalleryCategory(r.Context(), id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// POST /admin/gallery/images — загрузка картинки в галерею.
//
// Миниатюру готовит браузер (canvas) и присылает вместе с оригиналом: в
// проекте нет обработки изображений на сервере, и тянуть библиотеку ради
// превью не хочется. Без превью экран выбора фона тянул бы десятки мегабайт.
func (h *appearanceHandlers) uploadGalleryImage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		badRequest(w, "expected multipart/form-data")
		return
	}
	dir := filepath.Join(h.dataDir, galleryDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		internalError(w)
		return
	}

	name, err := saveUploadedImage(r, "file", dir, "")
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	thumb, err := saveUploadedImage(r, "thumb", dir, "t_")
	if err != nil {
		thumb = "" // превью необязательно: покажем оригинал
	}

	var categoryID *int64
	if v := r.FormValue("category_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			categoryID = &id
		}
	}
	img, err := h.store.AddGalleryImage(r.Context(), user.ID, store.GalleryImage{
		CategoryID: categoryID, Filename: name, Thumb: thumb,
		Title: strings.TrimSpace(r.FormValue("title")),
	})
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"image": img})
}

func (h *appearanceHandlers) updateGalleryImage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid image id")
		return
	}
	var req struct {
		Title      *string `json:"title"`
		CategoryID *int64  `json:"category_id"`
		MoveRoot   bool    `json:"move_to_root"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	setCat := req.CategoryID != nil || req.MoveRoot
	switch err := h.store.UpdateGalleryImage(r.Context(), id, req.Title, req.CategoryID, setCat); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "image not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (h *appearanceHandlers) deleteGalleryImage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid image id")
		return
	}
	filename, thumb, err := h.store.DeleteGalleryImage(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "image not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	dir := filepath.Join(h.dataDir, galleryDir)
	for _, f := range []string{filename, thumb} {
		if f != "" {
			_ = os.Remove(filepath.Join(dir, filepath.Base(f)))
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
