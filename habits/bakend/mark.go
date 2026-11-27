package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	zlog "github.com/rs/zerolog/log"

	"habits/internal/repository"
)

func (rd *RouteData) handleGet(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	categories, marks, err := rd.repo.GetUserData(user)
	if err != nil {
		rd.log.Error("handleGet: get user data", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	data := repository.Data{
		Categories: categories,
		Marks:      marks,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		rd.log.Error("handleGet: encode response", "error", err)
	}
}

func (rd *RouteData) handleSetCategories(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	var body struct {
		Categories []string `json:"categories"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := rd.repo.SetCategories(user, body.Categories); err != nil {
		rd.log.Error("handleSetCategories", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

func (rd *RouteData) handleToggle(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	var body struct {
		Category string `json:"category"`
		Date     string `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := rd.repo.ToggleMark(user, body.Category, body.Date); err != nil {
		rd.log.Error("handleToggle", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "time": time.Now()})
}

func (rd *RouteData) handleGetMarks(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	category := r.URL.Query().Get("category")
	month := r.URL.Query().Get("month")

	if user == "" || category == "" || month == "" {
		http.Error(w, "missing user, category or month", http.StatusBadRequest)
		return
	}

	days, err := rd.repo.GetMarksByMonth(user, category, month)
	if err != nil {
		rd.log.Error("handleGetMarks", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"days": days})
}

func (rd *RouteData) handleSetColor(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	var body struct {
		Category string `json:"category"`
		Color    string `json:"color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := rd.repo.SetCategoryColor(user, body.Category, body.Color); err != nil {
		rd.log.Error("handleSetColor", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

// ⚠️ Исправлено: вы передавали 3 значения, но указали 2 колонки.
// Теперь обновляем имя категории в таблице `categories` и цвет в `category_colors`.
func (rd *RouteData) handleSetName(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	var body struct {
		Category string `json:"category"` // старое имя
		Name     string `json:"name"`     // новое имя
		Color    string `json:"color"`    // опционально
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := rd.repo.SetCategoryName(user, body.Category, body.Name, body.Color); err != nil {
		rd.log.Error("handleSetName", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

// --- Checks ---

func (rd *RouteData) getChecks(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	groups, err := rd.repo.GetChecks(user)
	if err != nil {
		rd.log.Error("getChecks", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(groups)
}

func (rd *RouteData) addCheckGroup(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	var req struct{ Name string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := rd.repo.AddCheckGroup(user, req.Name); err != nil {
		rd.log.Error("addCheckGroup", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (rd *RouteData) toggleCheck(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	var req struct {
		Group string `json:"group"`
		Item  string `json:"item"`
		Done  bool   `json:"done"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := rd.repo.ToggleCheckItem(user, req.Group, req.Item, req.Done); err != nil {
		rd.log.Error("toggleCheck", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (rd *RouteData) renameCheckGroup(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	var req struct{ Old, New string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := rd.repo.RenameCheckGroup(user, req.Old, req.New); err != nil {
		rd.log.Error("renameCheckGroup", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (rd *RouteData) addCheckItem(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	var req struct {
		Group string `json:"group"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := rd.repo.AddCheckItem(user, req.Group, req.Name); err != nil {
		rd.log.Error("addCheckItem", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (rd *RouteData) deleteCheckGroup(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	var req struct{ Name string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := rd.repo.DeleteCheckGroup(user, req.Name); err != nil {
		rd.log.Error("deleteCheckGroup", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (rd *RouteData) deleteCheckItem(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	var req struct {
		Group string `json:"group"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := rd.repo.DeleteCheckItem(user, req.Group, req.Name); err != nil {
		rd.log.Error("deleteCheckItem", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (rd *RouteData) handleExportChecks(w http.ResponseWriter, r *http.Request) {
	userStr := r.URL.Query().Get("user")
	if userStr == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	userID, err := strconv.ParseInt(userStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid user ID (must be numeric Telegram ID)", http.StatusBadRequest)
		return
	}

	groups, err := rd.repo.GetChecks(userStr)
	if err != nil {
		rd.log.Error("handleExportChecks: load checks", "error", err)
		http.Error(w, "failed to load data", http.StatusInternalServerError)
		return
	}

	// Формируем CSV
	var data [][]string
	data = append(data, []string{"Group", "Item", "Checked"})
	for _, g := range groups {
		for _, item := range g.Items {
			data = append(data, []string{g.Name, item.Name, fmt.Sprintf("%t", item.Done)})
		}
	}

	var buf bytes.Buffer
	csvWriter := csv.NewWriter(&buf)
	for _, row := range data {
		_ = csvWriter.Write(row)
	}
	csvWriter.Flush()

	fileData := buf.Bytes()
	filename := fmt.Sprintf("checks_%s.csv", userStr)

	if len(fileData) <= MaxTelegramFileSize {
		if err := rd.bot.SendDoc(userID, filename, fileData); err != nil {
			zlog.Error().Err(err).Msg("Failed to send file via Telegram")
			http.Error(w, "failed to send via Telegram", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("вам отправлено в телеграм"))
	} else {
		url, err := saveFileAndGenerateURL(filename, fileData)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to save large file")
			http.Error(w, "failed to save file", http.StatusInternalServerError)
			return
		}
		if err := rd.bot.SendText(userID, fmt.Sprintf("Скачайте его по ссылке:\n%s", url)); err != nil {
			zlog.Error().Err(err).Msg("Failed to send diary file via Telegram")
			http.Error(w, "failed to send via Telegram", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("вам отправлена ссылка на файл"))
	}
}
