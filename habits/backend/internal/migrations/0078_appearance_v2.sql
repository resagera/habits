-- +goose Up
-- Оформление 2.0: несколько тем вместо пары «светлая/тёмная», своя тема с
-- сохранением, рамки, папки фонов и общая галерея.
--
-- Ключевое решение: оформление хранится ОДНИМ JSONB на режим входа, а не
-- колонками. Раньше каждая новая настройка (цвет текста, цвет фона,
-- непрозрачность карточек) означала миграцию и правки в четырёх местах;
-- колонок уже набралось восемь, а тем всё ещё было две.
--
-- Форма значения:
--   {"mode":"auto|fixed", "theme_id":"night", "auto_light":"day",
--    "auto_dark":"night", "draft":{<токены>}|null}
-- Токены: bg, text, text_secondary, accent, card_rgb, card_alpha, card_blur,
-- border_card_color, border_card_width, border_btn_color, border_btn_width,
-- cell_bg, hover_bg.
ALTER TABLE user_settings
    ADD COLUMN appearance JSONB NOT NULL DEFAULT '{}',
    -- своё оформление для входа по токену (веб, расширение) — как и тема
    -- раньше, на мини-приложение не влияет
    ADD COLUMN web_appearance JSONB NOT NULL DEFAULT '{}',
    -- фон: масштаб и смещение (для «по центру» и «замостить»),
    -- точка фокуса (для «заполнить» — какая часть картинки остаётся видимой)
    ADD COLUMN bg_scale INT NOT NULL DEFAULT 100 CHECK (bg_scale BETWEEN 10 AND 400),
    ADD COLUMN bg_offset_x INT NOT NULL DEFAULT 0 CHECK (bg_offset_x BETWEEN -100 AND 100),
    ADD COLUMN bg_offset_y INT NOT NULL DEFAULT 0 CHECK (bg_offset_y BETWEEN -100 AND 100),
    ADD COLUMN bg_focal_x INT NOT NULL DEFAULT 50 CHECK (bg_focal_x BETWEEN 0 AND 100),
    ADD COLUMN bg_focal_y INT NOT NULL DEFAULT 50 CHECK (bg_focal_y BETWEEN 0 AND 100);

-- Перенос нынешних настроек в черновик «Своя тема»: у тех, кто уже подбирал
-- цвета, вид не должен измениться. Тема берётся прежняя (theme), а токены —
-- только те, что человек реально менял (пустая строка = «как в теме»).
-- +goose StatementBegin
UPDATE user_settings SET appearance = jsonb_strip_nulls(jsonb_build_object(
    'mode', CASE WHEN theme = 'auto' THEN 'auto' ELSE 'fixed' END,
    'theme_id', CASE theme WHEN 'light' THEN 'day' WHEN 'dark' THEN 'night' ELSE 'night' END,
    'auto_light', 'day',
    'auto_dark', 'night',
    'draft', CASE WHEN text_color_dark = '' AND text_color_light = ''
                   AND bg_color_dark = '' AND bg_color_light = ''
                   AND card_opacity = 100 AND card_blur = 0
             THEN NULL
             ELSE jsonb_strip_nulls(jsonb_build_object(
                 'text', NULLIF(CASE WHEN theme = 'light' THEN text_color_light ELSE text_color_dark END, ''),
                 'bg', NULLIF(CASE WHEN theme = 'light' THEN bg_color_light ELSE bg_color_dark END, ''),
                 'card_alpha', card_opacity / 100.0,
                 'card_blur', card_blur))
             END));
-- +goose StatementEnd

-- Прежняя настройка «без фоновой картинки» в браузере — в web_appearance.
-- +goose StatementBegin
UPDATE user_settings SET web_appearance = jsonb_build_object(
    'mode', 'auto', 'theme_id', 'night', 'auto_light', 'day', 'auto_dark', 'night',
    'bg_off', web_bg_off)
WHERE web_bg_off OR web_theme <> '';
-- +goose StatementEnd

-- Сохранённые темы пользователя: токены + снимок оформления карточек и фона.
CREATE TABLE user_themes (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 60),
    kind TEXT NOT NULL DEFAULT 'dark' CHECK (kind IN ('light', 'dark')),
    tokens JSONB NOT NULL DEFAULT '{}',
    -- фон запоминается вместе с темой: «своя тема» — это весь вид целиком
    bg JSONB NOT NULL DEFAULT '{}',
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, name)
);
CREATE INDEX user_themes_user_idx ON user_themes (user_id, position, id);

-- Папки своих фонов: любая вложенность, сворачивание хранится на папке.
CREATE TABLE background_folders (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    parent_id BIGINT REFERENCES background_folders(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    position INT NOT NULL DEFAULT 0,
    collapsed BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX background_folders_user_idx ON background_folders (user_id, parent_id, position);

ALTER TABLE user_backgrounds
    ADD COLUMN folder_id BIGINT REFERENCES background_folders(id) ON DELETE SET NULL,
    -- уменьшенная копия: без неё экран выбора фона тянет десятки мегабайт
    ADD COLUMN thumb TEXT NOT NULL DEFAULT '';

-- Общая галерея фонов: наполняется админом, видна всем.
CREATE TABLE gallery_categories (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    parent_id BIGINT REFERENCES gallery_categories(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE gallery_images (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    category_id BIGINT REFERENCES gallery_categories(id) ON DELETE SET NULL,
    filename TEXT NOT NULL UNIQUE,
    thumb TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    position INT NOT NULL DEFAULT 0,
    uploaded_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX gallery_images_category_idx ON gallery_images (category_id, position, id);

-- +goose Down
DROP TABLE gallery_images;
DROP TABLE gallery_categories;
ALTER TABLE user_backgrounds DROP COLUMN folder_id, DROP COLUMN thumb;
DROP TABLE background_folders;
DROP TABLE user_themes;
ALTER TABLE user_settings
    DROP COLUMN appearance,
    DROP COLUMN web_appearance,
    DROP COLUMN bg_scale,
    DROP COLUMN bg_offset_x,
    DROP COLUMN bg_offset_y,
    DROP COLUMN bg_focal_x,
    DROP COLUMN bg_focal_y;
