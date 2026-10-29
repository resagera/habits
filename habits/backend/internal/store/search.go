package store

import "context"

// SearchHit — один результат глобального поиска.
type SearchHit struct {
	Page     string `json:"page"`     // код страницы (для иконки и роута)
	Title    string `json:"title"`    // что показать
	Subtitle string `json:"subtitle"` // контекст (группа, дата, URL…)
}


// GlobalSearch ищет по данным пользователя во всех приложениях (кроме
// паролей — они не покидают устройство). pages ограничивает источники
// видимыми пользователю страницами.
func (s *Store) GlobalSearch(ctx context.Context, userID int64, q string, pages map[string]bool) ([]SearchHit, error) {
	like := "%" + q + "%"
	var hits []SearchHit

	add := func(page, query string, args ...any) error {
		if !pages[page] {
			return nil
		}
		rows, err := s.pool.Query(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			h := SearchHit{Page: page}
			if err := rows.Scan(&h.Title, &h.Subtitle); err != nil {
				return err
			}
			hits = append(hits, h)
		}
		return rows.Err()
	}

	if err := add("tracker", `
		SELECT name, 'категория' FROM tracker_categories
		WHERE user_id = $1 AND name ILIKE $2 ORDER BY position LIMIT 5`,
		userID, like); err != nil {
		return nil, err
	}
	if err := add("checker", `
		SELECT g.name, 'чек-лист' FROM checker_groups g
		WHERE g.user_id = $1 AND g.name ILIKE $2
		UNION ALL
		SELECT i.name, 'пункт в «' || g.name || '»' FROM checker_items i
		JOIN checker_groups g ON g.id = i.group_id
		WHERE g.user_id = $1 AND i.name ILIKE $2
		UNION ALL
		SELECT t.name, 'шаблон' FROM checker_templates t
		WHERE t.user_id = $1 AND t.name ILIKE $2
		LIMIT 5`, userID, like); err != nil {
		return nil, err
	}
	if err := add("tasks", `
		SELECT t.title, COALESCE('категория «' || p.name || '»', 'задача')
		FROM tasks t LEFT JOIN task_projects p ON p.id = t.project_id
		WHERE t.user_id = $1 AND t.status_kind = 'open'
		  AND (t.title ILIKE $2 OR t.note ILIKE $2)
		ORDER BY t.due_date NULLS LAST LIMIT 5`, userID, like); err != nil {
		return nil, err
	}
	if err := add("diary", `
		SELECT left(text, 80), to_char(at, 'DD.MM.YYYY') FROM diary_entries
		WHERE user_id = $1 AND text ILIKE $2 ORDER BY at DESC LIMIT 5`,
		userID, like); err != nil {
		return nil, err
	}
	if err := add("links", `
		SELECT name, url FROM links
		WHERE user_id = $1 AND (name ILIKE $2 OR url ILIKE $2 OR EXISTS (
			SELECT 1 FROM unnest(tags) tag WHERE tag ILIKE $2))
		ORDER BY pinned DESC, clicks DESC LIMIT 5`,
		userID, like); err != nil {
		return nil, err
	}
	if err := add("metrics", `
		SELECT c.name, 'категория метрик' FROM metrics_categories c
		WHERE c.user_id = $1 AND c.name ILIKE $2
		UNION ALL
		SELECT i.name, 'график в «' || c.name || '»' FROM metrics_items i
		JOIN metrics_categories c ON c.id = i.category_id
		WHERE c.user_id = $1 AND i.name ILIKE $2
		LIMIT 5`, userID, like); err != nil {
		return nil, err
	}
	if err := add("reminders", `
		SELECT title, CASE WHEN enabled THEN 'напоминание' ELSE 'напоминание (выкл)' END
		FROM reminders WHERE user_id = $1 AND (title ILIKE $2 OR note ILIKE $2)
		ORDER BY enabled DESC LIMIT 5`, userID, like); err != nil {
		return nil, err
	}
	if err := add("articles", `
		SELECT title, 'статья' FROM articles
		WHERE user_id = $1 AND (title ILIKE $2 OR content ILIKE $2)
		ORDER BY updated_at DESC LIMIT 5`, userID, like); err != nil {
		return nil, err
	}
	if err := add("servers", `
		SELECT name, url FROM servers
		WHERE user_id = $1 AND (name ILIKE $2 OR url ILIKE $2) LIMIT 5`,
		userID, like); err != nil {
		return nil, err
	}
	return hits, nil
}

