package repository

func (r *Repo) GetUserCurrencies(user string) ([]string, error) {
	rows, err := r.db.Query("SELECT currency_code FROM user_currencies WHERE user_id = $1", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var codes []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, nil
}

func (r *Repo) AddUserCurrency(user, code string) error {
	_, err := r.db.Exec(`
		INSERT INTO user_currencies (user_id, currency_code)
		VALUES ($1, $2)
		ON CONFLICT (user_id, currency_code) DO NOTHING`,
		user, code)
	return err
}

func (r *Repo) RemoveUserCurrency(user, code string) error {
	_, err := r.db.Exec("DELETE FROM user_currencies WHERE user_id = $1 AND currency_code = $2", user, code)
	return err
}
