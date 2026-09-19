package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
	"streaks-backend/internal/terminal"
	"streaks-backend/internal/tvrelay"
)

// Страница «Пульт ТВ»: телефон управляет плеером на ТВ-приставке.
//
// Прод — только шина. Сообщения пересылаются как есть: он не знает ни про
// папки, ни про то, что играет. Всё это приставка берёт у своего агента по
// локальной сети напрямую, поэтому ни медиа, ни пути файлов через сервер не
// идут — а пульт работает и с мобильного интернета.

type tvHandlers struct {
	store   *store.Store
	hub     *tvrelay.Hub
	tickets *terminal.Tickets
	bot     *notify.Bot
	limit   *tvNotifyLimiter
	cast    *tvCast
}

var tvUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Приставку открывают со страницы агента (http://192.168.0.226/tv/), а
	// пульт — из мини-аппа. Origin у них разный и заранее неизвестен, поэтому
	// пускаем любой: доступ решают ключ комнаты и её владелец, а не Origin.
	CheckOrigin: func(r *http.Request) bool { return true },
}

const (
	tvMaxMessage = 256 * 1024 // листинг папки на 500 файлов сюда влезает с запасом
	tvPongWait   = 70 * time.Second
	tvPingPeriod = 30 * time.Second
)

// GET /api/v1/tv/rooms — мои приставки.
func (h *tvHandlers) rooms(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	list, err := h.store.ListTVRooms(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if list == nil {
		list = []store.TVRoom{}
	}
	type roomOut struct {
		store.TVRoom
		Online bool `json:"online"`
	}
	out := make([]roomOut, 0, len(list))
	for _, room := range list {
		tv, _ := h.hub.Present(room.Key)
		out = append(out, roomOut{TVRoom: room, Online: tv > 0})
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": out})
}

// POST /api/v1/tv/attach {key,label} — закрепить комнату за собой и получить
// одноразовый пропуск на веб-сокет. Пропуск нужен потому, что заголовок
// Authorization в WebSocket из браузера не поставить.
func (h *tvHandlers) attach(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Code  string `json:"code"`
		Key   string `json:"key"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	// Код с экрана телевизора — основной путь: адрес компьютера пользователь
	// знать не обязан, а по DHCP он ещё и меняется. Ключ комнаты остаётся для
	// тех, кто задал его руками.
	key := ""
	switch code := normalizeTVCode(req.Code); {
	case code != "":
		key = h.hub.Resolve(code)
		if key == "" {
			writeError(w, http.StatusNotFound, "not_found",
				"код не подошёл — проверьте, что плеер открыт на приставке")
			return
		}
	case strings.TrimSpace(req.Code) != "":
		// что-то ввели, но на код не похоже: не молчать про длину, иначе
		// человек видит «введите код», хотя он его как раз ввёл
		badRequest(w, "в коде 8 знаков — проверьте, что ввели его целиком")
		return
	default:
		key = store.NormalizeTVRoom(req.Key)
	}
	if key == "" {
		badRequest(w, "введите код с экрана приставки")
		return
	}
	room, err := h.store.ClaimTVRoom(r.Context(), user.ID, key, req.Label)
	switch {
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "эта приставка уже закреплена за другим аккаунтом")
		return
	case err != nil:
		internalError(w)
		return
	}
	tv, _ := h.hub.Present(key)
	writeJSON(w, http.StatusOK, map[string]any{
		"room": room, "online": tv > 0, "ticket": h.tickets.Issue(tvTicketID(key)),
	})
}

func (h *tvHandlers) deleteRoom(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	err := h.store.DeleteTVRoom(r.Context(), user.ID, store.NormalizeTVRoom(r.PathValue("key")))
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "приставка не найдена")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// Пропуска в terminal.Tickets хранят int64, а комната — строка. Держим рядом
// с пропуском саму строку: свой тип пропусков ради этого заводить не стоит.
// Замок обязателен — пропуска выдаются из разных запросов одновременно.
var tvRoomsByTicket = struct {
	mu sync.Mutex
	m  map[int64]string
	n  int64
}{m: map[int64]string{}}

func tvTicketID(key string) int64 {
	tvRoomsByTicket.mu.Lock()
	defer tvRoomsByTicket.mu.Unlock()
	tvRoomsByTicket.n++
	tvRoomsByTicket.m[tvRoomsByTicket.n] = key
	return tvRoomsByTicket.n
}

func tvTicketRoom(id int64) string {
	tvRoomsByTicket.mu.Lock()
	defer tvRoomsByTicket.mu.Unlock()
	key := tvRoomsByTicket.m[id]
	delete(tvRoomsByTicket.m, id)
	return key
}

// GET /api/v1/tv/remote/{ticket} — веб-сокет пульта.
func (h *tvHandlers) remoteWS(w http.ResponseWriter, r *http.Request) {
	id, ok := h.tickets.Redeem(r.PathValue("ticket"))
	if !ok {
		http.Error(w, "ticket not found", http.StatusUnauthorized)
		return
	}
	key := tvTicketRoom(id)
	if key == "" {
		http.Error(w, "ticket not found", http.StatusUnauthorized)
		return
	}
	h.serve(w, r, key, tvrelay.RoleRemote)
}

// GET /api/v1/tv/socket?room=… — веб-сокет приставки.
//
// Приставка не авторизована: её открывают на телевизоре, где никакого
// Telegram нет. Защита — в том, что комната закреплена за аккаунтом при первом
// подключении пульта, и чужую занять уже нельзя.
func (h *tvHandlers) tvWS(w http.ResponseWriter, r *http.Request) {
	key := store.NormalizeTVRoom(r.URL.Query().Get("room"))
	if key == "" {
		http.Error(w, "missing room", http.StatusBadRequest)
		return
	}
	h.serve(w, r, key, tvrelay.RoleTV)
}

// POST /api/v1/tv/notify {room, text} — сообщение от медиаагента владельцу
// комнаты в Telegram: «ушло в корзину», «стёрто». Агент шлёт его сам, даже
// когда плеер на телевизоре закрыт.
//
// Авторизации нет по той же причине, что и у веб-сокета приставки: агент
// живёт на домашнем компьютере без Telegram. Пропуск — ключ комнаты, который
// знает только домашняя сеть; писать можно лишь владельцу этой комнаты, с
// пометкой «📺» и не чаще tvNotifyPerHour раз в час — заспамить бота через
// угаданный ключ не выйдет.
func (h *tvHandlers) notify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Room string `json:"room"`
		Text string `json:"text"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	key := store.NormalizeTVRoom(req.Room)
	text := strings.TrimSpace(req.Text)
	if key == "" || text == "" {
		badRequest(w, "нужны room и text")
		return
	}
	if runes := []rune(text); len(runes) > tvNotifyMaxRunes {
		text = string(runes[:tvNotifyMaxRunes]) + "…"
	}
	owner, err := h.store.TVRoomOwner(r.Context(), key)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// пульт к этой приставке ещё не подключали — писать некому
		writeError(w, http.StatusNotFound, "not_found", "комната не закреплена за аккаунтом")
		return
	case err != nil:
		internalError(w)
		return
	}
	if !h.limit.allow(key, time.Now()) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "слишком много уведомлений, попробуйте позже")
		return
	}
	if err := h.bot.SendMessage(r.Context(), owner, "📺 "+text); err != nil {
		writeError(w, http.StatusBadGateway, "telegram", "Telegram не принял сообщение")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

const (
	tvNotifyPerHour  = 30
	tvNotifyMaxRunes = 1500
)

// tvNotifyLimiter — не больше tvNotifyPerHour сообщений на комнату за час
// (скользящее окно). В памяти: после перезапуска прода счёт начинается заново,
// и это не страшно.
type tvNotifyLimiter struct {
	mu   sync.Mutex
	sent map[string][]time.Time
}

func newTVNotifyLimiter() *tvNotifyLimiter {
	return &tvNotifyLimiter{sent: map[string][]time.Time{}}
}

func (l *tvNotifyLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-time.Hour)
	recent := l.sent[key][:0]
	for _, t := range l.sent[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) >= tvNotifyPerHour {
		l.sent[key] = recent
		return false
	}
	l.sent[key] = append(recent, now)
	return true
}

// randomBytes — источник случайности для кодов. Отдельной функцией, чтобы
// шина не зависела от crypto/rand и осталась проверяемой.
func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return make([]byte, n)
	}
	return b
}

// Кириллические двойники латинских букв. На телефоне с русской раскладкой
// код «K9KH STWX» набирается кириллицей и выглядит точно так же, а по кодам
// символов это другие буквы — такой код отвергался как «не код».
var tvCodeLookalikes = map[rune]rune{
	'А': 'A', 'В': 'B', 'Е': 'E', 'К': 'K', 'М': 'M', 'Н': 'H',
	'О': 'O', 'Р': 'P', 'С': 'C', 'Т': 'T', 'У': 'Y', 'Х': 'X',
}

// normalizeTVCode прощает пользователю пробелы, дефисы, регистр и русскую
// раскладку: код набирают руками, глядя на экран телевизора.
func normalizeTVCode(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(s)) {
		if latin, ok := tvCodeLookalikes[r]; ok {
			r = latin
		}
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() != 8 {
		return "" // не код — значит, введён ключ комнаты
	}
	return b.String()
}

func (h *tvHandlers) serve(w http.ResponseWriter, r *http.Request, key string, role tvrelay.Role) {
	ws, err := tvUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c, leave := h.hub.Join(key, role, ws)
	defer func() {
		leave()
		_ = ws.Close()
		h.announce(key)
	}()
	if role == tvrelay.RoleTV {
		// код приставка показывает на экране: пользователю не нужно знать ни
		// адрес компьютера, ни тем более адрес самой приставки
		code := h.hub.Code(key, randomBytes)
		msg, _ := json.Marshal(map[string]any{"t": "code", "code": code})
		c.Send(msg)
	}
	h.announce(key)

	ws.SetReadLimit(tvMaxMessage)
	_ = ws.SetReadDeadline(time.Now().Add(tvPongWait))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(tvPongWait))
	})
	// Мобильные сети рвут молчащие соединения за минуту-другую, а пульт может
	// висеть открытым весь фильм — держим его пингом.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		t := time.NewTicker(tvPingPeriod)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				c.Ping()
			}
		}
	}()

	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if len(data) == 0 {
			continue
		}
		h.hub.Broadcast(key, c, data)
	}
}

// announce рассылает присутствие: пульту важно знать, что приставка на связи,
// иначе кнопки нажимаются в пустоту.
func (h *tvHandlers) announce(key string) {
	tv, remotes := h.hub.Present(key)
	msg, _ := json.Marshal(map[string]any{
		"t": "presence", "tv": tv, "remotes": remotes,
	})
	h.hub.BroadcastAll(key, msg)
}
