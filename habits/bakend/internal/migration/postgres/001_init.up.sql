CREATE TABLE IF NOT EXISTS categories (
                                          user_id TEXT NOT NULL,
                                          name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS marks (
                                     user_id TEXT NOT NULL,
                                     category TEXT NOT NULL,
                                     date DATE NOT NULL
);

CREATE TABLE IF NOT EXISTS category_colors (
                                               user_id TEXT NOT NULL,
                                               category TEXT NOT NULL,
                                               color TEXT,
                                               PRIMARY KEY (user_id, category)
);

CREATE TABLE IF NOT EXISTS checks (
                                      user_id TEXT NOT NULL,
                                      group_name TEXT NOT NULL,
                                      item_name TEXT NOT NULL,
                                      done BOOL DEFAULT FALSE,
                                      PRIMARY KEY (user_id, group_name, item_name)
);

CREATE TABLE IF NOT EXISTS diary (
                                     id SERIAL PRIMARY KEY,
                                     user_id TEXT NOT NULL,
                                     date DATE NOT NULL,
                                     text TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
                                        user_id TEXT PRIMARY KEY,
                                        bg_url TEXT,
                                        bg_position TEXT,
                                        theme TEXT
);

CREATE TABLE IF NOT EXISTS metrics (
                                       id SERIAL PRIMARY KEY,
                                       user_id TEXT,
                                       name TEXT,
                                       max_per_day INT DEFAULT 1,
                                       color TEXT
);

CREATE TABLE IF NOT EXISTS metric_values (
                                             id SERIAL PRIMARY KEY,
                                             metric_id INT REFERENCES metrics(id),
                                             user_id TEXT,
                                             datetime TIMESTAMP,
                                             value DOUBLE PRECISION
);

CREATE INDEX IF NOT EXISTS idx_metric_values_user ON metric_values(user_id);
CREATE INDEX IF NOT EXISTS idx_metric_values_metric_id ON metric_values(metric_id);

CREATE TABLE IF NOT EXISTS user_currencies (
                                               user_id TEXT,
                                               currency_code TEXT,
                                               PRIMARY KEY(user_id, currency_code)
);

CREATE TABLE IF NOT EXISTS exchange_rates (
                                              base TEXT,            -- базовая валюта (например, "USD")
                                              target TEXT,          -- целевая валюта (например, "EUR")
                                              rate REAL,            -- курс: 1 base = rate * target
                                              updated_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS user_settings (
                                             id SERIAL PRIMARY KEY,
                                             user_id TEXT NOT NULL,
                                             active BOOL DEFAULT FALSE,
                                             name TEXT NOT NULL,
                                             value TEXT NOT NULL,
                                             options TEXT DEFAULT '-'
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_name_value_active
    ON user_settings (user_id, name, value)
    WHERE active = TRUE;

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_settings_user_name_theme
    ON user_settings (user_id, name)
    WHERE name = 'theme';
