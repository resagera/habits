-- +goose Up
-- Запись релиза в журнал (notified_at NULL — бот уведомит админа на старте).
-- +goose StatementBegin
INSERT INTO releases (version, released_on, title, public_notes, tech_notes) VALUES (
  '2.61.0',
  '2026-07-31',
  'Веб и расширение: тема как в Telegram, своё оформление и ссылки-инструкции',
  E'Исправлено: в браузере и расширении включалась светлая тема, даже если в Telegram выбрана тёмная — тема хранилась только в самом браузере, а у расширения хранилище своё. Теперь тема живёт на сервере и подхватывается везде одинаково.\n\nПри этом для браузера и расширения появилось собственное оформление (Настройки → «Оформление»): можно выбрать тему — «Как в Telegram», по системе, светлую или тёмную — и убрать фоновую картинку. На мини-приложение в Telegram эти настройки не влияют.\n\nВ разделе «Токены доступа» появился блок «Где использовать токен»: ссылка на веб-версию (копируется по клику), ссылка на расширение и короткие инструкции по установке для Chrome и Firefox. А на экране входа в браузере — кнопка «Открыть приложение в Telegram».',
  E'Миграция 0067: user_settings + theme (auto|light|dark, основная — из Telegram), web_theme ('''' = наследовать) и web_bg_off — переопределения только для токен-сессий. Причина бага: ThemePreference жил лишь в localStorage, а у extension-origin хранилище партиционировано, поэтому всегда стартовал auto и брал prefers-color-scheme браузера. API: GET/PUT /settings/appearance; PUT выбирает колонки ПО ВИДУ СЕССИИ (tma - theme, token - web_theme/web_bg_off), поэтому браузер физически не может изменить тему мини-приложения. shared/appearance.ts: loadAppearance применяет effectiveTheme (web_theme при isTokenMode, иначе theme) и кэширует в localStorage — первый кадр без мигания; bgHidden() гасит фон в applyBackground. Settings: onTheme пишет на сервер, в токен-режиме отдельный блок «Оформление». AccessTokens: details «Где использовать токен» — webUrl из location.origin+BASE_URL с копированием, ссылка на habits/extension и инструкции Chrome/Firefox. TokenGate: кнопка на t.me/resagerHelperBot/res_vault_flow. E2E: Telegram ставит dark - веб наследует dark - веб ставит light+bg_off - у Telegram по-прежнему dark - сброс web_theme возвращает наследование; в браузере визуально подтверждена тёмная тема и новый блок настроек.'
);
-- +goose StatementEnd

-- +goose Down
DELETE FROM releases WHERE version = '2.61.0';
