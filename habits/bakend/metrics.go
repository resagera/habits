package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"habits/internal/repository"
)

// helper: get user param (use query param user)
func getUser(r *http.Request) string {
	return r.URL.Query().Get("user")
}

// POST /api/resagerhelper/metrics/create
func (rd *RouteData) handleCreateMetric(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var m repository.Metric
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if m.User == "" || strings.TrimSpace(m.Name) == "" {
		http.Error(w, "missing user or name", http.StatusBadRequest)
		return
	}

	created, err := rd.repo.CreateMetric(m)
	if err != nil {
		rd.log.Error("handleCreateMetric", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(created)
}

// GET /api/resagerhelper/metrics/list?user=...
func (rd *RouteData) handleListMetrics(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	if user == "" {
		http.Error(w, "missing user", http.StatusBadRequest)
		return
	}

	metrics, err := rd.repo.GetMetrics(user)
	if err != nil {
		rd.log.Error("handleListMetrics", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// DELETE /api/resagerhelper/metrics?id=...&user=...
func (rd *RouteData) handleDeleteMetric(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := r.URL.Query().Get("id")
	user := getUser(r)
	if idStr == "" || user == "" {
		http.Error(w, "missing id or user", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := rd.repo.DeleteMetric(id, user); err != nil {
		if err.Error() == "not found" {
			http.Error(w, "not found", http.StatusNotFound)
		} else if err.Error() == "forbidden" {
			http.Error(w, "forbidden", http.StatusForbidden)
		} else {
			rd.log.Error("handleDeleteMetric", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// POST /api/resagerhelper/metrics/value
func (rd *RouteData) handleAddMetricValue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		User     string  `json:"user"`
		MetricID int     `json:"metric_id"`
		Value    float64 `json:"value"`
		Datetime string  `json:"datetime"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.User == "" || req.MetricID == 0 {
		http.Error(w, "missing user or metric_id", http.StatusBadRequest)
		return
	}

	maxPerDay, err := rd.repo.GetMetricMaxPerDay(req.MetricID, req.User)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "metric not found", http.StatusNotFound)
		} else {
			rd.log.Error("handleAddMetricValue: get max_per_day", "error", err)
			http.Error(w, "db error", http.StatusInternalServerError)
		}
		return
	}

	now := time.Now()
	dt := req.Datetime
	if strings.TrimSpace(dt) == "" {
		dt = now.Format(time.RFC3339)
	} else {
		if _, err := time.Parse(time.RFC3339, dt); err != nil {
			if parsed, err2 := time.Parse("2006-01-02T15:04", dt); err2 == nil {
				dt = parsed.Format(time.RFC3339)
			} else {
				dt = now.Format(time.RFC3339)
			}
		}
	}

	if maxPerDay > 0 {
		t, _ := time.Parse(time.RFC3339, dt)
		dateStr := t.UTC().Format("2006-01-02")
		cnt, err := rd.repo.CountMetricValuesByDate(req.MetricID, req.User, dateStr)
		if err != nil {
			rd.log.Error("handleAddMetricValue: count values", "error", err)
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		if cnt >= maxPerDay {
			http.Error(w, "max entries per day reached", http.StatusBadRequest)
			return
		}
	}

	v := repository.MetricValue{
		MetricID: req.MetricID,
		User:     req.User,
		Datetime: dt,
		Value:    req.Value,
	}

	if _, err := rd.repo.AddMetricValue(v); err != nil {
		rd.log.Error("handleAddMetricValue: insert", "error", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// DELETE /api/resagerhelper/metrics/value?id=...&user=...
func (rd *RouteData) handleDeleteMetricValue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := r.URL.Query().Get("id")
	user := getUser(r)
	if idStr == "" || user == "" {
		http.Error(w, "missing id or user", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := rd.repo.DeleteMetricValue(id, user); err != nil {
		if err.Error() == "not found" {
			http.Error(w, "not found", http.StatusNotFound)
		} else if err.Error() == "forbidden" {
			http.Error(w, "forbidden", http.StatusForbidden)
		} else {
			rd.log.Error("handleDeleteMetricValue", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// GET /api/resagerhelper/metrics/values?user=...&metric_id=...&period=...
func (rd *RouteData) handleGetMetricValues(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	metricIDStr := r.URL.Query().Get("metric_id")
	period := r.URL.Query().Get("period")
	if user == "" || metricIDStr == "" || period == "" {
		http.Error(w, "missing params", http.StatusBadRequest)
		return
	}

	metricID, err := strconv.Atoi(metricIDStr)
	if err != nil {
		http.Error(w, "invalid metric_id", http.StatusBadRequest)
		return
	}

	days := map[string]int{
		"week":    7,
		"month":   30,
		"quarter": 90,
		"year":    365,
	}[period]
	if days == 0 {
		days = 7
	}

	values, err := rd.repo.GetMetricValues(user, metricID, days)
	if err != nil {
		rd.log.Error("handleGetMetricValues", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Форматируем для клиента
	var res []map[string]any
	for _, v := range values {
		t, _ := time.Parse(time.RFC3339, v.Datetime)
		res = append(res, map[string]any{
			"id":       v.ID,
			"datetime": v.Datetime,
			"date":     t.Format("2006-01-02"),
			"time":     t.Format("15:04"),
			"value":    v.Value,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

// GET /api/resagerhelper/metrics/values_multi?user=...&metric_ids=1,2,3&period=week
func (rd *RouteData) handleGetMetricValuesMulti(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	metricIDsStr := r.URL.Query().Get("metric_ids")
	period := r.URL.Query().Get("period")
	if user == "" || metricIDsStr == "" || period == "" {
		http.Error(w, "missing params", http.StatusBadRequest)
		return
	}

	days := map[string]int{
		"week":    7,
		"month":   30,
		"quarter": 90,
		"year":    365,
	}[period]
	if days == 0 {
		days = 7
	}

	strIDs := strings.Split(metricIDsStr, ",")
	var ids []int
	for _, s := range strIDs {
		if id, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		http.Error(w, "invalid metric_ids", http.StatusBadRequest)
		return
	}

	raw, err := rd.repo.GetMetricValuesMulti(user, ids, days)
	if err != nil {
		rd.log.Error("handleGetMetricValuesMulti", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Преобразуем map[int][]MetricValue → map[string][]map[string]any
	out := make(map[string][]map[string]any)
	for metricID, vals := range raw {
		key := strconv.Itoa(metricID)
		for _, v := range vals {
			t, _ := time.Parse(time.RFC3339, v.Datetime)
			out[key] = append(out[key], map[string]any{
				"datetime": v.Datetime,
				"date":     t.Format("2006-01-02"),
				"time":     t.Format("15:04"),
				"value":    v.Value,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}
