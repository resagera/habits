package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// --- Food: приёмы пищи, шаблоны, рецепты, шаринг, статистика ---

// FoodItem — элемент приёма пищи/шаблона/рецепта: снимок данных продукта
// на момент добавления (изменение продукта историю не меняет).
type FoodItem struct {
	ID          int64   `json:"id"`
	ProductID   *int64  `json:"product_id"`
	Name        string  `json:"name"`
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

type FoodMeal struct {
	ID          int64      `json:"id"`
	Day         string     `json:"day"`
	AtTime      string     `json:"time"`
	MealType    string     `json:"meal_type"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Photo       string     `json:"photo"`
	SourceType  string     `json:"source_type"`
	SourceID    *int64     `json:"source_id"`
	Calories    float64    `json:"calories"`
	Protein     float64    `json:"protein"`
	Fat         float64    `json:"fat"`
	Carbs       float64    `json:"carbs"`
	Items       []FoodItem `json:"items"`
}

// normalizeFoodItems — пересчёт итогов каждого элемента и суммы.
func normalizeFoodItems(items []FoodItem) (c, p, f, cb float64) {
	for i := range items {
		it := &items[i]
		it.Calories, it.Protein, it.Fat, it.Carbs =
			FoodItemTotals(it.Grams, it.CaloriesPer, it.ProteinPer, it.FatPer, it.CarbsPer)
		c += it.Calories
		p += it.Protein
		f += it.Fat
		cb += it.Carbs
	}
	return
}

const foodItemCols = `id, product_id, name, amount, unit, grams, base_type,
	calories_per, protein_per, fat_per, carbs_per, calories, protein, fat, carbs`

func scanFoodItem(rows pgx.Rows) (FoodItem, error) {
	var it FoodItem
	err := rows.Scan(&it.ID, &it.ProductID, &it.Name, &it.Amount, &it.Unit, &it.Grams, &it.BaseType,
		&it.CaloriesPer, &it.ProteinPer, &it.FatPer, &it.CarbsPer,
		&it.Calories, &it.Protein, &it.Fat, &it.Carbs)
	return it, err
}

// insertFoodItems — вставка элементов в одну из трёх таблиц (белый список).
func insertFoodItems(ctx context.Context, tx pgx.Tx, table, fkCol string, fkID int64, items []FoodItem) error {
	switch table {
	case "food_meal_items", "food_template_items", "food_recipe_items":
	default:
		return errors.New("bad items table")
	}
	for i, it := range items {
		_, err := tx.Exec(ctx, `INSERT INTO `+table+` (`+fkCol+`, product_id, name, amount, unit,
			grams, base_type, calories_per, protein_per, fat_per, carbs_per,
			calories, protein, fat, carbs, position)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
			fkID, it.ProductID, it.Name, it.Amount, it.Unit, it.Grams, it.BaseType,
			it.CaloriesPer, it.ProteinPer, it.FatPer, it.CarbsPer,
			it.Calories, it.Protein, it.Fat, it.Carbs, i)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) loadFoodItems(ctx context.Context, table, fkCol string, ids []int64) (map[int64][]FoodItem, error) {
	switch table {
	case "food_meal_items", "food_template_items", "food_recipe_items":
	default:
		return nil, errors.New("bad items table")
	}
	rows, err := s.pool.Query(ctx, `SELECT `+fkCol+`, `+foodItemCols+` FROM `+table+`
		WHERE `+fkCol+` = ANY($1) ORDER BY position, id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]FoodItem{}
	for rows.Next() {
		var fk int64
		var it FoodItem
		if err := rows.Scan(&fk, &it.ID, &it.ProductID, &it.Name, &it.Amount, &it.Unit, &it.Grams,
			&it.BaseType, &it.CaloriesPer, &it.ProteinPer, &it.FatPer, &it.CarbsPer,
			&it.Calories, &it.Protein, &it.Fat, &it.Carbs); err != nil {
			return nil, err
		}
		out[fk] = append(out[fk], it)
	}
	return out, rows.Err()
}

const foodMealCols = `id, day, at_time, meal_type, name, description, photo,
	source_type, source_id, calories, protein, fat, carbs`

func scanFoodMeal(row pgx.Row) (*FoodMeal, error) {
	var m FoodMeal
	var day time.Time
	err := row.Scan(&m.ID, &day, &m.AtTime, &m.MealType, &m.Name, &m.Description, &m.Photo,
		&m.SourceType, &m.SourceID, &m.Calories, &m.Protein, &m.Fat, &m.Carbs)
	if err != nil {
		return nil, err
	}
	m.Day = day.Format("2006-01-02")
	m.Items = []FoodItem{}
	return &m, nil
}

// CreateFoodMeal — приём пищи с элементами; итоги считаются из элементов,
// без элементов принимаются ручные значения из m.
func (s *Store) CreateFoodMeal(ctx context.Context, userID int64, m FoodMeal) (*FoodMeal, error) {
	if len(m.Items) > 0 {
		m.Calories, m.Protein, m.Fat, m.Carbs = normalizeFoodItems(m.Items)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	created, err := scanFoodMeal(tx.QueryRow(ctx, `
		INSERT INTO food_meals (user_id, day, at_time, meal_type, name, description, photo,
			source_type, source_id, calories, protein, fat, carbs)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING `+foodMealCols,
		userID, m.Day, m.AtTime, m.MealType, m.Name, m.Description, m.Photo,
		m.SourceType, m.SourceID, m.Calories, m.Protein, m.Fat, m.Carbs))
	if err != nil {
		return nil, err
	}
	if err := insertFoodItems(ctx, tx, "food_meal_items", "meal_id", created.ID, m.Items); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	created.Items = m.Items
	return created, nil
}

// UpdateFoodMeal — полная замена полей; при replaceItems элементы перезаписываются.
func (s *Store) UpdateFoodMeal(ctx context.Context, userID, id int64, m FoodMeal, replaceItems bool) (*FoodMeal, error) {
	if replaceItems && len(m.Items) > 0 {
		m.Calories, m.Protein, m.Fat, m.Carbs = normalizeFoodItems(m.Items)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	updated, err := scanFoodMeal(tx.QueryRow(ctx, `
		UPDATE food_meals SET day=$3, at_time=$4, meal_type=$5, name=$6, description=$7,
			photo=$8, calories=$9, protein=$10, fat=$11, carbs=$12, updated_at=now()
		WHERE id = $1 AND user_id = $2
		RETURNING `+foodMealCols,
		id, userID, m.Day, m.AtTime, m.MealType, m.Name, m.Description, m.Photo,
		m.Calories, m.Protein, m.Fat, m.Carbs))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if replaceItems {
		if _, err := tx.Exec(ctx, `DELETE FROM food_meal_items WHERE meal_id = $1`, id); err != nil {
			return nil, err
		}
		if err := insertFoodItems(ctx, tx, "food_meal_items", "meal_id", id, m.Items); err != nil {
			return nil, err
		}
		updated.Items = m.Items
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	if !replaceItems {
		items, err := s.loadFoodItems(ctx, "food_meal_items", "meal_id", []int64{id})
		if err != nil {
			return nil, err
		}
		if items[id] != nil {
			updated.Items = items[id]
		}
	}
	return updated, nil
}

func (s *Store) GetFoodMeal(ctx context.Context, userID, id int64) (*FoodMeal, error) {
	m, err := scanFoodMeal(s.pool.QueryRow(ctx, `SELECT `+foodMealCols+` FROM food_meals
		WHERE id = $1 AND user_id = $2`, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	items, err := s.loadFoodItems(ctx, "food_meal_items", "meal_id", []int64{id})
	if err != nil {
		return nil, err
	}
	if items[id] != nil {
		m.Items = items[id]
	}
	return m, nil
}

// DeleteFoodMeal — возвращает photo для возможной очистки файла.
func (s *Store) DeleteFoodMeal(ctx context.Context, userID, id int64) (string, error) {
	var photo string
	err := s.pool.QueryRow(ctx, `DELETE FROM food_meals WHERE id = $1 AND user_id = $2
		RETURNING photo`, id, userID).Scan(&photo)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return photo, err
}

// FoodDiaryMeals — приёмы пищи за день с элементами (два запроса, без N+1).
func (s *Store) FoodDiaryMeals(ctx context.Context, userID int64, day string) ([]FoodMeal, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+foodMealCols+` FROM food_meals
		WHERE user_id = $1 AND day = $2
		ORDER BY at_time = '', at_time, id`, userID, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var meals []FoodMeal
	var ids []int64
	for rows.Next() {
		m, err := scanFoodMeal(rows)
		if err != nil {
			return nil, err
		}
		meals = append(meals, *m)
		ids = append(ids, m.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		items, err := s.loadFoodItems(ctx, "food_meal_items", "meal_id", ids)
		if err != nil {
			return nil, err
		}
		for i := range meals {
			if its := items[meals[i].ID]; its != nil {
				meals[i].Items = its
			}
		}
	}
	return meals, nil
}

// DuplicateFoodMeal — независимая копия записи на указанную дату.
func (s *Store) DuplicateFoodMeal(ctx context.Context, userID, id int64, day string) (*FoodMeal, error) {
	src, err := s.GetFoodMeal(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	src.Day = day
	return s.CreateFoodMeal(ctx, userID, *src)
}

// --- шаблоны ---

type FoodTemplate struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Photo       string     `json:"photo"`
	MealType    string     `json:"meal_type"`
	Archived    bool       `json:"archived"`
	Calories    float64    `json:"calories"`
	Protein     float64    `json:"protein"`
	Fat         float64    `json:"fat"`
	Carbs       float64    `json:"carbs"`
	Items       []FoodItem `json:"items"`
}

const foodTemplateCols = `id, name, description, photo, meal_type, archived,
	calories, protein, fat, carbs`

func scanFoodTemplate(row pgx.Row) (*FoodTemplate, error) {
	var t FoodTemplate
	err := row.Scan(&t.ID, &t.Name, &t.Description, &t.Photo, &t.MealType, &t.Archived,
		&t.Calories, &t.Protein, &t.Fat, &t.Carbs)
	if err != nil {
		return nil, err
	}
	t.Items = []FoodItem{}
	return &t, nil
}

func (s *Store) ListFoodTemplates(ctx context.Context, userID int64) ([]FoodTemplate, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+foodTemplateCols+` FROM food_templates
		WHERE user_id = $1 ORDER BY archived, name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FoodTemplate
	var ids []int64
	for rows.Next() {
		t, err := scanFoodTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
		ids = append(ids, t.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		items, err := s.loadFoodItems(ctx, "food_template_items", "template_id", ids)
		if err != nil {
			return nil, err
		}
		for i := range out {
			if its := items[out[i].ID]; its != nil {
				out[i].Items = its
			}
		}
	}
	return out, nil
}

func (s *Store) CreateFoodTemplate(ctx context.Context, userID int64, t FoodTemplate) (*FoodTemplate, error) {
	t.Calories, t.Protein, t.Fat, t.Carbs = normalizeFoodItems(t.Items)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	created, err := scanFoodTemplate(tx.QueryRow(ctx, `
		INSERT INTO food_templates (user_id, name, description, photo, meal_type,
			calories, protein, fat, carbs)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+foodTemplateCols,
		userID, t.Name, t.Description, t.Photo, t.MealType, t.Calories, t.Protein, t.Fat, t.Carbs))
	if err != nil {
		return nil, err
	}
	if err := insertFoodItems(ctx, tx, "food_template_items", "template_id", created.ID, t.Items); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	created.Items = t.Items
	return created, nil
}

func (s *Store) UpdateFoodTemplate(ctx context.Context, userID, id int64, t FoodTemplate, replaceItems bool) (*FoodTemplate, error) {
	if replaceItems {
		t.Calories, t.Protein, t.Fat, t.Carbs = normalizeFoodItems(t.Items)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	updated, err := scanFoodTemplate(tx.QueryRow(ctx, `
		UPDATE food_templates SET name=$3, description=$4, photo=$5, meal_type=$6,
			archived=$7, calories=$8, protein=$9, fat=$10, carbs=$11, updated_at=now()
		WHERE id = $1 AND user_id = $2 RETURNING `+foodTemplateCols,
		id, userID, t.Name, t.Description, t.Photo, t.MealType, t.Archived,
		t.Calories, t.Protein, t.Fat, t.Carbs))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if replaceItems {
		if _, err := tx.Exec(ctx, `DELETE FROM food_template_items WHERE template_id = $1`, id); err != nil {
			return nil, err
		}
		if err := insertFoodItems(ctx, tx, "food_template_items", "template_id", id, t.Items); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	items, err := s.loadFoodItems(ctx, "food_template_items", "template_id", []int64{id})
	if err != nil {
		return nil, err
	}
	if items[id] != nil {
		updated.Items = items[id]
	}
	return updated, nil
}

func (s *Store) DeleteFoodTemplate(ctx context.Context, userID, id int64) (string, error) {
	var photo string
	err := s.pool.QueryRow(ctx, `DELETE FROM food_templates WHERE id = $1 AND user_id = $2
		RETURNING photo`, id, userID).Scan(&photo)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return photo, err
}

func (s *Store) getFoodTemplate(ctx context.Context, userID, id int64) (*FoodTemplate, error) {
	t, err := scanFoodTemplate(s.pool.QueryRow(ctx, `SELECT `+foodTemplateCols+`
		FROM food_templates WHERE id = $1 AND user_id = $2`, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	items, err := s.loadFoodItems(ctx, "food_template_items", "template_id", []int64{id})
	if err != nil {
		return nil, err
	}
	if items[id] != nil {
		t.Items = items[id]
	}
	return t, nil
}

// CreateMealFromTemplate — независимая запись дневника со снимком
// ТЕКУЩЕГО состава шаблона; фото переиспользуется (тот же файл).
func (s *Store) CreateMealFromTemplate(ctx context.Context, userID, tplID int64, day, atTime, mealType string) (*FoodMeal, error) {
	t, err := s.getFoodTemplate(ctx, userID, tplID)
	if err != nil {
		return nil, err
	}
	if mealType == "" {
		mealType = t.MealType
	}
	srcID := tplID
	return s.CreateFoodMeal(ctx, userID, FoodMeal{
		Day: day, AtTime: atTime, MealType: mealType, Name: t.Name,
		Description: t.Description, Photo: t.Photo,
		SourceType: "template", SourceID: &srcID, Items: t.Items,
		Calories: t.Calories, Protein: t.Protein, Fat: t.Fat, Carbs: t.Carbs,
	})
}

// SaveMealAsTemplate — шаблон из записи дневника (снимок состава).
func (s *Store) SaveMealAsTemplate(ctx context.Context, userID, mealID int64, name string) (*FoodTemplate, error) {
	m, err := s.GetFoodMeal(ctx, userID, mealID)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = m.Name
	}
	if name == "" {
		name = "Шаблон"
	}
	return s.CreateFoodTemplate(ctx, userID, FoodTemplate{
		Name: name, Description: m.Description, Photo: m.Photo,
		MealType: m.MealType, Items: m.Items,
	})
}

// --- рецепты ---

type FoodRecipe struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Steps       string     `json:"steps"`
	Photo       string     `json:"photo"`
	FinalWeight float64    `json:"final_weight"`
	Portions    float64    `json:"portions"`
	Archived    bool       `json:"archived"`
	Calories    float64    `json:"calories"`
	Protein     float64    `json:"protein"`
	Fat         float64    `json:"fat"`
	Carbs       float64    `json:"carbs"`
	Items       []FoodItem `json:"items"`
}

const foodRecipeCols = `id, name, description, steps, photo, final_weight, portions,
	archived, calories, protein, fat, carbs`

func scanFoodRecipe(row pgx.Row) (*FoodRecipe, error) {
	var rec FoodRecipe
	err := row.Scan(&rec.ID, &rec.Name, &rec.Description, &rec.Steps, &rec.Photo,
		&rec.FinalWeight, &rec.Portions, &rec.Archived,
		&rec.Calories, &rec.Protein, &rec.Fat, &rec.Carbs)
	if err != nil {
		return nil, err
	}
	rec.Items = []FoodItem{}
	return &rec, nil
}

func (s *Store) ListFoodRecipes(ctx context.Context, userID int64) ([]FoodRecipe, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+foodRecipeCols+` FROM food_recipes
		WHERE user_id = $1 ORDER BY archived, name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FoodRecipe
	var ids []int64
	for rows.Next() {
		rec, err := scanFoodRecipe(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
		ids = append(ids, rec.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		items, err := s.loadFoodItems(ctx, "food_recipe_items", "recipe_id", ids)
		if err != nil {
			return nil, err
		}
		for i := range out {
			if its := items[out[i].ID]; its != nil {
				out[i].Items = its
			}
		}
	}
	return out, nil
}

func (s *Store) CreateFoodRecipe(ctx context.Context, userID int64, rec FoodRecipe) (*FoodRecipe, error) {
	rec.Calories, rec.Protein, rec.Fat, rec.Carbs = normalizeFoodItems(rec.Items)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	created, err := scanFoodRecipe(tx.QueryRow(ctx, `
		INSERT INTO food_recipes (user_id, name, description, steps, photo, final_weight,
			portions, calories, protein, fat, carbs)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+foodRecipeCols,
		userID, rec.Name, rec.Description, rec.Steps, rec.Photo, rec.FinalWeight, rec.Portions,
		rec.Calories, rec.Protein, rec.Fat, rec.Carbs))
	if err != nil {
		return nil, err
	}
	if err := insertFoodItems(ctx, tx, "food_recipe_items", "recipe_id", created.ID, rec.Items); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	created.Items = rec.Items
	return created, nil
}

func (s *Store) UpdateFoodRecipe(ctx context.Context, userID, id int64, rec FoodRecipe, replaceItems bool) (*FoodRecipe, error) {
	if replaceItems {
		rec.Calories, rec.Protein, rec.Fat, rec.Carbs = normalizeFoodItems(rec.Items)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	updated, err := scanFoodRecipe(tx.QueryRow(ctx, `
		UPDATE food_recipes SET name=$3, description=$4, steps=$5, photo=$6, final_weight=$7,
			portions=$8, archived=$9, calories=$10, protein=$11, fat=$12, carbs=$13, updated_at=now()
		WHERE id = $1 AND user_id = $2 RETURNING `+foodRecipeCols,
		id, userID, rec.Name, rec.Description, rec.Steps, rec.Photo, rec.FinalWeight,
		rec.Portions, rec.Archived, rec.Calories, rec.Protein, rec.Fat, rec.Carbs))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if replaceItems {
		if _, err := tx.Exec(ctx, `DELETE FROM food_recipe_items WHERE recipe_id = $1`, id); err != nil {
			return nil, err
		}
		if err := insertFoodItems(ctx, tx, "food_recipe_items", "recipe_id", id, rec.Items); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	items, err := s.loadFoodItems(ctx, "food_recipe_items", "recipe_id", []int64{id})
	if err != nil {
		return nil, err
	}
	if items[id] != nil {
		updated.Items = items[id]
	}
	return updated, nil
}

func (s *Store) DeleteFoodRecipe(ctx context.Context, userID, id int64) (string, error) {
	var photo string
	err := s.pool.QueryRow(ctx, `DELETE FROM food_recipes WHERE id = $1 AND user_id = $2
		RETURNING photo`, id, userID).Scan(&photo)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return photo, err
}

func (s *Store) getFoodRecipe(ctx context.Context, userID, id int64) (*FoodRecipe, error) {
	rec, err := scanFoodRecipe(s.pool.QueryRow(ctx, `SELECT `+foodRecipeCols+`
		FROM food_recipes WHERE id = $1 AND user_id = $2`, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rec, err
}

// CreateMealFromRecipe — запись дневника со снимком рецепта.
// grams > 0 — съеденный вес (нужен итоговый вес блюда для КБЖУ на 100 г);
// иначе portions > 0 — доля от рецепта по порциям.
func (s *Store) CreateMealFromRecipe(ctx context.Context, userID, recipeID int64,
	day, atTime, mealType string, grams, portions float64) (*FoodMeal, error) {
	rec, err := s.getFoodRecipe(ctx, userID, recipeID)
	if err != nil {
		return nil, err
	}
	srcID := recipeID
	var item FoodItem
	switch {
	case grams > 0 && rec.FinalWeight > 0:
		item = FoodItem{
			Name: rec.Name, Amount: grams, Unit: "g", Grams: grams, BaseType: "g",
			CaloriesPer: FoodPer100(rec.Calories, rec.FinalWeight),
			ProteinPer:  FoodPer100(rec.Protein, rec.FinalWeight),
			FatPer:      FoodPer100(rec.Fat, rec.FinalWeight),
			CarbsPer:    FoodPer100(rec.Carbs, rec.FinalWeight),
		}
	case portions > 0 && rec.Portions > 0:
		// вес порции известен только при заданном итоговом весе; иначе —
		// условные 100 г на порцию, per-значения = КБЖУ одной порции
		pg := 100.0
		if rec.FinalWeight > 0 {
			pg = rec.FinalWeight / rec.Portions
		}
		item = FoodItem{
			Name: rec.Name, Amount: portions, Unit: "portion",
			Grams: portions * pg, BaseType: "g",
			CaloriesPer: rec.Calories / rec.Portions / pg * 100,
			ProteinPer:  rec.Protein / rec.Portions / pg * 100,
			FatPer:      rec.Fat / rec.Portions / pg * 100,
			CarbsPer:    rec.Carbs / rec.Portions / pg * 100,
		}
	default:
		return nil, errors.New("grams or portions required")
	}
	if mealType == "" {
		mealType = "none"
	}
	return s.CreateFoodMeal(ctx, userID, FoodMeal{
		Day: day, AtTime: atTime, MealType: mealType, Name: rec.Name, Photo: rec.Photo,
		SourceType: "recipe", SourceID: &srcID, Items: []FoodItem{item},
	})
}

// --- шаринг дневника ---

type FoodShareFlags struct {
	ShowWeight bool `json:"show_weight"`
	ShowGoals  bool `json:"show_goals"`
	ShowPhotos bool `json:"show_photos"`
	ShowNotes  bool `json:"show_notes"`
}

type FoodShareUser struct {
	AccessUser
	FoodShareFlags
}

// ShareFoodDiary — доступ на чтение к дневнику fromUser для toUser.
func (s *Store) ShareFoodDiary(ctx context.Context, fromUser, toUser int64) (string, error) {
	_, err := s.pool.Exec(ctx, `INSERT INTO food_shares (user_id, target_id)
		VALUES ($1, $2) ON CONFLICT DO NOTHING`, fromUser, toUser)
	return "Дневник питания", err
}

func (s *Store) ListFoodShares(ctx context.Context, ownerID int64) ([]FoodShareUser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, COALESCE(u.username, ''), COALESCE(u.first_name, ''),
		       fs.show_weight, fs.show_goals, fs.show_photos, fs.show_notes
		FROM food_shares fs JOIN users u ON u.id = fs.target_id
		WHERE fs.user_id = $1 ORDER BY fs.created_at`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FoodShareUser
	for rows.Next() {
		var u FoodShareUser
		if err := rows.Scan(&u.ID, &u.Username, &u.FirstName,
			&u.ShowWeight, &u.ShowGoals, &u.ShowPhotos, &u.ShowNotes); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) ListFoodSharedWithMe(ctx context.Context, userID int64) ([]FoodShareUser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, COALESCE(u.username, ''), COALESCE(u.first_name, ''),
		       fs.show_weight, fs.show_goals, fs.show_photos, fs.show_notes
		FROM food_shares fs JOIN users u ON u.id = fs.user_id
		WHERE fs.target_id = $1 ORDER BY fs.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FoodShareUser
	for rows.Next() {
		var u FoodShareUser
		if err := rows.Scan(&u.ID, &u.Username, &u.FirstName,
			&u.ShowWeight, &u.ShowGoals, &u.ShowPhotos, &u.ShowNotes); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) UpdateFoodShare(ctx context.Context, ownerID, targetID int64, f FoodShareFlags) error {
	tag, err := s.pool.Exec(ctx, `UPDATE food_shares
		SET show_weight=$3, show_goals=$4, show_photos=$5, show_notes=$6
		WHERE user_id = $1 AND target_id = $2`,
		ownerID, targetID, f.ShowWeight, f.ShowGoals, f.ShowPhotos, f.ShowNotes)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RevokeFoodShare(ctx context.Context, ownerID, targetID int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM food_shares
		WHERE user_id = $1 AND target_id = $2`, ownerID, targetID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetFoodShareFlags — флаги доступа viewer к дневнику owner (ErrNotFound — нет доступа).
func (s *Store) GetFoodShareFlags(ctx context.Context, ownerID, viewerID int64) (*FoodShareFlags, error) {
	var f FoodShareFlags
	err := s.pool.QueryRow(ctx, `SELECT show_weight, show_goals, show_photos, show_notes
		FROM food_shares WHERE user_id = $1 AND target_id = $2`, ownerID, viewerID).
		Scan(&f.ShowWeight, &f.ShowGoals, &f.ShowPhotos, &f.ShowNotes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &f, err
}

// --- статистика ---

// FoodDayStat — агрегат дня; источник истины — food_meals/food_meal_items.
type FoodDayStat struct {
	Day       string  `json:"day"`
	Calories  float64 `json:"calories"`
	Protein   float64 `json:"protein"`
	Fat       float64 `json:"fat"`
	Carbs     float64 `json:"carbs"`
	Meals     int     `json:"meals"`
	Breakfast float64 `json:"breakfast"`
	Lunch     float64 `json:"lunch"`
	Dinner    float64 `json:"dinner"`
	Snack     float64 `json:"snack"`
	Other     float64 `json:"other"`
}

func (s *Store) FoodStats(ctx context.Context, userID int64, from, to string) ([]FoodDayStat, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT day, COALESCE(sum(calories),0), COALESCE(sum(protein),0),
		       COALESCE(sum(fat),0), COALESCE(sum(carbs),0), count(*),
		       COALESCE(sum(calories) FILTER (WHERE meal_type = 'breakfast'), 0),
		       COALESCE(sum(calories) FILTER (WHERE meal_type = 'lunch'), 0),
		       COALESCE(sum(calories) FILTER (WHERE meal_type = 'dinner'), 0),
		       COALESCE(sum(calories) FILTER (WHERE meal_type = 'snack'), 0),
		       COALESCE(sum(calories) FILTER (WHERE meal_type = 'none'), 0)
		FROM food_meals WHERE user_id = $1 AND day BETWEEN $2 AND $3
		GROUP BY day ORDER BY day`, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FoodDayStat
	for rows.Next() {
		var d FoodDayStat
		var day time.Time
		if err := rows.Scan(&day, &d.Calories, &d.Protein, &d.Fat, &d.Carbs, &d.Meals,
			&d.Breakfast, &d.Lunch, &d.Dinner, &d.Snack, &d.Other); err != nil {
			return nil, err
		}
		d.Day = day.Format("2006-01-02")
		out = append(out, d)
	}
	return out, rows.Err()
}

// FoodGoalsInRange — периоды целей, пересекающие диапазон (для линий цели).
func (s *Store) FoodGoalsInRange(ctx context.Context, userID int64, from, to string) ([]FoodGoal, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+foodGoalCols+` FROM food_goals
		WHERE user_id = $1 AND date_from <= $3 AND (date_to IS NULL OR date_to >= $2)
		ORDER BY date_from`, userID, from, to)
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
