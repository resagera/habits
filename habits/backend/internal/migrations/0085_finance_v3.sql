-- +goose Up
-- Finance, фазы 3 и 4: фактические траты с деревом категорий, счета
-- («откуда деньги») и цели «отложено на».
--
-- Ключевое решение фазы 3: реестр денег ОДИН — finance_transactions. Оплата
-- планового платежа создаёт обычную транзакцию (со ссылкой на план и на запись
-- истории), иначе отчёт по категориям пришлось бы собирать из двух источников
-- с разными правилами, и любая новая аналитика удваивалась бы.
--
-- finance_payments при этом остаётся: это журнал «за какую дату платежа
-- заплатили» — из него считаются среднее для примерных сумм и periods, чего в
-- транзакции нет.

-- Дерево категорий любой вложенности (образец — подгруппы Checker: parent_id
-- на себя). Удаление категории НЕ каскадит на деньги: обработчик перевешивает
-- детей и транзакции на родителя — потерять траты из-за уборки в справочнике
-- недопустимо.
CREATE TABLE finance_categories (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    parent_id BIGINT REFERENCES finance_categories(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    kind TEXT NOT NULL DEFAULT 'expense' CHECK (kind IN ('expense', 'income')),
    icon TEXT NOT NULL DEFAULT '',
    color TEXT NOT NULL DEFAULT '',
    position INT NOT NULL DEFAULT 0,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX finance_categories_user_idx ON finance_categories (user_id, parent_id, position);
-- имя уникально в пределах одного родителя (0 = корень: id всегда > 0)
CREATE UNIQUE INDEX finance_categories_uniq_idx
    ON finance_categories (user_id, COALESCE(parent_id, 0), lower(name));

-- Счета: «откуда деньги». Баланс не хранится полем — считается от стартового
-- остатка по транзакциям (то же правило, что у остатка долга).
CREATE TABLE finance_accounts (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    kind TEXT NOT NULL DEFAULT 'card'
        CHECK (kind IN ('cash', 'card', 'bank', 'savings', 'other')),
    currency TEXT NOT NULL CHECK (length(currency) BETWEEN 2 AND 10),
    start_balance NUMERIC(14,2) NOT NULL DEFAULT 0,
    -- «не считать в общий итог»: например, счёт другого человека под присмотром
    include_in_total BOOLEAN NOT NULL DEFAULT true,
    note TEXT NOT NULL DEFAULT '',
    position INT NOT NULL DEFAULT 0,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX finance_accounts_user_idx ON finance_accounts (user_id, position, id);

-- Фактические траты, доходы и переводы между счетами.
CREATE TABLE finance_transactions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind TEXT NOT NULL DEFAULT 'expense'
        CHECK (kind IN ('expense', 'income', 'transfer')),
    spent_on DATE NOT NULL,
    amount NUMERIC(14,2) NOT NULL CHECK (amount >= 0),
    currency TEXT NOT NULL,
    -- курс фиксируется на дату записи: без этого прошлые месяцы в отчёте
    -- пересчитывались бы при каждом изменении курса
    base_currency TEXT NOT NULL DEFAULT '',
    rate_to_base NUMERIC(18,8) NOT NULL DEFAULT 1,
    category_id BIGINT REFERENCES finance_categories(id) ON DELETE SET NULL,
    account_id BIGINT REFERENCES finance_accounts(id) ON DELETE SET NULL,
    -- куда переведено (только для kind = 'transfer')
    to_account_id BIGINT REFERENCES finance_accounts(id) ON DELETE SET NULL,
    -- откуда пришла запись: плановый платёж
    plan_id BIGINT REFERENCES finance_plans(id) ON DELETE SET NULL,
    payment_id BIGINT REFERENCES finance_payments(id) ON DELETE CASCADE,
    merchant TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT '',
    -- идемпотентность будущего импорта писем магазина
    external_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX finance_tx_user_date_idx ON finance_transactions (user_id, spent_on DESC, id DESC);
CREATE INDEX finance_tx_category_idx ON finance_transactions (user_id, category_id);
CREATE INDEX finance_tx_account_idx ON finance_transactions (user_id, account_id);
CREATE UNIQUE INDEX finance_tx_external_idx
    ON finance_transactions (user_id, external_id) WHERE external_id IS NOT NULL;

-- Цели «отложено на»: конверт поверх счёта, а не отдельный кошелёк — деньги
-- физически лежат на счёте, поэтому баланс счёта цель не меняет.
CREATE TABLE finance_goals (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    target_amount NUMERIC(14,2) NOT NULL CHECK (target_amount > 0),
    currency TEXT NOT NULL,
    account_id BIGINT REFERENCES finance_accounts(id) ON DELETE SET NULL,
    due_date DATE,
    note TEXT NOT NULL DEFAULT '',
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX finance_goals_user_idx ON finance_goals (user_id, due_date NULLS LAST, id);

-- Движения по цели: плюс — отложил, минус — снял. Накопленное считается по
-- истории, а не хранится полем.
CREATE TABLE finance_goal_moves (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    goal_id BIGINT NOT NULL REFERENCES finance_goals(id) ON DELETE CASCADE,
    moved_on DATE NOT NULL,
    amount NUMERIC(14,2) NOT NULL CHECK (amount <> 0),
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX finance_goal_moves_goal_idx ON finance_goal_moves (goal_id, moved_on);

-- План получает категорию из дерева и счёт списания. Текстовая category
-- остаётся денормализованным именем (её показывают списки и вхождения) и
-- обновляется вместе с category_id.
ALTER TABLE finance_plans
    ADD COLUMN category_id BIGINT REFERENCES finance_categories(id) ON DELETE SET NULL,
    ADD COLUMN account_id BIGINT REFERENCES finance_accounts(id) ON DELETE SET NULL;

-- Перенос существующих текстовых категорий планов в дерево (по названиям).
INSERT INTO finance_categories (user_id, name)
SELECT DISTINCT ON (user_id, lower(btrim(category))) user_id, btrim(category)
FROM finance_plans
WHERE btrim(category) <> ''
ORDER BY user_id, lower(btrim(category)), id;

UPDATE finance_plans p SET category_id = c.id
FROM finance_categories c
WHERE c.user_id = p.user_id
  AND c.parent_id IS NULL
  AND lower(c.name) = lower(btrim(p.category))
  AND btrim(p.category) <> '';

-- +goose Down
ALTER TABLE finance_plans DROP COLUMN category_id, DROP COLUMN account_id;
DROP TABLE finance_goal_moves;
DROP TABLE finance_goals;
DROP TABLE finance_transactions;
DROP TABLE finance_accounts;
DROP TABLE finance_categories;
