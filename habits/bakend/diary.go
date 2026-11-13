package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	zlog "github.com/rs/zerolog/log"
)

const MaxTelegramFileSize = 10 * 1024 * 1024 // 10 MB

// POST /api/resagerhelper/diary
func (rd *RouteData) saveDiaryEntry(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	var req struct {
		Date string `json:"date"`
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	date := req.Date
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	if err := rd.repo.CreateDiaryEntry(user, date, req.Text); err != nil {
		rd.log.Error("saveDiaryEntry", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GET /api/resagerhelper/diary
func (rd *RouteData) getDiaryEntries(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")

	entries, err := rd.repo.GetDiaryEntries(user, from, to)
	if err != nil {
		rd.log.Error("getDiaryEntries", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

// GET /api/resagerhelper/diary/search
func (rd *RouteData) searchDiaryEntries(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "missing q", http.StatusBadRequest)
		return
	}

	entries, err := rd.repo.SearchDiaryEntries(user, q)
	if err != nil {
		rd.log.Error("searchDiaryEntries", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

// PUT /api/resagerhelper/diary/{id}
func (rd *RouteData) updateDiaryEntry(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr := vars["id"]

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var req struct {
		Date string `json:"date"`
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := rd.repo.UpdateDiaryEntry(id, req.Date, req.Text); err != nil {
		rd.log.Error("updateDiaryEntry", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// DELETE /api/resagerhelper/diary/{id}
func (rd *RouteData) deleteDiaryEntry(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr := vars["id"]

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := rd.repo.DeleteDiaryEntry(id); err != nil {
		rd.log.Error("deleteDiaryEntry", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GET /api/resagerhelper/diary/export?from=...&to=...&type=...
func (rd *RouteData) exportDiaryHandler(w http.ResponseWriter, r *http.Request) {
	userStr := r.URL.Query().Get("user")
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	fileType := r.URL.Query().Get("type")

	if userStr == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	userID, err := strconv.ParseInt(userStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid user ID (must be numeric Telegram ID)", http.StatusBadRequest)
		return
	}

	entries, err := rd.repo.GetDiaryEntriesForExport(userStr, from, to)
	if err != nil {
		rd.log.Error("exportDiaryHandler: load entries", "error", err)
		http.Error(w, "failed to load data", http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	var filename string

	switch fileType {
	case "csv":
		filename = "diary.csv"
		writer := csv.NewWriter(&buf)
		writer.Write([]string{"Дата", "Текст"})
		for _, e := range entries {
			writer.Write([]string{e.Date, e.Text})
		}
		writer.Flush()
	case "txt":
		filename = "diary.txt"
		for _, e := range entries {
			fmt.Fprintf(&buf, "%s\n%s\n\n", e.Date, e.Text)
		}
	default:
		http.Error(w, "unknown type (use 'csv' or 'txt')", http.StatusBadRequest)
		return
	}

	fileData := buf.Bytes()

	if len(fileData) <= MaxTelegramFileSize {
		if err := rd.bot.SendDoc(userID, filename, fileData); err != nil {
			zlog.Error().Err(err).Msg("Failed to send diary file via Telegram")
			http.Error(w, "failed to send via Telegram", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("вам отправлено в телеграм"))
	} else {
		url, err := saveFileAndGenerateURL(filename, fileData)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to save large diary file")
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

// возможно заменить
// saveFileAndGenerateURL сохраняет файл на сервер и возвращает публичный URL.
func saveFileAndGenerateURL(filename string, fileData []byte) (string, error) {
	// Уникальное имя файла
	timestamp := time.Now().Unix()
	safeName := strings.ReplaceAll(filename, "/", "_")
	safeName = strings.ReplaceAll(safeName, "\\", "_")
	fullPath := filepath.Join("./uploads/", fmt.Sprintf("%d_%s", timestamp, safeName))

	err := os.WriteFile(fullPath, fileData, 0644)
	if err != nil {
		return "", err
	}

	// Предполагаем, что сервер раздаёт /uploads/ по корню
	// Например: http://example.com/uploads/12345_checks_user123.csv
	publicURL := fmt.Sprintf("/uploads/%s", filepath.Base(fullPath))
	return publicURL, nil
}
