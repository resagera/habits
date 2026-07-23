-- +goose Up
-- Food: дневник питания — профиль, периоды целей, каталог продуктов,
-- приёмы пищи со снимками КБЖУ, шаблоны, рецепты, шаринг дневника.

-- Профиль питания (одна строка на пользователя).
CREATE TABLE food_profiles (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    sex TEXT NOT NULL DEFAULT '' CHECK (sex IN ('', 'male', 'female')),
    birth_date DATE,
    height_cm DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (height_cm >= 0 AND height_cm <= 300),
    weight_kg DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (weight_kg >= 0 AND weight_kg <= 500),
    target_weight_kg DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (target_weight_kg >= 0 AND target_weight_kg <= 500),
    body_fat_percent DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (body_fat_percent >= 0 AND body_fat_percent <= 70),
    activity_level TEXT NOT NULL DEFAULT 'medium'
        CHECK (activity_level IN ('min', 'low', 'medium', 'high', 'max')),
    goal_type TEXT NOT NULL DEFAULT 'maintain' CHECK (goal_type IN ('lose', 'maintain', 'gain')),
    rate_kcal DOUBLE PRECISION NOT NULL DEFAULT 400 CHECK (rate_kcal >= 0 AND rate_kcal <= 1500),
    protein_base TEXT NOT NULL DEFAULT 'current'
        CHECK (protein_base IN ('current', 'target', 'lean', 'manual')),
    protein_base_kg DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (protein_base_kg >= 0 AND protein_base_kg <= 500),
    protein_coef DOUBLE PRECISION NOT NULL DEFAULT 1.2 CHECK (protein_coef >= 0.5 AND protein_coef <= 4),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Периоды дневных целей: смена цели закрывает прошлый период,
-- прошлые даты считаются по цели своего периода.
CREATE TABLE food_goals (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    date_from DATE NOT NULL,
    date_to DATE CHECK (date_to IS NULL OR date_to >= date_from),
    goal_type TEXT NOT NULL DEFAULT 'maintain' CHECK (goal_type IN ('lose', 'maintain', 'gain')),
    calories DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (calories >= 0),
    protein DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (protein >= 0),
    fat DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (fat >= 0),
    carbs DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (carbs >= 0),
    source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('auto', 'manual')),
    details TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX food_goals_user_idx ON food_goals (user_id, date_from);

-- Каталог продуктов пользователя. КБЖУ — на 100 г или 100 мл (base_type).
CREATE TABLE food_products (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    alt_name TEXT NOT NULL DEFAULT '',
    brand TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT '',
    photo TEXT NOT NULL DEFAULT '',
    base_type TEXT NOT NULL DEFAULT 'g' CHECK (base_type IN ('g', 'ml')),
    calories DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (calories >= 0 AND calories <= 10000),
    protein DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (protein >= 0 AND protein <= 1000),
    fat DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (fat >= 0 AND fat <= 1000),
    carbs DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (carbs >= 0 AND carbs <= 1000),
    piece_grams DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (piece_grams >= 0),
    portion_grams DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (portion_grams >= 0),
    archived BOOLEAN NOT NULL DEFAULT FALSE,
    used_count BIGINT NOT NULL DEFAULT 0,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX food_products_user_idx ON food_products (user_id, archived, last_used_at DESC NULLS LAST);

-- Приём пищи. Итоговое КБЖУ хранится (сумма элементов или ручной ввод).
CREATE TABLE food_meals (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day DATE NOT NULL,
    at_time TEXT NOT NULL DEFAULT '',
    meal_type TEXT NOT NULL DEFAULT 'none'
        CHECK (meal_type IN ('breakfast', 'lunch', 'dinner', 'snack', 'none')),
    name TEXT NOT NULL DEFAULT '' CHECK (length(name) <= 200),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
    photo TEXT NOT NULL DEFAULT '',
    source_type TEXT NOT NULL DEFAULT '' CHECK (source_type IN ('', 'template', 'recipe')),
    source_id BIGINT,
    calories DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (calories >= 0),
    protein DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (protein >= 0),
    fat DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (fat >= 0),
    carbs DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (carbs >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX food_meals_user_day_idx ON food_meals (user_id, day);

-- Элемент приёма пищи: СНИМОК данных продукта на момент добавления —
-- последующее изменение продукта историю не трогает.
CREATE TABLE food_meal_items (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    meal_id BIGINT NOT NULL REFERENCES food_meals(id) ON DELETE CASCADE,
    product_id BIGINT REFERENCES food_products(id) ON DELETE SET NULL,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    amount DOUBLE PRECISION NOT NULL CHECK (amount > 0),
    unit TEXT NOT NULL DEFAULT 'g' CHECK (unit IN ('g', 'ml', 'piece', 'portion')),
    grams DOUBLE PRECISION NOT NULL CHECK (grams > 0),
    base_type TEXT NOT NULL DEFAULT 'g' CHECK (base_type IN ('g', 'ml')),
    calories_per DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (calories_per >= 0),
    protein_per DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (protein_per >= 0),
    fat_per DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (fat_per >= 0),
    carbs_per DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (carbs_per >= 0),
    calories DOUBLE PRECISION NOT NULL DEFAULT 0,
    protein DOUBLE PRECISION NOT NULL DEFAULT 0,
    fat DOUBLE PRECISION NOT NULL DEFAULT 0,
    carbs DOUBLE PRECISION NOT NULL DEFAULT 0,
    position INT NOT NULL DEFAULT 0
);
CREATE INDEX food_meal_items_meal_idx ON food_meal_items (meal_id, position);

-- Шаблон приёма пищи (без даты); элементы — такие же снимки.
CREATE TABLE food_templates (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
    photo TEXT NOT NULL DEFAULT '',
    meal_type TEXT NOT NULL DEFAULT 'none'
        CHECK (meal_type IN ('breakfast', 'lunch', 'dinner', 'snack', 'none')),
    archived BOOLEAN NOT NULL DEFAULT FALSE,
    calories DOUBLE PRECISION NOT NULL DEFAULT 0,
    protein DOUBLE PRECISION NOT NULL DEFAULT 0,
    fat DOUBLE PRECISION NOT NULL DEFAULT 0,
    carbs DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX food_templates_user_idx ON food_templates (user_id, archived);

CREATE TABLE food_template_items (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    template_id BIGINT NOT NULL REFERENCES food_templates(id) ON DELETE CASCADE,
    product_id BIGINT REFERENCES food_products(id) ON DELETE SET NULL,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    amount DOUBLE PRECISION NOT NULL CHECK (amount > 0),
    unit TEXT NOT NULL DEFAULT 'g' CHECK (unit IN ('g', 'ml', 'piece', 'portion')),
    grams DOUBLE PRECISION NOT NULL CHECK (grams > 0),
    base_type TEXT NOT NULL DEFAULT 'g' CHECK (base_type IN ('g', 'ml')),
    calories_per DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (calories_per >= 0),
    protein_per DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (protein_per >= 0),
    fat_per DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (fat_per >= 0),
    carbs_per DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (carbs_per >= 0),
    calories DOUBLE PRECISION NOT NULL DEFAULT 0,
    protein DOUBLE PRECISION NOT NULL DEFAULT 0,
    fat DOUBLE PRECISION NOT NULL DEFAULT 0,
    carbs DOUBLE PRECISION NOT NULL DEFAULT 0,
    position INT NOT NULL DEFAULT 0
);
CREATE INDEX food_template_items_tpl_idx ON food_template_items (template_id, position);

-- Рецепт: ингредиенты-снимки, итоговый вес и порции — для КБЖУ на 100 г/порцию.
CREATE TABLE food_recipes (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
    steps TEXT NOT NULL DEFAULT '' CHECK (length(steps) <= 10000),
    photo TEXT NOT NULL DEFAULT '',
    final_weight DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (final_weight >= 0),
    portions DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (portions >= 0),
    archived BOOLEAN NOT NULL DEFAULT FALSE,
    calories DOUBLE PRECISION NOT NULL DEFAULT 0,
    protein DOUBLE PRECISION NOT NULL DEFAULT 0,
    fat DOUBLE PRECISION NOT NULL DEFAULT 0,
    carbs DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX food_recipes_user_idx ON food_recipes (user_id, archived);

CREATE TABLE food_recipe_items (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    recipe_id BIGINT NOT NULL REFERENCES food_recipes(id) ON DELETE CASCADE,
    product_id BIGINT REFERENCES food_products(id) ON DELETE SET NULL,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    amount DOUBLE PRECISION NOT NULL CHECK (amount > 0),
    unit TEXT NOT NULL DEFAULT 'g' CHECK (unit IN ('g', 'ml', 'piece', 'portion')),
    grams DOUBLE PRECISION NOT NULL CHECK (grams > 0),
    base_type TEXT NOT NULL DEFAULT 'g' CHECK (base_type IN ('g', 'ml')),
    calories_per DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (calories_per >= 0),
    protein_per DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (protein_per >= 0),
    fat_per DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (fat_per >= 0),
    carbs_per DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (carbs_per >= 0),
    calories DOUBLE PRECISION NOT NULL DEFAULT 0,
    protein DOUBLE PRECISION NOT NULL DEFAULT 0,
    fat DOUBLE PRECISION NOT NULL DEFAULT 0,
    carbs DOUBLE PRECISION NOT NULL DEFAULT 0,
    position INT NOT NULL DEFAULT 0
);
CREATE INDEX food_recipe_items_recipe_idx ON food_recipe_items (recipe_id, position);

-- Доступ к дневнику (read-only): владелец → получатель с флагами видимости.
CREATE TABLE food_shares (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    show_weight BOOLEAN NOT NULL DEFAULT TRUE,
    show_goals BOOLEAN NOT NULL DEFAULT TRUE,
    show_photos BOOLEAN NOT NULL DEFAULT TRUE,
    show_notes BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, target_id)
);
CREATE INDEX food_shares_target_idx ON food_shares (target_id);

-- +goose Down
DROP TABLE food_shares;
DROP TABLE food_recipe_items;
DROP TABLE food_recipes;
DROP TABLE food_template_items;
DROP TABLE food_templates;
DROP TABLE food_meal_items;
DROP TABLE food_meals;
DROP TABLE food_products;
DROP TABLE food_goals;
DROP TABLE food_profiles;
