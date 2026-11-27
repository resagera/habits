package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// POST /api/resagerhelper/settings/background
func saveBackgroundHandler(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	position := r.FormValue("position")
	preset := r.FormValue("preset")

	var bgURL string

	file, header, err := r.FormFile("file")
	if err == nil {
		defer file.Close()
		os.MkdirAll("uploads/backgrounds", 0755)
		path := fmt.Sprintf("uploads/backgrounds/%s_%s", user, header.Filename)
		out, _ := os.Create(path)
		io.Copy(out, file)
		out.Close()
		bgURL = "/" + path
	} else if preset != "" {
		bgURL = "/static/backgrounds/" + preset
	}

	_, err = db.Exec(`INSERT INTO settings (user, bg_url, bg_position)
		VALUES (?, ?, ?)
		ON CONFLICT(user) DO UPDATE SET bg_url = ?, bg_position = ?`,
		user, bgURL, position, bgURL, position)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.WriteHeader(200)
}

// GET /api/resagerhelper/settings/background
func getBackgroundHandler(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	row := db.QueryRow(`SELECT bg_url, bg_position FROM settings WHERE user = ?`, user)
	var url, pos string
	row.Scan(&url, &pos)
	json.NewEncoder(w).Encode(map[string]string{"url": url, "position": pos})
}

// POST /api/resagerhelper/settings/theme
func saveThemeHandler(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	var req struct {
		Theme string `json:"theme"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	_, err := db.Exec(`INSERT INTO settings (user, theme)
		VALUES (?, ?)
		ON CONFLICT(user) DO UPDATE SET theme = ?`,
		user, req.Theme, req.Theme)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(200)
}

// GET /api/resagerhelper/settings/theme
func getThemeHandler(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	row := db.QueryRow(`SELECT theme FROM settings WHERE user = ?`, user)
	var theme string
	row.Scan(&theme)
	json.NewEncoder(w).Encode(map[string]string{"theme": theme})
}
