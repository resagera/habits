package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// --- Food: план питания ---
//
// План — «намерение»: позиция хранит ССЫЛКУ на продукт/рецепт/шаблон плюс кэш
// КБЖУ, а не снимок, как дневник. Позиция может быть приблизительной (approx):
// свободный текст или продукт без количества — тогда КБЖУ не считается и в
// сводке отражается отдельным счётчиком «посчитано N из M».
//
// Слоты привязаны к day_index (0-based), а не к дате: план можно сдвинуть,
// продлить, скопировать неделю в неделю; план без start_date — «шаблонный».

const (
	FoodPlanMaxDays         = 90
	FoodPlanMaxParticipants = 10
	FoodPlanMaxSlots        = 500
	FoodPlanMaxItems        = 50
)

type FoodPlanItem struct {
	ID          int64   `json:"id"`
	Kind        string  `json:"kind"` // free | product | recipe | template
	RefID       *int64  `json:"ref_id"`
	Name        string  `json:"name"`
	Approx      bool    `json:"approx"`
	Amount      float64 `json:"amount"`
	Unit        string  `json:"unit"`
	Grams       float64 `json:"grams"`
	BaseType    string  `json:"base_type"`
	CaloriesPer float64 `json:"calories_per"`
	ProteinPer  float64 `json:"protein_per"`
	FatPer      float64 `json:"fat_per"`
	CarbsPer    float64 `json:"carbs_per"`
	Calories    float64 `json:"calories"`
	Protein     float64 `json:"protein"`
	Fat         float64 `json:"fat"`
	Carbs       float64 `json:"carbs"`
}

type FoodPlanSlot struct {
	ID            int64          `json:"id"`
	ParticipantID *int64         `json:"participant_id"`
	DayIndex      int            `json:"day_index"`
	MealType      string         `json:"meal_type"`
	AtTime        string         `json:"time"`
	Title         string         `json:"title"`
	Note          string         `json:"note"`
	Items         []FoodPlanItem `json:"items"`
}

type FoodPlanParticipant struct {
	ID     int64  `json:"id"`
	UserID *int64 `json:"user_id"`
	// UserLabel/IsMe — про привязку к пользователю Habits: она даёт участнику
	// возможность открыть план у себя и перенести СВОИ порции в свой дневник.
	UserLabel      string  `json:"user_label"`
	IsMe           bool    `json:"is_me"`
	Name           string  `json:"name"`
	Emoji          string  `json:"emoji"`
	PortionCoef    float64 `json:"portion_coef"`
	CaloriesTarget float64 `json:"calories_target"`
}

// FoodPlanPartTotal — итог дня для одного участника (общие слоты × его
// коэффициент порции + его персональные слоты).
type FoodPlanPartTotal struct {
	ParticipantID int64   `json:"participant_id"`
	Calories      float64 `json:"calories"`
	Protein       float64 `json:"protein"`
	Fat           float64 `json:"fat"`
	Carbs         float64 `json:"carbs"`
}

// FoodPlanDaySummary — сводка дня. Counted/Approx честно показывают,
// сколько позиций посчитано, а сколько задано «примерно».
type FoodPlanDaySummary struct {
	DayIndex      int                 `json:"day_index"`
	Calories      float64             `json:"calories"`
	Protein       float64             `json:"protein"`
	Fat           float64             `json:"fat"`
	Carbs         float64             `json:"carbs"`
	Counted       int                 `json:"counted"`
	Approx        int                 `json:"approx"`
	Slots         int                 `json:"slots"`
	ByParticipant []FoodPlanPartTotal `json:"by_participant"`
}

type FoodPlan struct {
	ID           int64                 `json:"id"`
	OwnerID      int64                 `json:"owner_id"`
	OwnerName    string                `json:"owner_name"` // заполняется только для чужого плана
	Name         string                `json:"name"`
	Description  string                `json:"description"`
	Days         int                   `json:"days"`
	StartDate    string                `json:"start_date"`
	Archived     bool                  `json:"archived"`
	IsOwner      bool                  `json:"is_owner"`
	CanEdit      bool                  `json:"can_edit"`
	GoalCalories float64               `json:"goal_calories"` // действующая цель смотрящего
	Participants []FoodPlanParticipant `json:"participants"`
	Slots        []FoodPlanSlot        `json:"slots"`
	Summary      []FoodPlanDaySummary  `json:"summary"`
}

// FoodPlanCard — строка списка планов (без слотов).
type FoodPlanCard struct {
	ID           int64   `json:"id"`
	OwnerID      int64   `json:"owner_id"`
	OwnerName    string  `json:"owner_name"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Days         int     `json:"days"`
	StartDate    string  `json:"start_date"`
	Archived     bool    `json:"archived"`
	IsOwner      bool    `json:"is_owner"`
	CanEdit      bool    `json:"can_edit"`
	Participants int     `json:"participants"`
	Slots        int     `json:"slots"`
	AvgCalories  float64 `json:"avg_calories"`
}

type FoodPlanShareUser struct {
	AccessUser
	CanEdit bool `json:"can_edit"`
}

// --- доступ ---

// FoodPlanAccess — права текущего пользователя на план (ErrNotFound — плана
// нет или доступа нет). Владелец может всё, can_edit — только состав.
func (s *Store) FoodPlanAccess(ctx context.Context, userID, planID int64) (isOwner, canEdit bool, err error) {
	var ownerID int64
	var shared bool
	err = s.pool.QueryRow(ctx, `
		SELECT p.user_id, COALESCE(sh.can_edit, FALSE)
		FROM food_plans p
		LEFT JOIN food_plan_shares sh ON sh.plan_id = p.id AND sh.user_id = $2
		WHERE p.id = $1 AND (p.user_id = $2 OR sh.user_id IS NOT NULL)`,
		planID, userID).Scan(&ownerID, &shared)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, ErrNotFound
	}
	if err != nil {
		return false, false, err
	}
	isOwner = ownerID == userID
	return isOwner, isOwner || shared, nil
}

// requireFoodPlanEdit — доступ на правку состава (владелец или can_edit).
func (s *Store) requireFoodPlanEdit(ctx context.Context, userID, planID int64) error {
	_, canEdit, err := s.FoodPlanAccess(ctx, userID, planID)
	if err != nil {
		return err
	}
	if !canEdit {
		return ErrForbidden
	}
	return nil
}

func (s *Store) requireFoodPlanOwner(ctx context.Context, userID, planID int64) error {
	isOwner, _, err := s.FoodPlanAccess(ctx, userID, planID)
	if err != nil {
		return err
	}
	if !isOwner {
		return ErrForbidden
	}
	return nil
}

// --- сводка ---

// normalizeFoodPlanItem — итоги позиции из кэша КБЖУ; приблизительная
// позиция итогов не даёт (её не с чем считать).
func normalizeFoodPlanItem(it *FoodPlanItem) {
	if it.Approx {
		it.Calories, it.Protein, it.Fat, it.Carbs = 0, 0, 0, 0
		return
	}
	it.Calories, it.Protein, it.Fat, it.Carbs =
		FoodItemTotals(it.Grams, it.CaloriesPer, it.ProteinPer, it.FatPer, it.CarbsPer)
}

type foodPlanTotals struct{ c, p, f, cb float64 }

func (t *foodPlanTotals) add(o foodPlanTotals, k float64) {
	t.c += o.c * k
	t.p += o.p * k
	t.f += o.f * k
	t.cb += o.cb * k
}

// foodPlanSummary — итоги по дням. Общие слоты (participant_id NULL) входят
// в итог каждого участника со своим коэффициентом порции; когда участников
// нет — считается один общий итог.
func foodPlanSummary(days int, parts []FoodPlanParticipant, slots []FoodPlanSlot) []FoodPlanDaySummary {
	common := make([]foodPlanTotals, days)
	own := make([]map[int64]foodPlanTotals, days)
	out := make([]FoodPlanDaySummary, days)
	for d := 0; d < days; d++ {
		own[d] = map[int64]foodPlanTotals{}
		out[d] = FoodPlanDaySummary{DayIndex: d, ByParticipant: []FoodPlanPartTotal{}}
	}
	for _, sl := range slots {
		if sl.DayIndex < 0 || sl.DayIndex >= days {
			continue // слот за пределами укороченного плана — не теряем, но и не считаем
		}
		var t foodPlanTotals
		for _, it := range sl.Items {
			if it.Approx {
				out[sl.DayIndex].Approx++
				continue
			}
			out[sl.DayIndex].Counted++
			t.c += it.Calories
			t.p += it.Protein
			t.f += it.Fat
			t.cb += it.Carbs
		}
		out[sl.DayIndex].Slots++
		if sl.ParticipantID == nil {
			common[sl.DayIndex].add(t, 1)
		} else {
			cur := own[sl.DayIndex][*sl.ParticipantID]
			cur.add(t, 1)
			own[sl.DayIndex][*sl.ParticipantID] = cur
		}
	}
	for d := 0; d < days; d++ {
		if len(parts) == 0 {
			out[d].Calories, out[d].Protein = common[d].c, common[d].p
			out[d].Fat, out[d].Carbs = common[d].f, common[d].cb
			continue
		}
		for _, p := range parts {
			var t foodPlanTotals
			t.add(common[d], p.PortionCoef)
			t.add(own[d][p.ID], 1)
			out[d].ByParticipant = append(out[d].ByParticipant, FoodPlanPartTotal{
				ParticipantID: p.ID, Calories: t.c, Protein: t.p, Fat: t.f, Carbs: t.cb,
			})
			out[d].Calories += t.c
			out[d].Protein += t.p
			out[d].Fat += t.f
			out[d].Carbs += t.cb
		}
	}
	return out
}

// --- планы ---

const foodPlanCols = `id, user_id, name, description, days, start_date, archived`

func scanFoodPlan(row pgx.Row) (*FoodPlan, error) {
	var p FoodPlan
	var start *time.Time
	if err := row.Scan(&p.ID, &p.OwnerID, &p.Name, &p.Description, &p.Days, &start, &p.Archived); err != nil {
		return nil, err
	}
	if start != nil {
		p.StartDate = start.Format("2006-01-02")
	}
	p.Participants = []FoodPlanParticipant{}
	p.Slots = []FoodPlanSlot{}
	p.Summary = []FoodPlanDaySummary{}
	return &p, nil
}

// nullDate — пустая строка даты как SQL NULL.
func nullDate(d string) any {
	if d == "" {
		return nil
	}
	return d
}

// ListFoodPlans — свои планы и планы, к которым открыт доступ.
func (s *Store) ListFoodPlans(ctx context.Context, userID int64, archived bool) ([]FoodPlanCard, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.user_id, COALESCE(u.first_name, ''), COALESCE(u.username, ''),
		       p.name, p.description, p.days, p.start_date, p.archived,
		       COALESCE(sh.can_edit, FALSE),
		       (SELECT count(*) FROM food_plan_participants pp WHERE pp.plan_id = p.id),
		       (SELECT count(*) FROM food_plan_slots sl WHERE sl.plan_id = p.id),
		       COALESCE((SELECT sum(i.calories) FROM food_plan_items i
		                 JOIN food_plan_slots sl ON sl.id = i.slot_id
		                 WHERE sl.plan_id = p.id AND NOT i.approx), 0)
		FROM food_plans p
		JOIN users u ON u.id = p.user_id
		LEFT JOIN food_plan_shares sh ON sh.plan_id = p.id AND sh.user_id = $1
		WHERE (p.user_id = $1 OR sh.user_id IS NOT NULL) AND p.archived = $2
		ORDER BY p.updated_at DESC`, userID, archived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FoodPlanCard
	for rows.Next() {
		var c FoodPlanCard
		var start *time.Time
		var firstName, username string
		var totalCalories float64
		if err := rows.Scan(&c.ID, &c.OwnerID, &firstName, &username, &c.Name, &c.Description,
			&c.Days, &start, &c.Archived, &c.CanEdit, &c.Participants, &c.Slots,
			&totalCalories); err != nil {
			return nil, err
		}
		if start != nil {
			c.StartDate = start.Format("2006-01-02")
		}
		c.IsOwner = c.OwnerID == userID
		c.CanEdit = c.CanEdit || c.IsOwner
		if !c.IsOwner {
			c.OwnerName = firstName
			if c.OwnerName == "" && username != "" {
				c.OwnerName = "@" + username
			}
		}
		if c.Days > 0 {
			c.AvgCalories = totalCalories / float64(c.Days)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetFoodPlan — план целиком (участники, слоты с позициями, сводка).
func (s *Store) GetFoodPlan(ctx context.Context, userID, planID int64) (*FoodPlan, error) {
	isOwner, canEdit, err := s.FoodPlanAccess(ctx, userID, planID)
	if err != nil {
		return nil, err
	}
	p, err := scanFoodPlan(s.pool.QueryRow(ctx,
		`SELECT `+foodPlanCols+` FROM food_plans WHERE id = $1`, planID))
	if err != nil {
		return nil, err
	}
	p.IsOwner, p.CanEdit = isOwner, canEdit
	if !isOwner {
		var firstName, username string
		if err := s.pool.QueryRow(ctx, `SELECT COALESCE(first_name, ''), COALESCE(username, '')
			FROM users WHERE id = $1`, p.OwnerID).Scan(&firstName, &username); err != nil {
			return nil, err
		}
		p.OwnerName = firstName
		if p.OwnerName == "" && username != "" {
			p.OwnerName = "@" + username
		}
	}
	if p.Participants, err = s.listFoodPlanParticipants(ctx, planID, userID); err != nil {
		return nil, err
	}
	if p.Slots, err = s.listFoodPlanSlots(ctx, planID, nil); err != nil {
		return nil, err
	}
	p.Summary = foodPlanSummary(p.Days, p.Participants, p.Slots)
	// цель СМОТРЯЩЕГО на период плана — чтобы день показывал «≈1850 / 2000»;
	// у плана с участниками осмысленны цели участников, а не эта
	goalDay := p.StartDate
	if goalDay == "" {
		goalDay = time.Now().UTC().Format("2006-01-02")
	}
	if goal, err := s.FoodGoalForDate(ctx, userID, goalDay); err == nil && goal != nil {
		p.GoalCalories = goal.Calories
	}
	return p, nil
}

func (s *Store) CreateFoodPlan(ctx context.Context, userID int64, p FoodPlan) (*FoodPlan, error) {
	created, err := scanFoodPlan(s.pool.QueryRow(ctx, `
		INSERT INTO food_plans (user_id, name, description, days, start_date)
		VALUES ($1,$2,$3,$4,$5) RETURNING `+foodPlanCols,
		userID, p.Name, p.Description, p.Days, nullDate(p.StartDate)))
	if err != nil {
		return nil, err
	}
	created.IsOwner, created.CanEdit = true, true
	created.Summary = foodPlanSummary(created.Days, nil, nil)
	return created, nil
}

// UpdateFoodPlan — поля плана (только владелец). Укорочение days не удаляет
// слоты за границей: вернув длину, пользователь получит их обратно.
func (s *Store) UpdateFoodPlan(ctx context.Context, userID, planID int64, p FoodPlan) (*FoodPlan, error) {
	if err := s.requireFoodPlanOwner(ctx, userID, planID); err != nil {
		return nil, err
	}
	updated, err := scanFoodPlan(s.pool.QueryRow(ctx, `
		UPDATE food_plans SET name=$2, description=$3, days=$4, start_date=$5,
			archived=$6, updated_at=now()
		WHERE id = $1 RETURNING `+foodPlanCols,
		planID, p.Name, p.Description, p.Days, nullDate(p.StartDate), p.Archived))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.GetFoodPlan(ctx, userID, updated.ID)
}

func (s *Store) DeleteFoodPlan(ctx context.Context, userID, planID int64) error {
	if err := s.requireFoodPlanOwner(ctx, userID, planID); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM food_plans WHERE id = $1`, planID)
	return err
}

// touchFoodPlan — отметка изменения (для сортировки списка).
func (s *Store) touchFoodPlan(ctx context.Context, planID int64) {
	_, _ = s.pool.Exec(ctx, `UPDATE food_plans SET updated_at = now() WHERE id = $1`, planID)
}

// --- участники ---

// listFoodPlanParticipants — участники плана; viewerID нужен, чтобы отметить
// участника, которым является сам смотрящий (is_me).
func (s *Store) listFoodPlanParticipants(ctx context.Context, planID, viewerID int64) ([]FoodPlanParticipant, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.user_id, p.name, p.emoji, p.portion_coef, p.calories_target,
		       COALESCE(u.first_name, ''), COALESCE(u.username, '')
		FROM food_plan_participants p
		LEFT JOIN users u ON u.id = p.user_id
		WHERE p.plan_id = $1 ORDER BY p.position, p.id`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FoodPlanParticipant{}
	for rows.Next() {
		var p FoodPlanParticipant
		var firstName, username string
		if err := rows.Scan(&p.ID, &p.UserID, &p.Name, &p.Emoji, &p.PortionCoef, &p.CaloriesTarget,
			&firstName, &username); err != nil {
			return nil, err
		}
		if p.UserID != nil {
			p.IsMe = *p.UserID == viewerID
			p.UserLabel = firstName
			if p.UserLabel == "" && username != "" {
				p.UserLabel = "@" + username
			}
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetFoodPlanParticipant — один участник (чтобы PUT мог быть частичным и не
// затирал поля, которых нет в запросе, — например привязку к пользователю).
func (s *Store) GetFoodPlanParticipant(ctx context.Context, planID, id int64) (*FoodPlanParticipant, error) {
	var p FoodPlanParticipant
	err := s.pool.QueryRow(ctx, `SELECT id, user_id, name, emoji, portion_coef, calories_target
		FROM food_plan_participants WHERE id = $1 AND plan_id = $2`, id, planID).
		Scan(&p.ID, &p.UserID, &p.Name, &p.Emoji, &p.PortionCoef, &p.CaloriesTarget)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

func (s *Store) CreateFoodPlanParticipant(ctx context.Context, userID, planID int64, p FoodPlanParticipant) (*FoodPlanParticipant, error) {
	if err := s.requireFoodPlanOwner(ctx, userID, planID); err != nil {
		return nil, err
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM food_plan_participants WHERE plan_id = $1`,
		planID).Scan(&n); err != nil {
		return nil, err
	}
	if n >= FoodPlanMaxParticipants {
		return nil, ErrLimit
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO food_plan_participants (plan_id, user_id, name, emoji, portion_coef, calories_target, position)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, user_id, name, emoji, portion_coef, calories_target`,
		planID, p.UserID, p.Name, p.Emoji, p.PortionCoef, p.CaloriesTarget, n).
		Scan(&p.ID, &p.UserID, &p.Name, &p.Emoji, &p.PortionCoef, &p.CaloriesTarget)
	if err != nil {
		return nil, err
	}
	s.touchFoodPlan(ctx, planID)
	return &p, nil
}

func (s *Store) UpdateFoodPlanParticipant(ctx context.Context, userID, planID, id int64, p FoodPlanParticipant) (*FoodPlanParticipant, error) {
	if err := s.requireFoodPlanOwner(ctx, userID, planID); err != nil {
		return nil, err
	}
	err := s.pool.QueryRow(ctx, `
		UPDATE food_plan_participants SET user_id=$3, name=$4, emoji=$5, portion_coef=$6, calories_target=$7
		WHERE id = $1 AND plan_id = $2
		RETURNING id, user_id, name, emoji, portion_coef, calories_target`,
		id, planID, p.UserID, p.Name, p.Emoji, p.PortionCoef, p.CaloriesTarget).
		Scan(&p.ID, &p.UserID, &p.Name, &p.Emoji, &p.PortionCoef, &p.CaloriesTarget)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.touchFoodPlan(ctx, planID)
	return &p, nil
}

// DeleteFoodPlanParticipant — вместе с его персональными слотами (каскад).
func (s *Store) DeleteFoodPlanParticipant(ctx context.Context, userID, planID, id int64) error {
	if err := s.requireFoodPlanOwner(ctx, userID, planID); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM food_plan_participants WHERE id = $1 AND plan_id = $2`, id, planID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	s.touchFoodPlan(ctx, planID)
	return nil
}

// --- слоты и позиции ---

const foodPlanItemCols = `id, kind, ref_id, name, approx, amount, unit, grams, base_type,
	calories_per, protein_per, fat_per, carbs_per, calories, protein, fat, carbs`

// listFoodPlanSlots — слоты плана; dayIndex != nil ограничивает одним днём
// (подсказка в Дневнике не должна тянуть все 500 слотов плана на 90 дней).
func (s *Store) listFoodPlanSlots(ctx context.Context, planID int64, dayIndex *int) ([]FoodPlanSlot, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, participant_id, day_index, meal_type, at_time, title, note
		FROM food_plan_slots WHERE plan_id = $1 AND ($2::int IS NULL OR day_index = $2)
		ORDER BY day_index, position, id`, planID, dayIndex)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	slots := []FoodPlanSlot{}
	var ids []int64
	for rows.Next() {
		var sl FoodPlanSlot
		if err := rows.Scan(&sl.ID, &sl.ParticipantID, &sl.DayIndex, &sl.MealType,
			&sl.AtTime, &sl.Title, &sl.Note); err != nil {
			return nil, err
		}
		sl.Items = []FoodPlanItem{}
		slots = append(slots, sl)
		ids = append(ids, sl.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return slots, nil
	}
	items, err := s.loadFoodPlanItems(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range slots {
		if its := items[slots[i].ID]; its != nil {
			slots[i].Items = its
		}
	}
	return slots, nil
}

func (s *Store) loadFoodPlanItems(ctx context.Context, slotIDs []int64) (map[int64][]FoodPlanItem, error) {
	rows, err := s.pool.Query(ctx, `SELECT slot_id, `+foodPlanItemCols+` FROM food_plan_items
		WHERE slot_id = ANY($1) ORDER BY position, id`, slotIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]FoodPlanItem{}
	for rows.Next() {
		var slotID int64
		var it FoodPlanItem
		if err := rows.Scan(&slotID, &it.ID, &it.Kind, &it.RefID, &it.Name, &it.Approx,
			&it.Amount, &it.Unit, &it.Grams, &it.BaseType,
			&it.CaloriesPer, &it.ProteinPer, &it.FatPer, &it.CarbsPer,
			&it.Calories, &it.Protein, &it.Fat, &it.Carbs); err != nil {
			return nil, err
		}
		out[slotID] = append(out[slotID], it)
	}
	return out, rows.Err()
}

func insertFoodPlanItems(ctx context.Context, tx pgx.Tx, slotID int64, items []FoodPlanItem) error {
	for i := range items {
		it := &items[i]
		normalizeFoodPlanItem(it)
		_, err := tx.Exec(ctx, `INSERT INTO food_plan_items (slot_id, kind, ref_id, name, approx,
			amount, unit, grams, base_type, calories_per, protein_per, fat_per, carbs_per,
			calories, protein, fat, carbs, position)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
			slotID, it.Kind, it.RefID, it.Name, it.Approx, it.Amount, it.Unit, it.Grams, it.BaseType,
			it.CaloriesPer, it.ProteinPer, it.FatPer, it.CarbsPer,
			it.Calories, it.Protein, it.Fat, it.Carbs, i)
		if err != nil {
			return err
		}
	}
	return nil
}

// CreateFoodPlanSlot — слот с позициями (владелец или can_edit).
func (s *Store) CreateFoodPlanSlot(ctx context.Context, userID, planID int64, sl FoodPlanSlot) (*FoodPlanSlot, error) {
	if err := s.requireFoodPlanEdit(ctx, userID, planID); err != nil {
		return nil, err
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM food_plan_slots WHERE plan_id = $1`,
		planID).Scan(&n); err != nil {
		return nil, err
	}
	if n >= FoodPlanMaxSlots {
		return nil, ErrLimit
	}
	if err := s.checkFoodPlanParticipant(ctx, planID, sl.ParticipantID); err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var pos int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(position) + 1, 0) FROM food_plan_slots
		WHERE plan_id = $1 AND day_index = $2`, planID, sl.DayIndex).Scan(&pos); err != nil {
		return nil, err
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO food_plan_slots (plan_id, participant_id, day_index, meal_type, at_time, title, note, position)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		planID, sl.ParticipantID, sl.DayIndex, sl.MealType, sl.AtTime, sl.Title, sl.Note, pos).Scan(&sl.ID)
	if err != nil {
		return nil, err
	}
	if err := insertFoodPlanItems(ctx, tx, sl.ID, sl.Items); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	s.touchFoodPlan(ctx, planID)
	return &sl, nil
}

// UpdateFoodPlanSlot — поля слота; при replaceItems позиции перезаписываются.
func (s *Store) UpdateFoodPlanSlot(ctx context.Context, userID, planID, id int64, sl FoodPlanSlot, replaceItems bool) (*FoodPlanSlot, error) {
	if err := s.requireFoodPlanEdit(ctx, userID, planID); err != nil {
		return nil, err
	}
	if err := s.checkFoodPlanParticipant(ctx, planID, sl.ParticipantID); err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `
		UPDATE food_plan_slots SET participant_id=$3, day_index=$4, meal_type=$5,
			at_time=$6, title=$7, note=$8
		WHERE id = $1 AND plan_id = $2 RETURNING id`,
		id, planID, sl.ParticipantID, sl.DayIndex, sl.MealType, sl.AtTime, sl.Title, sl.Note).Scan(&sl.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if replaceItems {
		if _, err := tx.Exec(ctx, `DELETE FROM food_plan_items WHERE slot_id = $1`, id); err != nil {
			return nil, err
		}
		if err := insertFoodPlanItems(ctx, tx, id, sl.Items); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	s.touchFoodPlan(ctx, planID)
	if !replaceItems {
		items, err := s.loadFoodPlanItems(ctx, []int64{id})
		if err != nil {
			return nil, err
		}
		sl.Items = items[id]
	}
	if sl.Items == nil {
		sl.Items = []FoodPlanItem{}
	}
	return &sl, nil
}

func (s *Store) DeleteFoodPlanSlot(ctx context.Context, userID, planID, id int64) error {
	if err := s.requireFoodPlanEdit(ctx, userID, planID); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM food_plan_slots WHERE id = $1 AND plan_id = $2`, id, planID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	s.touchFoodPlan(ctx, planID)
	return nil
}

// checkFoodPlanParticipant — участник (если указан) принадлежит плану.
func (s *Store) checkFoodPlanParticipant(ctx context.Context, planID int64, pid *int64) error {
	if pid == nil {
		return nil
	}
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM food_plan_participants
		WHERE id = $1 AND plan_id = $2)`, *pid, planID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

// --- копирование ---

// CopyFoodPlanDays — копия count дней плана, начиная с fromDay, в toDay и далее
// (перезаписывает целевые дни). Возвращает число скопированных слотов.
func (s *Store) CopyFoodPlanDays(ctx context.Context, userID, planID int64, fromDay, toDay, count int) (int, error) {
	if err := s.requireFoodPlanEdit(ctx, userID, planID); err != nil {
		return 0, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	shift := toDay - fromDay
	if _, err := tx.Exec(ctx, `DELETE FROM food_plan_slots
		WHERE plan_id = $1 AND day_index >= $2 AND day_index < $3`,
		planID, toDay, toDay+count); err != nil {
		return 0, err
	}
	// исходные слоты читаем целиком: INSERT ... SELECT ... RETURNING не
	// гарантирует порядок строк, а позиции надо разложить по новым слотам
	rows, err := tx.Query(ctx, `SELECT id, participant_id, day_index, meal_type, at_time, title, note, position
		FROM food_plan_slots WHERE plan_id = $1 AND day_index >= $2 AND day_index < $3
		ORDER BY day_index, position, id`, planID, fromDay, fromDay+count)
	if err != nil {
		return 0, err
	}
	type srcSlot struct {
		id       int64
		part     *int64
		dayIndex int
		mealType string
		atTime   string
		title    string
		note     string
		position int
	}
	var src []srcSlot
	for rows.Next() {
		var sl srcSlot
		if err := rows.Scan(&sl.id, &sl.part, &sl.dayIndex, &sl.mealType,
			&sl.atTime, &sl.title, &sl.note, &sl.position); err != nil {
			rows.Close()
			return 0, err
		}
		src = append(src, sl)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, sl := range src {
		var newID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO food_plan_slots (plan_id, participant_id, day_index, meal_type, at_time, title, note, position)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
			planID, sl.part, sl.dayIndex+shift, sl.mealType, sl.atTime, sl.title, sl.note, sl.position).
			Scan(&newID); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO food_plan_items (slot_id, kind, ref_id, name, approx, amount, unit, grams,
				base_type, calories_per, protein_per, fat_per, carbs_per,
				calories, protein, fat, carbs, position)
			SELECT $2, kind, ref_id, name, approx, amount, unit, grams,
				base_type, calories_per, protein_per, fat_per, carbs_per,
				calories, protein, fat, carbs, position
			FROM food_plan_items WHERE slot_id = $1`, sl.id, newID); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	s.touchFoodPlan(ctx, planID)
	return len(src), nil
}

// DuplicateFoodPlan — независимая копия плана целиком у пользователя userID
// (используется и для «Дублировать», и для приёма по ссылке-приглашению).
func (s *Store) DuplicateFoodPlan(ctx context.Context, userID, planID int64, name string) (*FoodPlan, error) {
	src, err := s.GetFoodPlan(ctx, userID, planID)
	if err != nil {
		return nil, err
	}
	// копия у того же пользователя сохраняет ссылки на его продукты/рецепты
	return s.copyFoodPlanTo(ctx, src, userID, name, src.OwnerID == userID)
}

// copyFoodPlanTo — копия загруженного плана новому владельцу. keepRefs —
// сохранять ли ссылки на продукты/рецепты (только когда владелец тот же).
func (s *Store) copyFoodPlanTo(ctx context.Context, src *FoodPlan, toUser int64, name string, keepRefs bool) (*FoodPlan, error) {
	if name == "" {
		name = src.Name
	}
	if len(name) > 200 {
		name = name[:200]
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var newID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO food_plans (user_id, name, description, days, start_date)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		toUser, name, src.Description, src.Days, nullDate(src.StartDate)).Scan(&newID); err != nil {
		return nil, err
	}
	// участники копируются без привязки к пользователям: доступ выдаёт владелец копии
	partMap := map[int64]int64{}
	for i, p := range src.Participants {
		var pid int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO food_plan_participants (plan_id, name, emoji, portion_coef, calories_target, position)
			VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
			newID, p.Name, p.Emoji, p.PortionCoef, p.CaloriesTarget, i).Scan(&pid); err != nil {
			return nil, err
		}
		partMap[p.ID] = pid
	}
	for i, sl := range src.Slots {
		var newPart *int64
		if sl.ParticipantID != nil {
			if mapped, ok := partMap[*sl.ParticipantID]; ok {
				newPart = &mapped
			}
		}
		var slotID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO food_plan_slots (plan_id, participant_id, day_index, meal_type, at_time, title, note, position)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
			newID, newPart, sl.DayIndex, sl.MealType, sl.AtTime, sl.Title, sl.Note, i).Scan(&slotID); err != nil {
			return nil, err
		}
		items := make([]FoodPlanItem, len(sl.Items))
		copy(items, sl.Items)
		if !keepRefs {
			// у нового владельца этих продуктов/рецептов нет: ссылки снимаем,
			// но кэш КБЖУ и названия сохраняются — план остаётся осмысленным
			for j := range items {
				items[j].RefID = nil
				items[j].Kind = "free"
			}
		}
		if err := insertFoodPlanItems(ctx, tx, slotID, items); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetFoodPlan(ctx, toUser, newID)
}

// --- применение в дневник ---

// FoodPlanApplyResult — итог применения дней плана в дневник.
type FoodPlanApplyResult struct {
	Created  int      `json:"created"`
	Skipped  int      `json:"skipped"`  // приблизительные позиции без КБЖУ
	Existing int      `json:"existing"` // уже применённые записи на этих датах
	Dates    []string `json:"dates"`
}

// ApplyFoodPlanDays переносит дни плана в СВОЙ дневник (в чужой — никогда).
// mode: "" — только проверка (при existing > 0 ничего не создаётся),
// "add" — добавить поверх, "replace" — заменить прежние записи из этого плана.
// Приблизительные позиции КБЖУ не дают и уходят в описание записи.
func (s *Store) ApplyFoodPlanDays(ctx context.Context, userID, planID int64,
	fromDay, count int, startDate string, participantID *int64, mode string) (*FoodPlanApplyResult, error) {
	plan, err := s.GetFoodPlan(ctx, userID, planID)
	if err != nil {
		return nil, err
	}
	coef := 1.0
	if participantID != nil {
		found := false
		for _, p := range plan.Participants {
			if p.ID == *participantID {
				coef, found = p.PortionCoef, true
			}
		}
		if !found {
			return nil, ErrNotFound
		}
	}
	base, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		return nil, errors.New("invalid start date")
	}
	res := &FoodPlanApplyResult{Dates: []string{}}
	dates := make([]string, 0, count)
	for i := 0; i < count; i++ {
		dates = append(dates, base.AddDate(0, 0, i).Format("2006-01-02"))
	}
	res.Dates = dates
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM food_meals
		WHERE user_id = $1 AND source_type = 'plan' AND source_id = $2 AND day = ANY($3::date[])`,
		userID, planID, dates).Scan(&res.Existing); err != nil {
		return nil, err
	}
	if res.Existing > 0 && mode != "add" && mode != "replace" {
		return res, nil
	}
	if mode == "replace" && res.Existing > 0 {
		if _, err := s.pool.Exec(ctx, `DELETE FROM food_meals
			WHERE user_id = $1 AND source_type = 'plan' AND source_id = $2 AND day = ANY($3::date[])`,
			userID, planID, dates); err != nil {
			return nil, err
		}
	}
	// актуальные КБЖУ своих продуктов — план хранит кэш, дневник должен
	// получить свежий снимок; чужие ссылки не трогаем (и не раскрываем)
	fresh, err := s.foodProductSnapshots(ctx, userID, plan.Slots)
	if err != nil {
		return nil, err
	}
	for _, sl := range plan.Slots {
		idx := sl.DayIndex - fromDay
		if idx < 0 || idx >= count {
			continue
		}
		if sl.ParticipantID != nil && (participantID == nil || *sl.ParticipantID != *participantID) {
			continue
		}
		k := coef
		if sl.ParticipantID != nil {
			k = 1 // персональный слот уже рассчитан на этого участника
		}
		meal, skipped := foodMealFromPlanSlot(sl, dates[idx], planID, k, fresh)
		if _, err := s.CreateFoodMeal(ctx, userID, meal); err != nil {
			return nil, err
		}
		res.Created++
		res.Skipped += skipped
	}
	return res, nil
}

// foodProductSnapshots — актуальные КБЖУ продуктов пользователя, на которые
// ссылаются точные позиции плана (ключ — id продукта).
func (s *Store) foodProductSnapshots(ctx context.Context, userID int64, slots []FoodPlanSlot) (map[int64]FoodProduct, error) {
	var ids []int64
	for _, sl := range slots {
		for _, it := range sl.Items {
			if it.Kind == "product" && !it.Approx && it.RefID != nil {
				ids = append(ids, *it.RefID)
			}
		}
	}
	out := map[int64]FoodProduct{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id, base_type, calories, protein, fat, carbs
		FROM food_products WHERE user_id = $1 AND id = ANY($2)`, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p FoodProduct
		if err := rows.Scan(&p.ID, &p.BaseType, &p.Calories, &p.Protein, &p.Fat, &p.Carbs); err != nil {
			return nil, err
		}
		out[p.ID] = p
	}
	return out, rows.Err()
}

// foodMealFromPlanSlot — запись дневника из слота плана: точные позиции
// становятся снимками состава, приблизительные — строкой в описании.
func foodMealFromPlanSlot(sl FoodPlanSlot, day string, planID int64, coef float64,
	fresh map[int64]FoodProduct) (FoodMeal, int) {
	srcID := planID
	m := FoodMeal{
		Day: day, AtTime: sl.AtTime, MealType: sl.MealType, Name: sl.Title,
		Description: sl.Note, SourceType: "plan", SourceID: &srcID,
		Items: []FoodItem{},
	}
	var approxNames []string
	for _, it := range sl.Items {
		if it.Approx {
			approxNames = append(approxNames, it.Name)
			continue
		}
		fi := FoodItem{
			Name: it.Name, Amount: it.Amount * coef, Unit: it.Unit,
			Grams: it.Grams * coef, BaseType: it.BaseType,
			CaloriesPer: it.CaloriesPer, ProteinPer: it.ProteinPer,
			FatPer: it.FatPer, CarbsPer: it.CarbsPer,
		}
		if it.Kind == "product" && it.RefID != nil {
			if p, ok := fresh[*it.RefID]; ok {
				id := p.ID
				fi.ProductID = &id
				fi.BaseType = p.BaseType
				fi.CaloriesPer, fi.ProteinPer = p.Calories, p.Protein
				fi.FatPer, fi.CarbsPer = p.Fat, p.Carbs
			}
		}
		m.Items = append(m.Items, fi)
	}
	if m.Name == "" {
		if len(m.Items) > 0 {
			m.Name = m.Items[0].Name
		} else if len(approxNames) > 0 {
			m.Name = approxNames[0]
		} else {
			m.Name = "Из плана"
		}
	}
	if len(approxNames) > 0 {
		line := "Примерно: " + strings.Join(approxNames, ", ")
		if m.Description != "" {
			m.Description += "\n"
		}
		m.Description += line
		if len(m.Description) > 2000 {
			m.Description = m.Description[:2000]
		}
	}
	return m, len(approxNames)
}

// --- «что по плану на эту дату» (подсказка в Дневнике) ---

// FoodPlanToday — день плана, попадающий на конкретную дату.
type FoodPlanToday struct {
	PlanID        int64          `json:"plan_id"`
	PlanName      string         `json:"plan_name"`
	DayIndex      int            `json:"day_index"`
	ParticipantID *int64         `json:"participant_id"` // мой участник, если план на нескольких
	Applied       int            `json:"applied"`        // уже перенесено записей на эту дату
	Slots         []FoodPlanSlot `json:"slots"`
}

// FoodPlansForDate — активные планы (свои и открытые мне), у которых на дату
// приходится день. Планы без start_date не участвуют: их день к дате не привязан.
func (s *Store) FoodPlansForDate(ctx context.Context, userID int64, date string) ([]FoodPlanToday, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.name, ($2::date - p.start_date)::int
		FROM food_plans p
		LEFT JOIN food_plan_shares sh ON sh.plan_id = p.id AND sh.user_id = $1
		WHERE (p.user_id = $1 OR sh.user_id IS NOT NULL)
		  AND NOT p.archived AND p.start_date IS NOT NULL
		  AND p.start_date <= $2::date AND $2::date < p.start_date + p.days
		ORDER BY p.updated_at DESC`, userID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FoodPlanToday{}
	for rows.Next() {
		var t FoodPlanToday
		if err := rows.Scan(&t.PlanID, &t.PlanName, &t.DayIndex); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		t := &out[i]
		// мой участник — чтобы показать и перенести именно свои порции
		var pid *int64
		if err := s.pool.QueryRow(ctx, `SELECT id FROM food_plan_participants
			WHERE plan_id = $1 AND user_id = $2 ORDER BY position LIMIT 1`,
			t.PlanID, userID).Scan(&pid); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		t.ParticipantID = pid
		slots, err := s.listFoodPlanSlots(ctx, t.PlanID, &t.DayIndex)
		if err != nil {
			return nil, err
		}
		t.Slots = []FoodPlanSlot{}
		for _, sl := range slots {
			if sl.ParticipantID != nil && (pid == nil || *sl.ParticipantID != *pid) {
				continue
			}
			t.Slots = append(t.Slots, sl)
		}
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM food_meals
			WHERE user_id = $1 AND day = $2 AND source_type = 'plan' AND source_id = $3`,
			userID, date, t.PlanID).Scan(&t.Applied); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// --- шаринг плана ---

// ShareFoodPlan — доступ к плану для toUser (kind food_plan в deliverShare).
// Доступ только на чтение; правку владелец включает отдельно.
func (s *Store) ShareFoodPlan(ctx context.Context, fromUser, planID, toUser int64) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `SELECT name FROM food_plans WHERE id = $1 AND user_id = $2`,
		planID, fromUser).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO food_plan_shares (plan_id, user_id)
		VALUES ($1, $2) ON CONFLICT DO NOTHING`, planID, toUser)
	return name, err
}

func (s *Store) ListFoodPlanShares(ctx context.Context, userID, planID int64) ([]FoodPlanShareUser, error) {
	if err := s.requireFoodPlanOwner(ctx, userID, planID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, COALESCE(u.username, ''), COALESCE(u.first_name, ''), sh.can_edit
		FROM food_plan_shares sh JOIN users u ON u.id = sh.user_id
		WHERE sh.plan_id = $1 ORDER BY sh.created_at`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FoodPlanShareUser{}
	for rows.Next() {
		var u FoodPlanShareUser
		if err := rows.Scan(&u.ID, &u.Username, &u.FirstName, &u.CanEdit); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) UpdateFoodPlanShare(ctx context.Context, userID, planID, targetID int64, canEdit bool) error {
	if err := s.requireFoodPlanOwner(ctx, userID, planID); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE food_plan_shares SET can_edit = $3
		WHERE plan_id = $1 AND user_id = $2`, planID, targetID, canEdit)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RevokeFoodPlanShare(ctx context.Context, userID, planID, targetID int64) error {
	if err := s.requireFoodPlanOwner(ctx, userID, planID); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM food_plan_shares
		WHERE plan_id = $1 AND user_id = $2`, planID, targetID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// LeaveFoodPlan — убрать у себя чужой план.
func (s *Store) LeaveFoodPlan(ctx context.Context, userID, planID int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM food_plan_shares
		WHERE plan_id = $1 AND user_id = $2`, planID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// EnsureFoodPlanShareToken — токен ссылки-приглашения (приём = независимая копия).
func (s *Store) EnsureFoodPlanShareToken(ctx context.Context, userID, planID int64) (string, error) {
	var token *string
	err := s.pool.QueryRow(ctx, `SELECT share_token FROM food_plans WHERE id = $1 AND user_id = $2`,
		planID, userID).Scan(&token)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if token != nil {
		return *token, nil
	}
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	fresh := hex.EncodeToString(buf)
	_, err = s.pool.Exec(ctx, `UPDATE food_plans SET share_token = $2 WHERE id = $1`, planID, fresh)
	return fresh, err
}

// RedeemFoodPlanShareToken — принять план по ссылке: независимая копия.
// Свой же план не дублируется.
func (s *Store) RedeemFoodPlanShareToken(ctx context.Context, userID int64, token string) (string, error) {
	var planID, ownerID int64
	err := s.pool.QueryRow(ctx, `SELECT id, user_id FROM food_plans WHERE share_token = $1`,
		token).Scan(&planID, &ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	src, err := s.GetFoodPlan(ctx, ownerID, planID)
	if err != nil {
		return "", err
	}
	if ownerID == userID {
		return src.Name, nil
	}
	copied, err := s.copyFoodPlanTo(ctx, src, userID, src.Name, false)
	if err != nil {
		return "", err
	}
	return copied.Name, nil
}
