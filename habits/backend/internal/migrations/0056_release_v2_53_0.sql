-- +goose Up
-- Запись релиза в журнал (notified_at NULL — бот уведомит админа на старте).
-- +goose StatementBegin
INSERT INTO releases (version, released_on, title, public_notes, tech_notes) VALUES (
  '2.53.0',
  '2026-07-29',
  'AI — режим «план → выполнить» и живой лог прогона',
  E'Два больших улучшения страницы AI:\n— галочка «📝 Сначала план»: первый прогон только планирует (без правок файлов), вы читаете план и запускаете выполнение кнопкой «▶️ Выполнить план» — агент помнит план и действует по нему. Безопасный способ работать с полным bypass прав;\n— живой лог: пока задача выполняется, видно, что агент делает прямо сейчас (🔧 команды и правки файлов, 💬 сообщения) — лог обновляется автоматически; после завершения ход выполнения доступен в свёртке.',
  E'Агент v1.4.0: оба раннера переведены на потоковый разбор stdout (StdoutPipe + bufio.Scanner, буфер 8МБ/строку). Claude: --output-format stream-json --verbose (verbose обязателен с -p); события assistant→content[]: tool_use → «🔧 Name: toolSummary(input)» (command/file_path/path/…), text → «💬 …»; финал — событие type=result (те же поля, что в json-режиме). Codex: item.started(command_execution)→«🔧 cmd», item.completed(agent_message)→«💬»+lastMsg; thread/turn как раньше. Строки уходят кадрами {kind:run_log,run_id,output} (best-effort, без ack — финал идёт надёжным run_result); сервер: hub.OnRunLog → AppendAIRunLog (append построчно, кап 256КБ, только активные прогоны). Режим plan: mode=plan в ai_runs (миграция 0055 mode+log), claude --permission-mode plan / codex --sandbox read-only ВМЕСТО bypass-флага; mode проходит через createTask/continueTask/dispatch/redelivery-очередь. UI: чекбокс в форме, бейдж «📝 план», кнопка «▶️ Выполнить план» (continueTask с фиксированным промптом, mode пустой), живой лог <pre> при running + свёртка «Ход выполнения» после. E2E: план не создал файл, «Выполнить план» создал (лог 🔧 Write/Read), живой лог виден посреди прогона (claude), codex-лог построчно. Бинарники ai-v1.4.0 (в latest — все агенты).'
);
-- +goose StatementEnd

-- +goose Down
DELETE FROM releases WHERE version = '2.53.0';
