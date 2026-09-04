package httpapi

import (
	"archive/zip"
	"bytes"
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

type testsHandlers struct {
	store   *store.Store
	dataDir string
}

// картинки вопросов: DATA_DIR/tests/<файл>, раздаются публично из /uploads/tests/
const testsImageDir = "tests"

// максимальный размер импорта: 1032 вопроса ≈ 500 КБ JSON, картинки ≈ 14 МБ
const maxImportSize = 64 << 20

// GET /tests/decks — колоды со сводкой прогресса + активный прогон по каждой.
func (h *testsHandlers) listDecks(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	decks, err := h.store.ListTestDecks(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if decks == nil {
		decks = []store.TestDeck{}
	}
	// незавершённый прогон — чтобы на странице была кнопка «продолжить»
	active := map[int64]store.TestSession{}
	for _, d := range decks {
		if s, err := h.store.ActiveTestSession(r.Context(), user.ID, d.ID); err == nil {
			active[d.ID] = s
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"decks": decks, "active": active})
}

// GET /tests/decks/{id}/groups — темы колоды со статистикой.
func (h *testsHandlers) listGroups(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	deckID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid deck id")
		return
	}
	groups, err := h.store.ListTestGroups(r.Context(), user.ID, deckID)
	if err != nil {
		internalError(w)
		return
	}
	if groups == nil {
		groups = []store.TestGroup{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

// POST /tests/sessions — начать прогон.
func (h *testsHandlers) startSession(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		DeckID  int64  `json:"deck_id"`
		Mode    string `json:"mode"`  // study | exam
		Scope   string `json:"scope"` // unpassed | all | wrong | group
		GroupID *int64 `json:"group_id"`
		Limit   int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Mode == "" {
		req.Mode = "study"
	}
	if req.Scope == "" {
		req.Scope = "unpassed"
	}
	if req.Mode != "study" && req.Mode != "exam" {
		badRequest(w, "mode must be study|exam")
		return
	}
	switch req.Scope {
	case "unpassed", "all", "wrong", "group":
	default:
		badRequest(w, "scope must be unpassed|all|wrong|group")
		return
	}
	if req.Scope == "group" && req.GroupID == nil {
		badRequest(w, "group_id is required for scope=group")
		return
	}

	deck, err := h.store.TestDeckByID(r.Context(), req.DeckID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "deck not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}

	p := store.StartTestSessionParams{
		DeckID: req.DeckID, Mode: req.Mode, Scope: req.Scope,
		GroupID: req.GroupID, Limit: req.Limit,
	}
	if req.Mode == "exam" {
		// экзамен всегда по правилам колоды: весь пул, размер и таймер оттуда
		p.Scope = "all"
		p.Limit = deck.ExamSize
		p.Minutes = deck.ExamMinutes
	}
	// один активный прогон на колоду: старый закрываем
	if err := h.store.AbandonTestSessions(r.Context(), user.ID, req.DeckID); err != nil {
		internalError(w)
		return
	}
	sess, err := h.store.StartTestSession(r.Context(), user.ID, p)
	switch {
	case errors.Is(err, store.ErrNoQuestions):
		writeError(w, http.StatusConflict, "no_questions",
			"в этом наборе не осталось вопросов — начните колоду сначала")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"session": sess})
	}
}

// GET /tests/sessions/{id} — прогон + текущий вопрос (без правильного ответа).
func (h *testsHandlers) getSession(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid session id")
		return
	}
	sess, err := h.store.TestSessionByID(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	out := map[string]any{"session": sess}
	if sess.FinishedAt == nil {
		q, pos, err := h.store.NextTestQuestion(r.Context(), sess.ID)
		if err == nil {
			out["question"] = q
			out["position"] = pos
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /tests/sessions/{id}/answer — ответ на вопрос.
// Правильный вариант приходит В ОТВЕТЕ, а не заранее: иначе его видно в
// девтулзах, и учебный режим теряет смысл.
func (h *testsHandlers) answer(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid session id")
		return
	}
	var req struct {
		QuestionID int64 `json:"question_id"`
		Chosen     int   `json:"chosen"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Chosen < 0 {
		badRequest(w, "chosen must be >= 0")
		return
	}
	streak, err := h.store.GetTestsPassStreak(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	res, err := h.store.AnswerTestQuestion(r.Context(), user.ID, id, req.QuestionID, req.Chosen, streak)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "question is not in this session or already answered")
	case err != nil:
		internalError(w)
	default:
		out := map[string]any{
			"correct":     res.Correct,
			"correct_idx": res.CorrectIdx,
			"status":      res.Status,
			"session":     res.Session,
		}
		if res.Session.FinishedAt == nil {
			if q, pos, err := h.store.NextTestQuestion(r.Context(), id); err == nil {
				out["next"] = q
				out["position"] = pos
			}
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// POST /tests/sessions/{id}/finish — завершить прогон досрочно.
func (h *testsHandlers) finishSession(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid session id")
		return
	}
	sess, err := h.store.FinishTestSession(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "session not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"session": sess})
	}
}

// GET /tests/sessions/{id}/review — разбор прогона (виден правильный ответ).
func (h *testsHandlers) review(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid session id")
		return
	}
	if _, err := h.store.TestSessionByID(r.Context(), user.ID, id); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	items, err := h.store.TestSessionItems(r.Context(), id)
	if err != nil {
		internalError(w)
		return
	}
	if items == nil {
		items = []store.TestSessionReview{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// POST /tests/decks/{id}/reset — начать колоду сначала.
// По умолчанию мягкий сброс: статистика ответов сохраняется, снимается только
// отметка «пройден». hard=true стирает прогресс целиком.
func (h *testsHandlers) reset(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	deckID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid deck id")
		return
	}
	var req struct {
		Hard bool `json:"hard"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // тело необязательно
	n, err := h.store.ResetTestProgress(r.Context(), user.ID, deckID, req.Hard)
	if err != nil {
		internalError(w)
		return
	}
	if err := h.store.AbandonTestSessions(r.Context(), user.ID, deckID); err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reset": n})
}

// GET /tests/settings, PUT /tests/settings — настройки страницы.
func (h *testsHandlers) getSettings(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	n, err := h.store.GetTestsPassStreak(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pass_streak": n})
}

func (h *testsHandlers) setSettings(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		PassStreak int `json:"pass_streak"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.PassStreak < 1 || req.PassStreak > 5 {
		badRequest(w, "pass_streak must be between 1 and 5")
		return
	}
	if err := h.store.SetTestsPassStreak(r.Context(), user.ID, req.PassStreak); err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pass_streak": req.PassStreak})
}

// POST /admin/tests/import — импорт колоды.
// multipart: поле deck (JSON) и необязательное images (zip с картинками).
// Идемпотентно по (deck.slug, question.num) — переимпорт обновляет тексты,
// не сбрасывая прогресс пользователей.
func (h *testsHandlers) importDeck(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		badRequest(w, "expected multipart/form-data with deck field")
		return
	}
	raw := []byte(r.FormValue("deck"))
	if len(raw) == 0 {
		if f, _, err := r.FormFile("deck"); err == nil {
			defer f.Close()
			raw, _ = io.ReadAll(io.LimitReader(f, maxImportSize))
		}
	}
	if len(raw) == 0 {
		badRequest(w, "deck is required")
		return
	}
	var deck store.ImportDeck
	if err := json.Unmarshal(raw, &deck); err != nil {
		badRequest(w, "deck: invalid JSON")
		return
	}
	if deck.Slug == "" || deck.Title == "" || len(deck.Questions) == 0 {
		badRequest(w, "deck must have slug, title and questions")
		return
	}
	for _, q := range deck.Questions {
		if len(q.Options) < 2 {
			badRequest(w, "question "+strconv.Itoa(q.Num)+": at least 2 options required")
			return
		}
		if q.CorrectIdx < 0 || q.CorrectIdx >= len(q.Options) {
			badRequest(w, "question "+strconv.Itoa(q.Num)+": correct_idx out of range")
			return
		}
	}

	images := 0
	if f, hdr, err := r.FormFile("images"); err == nil {
		defer f.Close()
		buf, rErr := io.ReadAll(io.LimitReader(f, maxImportSize))
		if rErr != nil {
			badRequest(w, "images: read error")
			return
		}
		n, uErr := h.unpackImages(buf, hdr.Size)
		if uErr != nil {
			badRequest(w, "images: "+uErr.Error())
			return
		}
		images = n
	}

	deckID, inserted, updated, err := h.store.ImportTestDeck(r.Context(), deck)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"deck_id": deckID, "inserted": inserted, "updated": updated, "images": images,
	})
}

// unpackImages распаковывает zip с картинками в DATA_DIR/tests.
// Имена берём только базовые — защита от путей вида ../../etc.
func (h *testsHandlers) unpackImages(buf []byte, size int64) (int, error) {
	zr, err := zip.NewReader(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		return 0, errors.New("not a zip archive")
	}
	dir := filepath.Join(h.dataDir, testsImageDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	n := 0
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		name := filepath.Base(zf.Name)
		if name == "." || name == ".." || strings.HasPrefix(name, ".") {
			continue
		}
		switch strings.ToLower(filepath.Ext(name)) {
		case ".webp", ".png", ".jpg", ".jpeg", ".gif", ".svg":
		default:
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return n, err
		}
		data, err := io.ReadAll(io.LimitReader(rc, 8<<20))
		rc.Close()
		if err != nil {
			return n, err
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
