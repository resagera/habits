-- +goose Up
-- Разбивка траты по категориям + группы товаров в чеках.
--
-- Решение: трата остаётся ОДНОЙ строкой (поход в магазин — одна трата), но
-- получает доли по категориям. Отчёт суммирует доли там, где они есть, и
-- трату целиком там, где их нет, — поэтому круговая диаграмма и отчёт по
-- категориям считаются одним механизмом, а не двумя параллельными.

CREATE TABLE finance_tx_splits (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tx_id BIGINT NOT NULL REFERENCES finance_transactions(id) ON DELETE CASCADE,
    -- NULL — «Не разобрано»: честный отдельный кусок диаграммы, а не молчаливое
    -- «Прочее»
    category_id BIGINT REFERENCES finance_categories(id) ON DELETE SET NULL,
    amount NUMERIC(14,2) NOT NULL,
    position INT NOT NULL DEFAULT 0
);
CREATE INDEX finance_tx_splits_tx_idx ON finance_tx_splits (tx_id, position);
CREATE INDEX finance_tx_splits_cat_idx ON finance_tx_splits (category_id);

-- Память решений: «этот товар у этого магазина — вот эта категория».
-- Названия в чеках повторяются посимвольно (на трёх чеках 30% повторов),
-- поэтому одна разметка закрывает товар навсегда.
CREATE TABLE finance_item_rules (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- пусто — правило для любого магазина
    merchant TEXT NOT NULL DEFAULT '',
    name_key TEXT NOT NULL,
    name_sample TEXT NOT NULL DEFAULT '',
    category_id BIGINT NOT NULL REFERENCES finance_categories(id) ON DELETE CASCADE,
    source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'ai')),
    hits INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX finance_item_rules_key_idx
    ON finance_item_rules (user_id, merchant, name_key);

-- Куда встроенные правила («alcohol», «household», …) ложатся в ДЕРЕВО
-- пользователя. Свои категории у всех разные, поэтому соответствие хранится, а
-- не угадывается по названию.
CREATE TABLE finance_item_groups (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code TEXT NOT NULL,
    category_id BIGINT NOT NULL REFERENCES finance_categories(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, code)
);

ALTER TABLE mail_receipt_items
    ADD COLUMN category_id BIGINT REFERENCES finance_categories(id) ON DELETE SET NULL,
    -- откуда взялась категория: rule (встроенный словарь), memory (твоё
    -- прежнее решение), manual, ai
    ADD COLUMN source TEXT NOT NULL DEFAULT '',
    -- нормализованное название: ключ для памяти решений и истории цен
    ADD COLUMN name_key TEXT NOT NULL DEFAULT '';
CREATE INDEX mail_receipt_items_key_idx ON mail_receipt_items (name_key);
CREATE INDEX mail_receipt_items_cat_idx ON mail_receipt_items (category_id);

-- +goose Down
DROP INDEX IF EXISTS mail_receipt_items_cat_idx;
DROP INDEX IF EXISTS mail_receipt_items_key_idx;
ALTER TABLE mail_receipt_items
    DROP COLUMN category_id, DROP COLUMN source, DROP COLUMN name_key;
DROP TABLE finance_item_groups;
DROP TABLE finance_item_rules;
DROP TABLE finance_tx_splits;
