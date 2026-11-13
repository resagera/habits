package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNoQuestions — в выбранном пуле не осталось вопросов (например, всё пройдено).
var ErrNoQuestions = errors.New("no questions in pool")

// TestDeck — колода вопросов. Контент общий для всех пользователей.
type TestDeck struct {
	ID                  int64  `json:"id"`
	Slug                string `json:"slug"`
	Title               string `json:"title"`
	Description         string `json:"description"`
	Lang                string `json:"lang"`
	SourceURL           string `json:"source_url"`
	Revision            string `json:"revision"`
	ExamSize            int    `json:"exam_size"`
	ExamMinutes         int    `json:"exam_minutes"`
	ExamAllowedMistakes int    `json:"exam_allowed_mistakes"`
	// сводка по пользователю
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Wrong  int `json:"wrong"`
}

// TestGroup — тема внутри колоды (со счётчиками прогресса пользователя).
type TestGroup struct {
	ID     int64  `json:"id"`
	Num    int    `json:"num"`
	Title  string `json:"title"`
	Total  int    `json:"total"`
	Passed int    `json:"passed"`
	Wrong  int    `json:"wrong"`
}

// TestQuestion — вопрос. CorrectIdx наружу отдаётся только после ответа.
type TestQuestion struct {
	ID          int64    `json:"id"`
	Num         int      `json:"num"`
	GroupID     *int64   `json:"group_id"`
	GroupTitle  string   `json:"group_title"`
	Text        string   `json:"text"`
	Options     []string `json:"options"`
	CorrectIdx  int      `json:"-"`
	Image       string   `json:"image"`
	Explanation string   `json:"explanation"`
}

// TestSession — прогон с зафиксированным порядком вопросов.
type TestSession struct {
	ID         int64      `json:"id"`
	DeckID     int64      `json:"deck_id"`
	Mode       string     `json:"mode"`
	Scope      string     `json:"scope"`
	GroupID    *int64     `json:"group_id"`
	Total      int        `json:"total"`
	Answered   int        `json:"answered"`
	Correct    int        `json:"correct"`
	ExpiresAt  *time.Time `json:"expires_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Passed     *bool      `json:"passed"`
	CreatedAt  time.Time  `json:"created_at"`
}

// TestSessionItem — вопрос в прогоне вместе с ответом пользователя.
type TestSessionItem struct {
	Position   int    `json:"position"`
	QuestionID int64  `json:"question_id"`
	ChosenIdx  *int16 `json:"chosen_idx"`
	IsCorrect  *bool  `json:"is_correct"`
}

// --- колоды ---

// ListTestDecks — колоды со сводкой прогресса пользователя.
func (s *Store) ListTestDecks(ctx context.Context, userID int64) ([]TestDeck, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.id, d.slug, d.title, d.description, d.lang, d.source_url, d.revision,
		       d.exam_size, d.exam_minutes, d.exam_allowed_mistakes,
		       count(q.id)::int AS total,
		       count(*) FILTER (WHERE p.status = 'passed')::int AS passed,
		       count(*) FILTER (WHERE p.status = 'wrong')::int  AS wrong
		FROM test_decks d
		LEFT JOIN test_questions q ON q.deck_id = d.id
		LEFT JOIN test_progress p  ON p.question_id = q.id AND p.user_id = $1
		GROUP BY d.id
		ORDER BY d.position, d.id`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[TestDeck])
}

// TestDeckBySlug — колода по slug (для импорта и ссылок).
func (s *Store) TestDeckBySlug(ctx context.Context, slug string) (TestDeck, error) {
	var d TestDeck
	err := s.pool.QueryRow(ctx, `
		SELECT id, slug, title, description, lang, source_url, revision,
		       exam_size, exam_minutes, exam_allowed_mistakes
		FROM test_decks WHERE slug = $1`, slug).Scan(
		&d.ID, &d.Slug, &d.Title, &d.Description, &d.Lang, &d.SourceURL, &d.Revision,
		&d.ExamSize, &d.ExamMinutes, &d.ExamAllowedMistakes)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

// TestDeckByID — колода по id (без сводки прогресса).
func (s *Store) TestDeckByID(ctx context.Context, id int64) (TestDeck, error) {
	var d TestDeck
	err := s.pool.QueryRow(ctx, `
		SELECT id, slug, title, description, lang, source_url, revision,
		       exam_size, exam_minutes, exam_allowed_mistakes
		FROM test_decks WHERE id = $1`, id).Scan(
		&d.ID, &d.Slug, &d.Title, &d.Description, &d.Lang, &d.SourceURL, &d.Revision,
		&d.ExamSize, &d.ExamMinutes, &d.ExamAllowedMistakes)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

// ListTestGroups — темы колоды со счётчиками прогресса.
func (s *Store) ListTestGroups(ctx context.Context, userID, deckID int64) ([]TestGroup, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT g.id, g.num, g.title,
		       count(q.id)::int AS total,
		       count(*) FILTER (WHERE p.status = 'passed')::int AS passed,
		       count(*) FILTER (WHERE p.status = 'wrong')::int  AS wrong
		FROM test_groups g
		LEFT JOIN test_questions q ON q.group_id = g.id
		LEFT JOIN test_progress p  ON p.question_id = q.id AND p.user_id = $1
		WHERE g.deck_id = $2
		GROUP BY g.id
		ORDER BY g.num`, userID, deckID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[TestGroup])
}

// --- прогоны ---

// StartTestSessionParams — что берём в прогон.
type StartTestSessionParams struct {
	DeckID  int64
	Mode    string // study | exam
	Scope   string // unpassed | all | wrong | group
	GroupID *int64
	Limit   int // 0 — без ограничения
	Minutes int // дедлайн для экзамена, 0 — без дедлайна
}

// StartTestSession создаёт прогон: выбирает пул, перемешивает его и
// сохраняет порядок. Перемешивание на сервере — чтобы порядок пережил
// перезагрузку страницы.
func (s *Store) StartTestSession(ctx context.Context, userID int64, p StartTestSessionParams) (TestSession, error) {
	var sess TestSession
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return sess, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // откат после успешного Commit — no-op

	// пул вопросов. Аргументы собираем по месту: лишний плейсхолдер, на который
	// в запросе нет ссылки, pgx считает ошибкой.
	where := `q.deck_id = $2`
	args := []any{userID, p.DeckID}
	switch p.Scope {
	case "unpassed":
		where += ` AND (pr.status IS NULL OR pr.status <> 'passed')`
	case "wrong":
		where += ` AND pr.status = 'wrong'`
	case "group":
		where += ` AND q.group_id = $3`
		args = append(args, p.GroupID)
	case "all":
	default:
		return sess, errors.New("unknown scope")
	}
	limit := ""
	if p.Limit > 0 {
		limit = " LIMIT " + itoa(p.Limit)
	}
	rows, err := tx.Query(ctx, `
		SELECT q.id
		FROM test_questions q
		LEFT JOIN test_progress pr ON pr.question_id = q.id AND pr.user_id = $1
		WHERE `+where+`
		ORDER BY random()`+limit, args...)
	if err != nil {
		return sess, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return sess, err
	}
	if len(ids) == 0 {
		return sess, ErrNoQuestions
	}

	var expires *time.Time
	if p.Minutes > 0 {
		t := time.Now().Add(time.Duration(p.Minutes) * time.Minute)
		expires = &t
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO test_sessions (user_id, deck_id, mode, scope, group_id, total, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, deck_id, mode, scope, group_id, total, answered, correct,
		          expires_at, finished_at, passed, created_at`,
		userID, p.DeckID, p.Mode, p.Scope, p.GroupID, len(ids), expires).Scan(
		&sess.ID, &sess.DeckID, &sess.Mode, &sess.Scope, &sess.GroupID, &sess.Total,
		&sess.Answered, &sess.Correct, &sess.ExpiresAt, &sess.FinishedAt, &sess.Passed,
		&sess.CreatedAt)
	if err != nil {
		return sess, err
	}

	batch := &pgx.Batch{}
	for i, qid := range ids {
		batch.Queue(`INSERT INTO test_session_items (session_id, position, question_id)
		             VALUES ($1, $2, $3)`, sess.ID, i, qid)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return sess, err
	}
	return sess, tx.Commit(ctx)
}

// TestSessionByID — прогон пользователя.
func (s *Store) TestSessionByID(ctx context.Context, userID, id int64) (TestSession, error) {
	var sess TestSession
	err := s.pool.QueryRow(ctx, `
		SELECT id, deck_id, mode, scope, group_id, total, answered, correct,
		       expires_at, finished_at, passed, created_at
		FROM test_sessions WHERE id = $1 AND user_id = $2`, id, userID).Scan(
		&sess.ID, &sess.DeckID, &sess.Mode, &sess.Scope, &sess.GroupID, &sess.Total,
		&sess.Answered, &sess.Correct, &sess.ExpiresAt, &sess.FinishedAt, &sess.Passed,
		&sess.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return sess, ErrNotFound
	}
	return sess, err
}

// ActiveTestSession — последний незавершённый прогон по колоде (кнопка «продолжить»).
func (s *Store) ActiveTestSession(ctx context.Context, userID, deckID int64) (TestSession, error) {
	var sess TestSession
	err := s.pool.QueryRow(ctx, `
		SELECT id, deck_id, mode, scope, group_id, total, answered, correct,
		       expires_at, finished_at, passed, created_at
		FROM test_sessions
		WHERE user_id = $1 AND deck_id = $2 AND finished_at IS NULL
		ORDER BY id DESC LIMIT 1`, userID, deckID).Scan(
		&sess.ID, &sess.DeckID, &sess.Mode, &sess.Scope, &sess.GroupID, &sess.Total,
		&sess.Answered, &sess.Correct, &sess.ExpiresAt, &sess.FinishedAt, &sess.Passed,
		&sess.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return sess, ErrNotFound
	}
	return sess, err
}

// NextTestQuestion — первый неотвеченный вопрос прогона.
func (s *Store) NextTestQuestion(ctx context.Context, sessionID int64) (TestQuestion, int, error) {
	var q TestQuestion
	var pos int
	var opts []byte
	var groupTitle *string
	err := s.pool.QueryRow(ctx, `
		SELECT i.position, q.id, q.num, q.group_id, g.title, q.text, q.options,
		       q.correct_idx, q.image, q.explanation
		FROM test_session_items i
		JOIN test_questions q ON q.id = i.question_id
		LEFT JOIN test_groups g ON g.id = q.group_id
		WHERE i.session_id = $1 AND i.answered_at IS NULL
		ORDER BY i.position LIMIT 1`, sessionID).Scan(
		&pos, &q.ID, &q.Num, &q.GroupID, &groupTitle, &q.Text, &opts,
		&q.CorrectIdx, &q.Image, &q.Explanation)
	if errors.Is(err, pgx.ErrNoRows) {
		return q, 0, ErrNotFound
	}
	if err != nil {
		return q, 0, err
	}
	if groupTitle != nil {
		q.GroupTitle = *groupTitle
	}
	if err := json.Unmarshal(opts, &q.Options); err != nil {
		return q, 0, err
	}
	return q, pos, nil
}

// AnswerResult — что вернуть после ответа.
type AnswerResult struct {
	Correct    bool   `json:"correct"`
	CorrectIdx int    `json:"correct_idx"`
	Status     string `json:"status"` // прогресс по вопросу после ответа
	Session    TestSession
}

// AnswerTestQuestion засчитывает ответ: пишет его в прогон и двигает прогресс.
// passStreak — сколько верных подряд нужно для статуса passed.
func (s *Store) AnswerTestQuestion(ctx context.Context, userID, sessionID, questionID int64,
	chosen int, passStreak int) (AnswerResult, error) {
	var res AnswerResult
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// вопрос должен быть в этом прогоне и ещё не отвечен
	var pos, correctIdx int
	err = tx.QueryRow(ctx, `
		SELECT i.position, q.correct_idx
		FROM test_session_items i
		JOIN test_questions q ON q.id = i.question_id
		JOIN test_sessions s ON s.id = i.session_id
		WHERE i.session_id = $1 AND i.question_id = $2 AND i.answered_at IS NULL
		  AND s.user_id = $3 AND s.finished_at IS NULL`,
		sessionID, questionID, userID).Scan(&pos, &correctIdx)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, ErrNotFound
	}
	if err != nil {
		return res, err
	}

	ok := chosen == correctIdx
	if _, err := tx.Exec(ctx, `
		UPDATE test_session_items
		SET chosen_idx = $3, is_correct = $4, answered_at = now()
		WHERE session_id = $1 AND position = $2`, sessionID, pos, chosen, ok); err != nil {
		return res, err
	}

	// прогресс: серия верных ответов, статус passed по достижении порога
	var status string
	err = tx.QueryRow(ctx, `
		INSERT INTO test_progress (user_id, question_id, status, correct_streak,
		                           correct_count, wrong_count, last_answer_at)
		VALUES ($1, $2,
		        CASE WHEN $3 AND 1 >= $4 THEN 'passed' ELSE 'wrong' END,
		        CASE WHEN $3 THEN 1 ELSE 0 END,
		        CASE WHEN $3 THEN 1 ELSE 0 END,
		        CASE WHEN $3 THEN 0 ELSE 1 END, now())
		ON CONFLICT (user_id, question_id) DO UPDATE SET
		    correct_streak = CASE WHEN $3 THEN test_progress.correct_streak + 1 ELSE 0 END,
		    status = CASE WHEN $3 AND test_progress.correct_streak + 1 >= $4
		                  THEN 'passed' ELSE 'wrong' END,
		    correct_count = test_progress.correct_count + CASE WHEN $3 THEN 1 ELSE 0 END,
		    wrong_count = test_progress.wrong_count + CASE WHEN $3 THEN 0 ELSE 1 END,
		    last_answer_at = now()
		RETURNING status`, userID, questionID, ok, passStreak).Scan(&status)
	if err != nil {
		return res, err
	}

	// счётчики прогона + автозавершение на последнем вопросе
	var sess TestSession
	err = tx.QueryRow(ctx, `
		UPDATE test_sessions
		SET answered = answered + 1,
		    correct = correct + CASE WHEN $2 THEN 1 ELSE 0 END,
		    finished_at = CASE WHEN answered + 1 >= total THEN now() ELSE NULL END
		WHERE id = $1
		RETURNING id, deck_id, mode, scope, group_id, total, answered, correct,
		          expires_at, finished_at, passed, created_at`, sessionID, ok).Scan(
		&sess.ID, &sess.DeckID, &sess.Mode, &sess.Scope, &sess.GroupID, &sess.Total,
		&sess.Answered, &sess.Correct, &sess.ExpiresAt, &sess.FinishedAt, &sess.Passed,
		&sess.CreatedAt)
	if err != nil {
		return res, err
	}
	// экзамен: зачёт считаем в момент завершения
	if sess.FinishedAt != nil && sess.Mode == "exam" {
		if err := finishExam(ctx, tx, &sess); err != nil {
			return res, err
		}
	}

	res = AnswerResult{Correct: ok, CorrectIdx: correctIdx, Status: status, Session: sess}
	return res, tx.Commit(ctx)
}

// finishExam проставляет «сдал / не сдал» по правилам колоды.
func finishExam(ctx context.Context, tx pgx.Tx, sess *TestSession) error {
	var allowed int
	if err := tx.QueryRow(ctx,
		`SELECT exam_allowed_mistakes FROM test_decks WHERE id = $1`, sess.DeckID).Scan(&allowed); err != nil {
		return err
	}
	passed := sess.Answered-sess.Correct <= allowed
	sess.Passed = &passed
	_, err := tx.Exec(ctx, `UPDATE test_sessions SET passed = $2 WHERE id = $1`, sess.ID, passed)
	return err
}

// FinishTestSession завершает прогон досрочно (кнопка «завершить» или таймаут).
func (s *Store) FinishTestSession(ctx context.Context, userID, sessionID int64) (TestSession, error) {
	var sess TestSession
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return sess, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	err = tx.QueryRow(ctx, `
		UPDATE test_sessions SET finished_at = COALESCE(finished_at, now())
		WHERE id = $1 AND user_id = $2
		RETURNING id, deck_id, mode, scope, group_id, total, answered, correct,
		          expires_at, finished_at, passed, created_at`, sessionID, userID).Scan(
		&sess.ID, &sess.DeckID, &sess.Mode, &sess.Scope, &sess.GroupID, &sess.Total,
		&sess.Answered, &sess.Correct, &sess.ExpiresAt, &sess.FinishedAt, &sess.Passed,
		&sess.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return sess, ErrNotFound
	}
	if err != nil {
		return sess, err
	}
	if sess.Mode == "exam" && sess.Passed == nil {
		if err := finishExam(ctx, tx, &sess); err != nil {
			return sess, err
		}
	}
	return sess, tx.Commit(ctx)
}

// TestSessionReview — разбор завершённого прогона: вопросы, ответы, верный вариант.
type TestSessionReview struct {
	Question   TestQuestion `json:"question"`
	CorrectIdx int          `json:"correct_idx"`
	ChosenIdx  *int16       `json:"chosen_idx"`
	IsCorrect  *bool        `json:"is_correct"`
}

// TestSessionItems — содержимое прогона для разбора.
func (s *Store) TestSessionItems(ctx context.Context, sessionID int64) ([]TestSessionReview, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT q.id, q.num, q.group_id, COALESCE(g.title, ''), q.text, q.options,
		       q.correct_idx, q.image, q.explanation, i.chosen_idx, i.is_correct
		FROM test_session_items i
		JOIN test_questions q ON q.id = i.question_id
		LEFT JOIN test_groups g ON g.id = q.group_id
		WHERE i.session_id = $1
		ORDER BY i.position`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TestSessionReview
	for rows.Next() {
		var r TestSessionReview
		var opts []byte
		if err := rows.Scan(&r.Question.ID, &r.Question.Num, &r.Question.GroupID,
			&r.Question.GroupTitle, &r.Question.Text, &opts, &r.CorrectIdx,
			&r.Question.Image, &r.Question.Explanation, &r.ChosenIdx, &r.IsCorrect); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(opts, &r.Question.Options); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ResetTestProgress — начать колоду сначала. hard=false оставляет статистику
// (сбрасываются только статусы), hard=true удаляет прогресс полностью.
func (s *Store) ResetTestProgress(ctx context.Context, userID, deckID int64, hard bool) (int64, error) {
	if hard {
		tag, err := s.pool.Exec(ctx, `
			DELETE FROM test_progress p
			USING test_questions q
			WHERE q.id = p.question_id AND p.user_id = $1 AND q.deck_id = $2`, userID, deckID)
		return tag.RowsAffected(), err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE test_progress p
		SET status = 'wrong', correct_streak = 0
		FROM test_questions q
		WHERE q.id = p.question_id AND p.user_id = $1 AND q.deck_id = $2
		  AND p.status = 'passed'`, userID, deckID)
	return tag.RowsAffected(), err
}

// AbandonTestSessions закрывает незавершённые прогоны колоды (при старте нового).
func (s *Store) AbandonTestSessions(ctx context.Context, userID, deckID int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE test_sessions SET finished_at = now()
		WHERE user_id = $1 AND deck_id = $2 AND finished_at IS NULL`, userID, deckID)
	return err
}

// --- импорт колоды (админ) ---

// ImportQuestion — вопрос во входном формате импорта.
type ImportQuestion struct {
	Num         int      `json:"num"`
	Group       int      `json:"group"`
	Text        string   `json:"text"`
	Options     []string `json:"options"`
	CorrectIdx  int      `json:"correct_idx"`
	Image       string   `json:"image"`
	Explanation string   `json:"explanation"`
}

// ImportDeck — колода целиком.
type ImportDeck struct {
	Slug                string           `json:"slug"`
	Title               string           `json:"title"`
	Description         string           `json:"description"`
	Lang                string           `json:"lang"`
	SourceURL           string           `json:"source_url"`
	Revision            string           `json:"revision"`
	ExamSize            int              `json:"exam_size"`
	ExamMinutes         int              `json:"exam_minutes"`
	ExamAllowedMistakes int              `json:"exam_allowed_mistakes"`
	Groups              []ImportTestGroup    `json:"groups"`
	Questions           []ImportQuestion `json:"questions"`
}

// ImportTestGroup — тема колоды.
type ImportTestGroup struct {
	Num   int    `json:"num"`
	Title string `json:"title"`
}

// ImportTestDeck заливает колоду идемпотентно: колода по slug, вопросы по
// (deck, num). Повторный импорт обновляет тексты и ответы, НЕ трогая прогресс
// пользователей (вопросы сохраняют id).
func (s *Store) ImportTestDeck(ctx context.Context, d ImportDeck) (deckID int64, inserted, updated int, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	err = tx.QueryRow(ctx, `
		INSERT INTO test_decks (slug, title, description, lang, source_url, revision,
		                        exam_size, exam_minutes, exam_allowed_mistakes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (slug) DO UPDATE SET
		    title = EXCLUDED.title, description = EXCLUDED.description,
		    lang = EXCLUDED.lang, source_url = EXCLUDED.source_url,
		    revision = EXCLUDED.revision, exam_size = EXCLUDED.exam_size,
		    exam_minutes = EXCLUDED.exam_minutes,
		    exam_allowed_mistakes = EXCLUDED.exam_allowed_mistakes,
		    updated_at = now()
		RETURNING id`,
		d.Slug, d.Title, d.Description, d.Lang, d.SourceURL, d.Revision,
		d.ExamSize, d.ExamMinutes, d.ExamAllowedMistakes).Scan(&deckID)
	if err != nil {
		return 0, 0, 0, err
	}

	groupIDs := map[int]int64{}
	for _, g := range d.Groups {
		var gid int64
		if err = tx.QueryRow(ctx, `
			INSERT INTO test_groups (deck_id, num, title) VALUES ($1, $2, $3)
			ON CONFLICT (deck_id, num) DO UPDATE SET title = EXCLUDED.title
			RETURNING id`, deckID, g.Num, g.Title).Scan(&gid); err != nil {
			return 0, 0, 0, err
		}
		groupIDs[g.Num] = gid
	}

	for _, q := range d.Questions {
		opts, mErr := json.Marshal(q.Options)
		if mErr != nil {
			return 0, 0, 0, mErr
		}
		var gid *int64
		if id, ok := groupIDs[q.Group]; ok {
			gid = &id
		}
		var isInsert bool
		if err = tx.QueryRow(ctx, `
			INSERT INTO test_questions (deck_id, group_id, num, text, options,
			                            correct_idx, image, explanation)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (deck_id, num) DO UPDATE SET
			    group_id = EXCLUDED.group_id, text = EXCLUDED.text,
			    options = EXCLUDED.options, correct_idx = EXCLUDED.correct_idx,
			    image = EXCLUDED.image, explanation = EXCLUDED.explanation
			RETURNING (xmax = 0)`,
			deckID, gid, q.Num, q.Text, opts, q.CorrectIdx, q.Image, q.Explanation).Scan(&isInsert); err != nil {
			return 0, 0, 0, err
		}
		if isInsert {
			inserted++
		} else {
			updated++
		}
	}
	return deckID, inserted, updated, tx.Commit(ctx)
}
