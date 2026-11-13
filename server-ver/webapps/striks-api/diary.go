package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	zlog "github.com/rs/zerolog/log"
)

const MaxTelegramFileSize = 10 * 1024 * 1024 // 10 MB

type DiaryEntry struct {
	ID   int    `json:"id"`
	Date string `json:"date"`
	Text string `json:"text"`
}

func saveDiaryEntry(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")

	var entry DiaryEntry
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	if entry.Date == "" {
		entry.Date = time.Now().Format("2006-01-02")
	}

	_, err := db.Exec(`INSERT INTO diary (user, date, text) VALUES (?, ?, ?)`,
		user, entry.Date, entry.Text)
	if err != nil {
		log.Println("saveDiaryEntry:", err)
		http.Error(w, "DB error", 500)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func getDiaryEntries(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")

	query := `SELECT id, date, text FROM diary WHERE user = ?`
	args := []interface{}{user}

	if from != "" {
		query += " AND date >= ?"
		args = append(args, from)
	}
	if to != "" {
		query += " AND date <= ?"
		args = append(args, to)
	}

	query += " ORDER BY date DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var entries []DiaryEntry
	for rows.Next() {
		var e DiaryEntry
		rows.Scan(&e.ID, &e.Date, &e.Text)
		entries = append(entries, e)
	}

	json.NewEncoder(w).Encode(entries)
}

func searchDiaryEntries(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	q := r.URL.Query().Get("q")

	rows, err := db.Query(`SELECT id, date, text FROM diary 
        WHERE user = ? AND text LIKE ? ORDER BY date DESC`, user, "%"+q+"%")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var entries []DiaryEntry
	for rows.Next() {
		var e DiaryEntry
		rows.Scan(&e.ID, &e.Date, &e.Text)
		entries = append(entries, e)
	}

	json.NewEncoder(w).Encode(entries)
}

func updateDiaryEntry(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var entry DiaryEntry
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	_, err := db.Exec(`UPDATE diary SET date = ?, text = ? WHERE id = ?`,
		entry.Date, entry.Text, id)
	if err != nil {
		log.Println("updateDiaryEntry:", err)
		http.Error(w, "DB error", 500)
		return
	}

	w.WriteHeader(http.StatusOK)
}
func deleteDiaryEntry(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	_, err := db.Exec(`DELETE FROM diary WHERE id = ?`, id)
	if err != nil {
		log.Println("deleteDiaryEntry:", err)
		http.Error(w, "DB error", 500)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GET /api/resagerhelper/diary/export?from=2025-10-01&to=2025-10-09&type=csv|txt
func (rd *RouteData) exportDiaryHandler(w http.ResponseWriter, r *http.Request) {
	userStr := r.URL.Query().Get("user")
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	fileType := r.URL.Query().Get("type")

	if userStr == "" {
		http.Error(w, "missing user", 400)
		return
	}

	userID, err := strconv.ParseInt(userStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid user ID (must be numeric Telegram ID)", 400)
		return
	}

	rows, err := db.Query(`SELECT date, text FROM diary WHERE user = ? AND date BETWEEN ? AND ? ORDER BY date ASC`, userStr, from, to)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var entries []struct {
		Date string
		Text string
	}
	for rows.Next() {
		var e struct {
			Date string
			Text string
		}
		rows.Scan(&e.Date, &e.Text)
		entries = append(entries, e)
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
		http.Error(w, "unknown type", 400)
		return
	}

	fileData := buf.Bytes()

	if len(fileData) <= MaxTelegramFileSize {
		if err := rd.bot.SendDoc(userID, filename, fileData); err != nil {
			zlog.Error().Err(err).Msg("Failed to send diary file via Telegram")
			http.Error(w, "failed to send via Telegram", 500)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("вам отправлено в телеграм"))
	} else {
		url, err := saveFileAndGenerateURL(filename, fileData)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to save large diary file")
			http.Error(w, "failed to save file", 500)
			return
		}
		if err := rd.bot.SendText(userID, fmt.Sprintf("Скачайте его по ссылке:\n%s", url)); err != nil {
			zlog.Error().Err(err).Msg("Failed to send diary file via Telegram")
			http.Error(w, "failed to send via Telegram", 500)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("вам отправлена ссылка на файл"))
	}
}
