-- +goose Up
-- Свои словарные правила: «всё, где есть <слово>, — в эту категорию».
--
-- Встроенный словарь один на всех и зашит в код. Своя группа («Корм коту»,
-- «Сладости») без правил работала бы только через ручную разметку каждого
-- нового товара — здесь пользователь задаёт слова сам. Свои правила
-- проверяются ДО встроенных, поэтому ими же можно переопределить словарь.
CREATE TABLE finance_word_rules (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    pattern TEXT NOT NULL CHECK (length(pattern) BETWEEN 2 AND 100),
    category_id BIGINT NOT NULL REFERENCES finance_categories(id) ON DELETE CASCADE,
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX finance_word_rules_uniq ON finance_word_rules (user_id, lower(pattern));
CREATE INDEX finance_word_rules_cat ON finance_word_rules (category_id);

-- +goose Down
DROP TABLE finance_word_rules;
