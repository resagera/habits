package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// История курсов.
//
// Всё хранится ОТНОСИТЕЛЬНО ДОЛЛАРА (сколько единиц валюты за 1 USD): любая
// пара получается делением, и не нужно держать матрицу «каждая к каждой».
// Источник отдаёт дневные срезы, поэтому день — DATE, а не момент времени.

// RatePoint — курс валюты за день.
type RatePoint struct {
	Day  time.Time `json:"day"`
	Code string    `json:"code"`
	Rate float64   `json:"rate"`
}

// SaveCurrencyRates кладёт срез за день. Повтор того же дня перезаписывает:
// источник в течение дня уточняет курс, и последнее значение вернее.
func (s *Store) SaveCurrencyRates(ctx context.Context, day time.Time, rates map[string]float64) error {
	if len(rates) == 0 {
		return nil
	}
	d := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	batch := &pgx.Batch{}
	for code, rate := range rates {
		if rate <= 0 {
			continue
		}
		batch.Queue(`
			INSERT INTO currency_history (day, code, rate) VALUES ($1,$2,$3)
			ON CONFLICT (day, code) DO UPDATE SET rate = EXCLUDED.rate`, d, code, rate)
	}
	if batch.Len() == 0 {
		return nil
	}
	return s.pool.SendBatch(ctx, batch).Close()
}

// CurrencyHistory — что уже накоплено по этим валютам за период.
func (s *Store) CurrencyHistory(ctx context.Context, codes []string, from, to time.Time) ([]RatePoint, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT day, code, rate FROM currency_history
		WHERE code = ANY($1) AND day >= $2 AND day <= $3
		ORDER BY day`, codes, from, to)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[RatePoint])
}

// CurrencyHistoryDays — за какие дни данные уже есть по ВСЕМ валютам сразу.
// Дни, где не хватает хотя бы одной, докачиваются: иначе на графике одной
// валюты появилась бы дырка, а у соседней в тот же день — точка.
func (s *Store) CurrencyHistoryDays(ctx context.Context, codes []string, from, to time.Time) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT day FROM currency_history
		WHERE code = ANY($1) AND day >= $2 AND day <= $3
		GROUP BY day HAVING count(DISTINCT code) = $4`,
		codes, from, to, len(codes))
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out[d.Format("2006-01-02")] = true
	}
	return out, rows.Err()
}
