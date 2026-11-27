package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const (
	bgOptionName    = "backgrounds"
	themeOptionName = "theme"
)

// POST /api/resagerhelper/settings/background
func (rd *RouteData) saveBackgroundHandler(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	position := r.FormValue("position")
	preset := r.FormValue("preset")
	id := r.FormValue("id")

	// Если передан ID — просто активируем существующий фон
	if id != "" {
		if err := rd.repo.UpdateActiveSetting(user, bgOptionName, id); err != nil {
			rd.log.Error("saveBackgroundHandler: update active", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	// Иначе — загружаем новый файл или используем preset
	var bgURL string

	file, header, err := r.FormFile("file")
	if err == nil {
		defer file.Close()
		dir := "uploads/backgrounds"
		if err := os.MkdirAll(dir, 0755); err != nil {
			rd.log.Error("saveBackgroundHandler: mkdir", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		path := filepath.Join(dir, fmt.Sprintf("%s_%s", user, header.Filename))
		out, err := os.Create(path)
		if err != nil {
			rd.log.Error("saveBackgroundHandler: create file", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer out.Close()
		if _, err := io.Copy(out, file); err != nil {
			rd.log.Error("saveBackgroundHandler: copy file", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		bgURL = "/" + path
	} else if preset != "" {
		bgURL = preset
	} else {
		http.Error(w, "missing file or preset", http.StatusBadRequest)
		return
	}

	if err := rd.repo.SaveSetting(user, bgOptionName, bgURL, position); err != nil {
		rd.log.Error("saveBackgroundHandler: save setting", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GET /api/resagerhelper/settings/background
func (rd *RouteData) getBackgroundHandler(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	active, err := rd.repo.GetActiveSetting(user, bgOptionName)
	if err != nil {
		rd.log.Error("getBackgroundHandler: get active", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	all, err := rd.repo.GetAllSettings(user, bgOptionName)
	if err != nil {
		rd.log.Error("getBackgroundHandler: get all", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	urls := make(map[int]string)
	for _, s := range all {
		urls[s.ID] = s.Value
	}

	response := map[string]interface{}{
		"url":      "",
		"position": "",
		"urls":     urls,
	}
	if active != nil {
		response["url"] = active.Value
		response["position"] = active.Options
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// DELETE /api/resagerhelper/settings/background
func (rd *RouteData) deleteBackgroundHandler(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	id := r.FormValue("id")
	if err := rd.repo.DeleteSetting(user, bgOptionName, id); err != nil {
		rd.log.Error("deleteBackgroundHandler", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// POST /api/resagerhelper/settings/theme
func (rd *RouteData) saveThemeHandler(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "Missing user parameter", http.StatusBadRequest)
		return
	}

	var req struct{ Theme string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := rd.repo.UpsertSetting(user, themeOptionName, req.Theme); err != nil {
		rd.log.Error("saveThemeHandler", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GET /api/resagerhelper/settings/theme
func (rd *RouteData) getThemeHandler(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	fmt.Println("cp test5")
	var theme string
	us, err := rd.repo.GetActiveSetting(user, themeOptionName)
	//err := rd.repo.db.QueryRow(`
	//	SELECT value FROM user_settings
	//	WHERE user = $1 AND name = $2`,
	//	user, themeOptionName).Scan(&theme)
	if err != nil {
		rd.log.Error("getThemeHandler", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	fmt.Println("test56", us)
	if us != nil {
		theme = us.Value
	}
	// Если не найдено — theme останется пустой (по умолчанию)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"theme": theme})
	w.WriteHeader(http.StatusOK)
}
