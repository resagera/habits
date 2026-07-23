package repository

import (
	"database/sql"
	"fmt"
	"strconv"
)

func (r *Repo) GetActiveSetting(user, name string) (*UserSetting, error) {
	var s UserSetting
	err := r.db.QueryRow(`
		SELECT id, user_id, name, value, options, active
		FROM user_settings
		WHERE user_id = $1 AND name = $2 AND active = true`,
		user, name).Scan(&s.ID, &s.User, &s.Name, &s.Value, &s.Options, &s.Active)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // не ошибка — просто нет активной
		}
		return nil, err
	}
	return &s, nil
}

func (r *Repo) GetAllSettings(user, name string) ([]UserSetting, error) {
	rows, err := r.db.Query(`
		SELECT id, user_id, name, value, options, active
		FROM user_settings
		WHERE user_id = $1 AND name = $2`,
		user, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var settings []UserSetting
	for rows.Next() {
		var s UserSetting
		if err := rows.Scan(&s.ID, &s.User, &s.Name, &s.Value, &s.Options, &s.Active); err != nil {
			return nil, err
		}
		settings = append(settings, s)
	}
	return settings, nil
}

func (r *Repo) SaveSetting(user, name, value, options string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Деактивировать текущую активную запись
	_, err = tx.Exec(`
		UPDATE user_settings 
		SET active = false 
		WHERE user_id = $1 AND name = $2 AND active = true`,
		user, name)
	if err != nil {
		return err
	}

	// Вставить новую
	_, err = tx.Exec(`
		INSERT INTO user_settings (user_id, active, name, value, options)
		VALUES ($1, $2, $3, $4, $5)`,
		user, true, name, value, options)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *Repo) UpdateActiveSetting(user, name, idStr string) error {
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return fmt.Errorf("invalid id: %v", err)
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Деактивировать текущую
	_, err = tx.Exec(`
		UPDATE user_settings 
		SET active = false 
		WHERE user_id = $1 AND name = $2 AND active = true`,
		user, name)
	if err != nil {
		return err
	}

	// Активировать выбранную
	_, err = tx.Exec(`
		UPDATE user_settings 
		SET active = true 
		WHERE user_id = $1 AND name = $2 AND id = $3`,
		user, name, id)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *Repo) DeleteSetting(user, name, idStr string) error {
	if idStr == "" {
		return nil
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return fmt.Errorf("invalid id: %v", err)
	}
	_, err = r.db.Exec(`
		DELETE FROM user_settings 
		WHERE user_id = $1 AND name = $2 AND id = $3`,
		user, name, id)
	return err
}

func (r *Repo) UpsertSetting(user, name, value string) error {
	_, err := r.db.Exec(`
		INSERT INTO user_settings (user_id, name, value, active)
		VALUES ($1, $2, $3, true)
		ON CONFLICT ON CONSTRAINT idx_user_name_value_active
		DO UPDATE SET value = EXCLUDED.value, active = true`,
		user, name, value)
	return err
}
