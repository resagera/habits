-- +goose Up
-- Страница «Тесты»: колоды вопросов с вариантами ответов и личным прогрессом.
-- Первая колода — официальные экзаменационные вопросы по ПДД Армении
-- (roadpolice.am, комплект от 25.12.2025), заливается не миграцией, а
-- админским импортом: 1032 вопроса и 669 картинок внутри .sql нечитаемы и
-- неисправимы, а импорт идемпотентен и повторяется при обновлении комплекта.
--
-- Контент (колода/группы/вопросы) общий для всех, прогресс — личный.

CREATE TABLE test_decks (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE CHECK (length(slug) BETWEEN 1 AND 64),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '',
    lang TEXT NOT NULL DEFAULT 'ru',
    source_url TEXT NOT NULL DEFAULT '',
    -- ревизия комплекта у источника: по ней видно, что пора переимпортировать
    revision TEXT NOT NULL DEFAULT '',
    -- параметры экзаменационного режима (у ПДД Армении: 20 вопросов, 30 минут,
    -- допускаются 2 ошибки) — у другой колоды будут свои
    exam_size INT NOT NULL DEFAULT 20 CHECK (exam_size > 0),
    exam_minutes INT NOT NULL DEFAULT 30 CHECK (exam_minutes > 0),
    exam_allowed_mistakes INT NOT NULL DEFAULT 2 CHECK (exam_allowed_mistakes >= 0),
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE test_groups (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    deck_id BIGINT NOT NULL REFERENCES test_decks(id) ON DELETE CASCADE,
    num INT NOT NULL,
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 300),
    UNIQUE (deck_id, num)
);

CREATE TABLE test_questions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    deck_id BIGINT NOT NULL REFERENCES test_decks(id) ON DELETE CASCADE,
    group_id BIGINT REFERENCES test_groups(id) ON DELETE SET NULL,
    -- номер вопроса в колоде: якорь для идемпотентного переимпорта
    num INT NOT NULL,
    text TEXT NOT NULL DEFAULT '',
    -- варианты ответов: массив строк, от 2 до 6
    options JSONB NOT NULL,
    -- индекс правильного варианта, 0-based
    correct_idx SMALLINT NOT NULL CHECK (correct_idx >= 0),
    -- имя файла в DATA_DIR/tests (пусто — вопрос без картинки)
    image TEXT NOT NULL DEFAULT '',
    explanation TEXT NOT NULL DEFAULT '',
    UNIQUE (deck_id, num)
);
CREATE INDEX test_questions_group_idx ON test_questions (group_id);

-- Личный прогресс по вопросу. status: new (записи нет) | wrong | passed.
-- «Пройден» — после N верных ответов подряд (N из user_settings.tests_pass_streak,
-- по умолчанию 1); ошибка сбрасывает серию и возвращает вопрос в пул.
CREATE TABLE test_progress (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    question_id BIGINT NOT NULL REFERENCES test_questions(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'wrong' CHECK (status IN ('wrong', 'passed')),
    correct_streak INT NOT NULL DEFAULT 0,
    correct_count INT NOT NULL DEFAULT 0,
    wrong_count INT NOT NULL DEFAULT 0,
    last_answer_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, question_id)
);
CREATE INDEX test_progress_user_status_idx ON test_progress (user_id, status);

-- Прогон: фиксированный порядок вопросов, чтобы перезаход в приложение не
-- перетасовывал колоду под пользователем.
CREATE TABLE test_sessions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    deck_id BIGINT NOT NULL REFERENCES test_decks(id) ON DELETE CASCADE,
    -- study — учебный (сразу показываем верный ответ), exam — экзамен
    mode TEXT NOT NULL DEFAULT 'study' CHECK (mode IN ('study', 'exam')),
    -- какой пул взяли: unpassed | all | wrong | group
    scope TEXT NOT NULL DEFAULT 'unpassed',
    group_id BIGINT REFERENCES test_groups(id) ON DELETE SET NULL,
    total INT NOT NULL DEFAULT 0,
    answered INT NOT NULL DEFAULT 0,
    correct INT NOT NULL DEFAULT 0,
    -- дедлайн экзамена (NULL в учебном режиме)
    expires_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    passed BOOLEAN,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX test_sessions_user_idx ON test_sessions (user_id, id DESC);

CREATE TABLE test_session_items (
    session_id BIGINT NOT NULL REFERENCES test_sessions(id) ON DELETE CASCADE,
    position INT NOT NULL,
    question_id BIGINT NOT NULL REFERENCES test_questions(id) ON DELETE CASCADE,
    chosen_idx SMALLINT,
    is_correct BOOLEAN,
    answered_at TIMESTAMPTZ,
    PRIMARY KEY (session_id, position)
);

-- Сколько верных ответов подряд нужно, чтобы вопрос ушёл из пула.
ALTER TABLE user_settings
    ADD COLUMN tests_pass_streak SMALLINT NOT NULL DEFAULT 1
        CHECK (tests_pass_streak BETWEEN 1 AND 5);

-- +goose Down
ALTER TABLE user_settings DROP COLUMN tests_pass_streak;
DROP TABLE test_session_items;
DROP TABLE test_sessions;
DROP TABLE test_progress;
DROP TABLE test_questions;
DROP TABLE test_groups;
DROP TABLE test_decks;
