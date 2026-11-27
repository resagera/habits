-- +goose Up
-- Запись релиза в журнал (notified_at NULL — бот уведомит админа на старте).
-- +goose StatementBegin
INSERT INTO releases (version, released_on, title, public_notes, tech_notes) VALUES (
  '2.129.0',
  '2026-11-13',
  'Пульт ТВ — доступ к приставке для других аккаунтов',
  E'Пульт ТВ. Приставкой теперь можно поделиться: у своей приставки в списке есть кнопка «👥» — там видно, кому открыт пульт, и можно открыть его ещё кому-то по @username или закрыть. Человек получит приглашение в «Контактах» (или сразу, если вы у него в контактах с автоприёмом), и приставка появится у него в списке — подключение одним нажатием, без кода и без «закреплена за другим аккаунтом».\n\nВключённые сейчас приставки стоят в списке первыми с пометкой «включена». Поле для кода больше не заполняется само.',
  E'Миграция 0187: tv_rooms.id (BIGSERIAL, уникальный) для общего шаринга, таблица tv_room_members (room_key → tv_rooms ON UPDATE/DELETE CASCADE, user_id). Вид шаринга tv_room в ShareTitle/ApplyShare/shareKindMsg; ShareTVRoom выдаёт и page_access tv. ClaimTVRoom: при чужом владельце пускает участника (memberTVRoom), иначе 409. ListTVRooms — UNION своих и открытых (shared, owner). DELETE /tv/rooms/{key} участника снимает только его доступ. Новые ручки: POST /tv/rooms/{key}/share, GET /tv/rooms/{key}/members, DELETE /tv/rooms/{key}/members/{uid}. Медиаагент: migrateCompatible — переезд между любыми видеобиблиотеками (video/anime).'
);
-- +goose StatementEnd

-- +goose Down
DELETE FROM releases WHERE version = '2.129.0';
