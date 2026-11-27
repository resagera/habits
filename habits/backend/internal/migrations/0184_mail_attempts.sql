-- +goose Up
-- Журнал отбитых попыток доставки.
--
-- Раньше по порту 25 хранились ТОЛЬКО счётчики (mail_ip_stats): построчный лог
-- всего, что стучится на порт 25, занял бы больше места, чем сама почта. Но по
-- счётчику нельзя ответить на вопрос «кто и на какой адрес пытался писать», а
-- именно это и нужно видеть. Поэтому журнал есть, но ограниченный:
--   * строка пишется на ОСМЫСЛЕННОМ отказе (неизвестный получатель, попытка
--     ретрансляции, запрет по отправителю, лимиты, блокировка);
--   * протокольный мусор одной сессии (битые команды сканера) сворачивается в
--     одну строку на сессию, а не строку на команду;
--   * хранение ограничено по сроку и количеству, чистку делает housekeeping
--     приёмника (см. store.PruneMailAttempts).
CREATE TABLE IF NOT EXISTS mail_attempts (
  id         BIGSERIAL PRIMARY KEY,
  at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  ip         TEXT NOT NULL,
  ptr        TEXT NOT NULL DEFAULT '',
  helo       TEXT NOT NULL DEFAULT '',
  mail_from  TEXT NOT NULL DEFAULT '',
  -- адрес, на который пытались писать: главное, что хотелось видеть
  rcpt       TEXT NOT NULL DEFAULT '',
  -- если такой адрес у нас всё-таки заведён (отказ был по only_from или лимиту)
  address_id BIGINT REFERENCES mail_addresses(id) ON DELETE SET NULL,
  -- на каком шаге разговора отказали: connect | helo | mail | rcpt | data | session
  stage      TEXT NOT NULL DEFAULT 'session',
  reason     TEXT NOT NULL DEFAULT '',
  -- чем ответили: '550 5.1.1' и т.п. Пусто — соединение просто закрыли
  code       TEXT NOT NULL DEFAULT '',
  -- отбили ли этой попыткой весь адрес (блокировка на час и дольше)
  blocked    BOOLEAN NOT NULL DEFAULT false,
  tls        BOOLEAN NOT NULL DEFAULT false,
  -- сколько однотипных отказов свёрнуто в эту строку (протокольный мусор)
  hits       INT NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS mail_attempts_at_idx ON mail_attempts (at DESC);
CREATE INDEX IF NOT EXISTS mail_attempts_ip_idx ON mail_attempts (ip, at DESC);

-- +goose Down
DROP TABLE IF EXISTS mail_attempts;
