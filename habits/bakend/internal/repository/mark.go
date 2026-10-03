package repository

import (
	"database/sql"
	"log"
	"time"

	_ "github.com/lib/pq"
)

type Repo struct {
	db *sql.DB
}

var _ Repository = &Repo{}

func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) GetUserData(user string) ([]Category, []Mark, error) {
	var categories []Category
	rows, err := r.db.Query(`
		SELECT c.name, COALESCE(cc.color, '#22c55e')
		FROM categories c
		LEFT JOIN category_colors cc ON c.user_id = cc.user_id AND c.name = cc.category
		WHERE c.user_id = $1`, user)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.Name, &c.Color); err != nil {
			return nil, nil, err
		}
		categories = append(categories, c)
	}

	var marks []Mark
	rows, err = r.db.Query("SELECT category, TO_CHAR(date, 'YYYY-MM-DD') FROM marks WHERE user_id = $1", user)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m Mark
		if err := rows.Scan(&m.Category, &m.Date); err != nil {
			return nil, nil, err
		}
		marks = append(marks, m)
	}

	return categories, marks, nil
}

func (r *Repo) SetCategories(user string, categories []string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("DELETE FROM categories WHERE user_id = $1", user)
	if err != nil {
		return err
	}

	for _, cat := range categories {
		_, err = tx.Exec("INSERT INTO categories (user_id, name) VALUES ($1, $2)", user, cat)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repo) ToggleMark(user, category, date string) error {
	var exists bool
	err := r.db.QueryRow("SELECT EXISTS(SELECT 1 FROM marks WHERE user_id = $1 AND category = $2 AND date = $3)",
		user, category, date).Scan(&exists)
	if err != nil {
		return err
	}

	if exists {
		_, err = r.db.Exec("DELETE FROM marks WHERE user_id = $1 AND category = $2 AND date = $3", user, category, date)
	} else {
		_, err = r.db.Exec("INSERT INTO marks (user_id, category, date) VALUES ($1, $2, $3)", user, category, date)
	}
	return err
}

func (r *Repo) GetMarksByMonth(user, category, month string) ([]int, error) {
	rows, err := r.db.Query(`
		SELECT to_char(date, 'YYYY-MM-DD') FROM marks
		WHERE user_id = $1 AND category = $2 AND to_char(date, 'YYYY-MM') = $3`, user, category, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var days []int
	for rows.Next() {
		var dateStr string
		if err := rows.Scan(&dateStr); err != nil {
			continue
		}
		t, err := time.Parse("2006-01-02", dateStr)
		if err == nil {
			days = append(days, t.Day())
		}
	}
	return days, nil
}

func (r *Repo) SetCategoryColor(user, category, color string) error {
	_, err := r.db.Exec(`
		INSERT INTO category_colors (user_id, category, color)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, category)
		DO UPDATE SET color = EXCLUDED.color`,
		user, category, color)
	return err
}

func (r *Repo) SetCategoryName(user, oldName, newName, color string) error {
	// Обновляем имя категории
	_, err := r.db.Exec("UPDATE categories SET name = $1 WHERE user_id = $2 AND name = $3", newName, user, oldName)
	if err != nil {
		return err
	}
	// Обновляем цвет (если нужно)
	if color != "" {
		_, err = r.db.Exec(`
			INSERT INTO category_colors (user_id, category, color)
			VALUES ($1, $2, $3)
			ON CONFLICT (user_id, category)
			DO UPDATE SET color = EXCLUDED.color`,
			user, newName, color)
	}
	return err
}

// --- Checks ---

func (r *Repo) GetChecks(user string) ([]CheckGroup, error) {
	rows, err := r.db.Query("SELECT group_name, item_name, done FROM checks WHERE user_id = $1", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groupsMap := make(map[string][]CheckItem)
	for rows.Next() {
		var groupName, itemName string
		var done bool
		if err := rows.Scan(&groupName, &itemName, &done); err != nil {
			continue
		}
		groupsMap[groupName] = append(groupsMap[groupName], CheckItem{Name: itemName, Done: done})
	}

	var groups []CheckGroup
	for name, items := range groupsMap {
		groups = append(groups, CheckGroup{Name: name, Items: items})
	}
	return groups, nil
}

func (r *Repo) AddCheckGroup(user, name string) error {
	_, err := r.db.Exec(`
		INSERT INTO checks (user_id, group_name, item_name, done)
		VALUES ($1, $2, '', false)
		ON CONFLICT DO NOTHING`, user, name)
	return err
}

func (r *Repo) AddCheckItem(user, group, name string) error {
	_, err := r.db.Exec(`
		INSERT INTO checks (user_id, group_name, item_name, done)
		VALUES ($1, $2, $3, false)
		ON CONFLICT (user_id, group_name, item_name)
		DO UPDATE SET done = EXCLUDED.done`, user, group, name)
	return err
}

func (r *Repo) ToggleCheckItem(user, group, item string, done bool) error {
	_, err := r.db.Exec(`
		INSERT INTO checks (user_id, group_name, item_name, done)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, group_name, item_name)
		DO UPDATE SET done = EXCLUDED.done`,
		user, group, item, done)
	return err
}

func (r *Repo) RenameCheckGroup(user, oldName, newName string) error {
	_, err := r.db.Exec("UPDATE checks SET group_name = $1 WHERE user_id = $2 AND group_name = $3",
		newName, user, oldName)
	return err
}

func (r *Repo) DeleteCheckGroup(user, name string) error {
	_, err := r.db.Exec("DELETE FROM checks WHERE user_id = $1 AND group_name = $2", user, name)
	return err
}

func (r *Repo) DeleteCheckItem(user, group, name string) error {
	_, err := r.db.Exec("DELETE FROM checks WHERE user_id = $1 AND group_name = $2 AND item_name = $3",
		user, group, name)
	return err
}

// TODO: remove
// deprecated
func (r *Repo) loadUserChecksFromDB(user string) {
	rows, err := r.db.Query("SELECT group_name, item_name, done FROM checks WHERE user_id = $1", user)
	if err != nil {
		log.Println("loadUserChecksFromDB:", err)
		return
	}
	defer rows.Close()

	groupsMap := map[string][]CheckItem{}
	for rows.Next() {
		var groupName, itemName string
		var doneInt int
		rows.Scan(&groupName, &itemName, &doneInt)
		groupsMap[groupName] = append(groupsMap[groupName], CheckItem{
			Name: itemName,
			Done: doneInt == 1,
		})
	}

	var groups []CheckGroup
	for name, items := range groupsMap {
		groups = append(groups, CheckGroup{Name: name, Items: items})
	}
	userChecks[user] = groups
}
