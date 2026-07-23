package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrBadChartType — неизвестный тип графика (нет в metrics_chart_types).
var ErrBadChartType = errors.New("unknown chart type")

type ChartType struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type MetricCategory struct {
	ID       int64        `json:"id"`
	Name     string       `json:"name"`
	Position int32        `json:"position"`
	Items    []MetricItem `json:"items"`
}

type MetricItem struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	ChartType string          `json:"chart_type"`
	Config    json.RawMessage `json:"config"`
	Position  int32           `json:"position"`
}

type MetricValue struct {
	ID        int64     `json:"id"`
	At        time.Time `json:"at"`
	Component string    `json:"component"`
	Value     float64   `json:"value"`
}

func isFKViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func (s *Store) ListChartTypes(ctx context.Context) ([]ChartType, error) {
	rows, err := s.pool.Query(ctx, `SELECT code, name FROM metrics_chart_types ORDER BY code`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ChartType])
}

func (s *Store) MetricsTree(ctx context.Context, userID int64) ([]MetricCategory, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, position FROM metrics_categories
		WHERE user_id = $1 ORDER BY position, id`, userID)
	if err != nil {
		return nil, err
	}
	categories, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (MetricCategory, error) {
		var c MetricCategory
		err := row.Scan(&c.ID, &c.Name, &c.Position)
		c.Items = []MetricItem{}
		return c, err
	})
	if err != nil || len(categories) == 0 {
		return categories, err
	}

	byID := make(map[int64]*MetricCategory, len(categories))
	for i := range categories {
		byID[categories[i].ID] = &categories[i]
	}
	itemRows, err := s.pool.Query(ctx, `
		SELECT i.id, i.category_id, i.name, i.chart_type, i.config, i.position
		FROM metrics_items i
		JOIN metrics_categories c ON c.id = i.category_id
		WHERE c.user_id = $1 ORDER BY i.position, i.id`, userID)
	if err != nil {
		return nil, err
	}
	defer itemRows.Close()
	for itemRows.Next() {
		var it MetricItem
		var catID int64
		if err := itemRows.Scan(&it.ID, &catID, &it.Name, &it.ChartType, &it.Config, &it.Position); err != nil {
			return nil, err
		}
		if c, ok := byID[catID]; ok {
			c.Items = append(c.Items, it)
		}
	}
	return categories, itemRows.Err()
}

func (s *Store) CreateMetricCategory(ctx context.Context, userID int64, name string) (MetricCategory, error) {
	c := MetricCategory{Items: []MetricItem{}}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO metrics_categories (user_id, name, position)
		VALUES ($1, $2,
		        (SELECT COALESCE(MAX(position) + 1, 0) FROM metrics_categories WHERE user_id = $1))
		RETURNING id, name, position`,
		userID, name).Scan(&c.ID, &c.Name, &c.Position)
	return c, err
}

func (s *Store) RenameMetricCategory(ctx context.Context, userID, id int64, name string) (MetricCategory, error) {
	c := MetricCategory{Items: []MetricItem{}}
	err := s.pool.QueryRow(ctx, `
		UPDATE metrics_categories SET name = $3
		WHERE id = $1 AND user_id = $2
		RETURNING id, name, position`,
		id, userID, name).Scan(&c.ID, &c.Name, &c.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func (s *Store) DeleteMetricCategory(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM metrics_categories WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateMetricItem(ctx context.Context, userID, categoryID int64, name, chartType string, config json.RawMessage) (MetricItem, error) {
	var it MetricItem
	err := s.pool.QueryRow(ctx, `
		INSERT INTO metrics_items (category_id, name, chart_type, config, position)
		SELECT c.id, $3, $4, $5,
		       (SELECT COALESCE(MAX(position) + 1, 0) FROM metrics_items WHERE category_id = c.id)
		FROM metrics_categories c
		WHERE c.id = $1 AND c.user_id = $2
		RETURNING id, name, chart_type, config, position`,
		categoryID, userID, name, chartType, config).
		Scan(&it.ID, &it.Name, &it.ChartType, &it.Config, &it.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return it, ErrNotFound
	}
	if isFKViolation(err) {
		return it, ErrBadChartType
	}
	return it, err
}

type MetricItemPatch struct {
	Name      *string
	ChartType *string
	Config    json.RawMessage // nil = не менять
	Position  *int32
}

func (s *Store) UpdateMetricItem(ctx context.Context, userID, id int64, p MetricItemPatch) (MetricItem, error) {
	var it MetricItem
	err := s.pool.QueryRow(ctx, `
		UPDATE metrics_items i
		SET name = COALESCE($3, i.name),
		    chart_type = COALESCE($4, i.chart_type),
		    config = COALESCE($5, i.config),
		    position = COALESCE($6, i.position)
		FROM metrics_categories c
		WHERE i.id = $1 AND c.id = i.category_id AND c.user_id = $2
		RETURNING i.id, i.name, i.chart_type, i.config, i.position`,
		id, userID, p.Name, p.ChartType, p.Config, p.Position).
		Scan(&it.ID, &it.Name, &it.ChartType, &it.Config, &it.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return it, ErrNotFound
	}
	if isFKViolation(err) {
		return it, ErrBadChartType
	}
	return it, err
}

func (s *Store) DeleteMetricItem(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM metrics_items i
		USING metrics_categories c
		WHERE i.id = $1 AND c.id = i.category_id AND c.user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) itemOwned(ctx context.Context, userID, itemID int64) (bool, error) {
	var owned bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM metrics_items i
			JOIN metrics_categories c ON c.id = i.category_id
			WHERE i.id = $1 AND c.user_id = $2)`, itemID, userID).Scan(&owned)
	return owned, err
}

func (s *Store) ListMetricValues(ctx context.Context, userID, itemID int64, from, to *time.Time, limit int32) ([]MetricValue, error) {
	if owned, err := s.itemOwned(ctx, userID, itemID); err != nil {
		return nil, err
	} else if !owned {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, at, component, value FROM metrics_values
		WHERE item_id = $1
		  AND ($2::timestamptz IS NULL OR at >= $2)
		  AND ($3::timestamptz IS NULL OR at <= $3)
		ORDER BY at, component
		LIMIT $4`, itemID, from, to, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[MetricValue])
}

// AddMetricValues вставляет одну «точку»: несколько компонентов с общим временем.
func (s *Store) AddMetricValues(ctx context.Context, userID, itemID int64, at time.Time, values map[string]float64) ([]MetricValue, error) {
	if owned, err := s.itemOwned(ctx, userID, itemID); err != nil {
		return nil, err
	} else if !owned {
		return nil, ErrNotFound
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	out := make([]MetricValue, 0, len(values))
	for component, value := range values {
		var v MetricValue
		if err := tx.QueryRow(ctx, `
			INSERT INTO metrics_values (item_id, at, component, value)
			VALUES ($1, $2, $3, $4)
			RETURNING id, at, component, value`,
			itemID, at, component, value).
			Scan(&v.ID, &v.At, &v.Component, &v.Value); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, tx.Commit(ctx)
}

func (s *Store) UpdateMetricValue(ctx context.Context, userID, id int64, at *time.Time, value *float64) (MetricValue, error) {
	var v MetricValue
	err := s.pool.QueryRow(ctx, `
		UPDATE metrics_values mv
		SET at = COALESCE($3, mv.at), value = COALESCE($4, mv.value)
		FROM metrics_items i
		JOIN metrics_categories c ON c.id = i.category_id
		WHERE mv.id = $1 AND i.id = mv.item_id AND c.user_id = $2
		RETURNING mv.id, mv.at, mv.component, mv.value`,
		id, userID, at, value).Scan(&v.ID, &v.At, &v.Component, &v.Value)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}

func (s *Store) DeleteMetricValue(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM metrics_values mv
		USING metrics_items i, metrics_categories c
		WHERE mv.id = $1 AND i.id = mv.item_id AND c.id = i.category_id AND c.user_id = $2`,
		id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
