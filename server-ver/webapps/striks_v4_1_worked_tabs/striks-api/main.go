package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	_ "github.com/mattn/go-sqlite3"
)

type DayMark struct {
	Date     string `json:"date"`
	Category string `json:"category"`
}

type UserData struct {
	UserID     string    `json:"user_id"`
	Categories []string  `json:"categories"`
	Marks      []DayMark `json:"marks"`
}

var db *sql.DB

func main() {
	var err error
	db, err = sql.Open("sqlite3", "./data.db")
	if err != nil {
		log.Fatal(err)
	}
	initDB()

	http.HandleFunc("/api/resagerhelper/get", handleGet)
	http.HandleFunc("/api/resagerhelper/toggle", handleToggle)
	http.HandleFunc("/api/resagerhelper/set_categories", handleSetCategories)
	http.Handle("/api/resagerhelper/webapp", http.FileServer(http.Dir("../webapp")))

	http.HandleFunc("/api/resagerhelper/get_checks", getChecks).Methods("GET")
	http.HandleFunc("/api/resagerhelper/add_check_group", addCheckGroup).Methods("POST")
	http.HandleFunc("/api/resagerhelper/toggle_check", toggleCheck).Methods("POST")

	http.HandleFunc("/api/resagerhelper/rename_check_group", renameCheckGroup).Methods("POST")
	http.HandleFunc("/api/resagerhelper/add_check_item", addCheckItem).Methods("POST")
	http.HandleFunc("/api/resagerhelper/delete_check_group", deleteCheckGroup).Methods("POST")
	http.HandleFunc("/api/resagerhelper/delete_check_item", deleteCheckItem).Methods("POST")


	log.Println("Server started on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func initDB() {
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS users (
		user_id TEXT PRIMARY KEY,
		categories TEXT
	);
	CREATE TABLE IF NOT EXISTS marks (
		user_id TEXT,
		category TEXT,
		date TEXT,
		PRIMARY KEY (user_id, category, date)
	);
	`)
	if err != nil {
		log.Fatal("DB init error:", err)
	}
}

// ---------- Helpers ----------

func getUserData(userID string) (*UserData, error) {
	row := db.QueryRow("SELECT categories FROM users WHERE user_id = ?", userID)
	var catJSON string
	err := row.Scan(&catJSON)
	if err == sql.ErrNoRows {
		defaultCats := []string{"Пробежка", "Йога", "Медитация"}
		catBytes, _ := json.Marshal(defaultCats)
		_, err = db.Exec("INSERT INTO users(user_id, categories) VALUES (?, ?)", userID, string(catBytes))
		if err != nil {
			return nil, err
		}
		catJSON = string(catBytes)
	} else if err != nil {
		return nil, err
	}

	var cats []string
	json.Unmarshal([]byte(catJSON), &cats)

	rows, err := db.Query("SELECT category, date FROM marks WHERE user_id = ?", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var marks []DayMark
	for rows.Next() {
		var m DayMark
		rows.Scan(&m.Category, &m.Date)
		marks = append(marks, m)
	}

	return &UserData{UserID: userID, Categories: cats, Marks: marks}, nil
}

func saveCategories(userID string, cats []string) error {
	catJSON, _ := json.Marshal(cats)
	_, err := db.Exec("INSERT OR REPLACE INTO users(user_id, categories) VALUES (?, ?)", userID, string(catJSON))
	return err
}

// ---------- Handlers ----------

func handleGet(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user")
	if userID == "" {
		http.Error(w, "missing user", 400)
		return
	}

	u, err := getUserData(userID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	json.NewEncoder(w).Encode(u)
}

func handleToggle(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user")
	if userID == "" {
		http.Error(w, "missing user", 400)
		return
	}

	var req DayMark
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	// Проверим — есть ли уже запись
	var exists bool
	row := db.QueryRow("SELECT 1 FROM marks WHERE user_id = ? AND category = ? AND date = ?", userID, req.Category, req.Date)
	err := row.Scan(&exists)
	if err == sql.ErrNoRows {
		_, err = db.Exec("INSERT INTO marks(user_id, category, date) VALUES (?, ?, ?)", userID, req.Category, req.Date)
	} else {
		_, err = db.Exec("DELETE FROM marks WHERE user_id = ? AND category = ? AND date = ?", userID, req.Category, req.Date)
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	u, _ := getUserData(userID)
	json.NewEncoder(w).Encode(u)
}

func handleSetCategories(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user")
	if userID == "" {
		http.Error(w, "missing user", 400)
		return
	}

	var req struct {
		Categories []string `json:"categories"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	if err := saveCategories(userID, req.Categories); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	u, _ := getUserData(userID)
	json.NewEncoder(w).Encode(u)
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
    json.NewEncoder(w).Encode(userChecks[user])
}

func addCheckGroup(w http.ResponseWriter, r *http.Request) {
    user := r.URL.Query().Get("user")
    var req struct{ Name string }
    json.NewDecoder(r.Body).Decode(&req)
    userChecks[user] = append(userChecks[user], CheckGroup{Name: req.Name})
}

func toggleCheck(w http.ResponseWriter, r *http.Request) {
    user := r.URL.Query().Get("user")
    var req struct {
        Group string
        Item  string
        Done  bool
    }
    json.NewDecoder(r.Body).Decode(&req)
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
}
