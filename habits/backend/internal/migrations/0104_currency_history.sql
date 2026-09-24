-- +goose Up
-- История курсов. Всё хранится ОТНОСИТЕЛЬНО ДОЛЛАРА (сколько единиц валюты за
-- 1 USD): так любая пара получается делением, и не нужно хранить матрицу
-- «каждая к каждой». Источник отдаёт дневные срезы, поэтому дата — DATE, а не
-- момент времени.
CREATE TABLE IF NOT EXISTS currency_history (
    day  DATE NOT NULL,
    code TEXT NOT NULL CHECK (length(code) BETWEEN 2 AND 10),
    rate DOUBLE PRECISION NOT NULL CHECK (rate > 0),
    PRIMARY KEY (day, code)
);

-- выборка идёт «по валюте за период» — индекс под неё
CREATE INDEX IF NOT EXISTS currency_history_code_day_idx ON currency_history (code, day);

-- +goose Down
DROP TABLE IF EXISTS currency_history;
