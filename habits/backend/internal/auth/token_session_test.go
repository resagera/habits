package auth

import (
	"context"
	"testing"
)

func TestIsTokenForbiddenPath(t *testing.T) {
	forbidden := []string{
		"/api/v1/admin/users",
		"/api/v1/admin/pages/tracker",
		"/api/v1/settings/tokens",
		"/api/v1/settings/tokens/5",
	}
	for _, p := range forbidden {
		if !isTokenForbiddenPath(p) {
			t.Errorf("%s должен быть закрыт для токен-сессии", p)
		}
	}
	allowed := []string{
		"/api/v1/tracker/categories",
		"/api/v1/settings/background", // прочие настройки токену доступны
		"/api/v1/me",
		"/api/v1/releases",
	}
	for _, p := range allowed {
		if isTokenForbiddenPath(p) {
			t.Errorf("%s не должен быть закрыт", p)
		}
	}
}

func TestIsAdminSession(t *testing.T) {
	m := &Middleware{AdminIDs: map[int64]bool{7: true}}

	tma := context.WithValue(context.Background(), ctxKey{}, TgUser{ID: 7})
	if !m.IsAdminSession(tma) {
		t.Fatal("админ из Telegram должен иметь админ-полномочия")
	}

	// тот же админ, но вошёл по токену — полномочий нет
	byToken := context.WithValue(context.Background(), ctxKey{}, TgUser{ID: 7, TokenSession: true})
	if m.IsAdminSession(byToken) {
		t.Fatal("токен-сессия админа не должна давать админ-полномочий")
	}
	// но «человек-админ» он по-прежнему: свои personal-страницы видит
	if !m.IsAdmin(7) {
		t.Fatal("IsAdmin(id) не зависит от способа входа")
	}

	notAdmin := context.WithValue(context.Background(), ctxKey{}, TgUser{ID: 9})
	if m.IsAdminSession(notAdmin) {
		t.Fatal("не-админ не должен получать полномочия")
	}
}
