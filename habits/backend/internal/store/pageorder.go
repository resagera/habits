package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Порядок страниц в меню и на плитках главной: закреплённые сверху, дальше
// те, где у пользователя есть данные, внизу пустые. Здесь — обе половины
// этого порядка: признак «страница заполнена» (PageUsage) и ручное
// закрепление (MenuPinned).

// pageDataSources — где искать признак «на странице есть данные пользователя».
// Берётся ГЛАВНАЯ таблица страницы, а не все её таблицы: вопрос стоит «начинал
// ли пользователь этой страницей пользоваться», а не «сколько там строк».
// Несколько таблиц через ИЛИ — там, где страница состоит из независимых
// разделов (в Food можно вести только рецепты и ни разу не открыть дневник).
//
// Страницы без источника (settings, help, админские) всегда считаются пустыми
// и уезжают вниз — своих данных у них нет.
var pageDataSources = []struct {
	Code   string
	Tables []string
}{
	{"tracker", []string{"tracker_categories"}},
	{"checker", []string{"checker_groups", "checker_templates"}},
	{"tasks", []string{"tasks", "task_projects"}},
	{"diary", []string{"diary_entries"}},
	{"metrics", []string{"metrics_categories"}},
	{"passwords", []string{"password_vaults"}},
	{"reminders", []string{"reminders"}},
	{"converter", []string{"user_currencies"}},
	{"links", []string{"links"}},
	{"articles", []string{"articles"}},
	{"servers", []string{"servers"}},
	{"files", []string{"file_machines"}},
	{"terminal", []string{"terminal_machines"}},
	{"tv", []string{"tv_rooms"}},
	{"contacts", []string{"contacts"}},
	{"projects", []string{"projects"}},
	{"food", []string{"food_meals", "food_recipes", "food_products", "food_plans"}},
	{"automation", []string{"automations", "automation_agents"}},
	{"ai", []string{"ai_tasks", "ai_machines"}},
	{"calendar", []string{"calendar_prefs"}},
	{"tests", []string{"test_sessions", "test_progress"}},
	{"finance", []string{"finance_transactions", "finance_accounts"}},
	{"mail", []string{"mail_addresses"}},
}

// usageQuery собирается один раз: имена таблиц — константы из кода, не ввод.
var usageQuery = buildUsageQuery()

func buildUsageQuery() string {
	var b strings.Builder
	b.WriteString("SELECT ")
	for i, p := range pageDataSources {
		if i > 0 {
			b.WriteString(", ")
		}
		for j, table := range p.Tables {
			if j > 0 {
				b.WriteString(" OR ")
			}
			b.WriteString("EXISTS(SELECT 1 FROM " + table + " WHERE user_id = $1)")
		}
	}
	return b.String()
}

// PageUsage — карта «на странице есть данные пользователя». Один запрос:
// EXISTS по индексу user_id останавливается на первой строке.
func (s *Store) PageUsage(ctx context.Context, userID int64) (map[string]bool, error) {
	flags := make([]bool, len(pageDataSources))
	dest := make([]any, len(pageDataSources))
	for i := range flags {
		dest[i] = &flags[i]
	}
	if err := s.pool.QueryRow(ctx, usageQuery, userID).Scan(dest...); err != nil {
		return nil, err
	}
	usage := make(map[string]bool, len(pageDataSources))
	for i, p := range pageDataSources {
		usage[p.Code] = flags[i]
	}
	return usage, nil
}

// MenuPrefs — ручная часть порядка меню: что закреплено вверху и что
// пользователь убрал с глаз. Скрытие — не доступ: страница остаётся рабочей
// по прямой ссылке, из меню и с плиток она просто исчезает.
type MenuPrefs struct {
	Pinned []string `json:"pinned"`
	Hidden []string `json:"hidden"`
}

// MenuPrefs читает обе настройки одним запросом — их всегда нужно две сразу.
func (s *Store) MenuPrefs(ctx context.Context, userID int64) (MenuPrefs, error) {
	prefs := MenuPrefs{Pinned: []string{}, Hidden: []string{}}
	var pinned, hidden []byte
	err := s.pool.QueryRow(ctx, `
		SELECT menu_pinned_pages, hidden_pages FROM user_settings WHERE user_id = $1`,
		userID).Scan(&pinned, &hidden)
	if errors.Is(err, pgx.ErrNoRows) {
		return prefs, nil // строки настроек ещё нет — умолчания
	}
	if err != nil {
		return prefs, err
	}
	prefs.Pinned = decodeCodes(pinned)
	prefs.Hidden = decodeCodes(hidden)
	return prefs, nil
}

func decodeCodes(raw []byte) []string {
	var codes []string
	if err := json.Unmarshal(raw, &codes); err != nil || codes == nil {
		return []string{}
	}
	return codes
}

// setMenuColumn — общая запись для обеих настроек. Имя колонки приходит из
// кода (две константы ниже), не из запроса.
func (s *Store) setMenuColumn(ctx context.Context, userID int64, column string, pages []string) error {
	if pages == nil {
		pages = []string{}
	}
	raw, err := json.Marshal(pages)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO user_settings (user_id, `+column+`) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE
		SET `+column+` = EXCLUDED.`+column+`, updated_at = now()`,
		userID, raw)
	return err
}

func (s *Store) SetMenuPinned(ctx context.Context, userID int64, pages []string) error {
	return s.setMenuColumn(ctx, userID, "menu_pinned_pages", pages)
}

func (s *Store) SetMenuHidden(ctx context.Context, userID int64, pages []string) error {
	return s.setMenuColumn(ctx, userID, "hidden_pages", pages)
}
