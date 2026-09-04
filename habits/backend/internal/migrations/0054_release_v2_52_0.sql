-- +goose Up
-- Запись релиза в журнал (notified_at NULL — бот уведомит админа на старте).
-- +goose StatementBegin
INSERT INTO releases (version, released_on, title, public_notes, tech_notes) VALUES (
  '2.52.0',
  '2026-07-29',
  'AI — уведомления бота, остановка прогона, markdown, очередь офлайн, расписания',
  E'Большое обновление страницы AI:\n— бот присылает уведомление о завершении задачи (✅/❌ с началом результата) — можно ставить задачу и закрывать приложение;\n— кнопка «⏹ Остановить» у выполняющегося прогона;\n— результат теперь красиво отрисовывается как markdown; кнопки «Копировать» и «В статью» (сохранить отчёт в Articles);\n— задачу можно ставить даже когда агент офлайн: она ждёт в очереди и запустится при подключении машины;\n— расписания: периодические задачи (ежедневно/еженедельно/каждые N часов, своя таймзона) — «каждое утро прогони тесты и пришли отчёт»; каждый запуск — новая задача 🕒, результат придёт ботом;\n— бейдж занятости машины (⏳ активные прогоны).',
  E'Агент v1.3.0: op cancel (kill контекста процесса; «остановлено пользователем» и в очереди семафора, и в рантайме), accepted-map — дедуп повторной доставки run-кадров (сервер дошлёт очередь на reconnect, а процесс агента мог не перезапускаться). Хаб: OnConnect → deliverQueued (redelivery queued-прогонов при подключении агента); FinishAIRun возвращает updated (первая доставка) — notifyRunFinished без дублей. dispatchRun больше не 503: ответ queued_offline, FailStaleAIRuns теперь только running>2ч (queued живёт до подключения/отмены). POST /ai/runs/{id}/cancel: queued отменяется в БД (CancelQueuedAIRun) + cancel-кадр агенту на случай гонки; running — cancel-кадр. Расписания: ai_schedules (0053), NextAIScheduleRun (daily/weekly/hours + tz_off, unit-тест), воркер internal/aischeduler (тик 60с; Advance ДО dispatch — сбой не зацикливает; офлайн → очередь), aiHub теперь создаётся в main.go (нужен роутеру и воркеру). Busy: ActiveAIRunCounts (queued+running) в GET /ai/machines. Фронт: MarkdownView для output, копия/createArticle, секция расписаний (toggle/edit/delete, время → at_minute, tz_off из браузера), badge ⏳, тосты про очередь. E2E: cancel (204→error «остановлено», ❌-notify), очередь (queued_offline→«queued delivered»→done), расписание (fired в срок, advance, ✅-notify), busy=1.'
);
-- +goose StatementEnd

-- +goose Down
DELETE FROM releases WHERE version = '2.52.0';
