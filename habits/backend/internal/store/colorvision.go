package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// ColorVisionResult — итог одного прохождения теста на цветовосприятие.
// Сами таблицы рисует клиент (они каждый раз новые), поэтому на сервере
// хранится только счёт: сколько таблиц узнали и где ошиблись.
type ColorVisionResult struct {
	ID      int64     `json:"id"`
	TakenAt time.Time `json:"taken_at"`
	Total   int32     `json:"total"`
	Correct int32     `json:"correct"`
	Protan  int32     `json:"protan"`
	Deutan  int32     `json:"deutan"`
	Tritan  int32     `json:"tritan"`
}

const colorVisionCols = `id, taken_at, total, correct, protan, deutan, tritan`

func (s *Store) ListColorVisionResults(ctx context.Context, userID int64, limit int) ([]ColorVisionResult, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+colorVisionCols+` FROM color_vision_results
		WHERE user_id = $1 ORDER BY taken_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ColorVisionResult])
}

func (s *Store) AddColorVisionResult(ctx context.Context, userID int64, r ColorVisionResult) (ColorVisionResult, error) {
	rows, err := s.pool.Query(ctx, `
		INSERT INTO color_vision_results (user_id, total, correct, protan, deutan, tritan)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+colorVisionCols,
		userID, r.Total, r.Correct, r.Protan, r.Deutan, r.Tritan)
	if err != nil {
		return ColorVisionResult{}, err
	}
	return pgx.CollectOneRow(rows, pgx.RowToStructByPos[ColorVisionResult])
}
