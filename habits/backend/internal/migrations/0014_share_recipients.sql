-- +goose Up

-- «Кому я делился» — для подсказок в формах шаринга (статьи, категории,
-- шаблоны чек-листов, папки паролей). Обновляется при каждой отправке.
CREATE TABLE share_recipients (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipient_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    last_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, recipient_id)
);

-- +goose Down
DROP TABLE share_recipients;
