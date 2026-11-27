package httpapi

import (
	"encoding/json"
	"errors"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/games"
	"streaks-backend/internal/store"
)

type gamesHandlers struct {
	store   *store.Store
	dataDir string
}

// gameRules — какие игры существуют, что в них считается лучшим результатом и
// под что можно выбрать картинки.
//
// Higher=true — больше лучше (очки 2048), false — меньше лучше (секунды в
// пятнашках и «найди пару»). Slots — назначения картинок: «game» это фон всей
// игры, он есть у всех; «cards» — рубашка карт. Новая игра — одна строка.
var gameRules = map[string]struct {
	Higher bool
	Slots  []string
}{
	"2048":     {Higher: true, Slots: []string{"game"}},
	"fifteen":  {Higher: false, Slots: []string{"game"}},
	"pairs":    {Higher: false, Slots: []string{"game", "cards"}},
	"maze":     {Higher: false, Slots: []string{"game"}},
	"reaction": {Higher: false, Slots: []string{"game"}},
	// «мишени» — режим реакции, но мерка другая: попаданий больше — лучше,
	// поэтому отдельный код рекорда, а не вариант
	"targets": {Higher: true, Slots: []string{"game"}},
	"puzzle":  {Higher: false, Slots: []string{"game", "picture"}},
}

// texturesDir — плитки стен, которые пополняет админ (DATA_DIR/textures).
const texturesDir = "textures"

// Код игры может нести вариант через двоеточие: «maze:huge». Рекорд у каждого
// варианта свой (минута в маленьком лабиринте и минута в гигантском — разные
// достижения), а правила и наборы картинок общие для игры.
var variantRe = regexp.MustCompile(`^[a-z0-9_]{1,16}$`)

func splitGame(code string) (base, variant string, ok bool) {
	base, variant, found := strings.Cut(code, ":")
	if _, known := gameRules[base]; !known {
		return "", "", false
	}
	if found && !variantRe.MatchString(variant) {
		return "", "", false
	}
	return base, variant, true
}

func slotAllowed(game, slot string) bool {
	for _, s := range gameRules[game].Slots {
		if s == slot {
			return true
		}
	}
	return false
}

// максимум картинок на игру: это выбор из своих фонов, а не галерея
const maxGameBackgrounds = 24

type gameImage struct {
	ID    int64  `json:"id"`
	URL   string `json:"url"`
	Thumb string `json:"thumb"`
}

// GET /games — рекорды и картинки, выбранные под каждую игру.
func (h *gamesHandlers) state(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	scores, err := h.store.GameScores(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if scores == nil {
		scores = []store.GameScore{}
	}
	byGame, err := h.store.GameBackgrounds(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	var all []int64
	for _, slots := range byGame {
		for _, ids := range slots {
			all = append(all, ids...)
		}
	}
	files, err := h.store.GameImageFiles(r.Context(), user.ID, all)
	if err != nil {
		internalError(w)
		return
	}
	backgrounds := map[string]map[string][]gameImage{}
	for game, slots := range byGame {
		backgrounds[game] = map[string][]gameImage{}
		for slot, ids := range slots {
			list := make([]gameImage, 0, len(ids))
			for _, id := range ids {
				f, ok := files[id]
				if !ok {
					continue // картинку удалили между запросами
				}
				thumb := ""
				if f[1] != "" {
					thumb = bgURL(f[1])
				}
				list = append(list, gameImage{ID: id, URL: bgURL(f[0]), Thumb: thumb})
			}
			backgrounds[game][slot] = list
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"scores": scores, "backgrounds": backgrounds})
}

// POST /games/{game}/result — результат партии. Партия засчитывается всегда,
// рекорд обновляется, только если он лучше (сравнивает сам запрос).
func (h *gamesHandlers) result(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	game := r.PathValue("game")
	base, _, ok := splitGame(game)
	if !ok {
		badRequest(w, "unknown game")
		return
	}
	rule := gameRules[base]
	var req struct {
		Best   int32           `json:"best"`
		Detail json.RawMessage `json:"detail"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Best < 0 || req.Best > 10_000_000 {
		badRequest(w, "best is out of range")
		return
	}
	if len(req.Detail) > 512 {
		badRequest(w, "detail is too large")
		return
	}
	score, err := h.store.SaveGameResult(r.Context(), user.ID, game, req.Best, req.Detail, rule.Higher)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"score": score})
}

// PUT /games/{game}/backgrounds/{slot} — набор картинок одного назначения целиком.
func (h *gamesHandlers) setBackgrounds(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	game := r.PathValue("game")
	slot := r.PathValue("slot")
	if _, ok := gameRules[game]; !ok {
		badRequest(w, "unknown game") // наборы картинок общие для всех вариантов
		return
	}
	if !slotAllowed(game, slot) {
		badRequest(w, "unknown slot for this game")
		return
	}
	var req struct {
		ImageIDs []int64 `json:"image_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if len(req.ImageIDs) > maxGameBackgrounds {
		badRequest(w, "too many images")
		return
	}
	if err := h.store.SetGameBackgrounds(r.Context(), user.ID, game, slot, req.ImageIDs); err != nil {
		internalError(w)
		return
	}
	h.state(w, r)
}

// GET /games/maze?cols=&rows=&seed= — новый лабиринт нужного размера.
//
// Генерируем по запросу, а не храним: лабиринт на сорок тысяч клеток строится
// за единицы миллисекунд, а размер зависит от экрана игрока. Seed возвращаем,
// чтобы тот же лабиринт можно было повторить.
func (h *gamesHandlers) maze(w http.ResponseWriter, r *http.Request) {
	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	if cols <= 0 || rows <= 0 {
		badRequest(w, "cols and rows are required")
		return
	}
	seed, err := strconv.ParseInt(r.URL.Query().Get("seed"), 10, 64)
	if err != nil || seed == 0 {
		seed = rand.Int63()
	}
	writeJSON(w, http.StatusOK, games.Generate(cols, rows, seed))
}

// GET /games/textures?game=maze — набор текстур, который пополняет админ.
func (h *gamesHandlers) textures(w http.ResponseWriter, r *http.Request) {
	game := r.URL.Query().Get("game")
	if _, ok := gameRules[game]; !ok {
		badRequest(w, "unknown game")
		return
	}
	list, err := h.store.ListGameTextures(r.Context(), game)
	if err != nil {
		internalError(w)
		return
	}
	for i := range list {
		list[i].URL = "uploads/" + texturesDir + "/" + list[i].Filename
	}
	writeJSON(w, http.StatusOK, map[string]any{"textures": list})
}

// POST /admin/games/textures — загрузка плитки (multipart: game, title, file).
//
// Плитка размножается по стене как паттерн, поэтому важно, чтобы картинка
// была бесшовной; проверить это сервер не может — пишем в интерфейсе.
func (h *gamesHandlers) uploadTexture(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		badRequest(w, "expected multipart/form-data")
		return
	}
	game := r.FormValue("game")
	if _, ok := gameRules[game]; !ok {
		badRequest(w, "unknown game")
		return
	}
	dir := filepath.Join(h.dataDir, texturesDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		internalError(w)
		return
	}
	name, err := saveUploadedImage(r, "file", dir, "")
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if len(title) > 60 {
		title = title[:60]
	}
	t, err := h.store.AddGameTexture(r.Context(), user.ID, game, title, name)
	if err != nil {
		os.Remove(filepath.Join(dir, name))
		internalError(w)
		return
	}
	t.URL = "uploads/" + texturesDir + "/" + t.Filename
	writeJSON(w, http.StatusCreated, map[string]any{"texture": t})
}

// DELETE /admin/games/textures/{id}
func (h *gamesHandlers) deleteTexture(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid texture id")
		return
	}
	name, err := h.store.DeleteGameTexture(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "texture not found")
	case err != nil:
		internalError(w)
	default:
		_ = os.Remove(filepath.Join(h.dataDir, texturesDir, filepath.Base(name)))
		w.WriteHeader(http.StatusNoContent)
	}
}
