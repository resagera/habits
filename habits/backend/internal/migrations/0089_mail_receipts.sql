-- +goose Up
-- Разбор писем магазинов в траты Finance (фаза 5 плана Finance).
--
-- Письмо с чеком пересылается на выделенный адрес (yerevancity@resager.ru),
-- приёмник разбирает его и создаёт ОДНУ трату на сумму Total. Именно Total, а
-- не сумма позиций: у магазина позиции показаны по каталожным ценам, и с
-- итогом они не сходятся (вес, скидки) — из кошелька уходит Total.
--
-- Позиции хранятся отдельно и в Finance не попадают: 20 покупок одного похода
-- в магазин — это одна трата, а не двадцать.

ALTER TABLE mail_addresses
    -- какой разборщик применять к письмам на этот адрес ('' — не разбирать)
    ADD COLUMN parser TEXT NOT NULL DEFAULT '',
    -- куда записывать трату
    ADD COLUMN parser_account_id BIGINT REFERENCES finance_accounts(id) ON DELETE SET NULL,
    ADD COLUMN parser_category_id BIGINT REFERENCES finance_categories(id) ON DELETE SET NULL;

CREATE TABLE mail_receipts (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message_id BIGINT REFERENCES mail_messages(id) ON DELETE CASCADE,
    parser TEXT NOT NULL,
    merchant TEXT NOT NULL DEFAULT '',
    order_no TEXT NOT NULL DEFAULT '',
    -- момент покупки (для показа) и КАЛЕНДАРНАЯ дата чека. Дата отдельной
    -- колонкой не для красоты: timestamptz читается в зоне сессии, и покупка
    -- в 23:23 по Еревану уезжала бы в трату следующего дня.
    purchased_at TIMESTAMPTZ,
    purchased_on DATE,
    currency TEXT NOT NULL DEFAULT 'amd',
    subtotal NUMERIC(14,2) NOT NULL DEFAULT 0,
    delivery_fee NUMERIC(14,2) NOT NULL DEFAULT 0,
    service_fee NUMERIC(14,2) NOT NULL DEFAULT 0,
    tip NUMERIC(14,2) NOT NULL DEFAULT 0,
    total NUMERIC(14,2) NOT NULL DEFAULT 0,
    paid_with TEXT NOT NULL DEFAULT '',
    -- созданная трата; NULL — чек разобран, но в Finance ещё не записан
    tx_id BIGINT REFERENCES finance_transactions(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'parsed'
        CHECK (status IN ('parsed', 'imported', 'failed', 'skipped')),
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Защита от повторов: письмо можно переслать дважды, магазин может прислать
-- дубль. Один заказ — один чек.
CREATE UNIQUE INDEX mail_receipts_order_idx
    ON mail_receipts (user_id, parser, order_no) WHERE order_no <> '';
CREATE INDEX mail_receipts_user_idx ON mail_receipts (user_id, created_at DESC);
CREATE INDEX mail_receipts_msg_idx ON mail_receipts (message_id);

CREATE TABLE mail_receipt_items (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    receipt_id BIGINT NOT NULL REFERENCES mail_receipts(id) ON DELETE CASCADE,
    position INT NOT NULL DEFAULT 0,
    name TEXT NOT NULL,
    qty NUMERIC(12,3) NOT NULL DEFAULT 1,
    unit TEXT NOT NULL DEFAULT '',
    amount NUMERIC(14,2) NOT NULL DEFAULT 0
);
CREATE INDEX mail_receipt_items_idx ON mail_receipt_items (receipt_id, position);

-- +goose Down
DROP TABLE mail_receipt_items;
DROP TABLE mail_receipts;
ALTER TABLE mail_addresses
    DROP COLUMN parser, DROP COLUMN parser_account_id, DROP COLUMN parser_category_id;
