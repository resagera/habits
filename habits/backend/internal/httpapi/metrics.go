package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/store"
)

const maxConfigBytes = 16 << 10

type metricsHandlers struct {
	store *store.Store
}

func (h *metricsHandlers) chartTypes(w http.ResponseWriter, r *http.Request) {
	types, err := h.store.ListChartTypes(r.Context())
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"chart_types": types})
}

func (h *metricsHandlers) tree(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	categories, err := h.store.MetricsTree(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if categories == nil {
		categories = []store.MetricCategory{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": categories})
}

func (h *metricsHandlers) createCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validLen(req.Name, 200) {
		badRequest(w, "name must be 1-200 characters")
		return
	}
	category, err := h.store.CreateMetricCategory(r.Context(), user.ID, req.Name)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"category": category})
}

func (h *metricsHandlers) renameCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid category id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validLen(req.Name, 200) {
		badRequest(w, "name must be 1-200 characters")
		return
	}
	category, err := h.store.RenameMetricCategory(r.Context(), user.ID, id, req.Name)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"category": category})
	}
}

func (h *metricsHandlers) deleteCategory(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid category id")
		return
	}
	switch err := h.store.DeleteMetricCategory(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func validConfig(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	if len(raw) > maxConfigBytes {
		return false
	}
	var v map[string]any
	return json.Unmarshal(raw, &v) == nil
}

func (h *metricsHandlers) createItem(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	categoryID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid category id")
		return
	}
	var req struct {
		Name      string          `json:"name"`
		ChartType string          `json:"chart_type"`
		Config    json.RawMessage `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !validLen(req.Name, 200) {
		badRequest(w, "name must be 1-200 characters")
		return
	}
	if req.ChartType == "" {
		req.ChartType = "line"
	}
	if !validConfig(req.Config) {
		badRequest(w, "config must be a JSON object up to 16KB")
		return
	}
	if len(req.Config) == 0 {
		req.Config = json.RawMessage(`{}`)
	}
	item, err := h.store.CreateMetricItem(r.Context(), user.ID, categoryID, req.Name, req.ChartType, req.Config)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "category not found")
	case errors.Is(err, store.ErrBadChartType):
		badRequest(w, "unknown chart_type")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"item": item})
	}
}

func (h *metricsHandlers) updateItem(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid item id")
		return
	}
	var req struct {
		Name      *string         `json:"name"`
		ChartType *string         `json:"chart_type"`
		Config    json.RawMessage `json:"config"`
		Position  *int32          `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Name == nil && req.ChartType == nil && req.Config == nil && req.Position == nil {
		badRequest(w, "nothing to update")
		return
	}
	if req.Name != nil && !validLen(*req.Name, 200) {
		badRequest(w, "name must be 1-200 characters")
		return
	}
	if !validConfig(req.Config) {
		badRequest(w, "config must be a JSON object up to 16KB")
		return
	}
	item, err := h.store.UpdateMetricItem(r.Context(), user.ID, id, store.MetricItemPatch{
		Name:      req.Name,
		ChartType: req.ChartType,
		Config:    req.Config,
		Position:  req.Position,
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "item not found")
	case errors.Is(err, store.ErrBadChartType):
		badRequest(w, "unknown chart_type")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"item": item})
	}
}

func (h *metricsHandlers) deleteItem(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid item id")
		return
	}
	switch err := h.store.DeleteMetricItem(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "item not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *metricsHandlers) listValues(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid item id")
		return
	}
	q := r.URL.Query()
	var from, to *time.Time
	if raw := q.Get("from"); raw != "" {
		t, err := time.Parse(dateLayout, raw)
		if err != nil {
			badRequest(w, "from must be YYYY-MM-DD")
			return
		}
		from = &t
	}
	if raw := q.Get("to"); raw != "" {
		t, err := time.Parse(dateLayout, raw)
		if err != nil {
			badRequest(w, "to must be YYYY-MM-DD")
			return
		}
		end := t.Add(24 * time.Hour)
		to = &end
	}
	limit, _ := strconv.ParseInt(q.Get("limit"), 10, 32)

	values, err := h.store.ListMetricValues(r.Context(), user.ID, id, from, to, int32(limit))
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "item not found")
	case err != nil:
		internalError(w)
	default:
		if values == nil {
			values = []store.MetricValue{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"values": values})
	}
}

func (h *metricsHandlers) addValues(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid item id")
		return
	}
	var req struct {
		At     *time.Time         `json:"at"`
		Values map[string]float64 `json:"values"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body (at — RFC3339)")
		return
	}
	if len(req.Values) == 0 || len(req.Values) > 20 {
		badRequest(w, "values must contain 1-20 components")
		return
	}
	at := time.Now()
	if req.At != nil {
		at = *req.At
	}
	values, err := h.store.AddMetricValues(r.Context(), user.ID, id, at, req.Values)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "item not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"values": values})
	}
}

func (h *metricsHandlers) updateValue(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid value id")
		return
	}
	var req struct {
		At    *time.Time `json:"at"`
		Value *float64   `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.At == nil && req.Value == nil) {
		badRequest(w, "provide at and/or value")
		return
	}
	value, err := h.store.UpdateMetricValue(r.Context(), user.ID, id, req.At, req.Value)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "value not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"value": value})
	}
}

func (h *metricsHandlers) deleteValue(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid value id")
		return
	}
	switch err := h.store.DeleteMetricValue(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "value not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
