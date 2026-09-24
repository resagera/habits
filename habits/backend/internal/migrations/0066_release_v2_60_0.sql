-- +goose Up
-- Запись релиза в журнал (notified_at NULL — бот уведомит админа на старте).
-- +goose StatementBegin
INSERT INTO releases (version, released_on, title, public_notes, tech_notes) VALUES (
  '2.60.0',
  '2026-07-31',
  'Веб-версия и расширение браузера — приложение работает вне Telegram',
  E'Habits теперь открывается в обычном браузере: зайдите на адрес приложения, вставьте токен из Настроек (Telegram → Settings → «Токены доступа») — и пользуйтесь всем как в Telegram. Вёрстка и данные те же. В таком режиме скрыты админ-раздел и «Оформление», а если токен отозвали или он истёк — снова появится экран входа. Дополнительно в репозитории лежит расширение для Chrome и Firefox: попап 400x600 с тем же приложением. Интерфейс расширение грузит с сервера, поэтому переустанавливать его при обновлениях Habits не нужно.',
  E'Фазы 2 и 3 плана; рантайм-детект режима, без отдельных сборок. shared/auth.ts: authMode = tma (есть initData) | token (токен в localStorage) | none; в dev возвращает tma, чтобы npm run dev не упирался в экран входа. client.ts: authHeader отдаёт Bearer, когда initData пуст (+5 строк), а на 401 вызывает onTokenRejected — токен сбрасывается и гейт показывается сам. components/TokenGate.vue: экран ввода с проверкой через GET /me и reload при успехе. App.vue: TokenGate вместо приложения при needsToken. Settings: «Оформление» и блок «Токены доступа» под v-if=!isTokenMode; админка и Релизы скрываются сами, так как /me в токен-режиме отдаёт is_admin=false (Фаза 1). Расширение habits/extension: manifest MV3 (+manifest.firefox.json с browser_specific_settings), popup.html с iframe на прод, иконки 16/48/128 (PIL, фирменный #863bff), README с установкой. Проверка в реальном браузере: экран входа, вход по токену, работа Tracker с данными, скрытие оформления и админки, возврат гейта после отзыва токена в БД, cross-origin iframe попапа (на проде нет CSP/X-Frame-Options — не блокируется).'
);
-- +goose StatementEnd

-- +goose Down
DELETE FROM releases WHERE version = '2.60.0';
