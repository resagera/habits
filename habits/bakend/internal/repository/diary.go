package repository

import (
	"fmt"
)

func (r *Repo) CreateDiaryEntry(user, date, text string) error {
	_, err := r.db.Exec("INSERT INTO diary (user_id, date, text) VALUES ($1, $2, $3)", user, date, text)
	return err
}

func (r *Repo) GetDiaryEntries(user, from, to string) ([]DiaryEntry, error) {
	query := `SELECT id, date, text FROM diary WHERE user_id = $1`
	args := []interface{}{user}
	argPos := 2

	if from != "" {
		query += fmt.Sprintf(" AND date >= $%d", argPos)
		args = append(args, from)
		argPos++
	}
	if to != "" {
		query += fmt.Sprintf(" AND date <= $%d", argPos)
		args = append(args, to)
	}
	query += " ORDER BY date DESC"

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []DiaryEntry
	for rows.Next() {
		var e DiaryEntry
		if err := rows.Scan(&e.ID, &e.Date, &e.Text); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (r *Repo) SearchDiaryEntries(user, q string) ([]DiaryEntry, error) {
	rows, err := r.db.Query(`
		SELECT id, date, text 
		FROM diary 
		WHERE user_id = $1 AND text ILIKE $2 
		ORDER BY date DESC`,
		user, "%"+q+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []DiaryEntry
	for rows.Next() {
		var e DiaryEntry
		if err := rows.Scan(&e.ID, &e.Date, &e.Text); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (r *Repo) UpdateDiaryEntry(id int, date, text string) error {
	_, err := r.db.Exec("UPDATE diary SET text = $1 WHERE id = $2", text, id)
	return err
}

func (r *Repo) DeleteDiaryEntry(id int) error {
	_, err := r.db.Exec("DELETE FROM diary WHERE id = $1", id)
	return err
}

func (r *Repo) GetDiaryEntriesForExport(user, from, to string) ([]DiaryEntry, error) {
	rows, err := r.db.Query(`
		SELECT date, text 
		FROM diary 
		WHERE user_id = $1 AND date BETWEEN $2 AND $3 
		ORDER BY date ASC`,
		user, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []DiaryEntry
	for rows.Next() {
		var e DiaryEntry
		// ID не нужен для экспорта
		if err := rows.Scan(&e.Date, &e.Text); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}
