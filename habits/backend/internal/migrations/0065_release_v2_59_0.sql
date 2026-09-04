-- +goose Up
-- Запись релиза в журнал (notified_at NULL — бот уведомит админа на старте).
-- +goose StatementBegin
INSERT INTO releases (version, released_on, title, public_notes, tech_notes) VALUES (
  '2.59.0',
  '2026-07-31',
  'Токены доступа — вход в приложение вне Telegram (фундамент веб-версии)',
  E'В Настройках появился раздел «Токены доступа». Токен позволит открывать приложение вне Telegram — в браузере или расширении (сами веб-версия и расширение будут следующим шагом). Токен показывается один раз при создании, в списке видны только первые символы, время последнего использования и устройство; любой токен можно отозвать. Можно задать срок действия (30/90/365 дней или бессрочно), одновременно — до 10 токенов. Безопасность: по токену недоступны админ-раздел и выпуск новых токенов — это только из Telegram.',
  E'Фаза 1 плана веб-версии/расширения; та же таблица пригодится MCP-серверу. Миграция 0064: access_tokens (user_id, token_hash UNIQUE, prefix, name, expires_at, last_used_at, last_device, revoked_at) — сам токен НЕ хранится, только sha256; формат hbt_ + 48 hex. auth.Middleware: ветка Authorization Bearer рядом с tma; authenticateToken ищет владельца по хэшу (активный, не истёкший), берёт username/first_name из БД (иначе TouchUser затёр бы их пустыми), пишет last_used_at не чаще раза в 5 мин через sync.Map. TgUser получил TokenSession/TokenID. Границы: isTokenForbiddenPath закрывает /api/v1/admin/ и /api/v1/settings/tokens прямо в Wrap — утёкший токен не даёт ни эскалации, ни самопродления. Новый IsAdminSession(ctx) = не токен-сессия И админ по ADMIN_IDS — применён в /me (is_admin), adminOnly и releases.list (tech_notes); при этом IsAdmin(id) оставлен в access.go и карточке пользователя: видимость СВОИХ personal-страниц от способа входа не зависит. API: GET/POST /settings/tokens, DELETE /settings/tokens/{id} (лимит 10, ErrTooManyTokens - 409). UI: сворачиваемый блок AccessTokens в Settings с одноразовым показом токена. Тесты: unit на гарды путей и IsAdminSession + E2E 11 сценариев (выпуск, вход, данные, 403 на админку и выпуск, скрытие tech_notes, регрессия админа из Telegram, last_used, мусорный/отозванный/истёкший токены - 401).'
);
-- +goose StatementEnd

-- +goose Down
DELETE FROM releases WHERE version = '2.59.0';
