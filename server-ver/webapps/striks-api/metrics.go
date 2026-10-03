package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// helper: get user param (use query param user)
func getUser(r *http.Request) string {
	return r.URL.Query().Get("user")
}

type Metric struct {
	ID        int    `json:"id"`
	User      string `json:"user"`
	Name      string `json:"name"`
	MaxPerDay int    `json:"max_per_day"`
	Color     string `json:"color"`
}

type MetricValue struct {
	ID       int     `json:"id"`
	MetricID int     `json:"metric_id"`
	User     string  `json:"user"`
	Datetime string  `json:"datetime"`
	Value    float64 `json:"value"`
}

// POST /api/resagerhelper/metrics/create
// body: {"user":"u","name":"Вес","max_per_day":1,"color":"#ff0000"}
func handleCreateMetric(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var m Metric
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if m.User == "" || strings.TrimSpace(m.Name) == "" {
		http.Error(w, "missing user or name", 400)
		return
	}
	if m.MaxPerDay < 0 {
		m.MaxPerDay = 0
	}
	if m.Color == "" {
		m.Color = "#22c55e"
	}
	res, err := db.Exec("INSERT INTO metrics (user,name,max_per_day,color) VALUES (?,?,?,?)",
		m.User, m.Name, m.MaxPerDay, m.Color)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	id, _ := res.LastInsertId()
	m.ID = int(id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(m)
}

// GET /api/resagerhelper/metrics/list?user=...
func handleListMetrics(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	if user == "" {
		http.Error(w, "missing user", 400)
		return
	}
	rows, err := db.Query("SELECT id, user, name, max_per_day, color FROM metrics WHERE user = ?", user)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	var res []Metric
	for rows.Next() {
		var m Metric
		rows.Scan(&m.ID, &m.User, &m.Name, &m.MaxPerDay, &m.Color)
		res = append(res, m)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

// DELETE /api/resagerhelper/metrics?id=...&user=...
func handleDeleteMetric(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", 405)
		return
	}
	id := r.URL.Query().Get("id")
	user := getUser(r)
	if id == "" || user == "" {
		http.Error(w, "missing id or user", 400)
		return
	}
	tx, _ := db.Begin()
	// optional ownership check
	var owner string
	err := tx.QueryRow("SELECT user FROM metrics WHERE id = ?", id).Scan(&owner)
	if err == sql.ErrNoRows {
		http.Error(w, "not found", 404)
		tx.Rollback()
		return
	}
	if err != nil {
		http.Error(w, "db error", 500)
		tx.Rollback()
		return
	}
	if owner != user {
		http.Error(w, "forbidden", 403)
		tx.Rollback()
		return
	}
	if _, err := tx.Exec("DELETE FROM metric_values WHERE metric_id = ? AND user = ?", id, user); err != nil {
		http.Error(w, "db error", 500)
		tx.Rollback()
		return
	}
	if _, err := tx.Exec("DELETE FROM metrics WHERE id = ? AND user = ?", id, user); err != nil {
		http.Error(w, "db error", 500)
		tx.Rollback()
		return
	}
	tx.Commit()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// POST /api/resagerhelper/metrics/value
// body: {"user":"u","metric_id":1,"value":72.5,"datetime":"2025-10-12T08:00:00"} datetime optional -> server now
func handleAddMetricValue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var v struct {
		User     string  `json:"user"`
		MetricID int     `json:"metric_id"`
		Value    float64 `json:"value"`
		Datetime string  `json:"datetime"`
	}
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if v.User == "" || v.MetricID == 0 {
		http.Error(w, "missing user or metric_id", 400)
		return
	}
	// get metric.max_per_day
	var maxPerDay int
	err := db.QueryRow("SELECT max_per_day FROM metrics WHERE id = ? AND user = ?", v.MetricID, v.User).Scan(&maxPerDay)
	if err == sql.ErrNoRows {
		http.Error(w, "metric not found", 404)
		return
	}
	if err != nil {
		http.Error(w, "db error", 500)
		return
	}

	now := time.Now()
	dt := v.Datetime
	if strings.TrimSpace(dt) == "" {
		dt = now.Format(time.RFC3339)
	} else {
		// try parse and normalize to RFC3339, if fails use now
		if _, err := time.Parse(time.RFC3339, dt); err != nil {
			if parsed, err2 := time.Parse("2006-01-02T15:04", dt); err2 == nil {
				dt = parsed.Format(time.RFC3339)
			} else {
				dt = now.Format(time.RFC3339)
			}
		}
	}

	// if maxPerDay > 0 enforce by date (local date of stored datetime)
	if maxPerDay > 0 {
		// count existing values for same metric and same calendar date (UTC date to be consistent)
		t, _ := time.Parse(time.RFC3339, dt)
		dateStr := t.UTC().Format("2006-01-02")
		var cnt int
		err := db.QueryRow("SELECT COUNT(*) FROM metric_values WHERE metric_id=? AND user=? AND date(datetime)=?", v.MetricID, v.User, dateStr).Scan(&cnt)
		if err != nil {
			http.Error(w, "db error", 500)
			return
		}
		if cnt >= maxPerDay {
			http.Error(w, "max entries per day reached", 400)
			return
		}
	}

	_, err = db.Exec("INSERT INTO metric_values (metric_id, user, datetime, value) VALUES (?,?,?,?)",
		v.MetricID, v.User, dt, v.Value)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// DELETE /api/resagerhelper/metrics/value?id=...&user=...
func handleDeleteMetricValue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", 405)
		return
	}
	id := r.URL.Query().Get("id")
	user := getUser(r)
	if id == "" || user == "" {
		http.Error(w, "missing id or user", 400)
		return
	}
	// check ownership
	var owner string
	err := db.QueryRow("SELECT user FROM metric_values WHERE id = ?", id).Scan(&owner)
	if err == sql.ErrNoRows {
		http.Error(w, "not found", 404)
		return
	}
	if err != nil {
		http.Error(w, "db error", 500)
		return
	}
	if owner != user {
		http.Error(w, "forbidden", 403)
		return
	}
	if _, err := db.Exec("DELETE FROM metric_values WHERE id = ? AND user = ?", id, user); err != nil {
		http.Error(w, "db error", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// GET /api/resagerhelper/metrics/values?user=...&metric_id=...&period=week|month|quarter|year
// returns [{date:"2025-10-12", time:"08:00", value:72.5, id:...}, ...] ordered asc by datetime
func handleGetMetricValues(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	metricID := r.URL.Query().Get("metric_id")
	period := r.URL.Query().Get("period")
	if user == "" || metricID == "" || period == "" {
		http.Error(w, "missing params", 400)
		return
	}
	days := 7
	switch period {
	case "week":
		days = 7
	case "month":
		days = 30
	case "quarter":
		days = 90
	case "year":
		days = 365
	default:
		days = 7
	}
	query := `SELECT id, datetime, value FROM metric_values WHERE user=? AND metric_id=? AND datetime >= datetime('now', ?) ORDER BY datetime ASC`
	rows, err := db.Query(query, user, metricID, fmt.Sprintf("-%d day", days))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	var res []map[string]any
	for rows.Next() {
		var id int
		var dt string
		var val float64
		rows.Scan(&id, &dt, &val)
		// split date/time for client convenience
		t, _ := time.Parse(time.RFC3339, dt)
		res = append(res, map[string]any{
			"id":       id,
			"datetime": dt,
			"date":     t.Format("2006-01-02"),
			"time":     t.Format("15:04"),
			"value":    val,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

// GET /api/resagerhelper/metrics/values_multi?user=...&metric_ids=1,2,3&period=week
// returns { metric_id: [ {date..., value...}, ... ], ... }
func handleGetMetricValuesMulti(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	metricIDs := r.URL.Query().Get("metric_ids")
	period := r.URL.Query().Get("period")
	if user == "" || metricIDs == "" || period == "" {
		http.Error(w, "missing params", 400)
		return
	}
	days := 7
	switch period {
	case "week":
		days = 7
	case "month":
		days = 30
	case "quarter":
		days = 90
	case "year":
		days = 365
	default:
		days = 7
	}
	ids := strings.Split(metricIDs, ",")
	out := map[string][]map[string]any{}
	for _, id := range ids {
		rows, err := db.Query(`SELECT datetime, value FROM metric_values WHERE user=? AND metric_id=? AND datetime >= datetime('now', ?) ORDER BY datetime ASC`, user, id, fmt.Sprintf("-%d day", days))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		var arr []map[string]any
		for rows.Next() {
			var dt string
			var val float64
			rows.Scan(&dt, &val)
			t, _ := time.Parse(time.RFC3339, dt)
			arr = append(arr, map[string]any{"datetime": dt, "date": t.Format("2006-01-02"), "time": t.Format("15:04"), "value": val})
		}
		out[id] = arr
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}
