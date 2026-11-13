package connector

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

// Engine прогоняет события источников через правила пользователя.
//
// Ошибки правила никогда не ломают само событие: письмо уже сохранено, и
// неудачная отметка в трекере не повод терять письмо. Всё, что не получилось,
// попадает в журнал срабатываний и видно на странице.
type Engine struct {
	Store  *store.Store
	Bot    *notify.Bot
	Logger *slog.Logger
}

// runTimeout — сколько даём правилам на одно событие. Приёмник почты ждёт
// результат в своей горутине, и подвисшая отметка задержала бы ответ 250 OK.
const runTimeout = 30 * time.Second

// OnMail — письмо принято и сохранено. Вызывается приёмником почты и
// обработчиком «разобрать ещё раз».
func (e *Engine) OnMail(ctx context.Context, userID, messageID int64) {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	rules, err := e.Store.ConnectorRulesForSource(ctx, userID, SourceMail)
	if err != nil {
		e.Logger.Error("connector: правила источника", "source", SourceMail, "error", err)
		return
	}
	if len(rules) == 0 {
		return
	}
	ev, err := e.MailEvent(ctx, userID, messageID)
	if err != nil {
		e.Logger.Error("connector: событие письма", "message_id", messageID, "error", err)
		return
	}
	for _, r := range rules {
		if _, err := e.Run(ctx, r, ev, false); err != nil {
			e.Logger.Error("connector: правило", "rule", r.ID, "error", err)
		}
	}
}

// MailEvent собирает событие из сохранённого письма.
func (e *Engine) MailEvent(ctx context.Context, userID, messageID int64) (Event, error) {
	m, err := e.Store.MailMessageByID(ctx, userID, messageID)
	if err != nil {
		return Event{}, err
	}
	atts, err := e.Store.ListMailAttachments(ctx, messageID)
	if err != nil {
		return Event{}, err
	}
	at := m.ReceivedAt
	// дата из письма точнее времени приёма: пересылка и очереди сдвигают приём
	if m.SentAt != nil && !m.SentAt.IsZero() {
		at = *m.SentAt
	}
	from := m.FromAddr
	if from == "" {
		from = m.MailFrom
	}
	label := strings.TrimSpace(m.Subject)
	if label == "" {
		label = "(без темы)"
	}
	if from != "" {
		label = label + " — " + from
	}
	return Event{
		Source: SourceMail,
		ID:     m.ID,
		Label:  label,
		At:     at,
		Text: map[string]string{
			"from": from, "from_name": m.FromName, "envelope": m.MailFrom,
			"subject": m.Subject, "text": m.TextBody, "rcpt": m.Rcpt,
			"ip": m.RemoteIP, "helo": m.Helo, "spf": m.SPF,
		},
		Flags: map[string]bool{
			"spam": m.IsSpam, "attachments": len(atts) > 0, "tls": m.TLS,
		},
	}, nil
}

// Result — что получилось у правила на этом событии.
type Result struct {
	Matched bool     `json:"matched"`
	Trace   Trace    `json:"trace"`
	Done    []string `json:"done"`    // человеческое описание сделанного
	Skipped bool     `json:"skipped"` // событие уже обрабатывалось этим правилом
	Error   string   `json:"error,omitempty"`
}

// Run проверяет правило на событии и, если оно подошло, выполняет результаты.
//
// force — ручное применение: человек просит повторить, и защита от повторного
// срабатывания уступает. Автоматический проход всегда идёт без него.
func (e *Engine) Run(ctx context.Context, r store.ConnectorRule, ev Event, force bool) (Result, error) {
	var res Result
	ok, trace := Eval(r.Match, ev)
	res.Matched, res.Trace = ok, trace
	if !ok {
		// «не подошло» в журнал не пишем: он заполнился бы мусором, а на
		// вопрос «почему не сработало» отвечает кнопка «Проверить»
		return res, nil
	}

	var eventID *int64
	if ev.ID > 0 {
		eventID = &ev.ID
	}
	runID, claimed, err := e.Store.ClaimConnectorRun(ctx, r.ID, r.UserID, ev.Source,
		eventID, ev.Label, force)
	if err != nil {
		return res, err
	}
	if !claimed {
		res.Skipped = true
		return res, nil
	}

	var problems []string
	for _, a := range r.Actions {
		done, err := e.act(ctx, r, a, ev)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		res.Done = append(res.Done, done)
	}
	status, detail := "done", strings.Join(res.Done, "; ")
	if len(problems) > 0 {
		status = "error"
		res.Error = strings.Join(problems, "; ")
		detail = strings.TrimSpace(detail + " | не вышло: " + res.Error)
	}
	if err := e.Store.FinishConnectorRun(ctx, runID, r.ID, status, detail); err != nil {
		return res, err
	}
	return res, nil
}

// act выполняет один результат и возвращает описание для журнала.
func (e *Engine) act(ctx context.Context, r store.ConnectorRule,
	a store.ConnectorAction, ev Event) (string, error) {
	switch a.Type {
	case ActionTrackerMark:
		return e.mark(ctx, r, a, ev)
	case ActionNotify:
		text := expand(a.Text, ev)
		if err := e.Bot.SendKind(ctx, r.UserID, "connector", text); err != nil {
			return "", fmt.Errorf("сообщение не ушло: %w", err)
		}
		return "сообщение отправлено", nil
	case ActionMailFlag:
		return e.mailFlag(ctx, r, a, ev)
	}
	return "", fmt.Errorf("неизвестный результат: %s", a.Type)
}

func (e *Engine) mark(ctx context.Context, r store.ConnectorRule,
	a store.ConnectorAction, ev Event) (string, error) {
	cat, err := e.Store.CategoryByID(ctx, r.UserID, a.CategoryID)
	if err != nil {
		return "", fmt.Errorf("трекер недоступен (id %d)", a.CategoryID)
	}
	// день события, а не день срабатывания: письмо о поездке может прийти в
	// час ночи, а поездка была вечером. Сутки трекера сдвинуты своим началом
	when := ev.At
	if a.Day == "today" || when.IsZero() {
		when = time.Now()
	}
	day := store.TrackerDay(when, r.TzOffsetMinutes, cat.DayStartMinutes)

	if cat.Kind == "counter" {
		n := a.Count
		if n <= 0 {
			n = 1
		}
		total, err := e.Store.IncrementMark(ctx, r.UserID, cat.ID, day, n)
		if err != nil {
			return "", fmt.Errorf("счётчик «%s» не изменился: %w", cat.Name, err)
		}
		return fmt.Sprintf("«%s» +%d за %s (стало %d)", cat.Name, n,
			day.Format("02.01.2006"), total), nil
	}
	var color, emoji *string
	if a.Color != "" {
		color = &a.Color
	}
	if a.Emoji != "" {
		emoji = &a.Emoji
	}
	created, err := e.Store.SetMark(ctx, r.UserID, cat.ID, day, color, emoji)
	if err != nil {
		return "", fmt.Errorf("отметка в «%s» не поставилась: %w", cat.Name, err)
	}
	if !created {
		return fmt.Sprintf("«%s» за %s уже была отмечена", cat.Name,
			day.Format("02.01.2006")), nil
	}
	return fmt.Sprintf("отметка в «%s» за %s", cat.Name, day.Format("02.01.2006")), nil
}

func (e *Engine) mailFlag(ctx context.Context, r store.ConnectorRule,
	a store.ConnectorAction, ev Event) (string, error) {
	if ev.Source != SourceMail || ev.ID <= 0 {
		return "", fmt.Errorf("пометить письмо можно только у события почты")
	}
	var err error
	if a.Flag == "archived" {
		err = e.Store.ArchiveMailMessage(ctx, r.UserID, ev.ID, a.Value)
	} else {
		err = e.Store.SetMailFlag(ctx, r.UserID, ev.ID, a.Flag, a.Value)
	}
	if err != nil {
		return "", fmt.Errorf("пометка письма не применилась: %w", err)
	}
	for _, f := range MailFlags {
		if f.Code == a.Flag {
			if a.Value {
				return "письмо: " + f.Title, nil
			}
			return "письмо: снято «" + f.Title + "»", nil
		}
	}
	return "пометка письма", nil
}

// expand подставляет поля события в текст сообщения.
func expand(text string, ev Event) string {
	rep := strings.NewReplacer(
		"{subject}", ev.Text["subject"],
		"{from}", ev.Text["from"],
		"{rcpt}", ev.Text["rcpt"],
		"{date}", ev.At.Format("02.01.2006 15:04"),
	)
	out := rep.Replace(text)
	if len(out) > 1000 {
		out = out[:1000]
	}
	return out
}
