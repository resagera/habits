package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// GameScore — рекорд по одной игре.
//
// Что считается лучшим, зависит от игры: в 2048 это больше очков, в пятнашках
// и «найди пару» — меньше секунд. Поэтому направление живёт в коде (gameRules
// в httpapi), а не в колонке: добавить игру должно быть одной строкой.
type GameScore struct {
	Game   string          `json:"game"`
	Best   int32           `json:"best"`
	Detail json.RawMessage `json:"detail,omitempty"`
	Played int32           `json:"played"`
}

func (s *Store) GameScores(ctx context.Context, userID int64) ([]GameScore, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT game, best, detail, played FROM game_scores
		WHERE user_id = $1 ORDER BY game`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[GameScore])
}

// SaveGameResult всегда считает партию сыгранной, а рекорд обновляет только
// если он лучше. Сравнение делает сам запрос: два устройства с результатами
// подряд не должны затирать друг друга чтением-записью.
func (s *Store) SaveGameResult(ctx context.Context, userID int64, game string, value int32, detail []byte, higher bool) (GameScore, error) {
	better := "LEAST(game_scores.best, EXCLUDED.best)"
	if higher {
		better = "GREATEST(game_scores.best, EXCLUDED.best)"
	}
	// detail принадлежит рекорду, поэтому меняется вместе с ним
	rows, err := s.pool.Query(ctx, `
		INSERT INTO game_scores (user_id, game, best, detail, played)
		VALUES ($1, $2, $3, $4, 1)
		ON CONFLICT (user_id, game) DO UPDATE SET
			best = `+better+`,
			detail = CASE WHEN `+better+` = EXCLUDED.best
			              THEN EXCLUDED.detail ELSE game_scores.detail END,
			played = game_scores.played + 1,
			updated_at = now()
		RETURNING game, best, detail, played`,
		userID, game, value, detail)
	if err != nil {
		return GameScore{}, err
	}
	return pgx.CollectOneRow(rows, pgx.RowToStructByPos[GameScore])
}

// GameBackgrounds — id картинок по игре и назначению (slot): фон всей игры,
// рубашка карт и что появится дальше.
func (s *Store) GameBackgrounds(ctx context.Context, userID int64) (map[string]map[string][]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT game, slot, image_id FROM game_backgrounds
		WHERE user_id = $1 ORDER BY image_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string][]int64{}
	for rows.Next() {
		var game, slot string
		var id int64
		if err := rows.Scan(&game, &slot, &id); err != nil {
			return nil, err
		}
		if out[game] == nil {
			out[game] = map[string][]int64{}
		}
		out[game][slot] = append(out[game][slot], id)
	}
	return out, rows.Err()
}

// SetGameBackgrounds заменяет набор одного назначения целиком. Чужие id просто
// не вставятся: условие берёт только картинки этого пользователя.
func (s *Store) SetGameBackgrounds(ctx context.Context, userID int64, game, slot string, ids []int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`DELETE FROM game_backgrounds WHERE user_id = $1 AND game = $2 AND slot = $3`,
		userID, game, slot); err != nil {
		return err
	}
	if len(ids) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO game_backgrounds (user_id, game, slot, image_id)
			SELECT $1, $2, $3, id FROM user_backgrounds
			WHERE user_id = $1 AND id = ANY($4)
			ON CONFLICT DO NOTHING`, userID, game, slot, ids); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// GameImageURLs — имена файлов выбранных картинок, чтобы отдать клиенту
// готовые адреса вместе с рекордами (иначе страница игр тянула бы ещё и
// полный список фонов ради трёх превью).
func (s *Store) GameImageFiles(ctx context.Context, userID int64, ids []int64) (map[int64][2]string, error) {
	if len(ids) == 0 {
		return map[int64][2]string{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, filename, COALESCE(thumb, '') FROM user_backgrounds
		WHERE user_id = $1 AND id = ANY($2)`, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][2]string{}
	for rows.Next() {
		var id int64
		var file, thumb string
		if err := rows.Scan(&id, &file, &thumb); err != nil {
			return nil, err
		}
		out[id] = [2]string{file, thumb}
	}
	return out, rows.Err()
}

// --- текстуры стен (пополняет админ, видят все) ---

type GameTexture struct {
	ID       int64  `json:"id"`
	Game     string `json:"game"`
	Title    string `json:"title"`
	Filename string `json:"-"`
	URL      string `json:"url"`
}

func (s *Store) ListGameTextures(ctx context.Context, game string) ([]GameTexture, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, game, title, filename FROM game_textures
		WHERE game = $1 ORDER BY id`, game)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GameTexture{}
	for rows.Next() {
		var t GameTexture
		if err := rows.Scan(&t.ID, &t.Game, &t.Title, &t.Filename); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) AddGameTexture(ctx context.Context, adminID int64, game, title, filename string) (GameTexture, error) {
	t := GameTexture{Game: game, Title: title, Filename: filename}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO game_textures (game, title, filename, uploaded_by)
		VALUES ($1, $2, $3, $4) RETURNING id`, game, title, filename, adminID).Scan(&t.ID)
	return t, err
}

// DeleteGameTexture возвращает имя файла, чтобы удалить его с диска.
func (s *Store) DeleteGameTexture(ctx context.Context, id int64) (string, error) {
	var filename string
	err := s.pool.QueryRow(ctx,
		`DELETE FROM game_textures WHERE id = $1 RETURNING filename`, id).Scan(&filename)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return filename, err
}
