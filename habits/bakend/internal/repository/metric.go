package repository

import (
	"database/sql"
	"fmt"
	"strings"
)

func (r *Repo) GetMetrics(user string) ([]Metric, error) {
	rows, err := r.db.Query("SELECT id, user_id, name, max_per_day, color FROM metrics WHERE user_id = $1", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []Metric
	for rows.Next() {
		var m Metric
		if err := rows.Scan(&m.ID, &m.User, &m.Name, &m.MaxPerDay, &m.Color); err != nil {
			return nil, err
		}
		res = append(res, m)
	}
	return res, nil
}

func (r *Repo) CreateMetric(m Metric) (Metric, error) {
	if m.MaxPerDay < 0 {
		m.MaxPerDay = 0
	}
	if m.Color == "" {
		m.Color = "#22c55e"
	}

	var newID int
	err := r.db.QueryRow(`
		INSERT INTO metrics (user_id, name, max_per_day, color)
		VALUES ($1, $2, $3, $4)
		RETURNING id`,
		m.User, m.Name, m.MaxPerDay, m.Color).Scan(&newID)
	if err != nil {
		return m, err
	}
	m.ID = newID
	return m, nil
}

func (r *Repo) DeleteMetric(id int, user string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var owner string
	err = tx.QueryRow("SELECT user_id FROM metrics WHERE id = $1", id).Scan(&owner)
	if err == sql.ErrNoRows {
		return fmt.Errorf("metric not found")
	}
	if err != nil {
		return err
	}
	if owner != user {
		return fmt.Errorf("forbidden")
	}

	if _, err := tx.Exec("DELETE FROM metric_values WHERE metric_id = $1 AND user_id = $2", id, user); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM metrics WHERE id = $1 AND user_id = $2", id, user); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repo) GetMetricMaxPerDay(metricID int, user string) (int, error) {
	var maxPerDay int
	err := r.db.QueryRow("SELECT max_per_day FROM metrics WHERE id = $1 AND user_id = $2", metricID, user).Scan(&maxPerDay)
	if err != nil {
		return 0, err
	}
	return maxPerDay, nil
}

func (r *Repo) CountMetricValuesByDate(metricID int, user, dateStr string) (int, error) {
	var cnt int
	err := r.db.QueryRow(`
		SELECT COUNT(*) 
		FROM metric_values 
		WHERE metric_id = $1 AND user_id = $2 AND DATE(datetime) = $3`,
		metricID, user, dateStr).Scan(&cnt)
	return cnt, err
}

func (r *Repo) AddMetricValue(v MetricValue) (MetricValue, error) {
	var newID int
	err := r.db.QueryRow(`
		INSERT INTO metric_values (metric_id, user_id, datetime, value)
		VALUES ($1, $2, $3, $4)
		RETURNING id`,
		v.MetricID, v.User, v.Datetime, v.Value).Scan(&newID)
	if err != nil {
		return v, err
	}
	v.ID = newID
	return v, nil
}

func (r *Repo) DeleteMetricValue(id int, user string) error {
	var owner string
	err := r.db.QueryRow("SELECT user_id FROM metric_values WHERE id = $1", id).Scan(&owner)
	if err == sql.ErrNoRows {
		return fmt.Errorf("not found")
	}
	if err != nil {
		return err
	}
	if owner != user {
		return fmt.Errorf("forbidden")
	}
	_, err = r.db.Exec("DELETE FROM metric_values WHERE id = $1 AND user_id = $2", id, user)
	return err
}

func (r *Repo) GetMetricValues(user string, metricID int, periodDays int) ([]MetricValue, error) {
	query := `
		SELECT id, datetime, value 
		FROM metric_values 
		WHERE user_id = $1 AND metric_id = $2 AND datetime >= NOW() - INTERVAL '1 day' * $3
		ORDER BY datetime ASC`
	rows, err := r.db.Query(query, user, metricID, periodDays)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []MetricValue
	for rows.Next() {
		var v MetricValue
		if err := rows.Scan(&v.ID, &v.Datetime, &v.Value); err != nil {
			continue
		}
		v.User = user
		v.MetricID = metricID
		res = append(res, v)
	}
	return res, nil
}

func (r *Repo) GetMetricValuesMulti(user string, metricIDs []int, periodDays int) (map[int][]MetricValue, error) {
	// Подготавливаем placeholders: $1, $2, ...
	placeholders := make([]string, len(metricIDs))
	args := make([]interface{}, len(metricIDs)+2)
	args[0] = user
	args[1] = periodDays
	for i, id := range metricIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+3)
		args[i+2] = id
	}

	query := fmt.Sprintf(`
		SELECT metric_id, id, datetime, value 
		FROM metric_values 
		WHERE user_id = $1 AND metric_id IN (%s) AND datetime >= NOW() - INTERVAL '1 day' * $2
		ORDER BY datetime ASC`,
		strings.Join(placeholders, ","))

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int][]MetricValue)
	for rows.Next() {
		var metricID, id int
		var dt string
		var val float64
		if err := rows.Scan(&metricID, &id, &dt, &val); err != nil {
			continue
		}
		result[metricID] = append(result[metricID], MetricValue{
			ID:       id,
			MetricID: metricID,
			User:     user,
			Datetime: dt,
			Value:    val,
		})
	}
	return result, nil
}
