-- +goose Up
-- Food: план питания — «намерение» на N дней (2 недели и т.п.), возможно
-- на нескольких участников, возможно приблизительно.
--
-- Отличие от дневника/шаблонов/рецептов: те хранят СНИМОК продукта, а план
-- хранит ССЫЛКУ (ref_id) + кэш КБЖУ. Применение дня плана в дневник делает
-- свежий снимок — дальнейшие правки плана записи дневника не трогают.

-- План. Слоты привязаны к day_index (0-based), а не к дате: план можно
-- сдвинуть, продлить, скопировать неделю в неделю. start_date NULL —
-- «шаблонный» план, применяется к любой дате.
CREATE TABLE food_plans (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
    days INT NOT NULL DEFAULT 7 CHECK (days BETWEEN 1 AND 90),
    start_date DATE,
    archived BOOLEAN NOT NULL DEFAULT FALSE,
    share_token TEXT UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX food_plans_user_idx ON food_plans (user_id, archived, updated_at DESC);

-- Участник плана: именованное лицо (жена, ребёнок) — необязательно
-- пользователь Habits. portion_coef — множитель порции для ОБЩИХ слотов.
CREATE TABLE food_plan_participants (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    plan_id BIGINT NOT NULL REFERENCES food_plans(id) ON DELETE CASCADE,
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    emoji TEXT NOT NULL DEFAULT '' CHECK (length(emoji) <= 8),
    portion_coef DOUBLE PRECISION NOT NULL DEFAULT 1 CHECK (portion_coef > 0 AND portion_coef <= 10),
    calories_target DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (calories_target >= 0 AND calories_target <= 100000),
    position INT NOT NULL DEFAULT 0
);
CREATE INDEX food_plan_participants_plan_idx ON food_plan_participants (plan_id, position);

-- Слот: приём пищи одного дня плана. participant_id NULL — общий слот
-- (готовится на всех, каждому по своему коэффициенту порции).
CREATE TABLE food_plan_slots (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    plan_id BIGINT NOT NULL REFERENCES food_plans(id) ON DELETE CASCADE,
    participant_id BIGINT REFERENCES food_plan_participants(id) ON DELETE CASCADE,
    day_index INT NOT NULL CHECK (day_index >= 0 AND day_index < 90),
    meal_type TEXT NOT NULL DEFAULT 'none'
        CHECK (meal_type IN ('breakfast', 'lunch', 'dinner', 'snack', 'none')),
    at_time TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '' CHECK (length(title) <= 200),
    note TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 2000),
    position INT NOT NULL DEFAULT 0
);
CREATE INDEX food_plan_slots_plan_idx ON food_plan_slots (plan_id, day_index, position);

-- Позиция слота. Три уровня точности:
--   kind='free'                — свободный текст («что-нибудь мясное»), КБЖУ нет;
--   approx = TRUE  + ref       — продукт/рецепт без количества, КБЖУ нет;
--   approx = FALSE + ref       — количество известно, КБЖУ считается.
-- ref_id БЕЗ внешнего ключа: удаление рецепта/продукта не должно ломать
-- план — позиция деградирует в свободную с сохранённым названием.
CREATE TABLE food_plan_items (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slot_id BIGINT NOT NULL REFERENCES food_plan_slots(id) ON DELETE CASCADE,
    kind TEXT NOT NULL DEFAULT 'free' CHECK (kind IN ('free', 'product', 'recipe', 'template')),
    ref_id BIGINT,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    approx BOOLEAN NOT NULL DEFAULT TRUE,
    amount DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (amount >= 0),
    unit TEXT NOT NULL DEFAULT 'g' CHECK (unit IN ('g', 'ml', 'piece', 'portion')),
    grams DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (grams >= 0),
    base_type TEXT NOT NULL DEFAULT 'g' CHECK (base_type IN ('g', 'ml')),
    calories_per DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (calories_per >= 0),
    protein_per DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (protein_per >= 0),
    fat_per DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (fat_per >= 0),
    carbs_per DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (carbs_per >= 0),
    calories DOUBLE PRECISION NOT NULL DEFAULT 0,
    protein DOUBLE PRECISION NOT NULL DEFAULT 0,
    fat DOUBLE PRECISION NOT NULL DEFAULT 0,
    carbs DOUBLE PRECISION NOT NULL DEFAULT 0,
    position INT NOT NULL DEFAULT 0,
    CHECK (approx OR grams > 0)
);
CREATE INDEX food_plan_items_slot_idx ON food_plan_items (slot_id, position);

-- Совместный доступ к плану: can_edit — правка слотов/позиций (участники,
-- шаринг и удаление плана остаются за владельцем).
CREATE TABLE food_plan_shares (
    plan_id BIGINT NOT NULL REFERENCES food_plans(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    can_edit BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (plan_id, user_id)
);
CREATE INDEX food_plan_shares_user_idx ON food_plan_shares (user_id);

-- Записи дневника, созданные из плана (source_id = id плана).
ALTER TABLE food_meals DROP CONSTRAINT food_meals_source_type_check;
ALTER TABLE food_meals ADD CONSTRAINT food_meals_source_type_check
    CHECK (source_type IN ('', 'template', 'recipe', 'plan'));

-- +goose Down
-- откат не теряет записи дневника: source_type сбрасывается, сами записи остаются
ALTER TABLE food_meals DROP CONSTRAINT food_meals_source_type_check;
UPDATE food_meals SET source_type = '', source_id = NULL WHERE source_type = 'plan';
ALTER TABLE food_meals ADD CONSTRAINT food_meals_source_type_check
    CHECK (source_type IN ('', 'template', 'recipe'));
DROP TABLE food_plan_shares;
DROP TABLE food_plan_items;
DROP TABLE food_plan_slots;
DROP TABLE food_plan_participants;
DROP TABLE food_plans;
