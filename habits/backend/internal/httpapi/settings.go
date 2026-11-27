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
	"time"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/store"
)

const maxBackgroundBytes = 5 << 20 // 5 MB

type settingsHandlers struct {
	store   *store.Store
	dataDir string
}

func (h *settingsHandlers) backgroundsDir() string {
	return filepath.Join(h.dataDir, "backgrounds")
}

// bgURL — путь картинки относительно корня приложения (клиент добавит BASE_URL).
func bgURL(filename string) string {
	return "uploads/backgrounds/" + filename
}

type backgroundResponse struct {
	Kind        string               `json:"kind"`
	URL         string               `json:"url"`
	Position    string               `json:"position"`
	Blur        int32                `json:"blur"`
	Dim         int32                `json:"dim"`
	TextDark    string               `json:"text_dark"`
	TextLight   string               `json:"text_light"`
	BgDark      string               `json:"bg_dark"`
	BgLight     string               `json:"bg_light"`
	CardOpacity int32                `json:"card_opacity"`
	CardBlur    int32                `json:"card_blur"`
	Images      []backgroundImage    `json:"images"`
	Auto        store.BackgroundAuto `json:"auto"`
}

type backgroundImage struct {
	ID       int64  `json:"id"`
	URL      string `json:"url"`
	FolderID *int64 `json:"folder_id"`
	Thumb    string `json:"thumb"`
	// вес файла в байтах: из него страница настроек считает занятое место,
	// а просмотрщик показывает размер картинки. Колонки в базе нет намеренно —
	// файл на диске не соврёт, а os.Stat на десяток картинок ничего не стоит.
	Size int64 `json:"size"`
}

// GET /settings/notifications — что бот присылает и когда молчит.
func (h *settingsHandlers) getNotifyPrefs(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	p, err := h.store.GetNotifyPrefs(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *settingsHandlers) setNotifyPrefs(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req store.NotifyPrefs
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.QuietFrom < 0 || req.QuietFrom > 1439 || req.QuietTo < 0 || req.QuietTo > 1439 {
		badRequest(w, "quiet_from and quiet_to must be minutes 0-1439")
		return
	}
	if req.TzOff < -14*60 || req.TzOff > 14*60 {
		badRequest(w, "tz_off must be minutes -840..840")
		return
	}
	if len(req.Off) > 50 {
		badRequest(w, "too many kinds")
		return
	}
	if err := h.store.SetNotifyPrefs(r.Context(), user.ID, req); err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// GET /settings/export — все данные пользователя одним файлом.
func (h *settingsHandlers) exportData(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	data, err := h.store.ExportUserData(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	body := map[string]any{
		"app":      "habits",
		"kind":     "export",
		"version":  1,
		"user_id":  user.ID,
		"exported": time.Now().UTC().Format(time.RFC3339),
		"tables":   data,
		"note":     "Файлы (картинки фонов, вложения, сейф) сюда не входят — это выгрузка базы.",
	}
	w.Header().Set("Content-Disposition",
		`attachment; filename="habits-export-`+time.Now().Format("2006-01-02")+`.json"`)
	writeJSON(w, http.StatusOK, body)
}

// POST /settings/account/delete — удаление аккаунта со всеми данными.
// POST, а не DELETE: тело с подтверждением у DELETE поддерживают не все
// клиенты и прокси.
// Требует точного слова в теле: случайный запрос не должен ничего стереть.
func (h *settingsHandlers) deleteAccount(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Confirm string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Confirm) != "УДАЛИТЬ" {
		badRequest(w, "confirm must be the word УДАЛИТЬ")
		return
	}
	files, err := h.store.DeleteAccount(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	for _, f := range files {
		_ = os.Remove(filepath.Join(h.backgroundsDir(), filepath.Base(f)))
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// GET /settings/profile — «кто я» и мои устройства (те же связки IP+устройство,
// что видит админ в чужой карточке; свои показываем и владельцу).
func (h *settingsHandlers) profile(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	p, devices, err := h.store.SelfProfile(r.Context(), user.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	if devices == nil {
		devices = []store.UserDevice{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": p, "devices": devices})
}

func (h *settingsHandlers) getBackground(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	resp, err := h.backgroundState(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// backgroundState собирает ответ о фоне: его отдают и GET, и автосмена.
func (h *settingsHandlers) backgroundState(ctx context.Context, userID int64) (backgroundResponse, error) {
	bg, images, err := h.store.GetBackground(ctx, userID)
	if err != nil {
		return backgroundResponse{}, err
	}
	auto, err := h.store.GetBackgroundAuto(ctx, userID)
	if err != nil {
		return backgroundResponse{}, err
	}
	resp := backgroundResponse{
		Auto: auto,
		Kind: bg.Kind, Position: bg.Position, Blur: bg.Blur, Dim: bg.Dim,
		TextDark: bg.TextDark, TextLight: bg.TextLight,
		BgDark: bg.BgDark, BgLight: bg.BgLight,
		CardOpacity: bg.CardOpacity, CardBlur: bg.CardBlur, Images: []backgroundImage{},
	}
	switch bg.Kind {
	case "file":
		resp.URL = bgURL(bg.Value)
	case "url":
		resp.URL = bg.Value
	}
	for _, img := range images {
		thumb := ""
		if img.Thumb != "" {
			thumb = bgURL(img.Thumb)
		}
		var size int64
		if st, err := os.Stat(filepath.Join(h.backgroundsDir(), img.Filename)); err == nil {
			size = st.Size()
		}
		resp.Images = append(resp.Images, backgroundImage{
			ID: img.ID, URL: bgURL(img.Filename), FolderID: img.FolderID, Thumb: thumb, Size: size})
	}
	return resp, nil
}

// PUT /settings/background/auto — режим автосмены и папки-источники.
func (h *settingsHandlers) setBackgroundAuto(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req store.BackgroundAuto
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Mode != "off" && req.Mode != "open" && req.Mode != "daily" {
		badRequest(w, "mode must be off|open|daily")
		return
	}
	if err := h.store.SetBackgroundAuto(r.Context(), user.ID, req); err != nil {
		internalError(w)
		return
	}
	resp, err := h.backgroundState(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// POST /settings/background/rotate — взять следующую картинку прямо сейчас.
// Решение «пора ли» принимает клиент (он знает тему и что приложение
// открылось), а выбор картинки — сервер: иначе два устройства в один день
// выбрали бы разные и перетёрли бы друг друга.
func (h *settingsHandlers) rotateBackground(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	scheme := r.URL.Query().Get("scheme")
	if scheme != "light" {
		scheme = "dark"
	}
	// mode=daily — менять не чаще раза в сутки; open и force меняют сразу
	onlyNewDay := r.URL.Query().Get("mode") == "daily"
	if _, err := h.store.RotateBackground(r.Context(), user.ID, scheme, onlyNewDay); err != nil {
		internalError(w)
		return
	}
	resp, err := h.backgroundState(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *settingsHandlers) uploadBackground(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, maxBackgroundBytes)
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

	if err := os.MkdirAll(h.backgroundsDir(), 0o755); err != nil {
		internalError(w)
		return
	}
	dst, err := os.Create(filepath.Join(h.backgroundsDir(), filename))
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

	img, err := h.store.AddBackgroundImage(r.Context(), user.ID, filename)
	if err != nil {
		os.Remove(dst.Name())
		internalError(w)
		return
	}
	// уменьшенную копию готовит браузер: на сервере обработки картинок нет,
	// а без превью экран выбора фона тянул бы десятки мегабайт
	thumbURL := ""
	if thumb, err := saveUploadedImage(r, "thumb", h.backgroundsDir(), "t_"); err == nil {
		if err := h.store.SetBackgroundImageThumb(r.Context(), user.ID, img.ID, thumb); err == nil {
			thumbURL = bgURL(thumb)
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"image": backgroundImage{ID: img.ID, URL: bgURL(img.Filename), Thumb: thumbURL},
	})
}

func (h *settingsHandlers) setBackground(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Kind        string `json:"kind"`
		ImageID     *int64 `json:"image_id"`
		URL         string `json:"url"`
		Position    string `json:"position"`
		Blur        int32  `json:"blur"`
		Dim         int32  `json:"dim"`
		TextDark    string `json:"text_dark"`
		TextLight   string `json:"text_light"`
		BgDark      string `json:"bg_dark"`
		BgLight     string `json:"bg_light"`
		CardOpacity *int32 `json:"card_opacity"`
		CardBlur    int32  `json:"card_blur"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	// card_opacity по умолчанию 100 (сплошной), если клиент не прислал
	cardOpacity := int32(100)
	if req.CardOpacity != nil {
		cardOpacity = *req.CardOpacity
	}
	if cardOpacity < 20 || cardOpacity > 100 {
		badRequest(w, "card_opacity must be 20-100")
		return
	}
	if req.CardBlur < 0 || req.CardBlur > 30 {
		badRequest(w, "card_blur must be 0-30")
		return
	}
	if req.Position == "" {
		req.Position = "cover"
	}
	if req.Position != "cover" && req.Position != "contain" &&
		req.Position != "repeat" && req.Position != "center" {
		badRequest(w, "position must be cover|contain|repeat|center")
		return
	}
	if req.Blur < 0 || req.Blur > 30 {
		badRequest(w, "blur must be 0-30")
		return
	}
	if req.Dim < -70 || req.Dim > 70 {
		badRequest(w, "dim must be -70..70")
		return
	}

	if !validColor(req.TextDark) || !validColor(req.TextLight) ||
		!validColor(req.BgDark) || !validColor(req.BgLight) {
		badRequest(w, "colors must be #rrggbb or empty")
		return
	}
	bg := store.BackgroundSettings{
		Kind: req.Kind, Position: req.Position, Blur: req.Blur, Dim: req.Dim,
		TextDark: req.TextDark, TextLight: req.TextLight,
		BgDark: req.BgDark, BgLight: req.BgLight,
		CardOpacity: cardOpacity, CardBlur: req.CardBlur,
	}
	switch req.Kind {
	case "none":
	case "file":
		if req.ImageID == nil {
			badRequest(w, "image_id is required for kind=file")
			return
		}
		filename, err := h.store.BackgroundImageFilename(r.Context(), user.ID, *req.ImageID)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "image not found")
			return
		} else if err != nil {
			internalError(w)
			return
		}
		bg.Value = filename
	case "url":
		u := strings.TrimSpace(req.URL)
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") || len(u) > 2000 {
			badRequest(w, "url must be http(s) and at most 2000 characters")
			return
		}
		bg.Value = u
	default:
		badRequest(w, "kind must be none|file|url")
		return
	}

	if err := h.store.SetBackground(r.Context(), user.ID, bg); err != nil {
		internalError(w)
		return
	}
	h.getBackground(w, r)
}

func (h *settingsHandlers) deleteBackgroundImage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid image id")
		return
	}
	filename, err := h.store.DeleteBackgroundImage(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "image not found")
	case err != nil:
		internalError(w)
	default:
		os.Remove(filepath.Join(h.backgroundsDir(), filepath.Base(filename)))
		w.WriteHeader(http.StatusNoContent)
	}
}

// GET /settings/collapsed — свёрнутые группы: {"checker":[ids],"tracker":[ids]}.
func (h *settingsHandlers) getCollapsed(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	raw, err := h.store.GetCollapsed(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"collapsed":`))
	w.Write(raw)
	w.Write([]byte(`}`))
}

// приложения, у которых есть свёрнутые блоки
var collapsedApps = map[string]bool{
	"checker": true, "tracker": true, "tasks": true, "reminders": true,
	"projects": true, "projects_cat": true, "food_plan_open": true, "settings": true,
	// main: id 1 — блок плиток на главной
	"main": true,
}

// PUT /settings/collapsed — полная замена списка для одного приложения.
func (h *settingsHandlers) setCollapsed(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		App string  `json:"app"`
		IDs []int64 `json:"ids"`
	}
	// food_plan_open — раскрытые дни плана питания (ключ planId*100 + day)
	// settings — свёрнутые блоки самих настроек (id 1 — «Оформление»)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !collapsedApps[req.App] {
		badRequest(w, "unknown app; ids must be an array")
		return
	}
	if len(req.IDs) > 1000 {
		badRequest(w, "too many ids")
		return
	}
	if err := h.store.SetCollapsedApp(r.Context(), user.ID, req.App, req.IDs); err != nil {
		internalError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validColor(c string) bool {
	if c == "" {
		return true
	}
	if len(c) != 7 || c[0] != '#' {
		return false
	}
	for _, r := range c[1:] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// --- оформление: тема ---

var themeValues = map[string]bool{"auto": true, "light": true, "dark": true}

// GET /settings/appearance — тема из Telegram + переопределение для браузера.
func (h *settingsHandlers) getAppearance(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	a, err := h.store.GetAppearance(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// PUT /settings/appearance — куда писать, решает вид сессии: из Telegram
// меняется основная тема, из браузера/расширения (токен) — только их
// собственные настройки. Так они не влияют друг на друга.
func (h *settingsHandlers) setAppearance(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Theme string `json:"theme"`
		BgOff *bool  `json:"bg_off"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if user.TokenSession {
		// '' — «как в Telegram»
		if req.Theme != "" && !themeValues[req.Theme] {
			badRequest(w, "theme must be '', auto, light or dark")
			return
		}
		bgOff := false
		if req.BgOff != nil {
			bgOff = *req.BgOff
		}
		if err := h.store.SetWebAppearance(r.Context(), user.ID, req.Theme, bgOff); err != nil {
			internalError(w)
			return
		}
	} else {
		if !themeValues[req.Theme] {
			badRequest(w, "theme must be auto, light or dark")
			return
		}
		if err := h.store.SetTelegramTheme(r.Context(), user.ID, req.Theme); err != nil {
			internalError(w)
			return
		}
	}
	h.getAppearance(w, r)
}

// закреплённые заголовки: отдельная пара ручек, а не часть appearance.
// У appearance запись зависит от вида сессии (Telegram против токена), а эта
// настройка общая для всех режимов входа — мешать их в одном обработчике
// значит напрашиваться на ошибку.
const maxPinnedPages = 64

func (h *settingsHandlers) getHeaders(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	hs, err := h.store.GetHeaderSettings(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, hs)
}

func (h *settingsHandlers) setHeaders(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req store.HeaderSettings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if len(req.Pages) > maxPinnedPages {
		badRequest(w, "too many pages")
		return
	}
	// имена роутов, а не произвольный текст: не даём складывать в настройки мусор
	seen := make(map[string]bool, len(req.Pages))
	pages := make([]string, 0, len(req.Pages))
	for _, p := range req.Pages {
		if p == "" || len(p) > 64 || seen[p] {
			continue
		}
		seen[p] = true
		pages = append(pages, p)
	}
	req.Pages = pages
	if err := h.store.SetHeaderSettings(r.Context(), user.ID, req); err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, req)
}
