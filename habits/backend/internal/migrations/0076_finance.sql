-- +goose Up
-- Страница Finance, фазы 1+2: плановые траты с оплатой и напоминаниями,
-- долги «нам должны» / «я должен» с частичными возвратами.
--
-- Ключевое решение: будущие платежи НЕ материализуются. Хранится план и
-- правило повтора, а вхождения на месяц вперёд считаются на лету (как Calendar
-- раскладывает повторяющиеся напоминания). Иначе пришлось бы переписывать
-- тысячи будущих строк при каждом изменении суммы.
--
-- Суммы — NUMERIC(14,2): деньги в float хранить нельзя.

CREATE TABLE finance_plans (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    amount NUMERIC(14,2) NOT NULL CHECK (amount >= 0),
    currency TEXT NOT NULL CHECK (length(currency) BETWEEN 2 AND 10),
    -- примерная сумма (ЖКХ): в списках показывается со знаком «≈»,
    -- при оплате подставляется среднее последних платежей
    is_estimate BOOLEAN NOT NULL DEFAULT false,
    next_due_date DATE NOT NULL,
    repeat_kind TEXT NOT NULL DEFAULT 'monthly'
        CHECK (repeat_kind IN ('once', 'monthly', 'yearly', 'interval_months', 'interval_days')),
    interval_n INT NOT NULL DEFAULT 1 CHECK (interval_n BETWEEN 1 AND 365),
    category TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT '',
    -- автоплатёж: не напоминаем заранее (деньги уйдут сами), но на следующий
    -- день спрашиваем «списалось?» — данные должны оставаться честными
    autopay BOOLEAN NOT NULL DEFAULT false,
    notify_days_before SMALLINT NOT NULL DEFAULT 1
        CHECK (notify_days_before BETWEEN 0 AND 30),
    -- «заморозить» подписку до даты: из списков уходит, не напоминает
    paused_until DATE,
    enabled BOOLEAN NOT NULL DEFAULT true,
    archived_at TIMESTAMPTZ,
    -- дедуп уведомлений: за какую дату платежа уже писали
    notified_for DATE,
    autopay_asked_for DATE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX finance_plans_user_due_idx ON finance_plans (user_id, next_due_date);

-- История оплат. Курс фиксируется на дату платежа: иначе прошлые месяцы
-- «плывут» при каждом изменении курса.
CREATE TABLE finance_payments (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan_id BIGINT NOT NULL REFERENCES finance_plans(id) ON DELETE CASCADE,
    paid_on DATE NOT NULL,
    due_date DATE NOT NULL,            -- за какую дату платежа
    amount NUMERIC(14,2) NOT NULL CHECK (amount >= 0),
    currency TEXT NOT NULL,
    base_currency TEXT NOT NULL DEFAULT '',
    rate_to_base NUMERIC(18,8) NOT NULL DEFAULT 1,
    periods INT NOT NULL DEFAULT 1 CHECK (periods BETWEEN 1 AND 120),
    -- пропуск: дата сдвинута, деньги не потрачены
    skipped BOOLEAN NOT NULL DEFAULT false,
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX finance_payments_plan_idx ON finance_payments (plan_id, paid_on DESC);
CREATE INDEX finance_payments_user_date_idx ON finance_payments (user_id, paid_on);

-- Долги в обе стороны: «нам должны» и «я должен» — одна таблица с направлением.
CREATE TABLE finance_debts (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    direction TEXT NOT NULL DEFAULT 'owed_to_me'
        CHECK (direction IN ('owed_to_me', 'i_owe')),
    person TEXT NOT NULL CHECK (length(person) BETWEEN 1 AND 200),
    -- необязательная связь с пользователем приложения из Contacts
    contact_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    subject TEXT NOT NULL DEFAULT '',
    amount NUMERIC(14,2) NOT NULL CHECK (amount > 0),
    currency TEXT NOT NULL,
    expected_on DATE,
    -- forgiven — простили остаток; paid/partial считаются по возвратам
    status TEXT NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'partial', 'paid', 'forgiven')),
    note TEXT NOT NULL DEFAULT '',
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX finance_debts_user_idx ON finance_debts (user_id, status, expected_on);

-- Возвраты частями. Остаток считается по истории, а не хранится полем —
-- хранимое поле неизбежно разъезжается с записями.
CREATE TABLE finance_debt_payments (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    debt_id BIGINT NOT NULL REFERENCES finance_debts(id) ON DELETE CASCADE,
    paid_on DATE NOT NULL,
    amount NUMERIC(14,2) NOT NULL CHECK (amount > 0),
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX finance_debt_payments_debt_idx ON finance_debt_payments (debt_id, paid_on);

-- Настройки страницы: базовая валюта для сводки, час утреннего уведомления,
-- часовой пояс (приходит с клиента) и «скрыть суммы» для показа экрана.
ALTER TABLE user_settings
    ADD COLUMN finance_base_currency TEXT NOT NULL DEFAULT 'amd',
    ADD COLUMN finance_notify_hour SMALLINT NOT NULL DEFAULT 10
        CHECK (finance_notify_hour BETWEEN 0 AND 23),
    ADD COLUMN finance_tz_off SMALLINT NOT NULL DEFAULT 0
        CHECK (finance_tz_off BETWEEN -840 AND 840),
    ADD COLUMN finance_hide_amounts BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE user_settings
    DROP COLUMN finance_base_currency,
    DROP COLUMN finance_notify_hour,
    DROP COLUMN finance_tz_off,
    DROP COLUMN finance_hide_amounts;
DROP TABLE finance_debt_payments;
DROP TABLE finance_debts;
DROP TABLE finance_payments;
DROP TABLE finance_plans;
