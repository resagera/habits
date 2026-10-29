package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// --- Food: профиль, цели, каталог продуктов, расчёты ---

// FoodActivityCoef — коэффициенты активности для TDEE (конфигурация,
// меняется без правки бизнес-логики).
var FoodActivityCoef = map[string]float64{
	"min":    1.2,
	"low":    1.375,
	"medium": 1.55,
	"high":   1.725,
	"max":    1.9,
}

// FoodActivityLabels — русские названия уровней (для details).
var FoodActivityLabels = map[string]string{
	"min": "минимальная", "low": "низкая", "medium": "средняя",
	"high": "высокая", "max": "очень высокая",
}

type FoodProfile struct {
	UserID        int64   `json:"-"`
	Sex           string  `json:"sex"`
	BirthDate     string  `json:"birth_date"`
	HeightCm      float64 `json:"height_cm"`
	WeightKg      float64 `json:"weight_kg"`
	TargetWeight  float64 `json:"target_weight_kg"`
	BodyFat       float64 `json:"body_fat_percent"`
	ActivityLevel string  `json:"activity_level"`
	GoalType      string  `json:"goal_type"`
	RateKcal      float64 `json:"rate_kcal"`
	ProteinBase   string  `json:"protein_base"`
	ProteinBaseKg float64 `json:"protein_base_kg"`
	ProteinCoef   float64 `json:"protein_coef"`
}

type FoodTargets struct {
	BMR      float64 `json:"bmr"`
	TDEE     float64 `json:"tdee"`
	Calories float64 `json:"calories"`
	Protein  float64 `json:"protein"`
	Fat      float64 `json:"fat"`
	Carbs    float64 `json:"carbs"`
	Details  string  `json:"details"`
}

// FoodBMR — базовый обмен по Mifflin–St Jeor.
func FoodBMR(sex string, weightKg, heightCm float64, age int) float64 {
	bmr := 10*weightKg + 6.25*heightCm - 5*float64(age)
	if sex == "female" {
		return bmr - 161
	}
	return bmr + 5
}

// FoodAge — полных лет на дату now.
func FoodAge(birth, now time.Time) int {
	age := now.Year() - birth.Year()
	if now.Month() < birth.Month() || (now.Month() == birth.Month() && now.Day() < birth.Day()) {
		age--
	}
	if age < 0 {
		age = 0
	}
	return age
}

// FoodProteinWeight — база расчёта белка по профилю (кг).
func FoodProteinWeight(p FoodProfile) (kg float64, label string) {
	switch p.ProteinBase {
	case "target":
		if p.TargetWeight > 0 {
			return p.TargetWeight, "целевого веса"
		}
	case "lean":
		if p.BodyFat > 0 && p.WeightKg > 0 {
			return p.WeightKg * (1 - p.BodyFat/100), "безжировой массы"
		}
	case "manual":
		if p.ProteinBaseKg > 0 {
			return p.ProteinBaseKg, "заданного веса"
		}
	}
	return p.WeightKg, "текущего веса"
}

// FoodCalcTargets — дневные цели по профилю: BMR → TDEE → коррекция цели,
// белок = база × коэффициент, жиры 30% калорий, углеводы — остаток.
func FoodCalcTargets(p FoodProfile, now time.Time) (FoodTargets, error) {
	if p.Sex == "" || p.HeightCm <= 0 || p.WeightKg <= 0 || p.BirthDate == "" {
		return FoodTargets{}, errors.New("profile incomplete")
	}
	birth, err := time.Parse("2006-01-02", p.BirthDate)
	if err != nil {
		return FoodTargets{}, errors.New("invalid birth_date")
	}
	coef, ok := FoodActivityCoef[p.ActivityLevel]
	if !ok {
		return FoodTargets{}, errors.New("invalid activity_level")
	}
	age := FoodAge(birth, now)
	bmr := FoodBMR(p.Sex, p.WeightKg, p.HeightCm, age)
	tdee := bmr * coef
	calories := tdee
	adjust := ""
	switch p.GoalType {
	case "lose":
		calories = tdee - p.RateKcal
		adjust = fmt.Sprintf(" − дефицит %.0f", p.RateKcal)
	case "gain":
		calories = tdee + p.RateKcal
		adjust = fmt.Sprintf(" + профицит %.0f", p.RateKcal)
	}
	if calories < 0 {
		calories = 0
	}
	protKg, protLabel := FoodProteinWeight(p)
	protein := protKg * p.ProteinCoef
	fat := calories * 0.30 / 9
	carbs := (calories - protein*4 - fat*9) / 4
	if carbs < 0 {
		carbs = 0
	}
	details := fmt.Sprintf(
		"BMR %.0f ккал (Mifflin–St Jeor, %d лет) × %.3g (активность: %s) = %.0f ккал%s → %.0f ккал. "+
			"Белок: %.1f кг (%s) × %.1f г/кг = %.0f г. Жиры: 30%% калорий = %.0f г. Углеводы: остаток = %.0f г.",
		bmr, age, coef, FoodActivityLabels[p.ActivityLevel], tdee, adjust, calories,
		protKg, protLabel, p.ProteinCoef, protein, fat, carbs)
	return FoodTargets{
		BMR: math.Round(bmr), TDEE: math.Round(tdee), Calories: math.Round(calories),
		Protein: math.Round(protein), Fat: math.Round(fat), Carbs: math.Round(carbs),
		Details: details,
	}, nil
}

// FoodGramsFor — перевод количества в граммы/мл по единице измерения.
// Для piece/portion нужен вес единицы из продукта; 0 — неизвестно
// (клиент обязан прислать вес вручную).
func FoodGramsFor(unit string, amount, pieceGrams, portionGrams float64) float64 {
	switch unit {
	case "g", "ml":
		return amount
	case "piece":
		return amount * pieceGrams
	case "portion":
		return amount * portionGrams
	}
	return 0
}

// FoodItemTotals — КБЖУ элемента: grams/100 × значения на 100 г(мл).
func FoodItemTotals(grams, calPer, protPer, fatPer, carbPer float64) (c, p, f, cb float64) {
	k := grams / 100
	return k * calPer, k * protPer, k * fatPer, k * carbPer
}

// FoodPer100 — пересчёт итогов рецепта на 100 г готового блюда.
func FoodPer100(total, finalWeight float64) float64 {
	if finalWeight <= 0 {
		return 0
	}
	return total / finalWeight * 100
}

// --- профиль ---

func (s *Store) GetFoodProfile(ctx context.Context, userID int64) (*FoodProfile, error) {
	p := FoodProfile{UserID: userID}
	var birth *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT sex, birth_date, height_cm, weight_kg, target_weight_kg, body_fat_percent,
		       activity_level, goal_type, rate_kcal, protein_base, protein_base_kg, protein_coef
		FROM food_profiles WHERE user_id = $1`, userID).Scan(
		&p.Sex, &birth, &p.HeightCm, &p.WeightKg, &p.TargetWeight, &p.BodyFat,
		&p.ActivityLevel, &p.GoalType, &p.RateKcal, &p.ProteinBase, &p.ProteinBaseKg, &p.ProteinCoef)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if birth != nil {
		p.BirthDate = birth.Format("2006-01-02")
	}
	return &p, nil
}

func (s *Store) SaveFoodProfile(ctx context.Context, p FoodProfile) error {
	var birth any
	if p.BirthDate != "" {
		birth = p.BirthDate
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO food_profiles (user_id, sex, birth_date, height_cm, weight_kg,
		    target_weight_kg, body_fat_percent, activity_level, goal_type, rate_kcal,
		    protein_base, protein_base_kg, protein_coef)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (user_id) DO UPDATE SET
		    sex = EXCLUDED.sex, birth_date = EXCLUDED.birth_date,
		    height_cm = EXCLUDED.height_cm, weight_kg = EXCLUDED.weight_kg,
		    target_weight_kg = EXCLUDED.target_weight_kg,
		    body_fat_percent = EXCLUDED.body_fat_percent,
		    activity_level = EXCLUDED.activity_level, goal_type = EXCLUDED.goal_type,
		    rate_kcal = EXCLUDED.rate_kcal, protein_base = EXCLUDED.protein_base,
		    protein_base_kg = EXCLUDED.protein_base_kg, protein_coef = EXCLUDED.protein_coef,
		    updated_at = now()`,
		p.UserID, p.Sex, birth, p.HeightCm, p.WeightKg, p.TargetWeight, p.BodyFat,
		p.ActivityLevel, p.GoalType, p.RateKcal, p.ProteinBase, p.ProteinBaseKg, p.ProteinCoef)
	return err
}

// --- периоды целей ---

type FoodGoal struct {
	ID       int64   `json:"id"`
	DateFrom string  `json:"date_from"`
	DateTo   string  `json:"date_to"`
	GoalType string  `json:"goal_type"`
	Calories float64 `json:"calories"`
	Protein  float64 `json:"protein"`
	Fat      float64 `json:"fat"`
	Carbs    float64 `json:"carbs"`
	Source   string  `json:"source"`
	Details  string  `json:"details"`
}

func scanFoodGoal(row pgx.Row) (*FoodGoal, error) {
	var g FoodGoal
	var to *time.Time
	var from time.Time
	err := row.Scan(&g.ID, &from, &to, &g.GoalType, &g.Calories, &g.Protein, &g.Fat, &g.Carbs, &g.Source, &g.Details)
	if err != nil {
		return nil, err
	}
	g.DateFrom = from.Format("2006-01-02")
	if to != nil {
		g.DateTo = to.Format("2006-01-02")
	}
	return &g, nil
}

const foodGoalCols = `id, date_from, date_to, goal_type, calories, protein, fat, carbs, source, details`

func (s *Store) ListFoodGoals(ctx context.Context, userID int64) ([]FoodGoal, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+foodGoalCols+` FROM food_goals
		WHERE user_id = $1 ORDER BY date_from DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FoodGoal
	for rows.Next() {
		g, err := scanFoodGoal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

// FoodGoalForDate — действующий период цели на дату (или nil).
func (s *Store) FoodGoalForDate(ctx context.Context, userID int64, day string) (*FoodGoal, error) {
	g, err := scanFoodGoal(s.pool.QueryRow(ctx, `SELECT `+foodGoalCols+` FROM food_goals
		WHERE user_id = $1 AND date_from <= $2 AND (date_to IS NULL OR date_to >= $2)
		ORDER BY date_from DESC LIMIT 1`, userID, day))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return g, err
}

// CreateFoodGoal — новый период с даты dateFrom: прошлый открытый период
// закрывается днём раньше, период с той же датой начала заменяется.
func (s *Store) CreateFoodGoal(ctx context.Context, userID int64, g FoodGoal) (*FoodGoal, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM food_goals WHERE user_id = $1 AND date_from = $2`,
		userID, g.DateFrom); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE food_goals
		SET date_to = ($2::date - 1)
		WHERE user_id = $1 AND date_from < $2 AND (date_to IS NULL OR date_to >= $2)`,
		userID, g.DateFrom); err != nil {
		return nil, err
	}
	created, err := scanFoodGoal(tx.QueryRow(ctx, `
		INSERT INTO food_goals (user_id, date_from, goal_type, calories, protein, fat, carbs, source, details)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING `+foodGoalCols, userID, g.DateFrom, g.GoalType, g.Calories, g.Protein, g.Fat, g.Carbs, g.Source, g.Details))
	if err != nil {
		return nil, err
	}
	return created, tx.Commit(ctx)
}

// --- каталог продуктов ---

type FoodProduct struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	AltName      string  `json:"alt_name"`
	Brand        string  `json:"brand"`
	Category     string  `json:"category"`
	Photo        string  `json:"photo"`
	BaseType     string  `json:"base_type"`
	Calories     float64 `json:"calories"`
	Protein      float64 `json:"protein"`
	Fat          float64 `json:"fat"`
	Carbs        float64 `json:"carbs"`
	PieceGrams   float64 `json:"piece_grams"`
	PortionGrams float64 `json:"portion_grams"`
	Archived     bool    `json:"archived"`
	UsedCount    int64   `json:"used_count"`
	Recent       bool    `json:"recent"`
}

const foodProductCols = `id, name, alt_name, brand, category, photo, base_type,
	calories, protein, fat, carbs, piece_grams, portion_grams, archived, used_count,
	(last_used_at IS NOT NULL AND last_used_at > now() - interval '14 days') AS recent`

func scanFoodProduct(row pgx.Row) (*FoodProduct, error) {
	var p FoodProduct
	err := row.Scan(&p.ID, &p.Name, &p.AltName, &p.Brand, &p.Category, &p.Photo, &p.BaseType,
		&p.Calories, &p.Protein, &p.Fat, &p.Carbs, &p.PieceGrams, &p.PortionGrams,
		&p.Archived, &p.UsedCount, &p.Recent)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListFoodProducts — поиск по названию/альт. названию/производителю;
// сначала недавние и частые. Без q — недавние/частые первыми.
func (s *Store) ListFoodProducts(ctx context.Context, userID int64, q string, archived bool, limit, offset int) ([]FoodProduct, error) {
	where := `user_id = $1 AND archived = $2`
	args := []any{userID, archived}
	if q = strings.TrimSpace(q); q != "" {
		where += ` AND (name ILIKE $3 OR alt_name ILIKE $3 OR brand ILIKE $3)`
		args = append(args, "%"+q+"%")
	}
	args = append(args, limit, offset)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`SELECT %s FROM food_products WHERE %s
		ORDER BY (last_used_at IS NULL), last_used_at DESC, used_count DESC, name
		LIMIT $%d OFFSET $%d`, foodProductCols, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FoodProduct
	for rows.Next() {
		p, err := scanFoodProduct(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) GetFoodProduct(ctx context.Context, userID, id int64) (*FoodProduct, error) {
	p, err := scanFoodProduct(s.pool.QueryRow(ctx, `SELECT `+foodProductCols+`
		FROM food_products WHERE id = $1 AND user_id = $2`, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

func (s *Store) CreateFoodProduct(ctx context.Context, userID int64, p FoodProduct) (*FoodProduct, error) {
	return scanFoodProduct(s.pool.QueryRow(ctx, `
		INSERT INTO food_products (user_id, name, alt_name, brand, category, photo, base_type,
			calories, protein, fat, carbs, piece_grams, portion_grams)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING `+foodProductCols,
		userID, p.Name, p.AltName, p.Brand, p.Category, p.Photo, p.BaseType,
		p.Calories, p.Protein, p.Fat, p.Carbs, p.PieceGrams, p.PortionGrams))
}

func (s *Store) UpdateFoodProduct(ctx context.Context, userID, id int64, p FoodProduct) (*FoodProduct, error) {
	updated, err := scanFoodProduct(s.pool.QueryRow(ctx, `
		UPDATE food_products SET name=$3, alt_name=$4, brand=$5, category=$6, photo=$7,
			base_type=$8, calories=$9, protein=$10, fat=$11, carbs=$12,
			piece_grams=$13, portion_grams=$14, archived=$15, updated_at=now()
		WHERE id = $1 AND user_id = $2
		RETURNING `+foodProductCols,
		id, userID, p.Name, p.AltName, p.Brand, p.Category, p.Photo, p.BaseType,
		p.Calories, p.Protein, p.Fat, p.Carbs, p.PieceGrams, p.PortionGrams, p.Archived))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return updated, err
}

// FoodProductUsed — использовался ли продукт в истории (записи/шаблоны/рецепты).
func (s *Store) FoodProductUsed(ctx context.Context, id int64) (bool, error) {
	var used bool
	err := s.pool.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM food_meal_items WHERE product_id = $1) OR
		EXISTS (SELECT 1 FROM food_template_items WHERE product_id = $1) OR
		EXISTS (SELECT 1 FROM food_recipe_items WHERE product_id = $1)`, id).Scan(&used)
	return used, err
}

// DeleteFoodProduct — архивирует использованный продукт, неиспользованный
// удаляет физически. Возвращает (archived, photoURL для очистки).
func (s *Store) DeleteFoodProduct(ctx context.Context, userID, id int64) (bool, string, error) {
	if _, err := s.GetFoodProduct(ctx, userID, id); err != nil {
		return false, "", err
	}
	used, err := s.FoodProductUsed(ctx, id)
	if err != nil {
		return false, "", err
	}
	if used {
		_, err = s.pool.Exec(ctx, `UPDATE food_products SET archived = TRUE, updated_at = now()
			WHERE id = $1 AND user_id = $2`, id, userID)
		return true, "", err
	}
	var photo string
	err = s.pool.QueryRow(ctx, `DELETE FROM food_products WHERE id = $1 AND user_id = $2
		RETURNING photo`, id, userID).Scan(&photo)
	return false, photo, err
}

// TouchFoodProducts — отметка использования продуктов (недавние/частые в поиске).
func (s *Store) TouchFoodProducts(ctx context.Context, userID int64, ids []int64) {
	if len(ids) == 0 {
		return
	}
	_, _ = s.pool.Exec(ctx, `UPDATE food_products
		SET used_count = used_count + 1, last_used_at = now()
		WHERE user_id = $1 AND id = ANY($2)`, userID, ids)
}

// FoodPhotoRefCount — сколько сущностей ссылаются на файл фото
// (физическое удаление файла — только при нуле ссылок).
func (s *Store) FoodPhotoRefCount(ctx context.Context, url string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM food_meals WHERE photo = $1) +
		(SELECT count(*) FROM food_templates WHERE photo = $1) +
		(SELECT count(*) FROM food_recipes WHERE photo = $1) +
		(SELECT count(*) FROM food_products WHERE photo = $1)`, url).Scan(&n)
	return n, err
}
