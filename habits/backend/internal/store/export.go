package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Таблицы, которых в выгрузке быть не должно: хэши токенов и внутренняя
// очередь уведомлений человеку ничего не говорят.
var exportSkip = map[string]bool{
	"access_tokens": true,
	"notify_queue":  true,
}

// ExportUserData — все данные пользователя одним JSON.
//
// Список таблиц не зашит в код, а берётся из схемы: сначала всё, где есть
// колонка user_id, затем то, что связано с ними внешними ключами (записи
// трекера лежат в категориях, пункты — в чек-листах). Захардкоженный список
// устарел бы на первой же новой странице.
func (s *Store) ExportUserData(ctx context.Context, userID int64) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	where := map[string]string{} // таблица → условие «это данные пользователя»

	rows, err := s.pool.Query(ctx, `
		SELECT table_name FROM information_schema.columns
		WHERE table_schema = 'public' AND column_name = 'user_id'
		ORDER BY table_name`)
	if err != nil {
		return nil, err
	}
	roots, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	for _, t := range roots {
		if exportSkip[t] {
			continue
		}
		where[t] = fmt.Sprintf(`%s.user_id = $1`, quoteIdent(t))
	}

	// внешние ключи: чем связаны дочерние таблицы с родительскими
	rows, err = s.pool.Query(ctx, `
		SELECT tc.table_name, kcu.column_name, ccu.table_name, ccu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema
		JOIN information_schema.constraint_column_usage ccu
		  ON ccu.constraint_name = tc.constraint_name AND ccu.table_schema = tc.table_schema
		WHERE tc.constraint_type = 'FOREIGN KEY' AND tc.table_schema = 'public'`)
	if err != nil {
		return nil, err
	}
	type fk struct{ child, col, parent, ref string }
	var fks []fk
	for rows.Next() {
		var f fk
		if err := rows.Scan(&f.child, &f.col, &f.parent, &f.ref); err != nil {
			rows.Close()
			return nil, err
		}
		fks = append(fks, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// несколько проходов: внуки подтягиваются после детей
	for pass := 0; pass < 3; pass++ {
		added := false
		for _, f := range fks {
			if f.child == f.parent || exportSkip[f.child] || f.parent == "users" {
				continue //ссылки на себя и на users уже покрыты корнем
			}
			if where[f.child] != "" || where[f.parent] == "" {
				continue
			}
			where[f.child] = fmt.Sprintf(`%s.%s IN (SELECT %s.%s FROM %s WHERE %s)`,
				quoteIdent(f.child), quoteIdent(f.col),
				quoteIdent(f.parent), quoteIdent(f.ref), quoteIdent(f.parent), where[f.parent])
			added = true
		}
		if !added {
			break
		}
	}

	// сам профиль: в users колонка называется id, под правило «где есть
	// user_id» он не попадает, а данные это тоже пользовательские
	where["users"] = `"users".id = $1`

	for table, cond := range where {
		var raw []byte
		q := fmt.Sprintf(
			`SELECT COALESCE(json_agg(row_to_json(x)), '[]'::json) FROM (SELECT * FROM %s WHERE %s) x`,
			quoteIdent(table), cond)
		if err := s.pool.QueryRow(ctx, q, userID).Scan(&raw); err != nil {
			continue // таблица без прав или с экзотическим типом не должна ронять выгрузку
		}
		if len(raw) > 2 { // «[]» не кладём: пустые таблицы только мешают читать
			out[table] = raw
		}
	}
	return out, nil
}

// DeleteAccount удаляет пользователя. Всё остальное уносят каскады (почти все
// таблицы объявлены с ON DELETE CASCADE). Возвращает имена файлов фонов —
// их с диска каскад не уберёт.
func (s *Store) DeleteAccount(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT filename, COALESCE(thumb, '') FROM user_backgrounds WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	var files []string
	for rows.Next() {
		var name, thumb string
		if err := rows.Scan(&name, &thumb); err != nil {
			rows.Close()
			return nil, err
		}
		files = append(files, name)
		if thumb != "" {
			files = append(files, thumb)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		return nil, err
	}
	return files, nil
}

// quoteIdent — имя таблицы или колонки из схемы в кавычках. Значения приходят
// только из information_schema, но подставляются в текст запроса, поэтому
// кавычки обязательны.
func quoteIdent(name string) string {
	return pgx.Identifier{name}.Sanitize()
}
