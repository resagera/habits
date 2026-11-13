package main

import (
	"log"

	_ "github.com/mattn/go-sqlite3"
)

func initDB() {
	var err error
	// Создаём таблицы
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS categories (
			user TEXT,
			name TEXT
		);
	`)
	if err != nil {
		log.Fatal(err)
	}
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS marks (
			user TEXT,
			category TEXT,
			date TEXT
		);
	`)
	if err != nil {
		log.Fatal(err)
	}
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS category_colors (
			user TEXT,
			category TEXT,
			color TEXT,
    		PRIMARY KEY (user, category)
		);
	`)
	if err != nil {
		log.Fatal(err)
	}
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS checks (
            user TEXT,
            group_name TEXT,
            item_name TEXT,
            done INTEGER,
            PRIMARY KEY (user, group_name, item_name)
        )
    `)
	if err != nil {
		log.Fatal(err)
	}
	// таблица дневника
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS diary (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            user TEXT,
            date TEXT,
            text TEXT
        )
    `)
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS settings (
			user TEXT PRIMARY KEY,
			bg_url TEXT,
			bg_position TEXT,
    		theme TEXT
		);
    `)
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(`
		-- metrics:
		CREATE TABLE IF NOT EXISTS metrics (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user TEXT,
			name TEXT,
			max_per_day INTEGER DEFAULT 1,
			color TEXT
		);
		-- metric_values:
		CREATE TABLE IF NOT EXISTS metric_values (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			metric_id INTEGER,
			user TEXT,
			datetime TEXT,
			value REAL,
			FOREIGN KEY (metric_id) REFERENCES metrics(id)
		);
    `)
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(`
		-- exchanges:
		CREATE TABLE IF NOT EXISTS user_currencies (
		  user TEXT,
		  currency_code TEXT,
		  PRIMARY KEY(user, currency_code)
		);
		
		CREATE TABLE IF NOT EXISTS exchange_rates (
		  base TEXT,            -- базовая валюта (например, "USD")
		  target TEXT,          -- целевая валюта (например, "EUR")
		  rate REAL,            -- курс: 1 base = rate * target
		  updated_at DATETIME
		);
    `)
	if err != nil {
		log.Fatal(err)
	}
}
