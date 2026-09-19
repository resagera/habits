package main

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	zlog "github.com/rs/zerolog/log"

	_ "github.com/mattn/go-sqlite3"

	"habits/bot"
	"habits/internal/logger"
	"habits/internal/version"
)

type Data struct {
	Categories []Category `json:"categories"`
	Marks      []Mark     `json:"marks"`
}

type Category struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Mark struct {
	Category string `json:"category"`
	Date     string `json:"date"`
}

var db *sql.DB

func main() {
	print("App version: " + version.Get().SemVer() + " " + runtime.Version() + " " + runtime.GOOS + "_" + runtime.GOARCH + "\n")
	showVer := flag.Bool("v", false, "show version")
	//debugMode := flag.Bool("debug", false, "debug mode")
	flag.Parse()
	if *showVer {
		os.Exit(0)
	}

	lg, err := logger.New(logger.Config{
		Level:   slog.LevelInfo,
		File:    "tg-webapp.log", // лог в файл
		Console: false,           // + вывод в консоль
		JSON:    true,            // текстовый формат
	})
	if err != nil {
		panic(err)
	}
	slog.SetDefault(lg)

	db, err = sql.Open("sqlite3", "./data.db")
	if err != nil {
		log.Fatal(err)
	}
	bot, err := tgBot.NewTgBot("${BOT_TOKEN}")
	initDB()
	router := NewRouter(bot, lg)

	slog.Info("Server v1.911 started on http://localhost:8080")
	logger.Fatal("Start server", http.ListenAndServe(":8080", router))
}

func handleGet(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", 400)
		return
	}

	var data Data

	// Получаем категории с цветами
	rows, err := db.Query(`
		SELECT c.name, IFNULL(cc.color, '#22c55e')
		FROM categories c
		LEFT JOIN category_colors cc ON c.user = cc.user AND c.name = cc.category
		WHERE c.user = ?`, user)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	for rows.Next() {
		var c Category
		rows.Scan(&c.Name, &c.Color)
		data.Categories = append(data.Categories, c)
	}
	rows.Close()

	// Получаем отметки
	rows, err = db.Query("SELECT category, date FROM marks WHERE user=?", user)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	for rows.Next() {
		var m Mark
		rows.Scan(&m.Category, &m.Date)
		data.Marks = append(data.Marks, m)
	}
	rows.Close()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func handleSetCategories(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", 400)
		return
	}

	var body struct {
		Categories []string `json:"categories"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer tx.Rollback()
	_, _ = tx.Exec("DELETE FROM categories WHERE user=?", user)
	for _, c := range body.Categories {
		_, _ = tx.Exec("INSERT INTO categories (user, name) VALUES (?, ?)", user, c)
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Write([]byte(`{"ok":true}`))
}

func handleToggle(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", 400)
		return
	}

	var body struct {
		Category string `json:"category"`
		Date     string `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	var exists bool
	err := db.QueryRow("SELECT 1 FROM marks WHERE user=? AND category=? AND date=?", user, body.Category, body.Date).Scan(&exists)
	if err == sql.ErrNoRows {
		_, err = db.Exec("INSERT INTO marks (user, category, date) VALUES (?, ?, ?)", user, body.Category, body.Date)
	} else if err == nil {
		_, err = db.Exec("DELETE FROM marks WHERE user=? AND category=? AND date=?", user, body.Category, body.Date)
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "time": time.Now()})
}

// 📅 GET /api/resagerhelper/marks?user=123&category=run&month=2025-10
func handleGetMarks(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	category := r.URL.Query().Get("category")
	month := r.URL.Query().Get("month")

	if user == "" || category == "" || month == "" {
		http.Error(w, "missing user, category or month", http.StatusBadRequest)
		return
	}

	rows, err := db.Query(`
		SELECT date FROM marks
		WHERE user = ? AND category = ? AND date LIKE ?
	`, user, category, month+"%")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var days []int
	for rows.Next() {
		var dateStr string
		if err := rows.Scan(&dateStr); err != nil {
			continue
		}
		t, err := time.Parse("2006-01-02", dateStr)
		if err == nil {
			days = append(days, t.Day())
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"days": days,
	})
}

// 🟩 Новый endpoint: сохранение цвета
func handleSetColor(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", 400)
		return
	}

	var body struct {
		Category string `json:"category"`
		Color    string `json:"color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	_, err := db.Exec(`
		INSERT INTO category_colors (user, category, color)
		VALUES (?, ?, ?)
		ON CONFLICT(user, category)
		DO UPDATE SET color=excluded.color
	`, user, body.Category, body.Color)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Write([]byte(`{"ok":true}`))
}

// 🟩 Новый endpoint: сохранение цвета
func handleSetName(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if user == "" {
		http.Error(w, "missing user", 400)
		return
	}

	var body struct {
		Category string `json:"category"`
		Name     string `json:"name"`
		Color    string `json:"color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	_, err := db.Exec(`
		INSERT INTO category_colors (user, category)
		VALUES (?, ?)
		ON CONFLICT(user, category)
		DO UPDATE SET category=excluded.category
	`, user, body.Category, body.Name)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Write([]byte(`{"ok":true}`))
}

var userChecks = map[string][]CheckGroup{}

type CheckGroup struct {
	Name  string      `json:"name"`
	Items []CheckItem `json:"items"`
}

type CheckItem struct {
	Name string `json:"name"`
	Done bool   `json:"done"`
}

func getChecks(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	if _, ok := userChecks[user]; !ok {
		loadUserChecksFromDB(user)
	}
	json.NewEncoder(w).Encode(userChecks[user])
}

func addCheckGroup(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	var req struct{ Name string }
	json.NewDecoder(r.Body).Decode(&req)

	// Добавляем в память
	userChecks[user] = append(userChecks[user], CheckGroup{Name: req.Name, Items: []CheckItem{}})

	// Сохраняем в БД (группа без элементов)
	_, err := db.Exec("INSERT OR IGNORE INTO checks (user, group_name, item_name, done) VALUES (?, ?, ?, ?)",
		user, req.Name, "", 0)
	if err != nil {
		log.Println("addCheckGroup:", err)
	}
}

func toggleCheck(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	var req struct {
		Group string
		Item  string
		Done  bool
	}
	json.NewDecoder(r.Body).Decode(&req)

	// Обновляем в памяти
	for gi, g := range userChecks[user] {
		if g.Name == req.Group {
			found := false
			for ii, i := range g.Items {
				if i.Name == req.Item {
					userChecks[user][gi].Items[ii].Done = req.Done
					found = true
					break
				}
			}
			if !found {
				userChecks[user][gi].Items = append(userChecks[user][gi].Items, CheckItem{Name: req.Item, Done: req.Done})
			}
		}
	}

	// Сохраняем в БД
	done := 0
	if req.Done {
		done = 1
	}
	_, err := db.Exec("INSERT OR REPLACE INTO checks (user, group_name, item_name, done) VALUES (?, ?, ?, ?)",
		user, req.Group, req.Item, done)
	if err != nil {
		log.Println("toggleCheck:", err)
	}
}

func renameCheckGroup(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	var req struct{ Old, New string }
	json.NewDecoder(r.Body).Decode(&req)

	for i, g := range userChecks[user] {
		if g.Name == req.Old {
			userChecks[user][i].Name = req.New
		}
	}

	_, err := db.Exec("UPDATE checks SET group_name = ? WHERE user = ? AND group_name = ?",
		req.New, user, req.Old)
	if err != nil {
		log.Println("renameCheckGroup:", err)
	}
}

func addCheckItem(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	var req struct{ Group, Name string }
	json.NewDecoder(r.Body).Decode(&req)

	for i, g := range userChecks[user] {
		if g.Name == req.Group {
			userChecks[user][i].Items = append(userChecks[user][i].Items, CheckItem{Name: req.Name})
		}
	}

	_, err := db.Exec("INSERT OR REPLACE INTO checks (user, group_name, item_name, done) VALUES (?, ?, ?, ?)",
		user, req.Group, req.Name, 0)
	if err != nil {
		log.Println("addCheckItem:", err)
	}
}

func deleteCheckGroup(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	var req struct{ Name string }
	json.NewDecoder(r.Body).Decode(&req)

	var newGroups []CheckGroup
	for _, g := range userChecks[user] {
		if g.Name != req.Name {
			newGroups = append(newGroups, g)
		}
	}
	userChecks[user] = newGroups

	_, err := db.Exec("DELETE FROM checks WHERE user = ? AND group_name = ?", user, req.Name)
	if err != nil {
		log.Println("deleteCheckGroup:", err)
	}
}

func deleteCheckItem(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user")
	var req struct{ Group, Name string }
	json.NewDecoder(r.Body).Decode(&req)

	for gi, g := range userChecks[user] {
		if g.Name == req.Group {
			var newItems []CheckItem
			for _, i := range g.Items {
				if i.Name != req.Name {
					newItems = append(newItems, i)
				}
			}
			userChecks[user][gi].Items = newItems
		}
	}

	_, err := db.Exec("DELETE FROM checks WHERE user = ? AND group_name = ? AND item_name = ?",
		user, req.Group, req.Name)
	if err != nil {
		log.Println("deleteCheckItem:", err)
	}
}

func loadUserChecksFromDB(user string) {
	rows, err := db.Query("SELECT group_name, item_name, done FROM checks WHERE user = ?", user)
	if err != nil {
		log.Println("loadUserChecksFromDB:", err)
		return
	}
	defer rows.Close()

	groupsMap := map[string][]CheckItem{}
	for rows.Next() {
		var groupName, itemName string
		var doneInt int
		rows.Scan(&groupName, &itemName, &doneInt)
		groupsMap[groupName] = append(groupsMap[groupName], CheckItem{
			Name: itemName,
			Done: doneInt == 1,
		})
	}

	var groups []CheckGroup
	for name, items := range groupsMap {
		groups = append(groups, CheckGroup{Name: name, Items: items})
	}
	userChecks[user] = groups
}

func (rd *RouteData) handleExportChecks(w http.ResponseWriter, r *http.Request) {
	userStr := r.URL.Query().Get("user")
	if userStr == "" {
		http.Error(w, "missing user", 400)
		return
	}

	userID, err := strconv.ParseInt(userStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid user ID (must be numeric Telegram ID)", 400)
		return
	}

	rows, err := db.Query("SELECT group_name, item_name, done FROM checks WHERE user=?", userStr)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var data [][]string
	data = append(data, []string{"Group", "Item", "Checked"})
	for rows.Next() {
		var group, name string
		var checked bool
		if err := rows.Scan(&group, &name, &checked); err == nil {
			data = append(data, []string{group, name, fmt.Sprintf("%v", checked)})
		}
	}

	// Генерируем CSV в памяти
	var buf bytes.Buffer
	csvWriter := csv.NewWriter(&buf)
	for _, row := range data {
		csvWriter.Write(row)
	}
	csvWriter.Flush()

	fileData := buf.Bytes()
	filename := fmt.Sprintf("checks_%s.csv", userStr)

	if len(fileData) <= MaxTelegramFileSize {
		if err := rd.bot.SendDoc(userID, filename, fileData); err != nil {
			zlog.Error().Err(err).Msg("Failed to send file via Telegram")
			http.Error(w, "failed to send via Telegram", 500)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("вам отправлено в телеграм"))
	} else {
		url, err := saveFileAndGenerateURL(filename, fileData)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to save large file")
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
