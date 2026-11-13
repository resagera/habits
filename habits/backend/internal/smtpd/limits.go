package smtpd

import (
	"sync"
	"time"
)

// Пределы одной сессии и одного адреса. Порт 25 перебирают круглосуточно,
// поэтому дешёвые ограничения важнее умной фильтрации: бот должен упереться в
// стену до того, как мы потратим на него работу.
const (
	maxSize        = 15 << 20 // письмо целиком
	maxRcpt        = 5        // получателей в одном письме
	maxErrors      = 5        // ошибочных команд до разрыва
	maxCommands    = 50       // команд за сессию
	maxConnPerHour = 60       // подключений с одного IP
	maxMsgPerHour  = 30       // писем с одного IP
	maxConcurrent  = 30       // одновременных сессий всего
	idleTimeout    = 60 * time.Second
	sessionMax     = 5 * time.Minute
	greetDelay     = 1500 * time.Millisecond // пауза перед приветствием
)

// blockFor — на сколько закрываем адрес. Повторные нарушения караются дольше:
// один случайный скан не должен блокировать надолго, а упорный бот должен
// отвалиться.
func blockFor(strikes int) time.Duration {
	switch {
	case strikes <= 1:
		return time.Hour
	case strikes == 2:
		return 6 * time.Hour
	default:
		return 24 * time.Hour
	}
}

type ipState struct {
	conns    []time.Time
	msgs     []time.Time
	strikes  int
	blockTil time.Time
}

// limiter — счётчики по IP в памяти. В базу пишутся только итоги: каждое
// подключение бота не заслуживает похода в Postgres.
type limiter struct {
	mu     sync.Mutex
	ips    map[string]*ipState
	active int
}

func newLimiter() *limiter {
	return &limiter{ips: map[string]*ipState{}}
}

// Load поднимает действующие блокировки из базы при старте: перезапуск
// приложения не должен открывать дверь заново заблокированным ботам.
func (l *limiter) Load(blocked map[string]time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for ip, until := range blocked {
		l.ips[ip] = &ipState{blockTil: until, strikes: 1}
	}
}

type verdict int

const (
	allow verdict = iota
	blocked
	tooMany
	busy
)

// Begin решает, пускать ли подключение, и занимает слот. Освобождать — End.
func (l *limiter) Begin(ip string, now time.Time) verdict {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active >= maxConcurrent {
		return busy
	}
	st := l.ips[ip]
	if st == nil {
		st = &ipState{}
		l.ips[ip] = st
	}
	if now.Before(st.blockTil) {
		return blocked
	}
	st.conns = trim(st.conns, now)
	if len(st.conns) >= maxConnPerHour {
		return tooMany
	}
	st.conns = append(st.conns, now)
	l.active++
	return allow
}

func (l *limiter) End() {
	l.mu.Lock()
	if l.active > 0 {
		l.active--
	}
	l.mu.Unlock()
}

// Message отмечает принятое письмо; false — исчерпан часовой лимит.
func (l *limiter) Message(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.ips[ip]
	if st == nil {
		st = &ipState{}
		l.ips[ip] = st
	}
	st.msgs = trim(st.msgs, now)
	if len(st.msgs) >= maxMsgPerHour {
		return false
	}
	st.msgs = append(st.msgs, now)
	return true
}

// Unblock снимает блокировку и обнуляет счётчики: без этого «разблокировать»
// в интерфейсе чистило бы только базу, а приёмник продолжал бы отвечать 421 из
// памяти до перезапуска.
func (l *limiter) Unblock(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if st := l.ips[ip]; st != nil {
		st.blockTil = time.Time{}
		st.strikes = 0
		st.conns = nil
		st.msgs = nil
	}
}

// Block закрывает адрес и возвращает, до какого момента.
func (l *limiter) Block(ip string, now time.Time) time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.ips[ip]
	if st == nil {
		st = &ipState{}
		l.ips[ip] = st
	}
	st.strikes++
	st.blockTil = now.Add(blockFor(st.strikes))
	return st.blockTil
}

// Cleanup выбрасывает адреса, о которых нечего помнить: иначе карта растёт
// вместе с интернетом.
func (l *limiter) Cleanup(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for ip, st := range l.ips {
		st.conns = trim(st.conns, now)
		st.msgs = trim(st.msgs, now)
		if len(st.conns) == 0 && len(st.msgs) == 0 && now.After(st.blockTil) {
			delete(l.ips, ip)
		}
	}
}

func trim(list []time.Time, now time.Time) []time.Time {
	cut := now.Add(-time.Hour)
	i := 0
	for ; i < len(list); i++ {
		if list[i].After(cut) {
			break
		}
	}
	return list[i:]
}
