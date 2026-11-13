// Package connector — правила «источник → условия → результат».
//
// Коннектор связывает страницы между собой: событие на одной странице
// (пришло письмо) превращается в действие на другой (отметка в трекере).
// Реестр источников, полей, операций и результатов живёт ЗДЕСЬ и отдаётся
// интерфейсу: страница рисует формы по нему, поэтому новый источник или новое
// поле — это запись в этом файле плюс функция, а не переделка фронтенда.
package connector

// Source — откуда берутся события.
type Source struct {
	Code   string  `json:"code"`
	Title  string  `json:"title"`
	Icon   string  `json:"icon"`
	Event  string  `json:"event"` // что считается событием, человеческими словами
	Fields []Field `json:"fields"`
}

// Field — что у события можно спросить. Kind определяет набор операций.
type Field struct {
	Code  string `json:"code"`
	Title string `json:"title"`
	Kind  string `json:"kind"` // text | flag
	Hint  string `json:"hint,omitempty"`
}

// Cmp — операция сравнения. NoValue — операциям вроде «пусто» значение не нужно.
type Cmp struct {
	Code    string `json:"code"`
	Title   string `json:"title"`
	Kind    string `json:"kind"` // к полям какого вида применима
	NoValue bool   `json:"no_value,omitempty"`
}

// ActionType — что правило умеет делать.
type ActionType struct {
	Code  string `json:"code"`
	Title string `json:"title"`
	Hint  string `json:"hint,omitempty"`
}

const (
	SourceMail = "mail"

	ActionTrackerMark = "tracker_mark"
	ActionNotify      = "notify"
	ActionMailFlag    = "mail_flag"
)

// Sources — все источники. Сейчас один: почта. Дерево условий, журнал и
// интерфейс от этого не зависят — они написаны под список.
var Sources = []Source{{
	Code: SourceMail, Title: "Почта", Icon: "📬", Event: "пришло письмо",
	Fields: []Field{
		{Code: "from", Title: "От кого (адрес)", Kind: "text",
			Hint: "адрес из заголовка From, например no-reply@taxi.yandex.ru"},
		{Code: "from_name", Title: "Имя отправителя", Kind: "text"},
		{Code: "envelope", Title: "Конверт (MAIL FROM)", Kind: "text",
			Hint: "адрес, который назвал сам сервер отправителя"},
		{Code: "subject", Title: "Тема", Kind: "text"},
		{Code: "text", Title: "Текст письма", Kind: "text",
			Hint: "текстовая часть; у писем-картинок она бывает пустой"},
		{Code: "rcpt", Title: "Адрес получателя", Kind: "text",
			Hint: "на какой из ваших адресов пришло письмо"},
		{Code: "ip", Title: "IP отправителя", Kind: "text"},
		{Code: "helo", Title: "HELO сервера", Kind: "text"},
		{Code: "spf", Title: "Результат SPF", Kind: "text",
			Hint: "pass, fail, softfail, neutral, none"},
		{Code: "spam", Title: "Помечено спамом", Kind: "flag"},
		{Code: "attachments", Title: "Есть вложения", Kind: "flag"},
		{Code: "tls", Title: "Пришло по TLS", Kind: "flag"},
	},
}}

// Cmps — операции. Текстовые сравнения регистр не различают: адреса и темы
// писем приходят в разном регистре, и «Яндекс» не должен промахиваться мимо
// «ЯНДЕКС». Регулярка — исключение, её регистром человек управляет сам
// через (?i).
var Cmps = []Cmp{
	{Code: "contains", Title: "содержит", Kind: "text"},
	{Code: "not_contains", Title: "не содержит", Kind: "text"},
	{Code: "equals", Title: "равно", Kind: "text"},
	{Code: "not_equals", Title: "не равно", Kind: "text"},
	{Code: "starts", Title: "начинается с", Kind: "text"},
	{Code: "ends", Title: "заканчивается на", Kind: "text"},
	{Code: "regex", Title: "подходит под регулярку", Kind: "text"},
	{Code: "empty", Title: "пусто", Kind: "text", NoValue: true},
	{Code: "not_empty", Title: "не пусто", Kind: "text", NoValue: true},
	{Code: "is_true", Title: "да", Kind: "flag", NoValue: true},
	{Code: "is_false", Title: "нет", Kind: "flag", NoValue: true},
}

// ActionTypes — результаты правила.
var ActionTypes = []ActionType{
	{Code: ActionTrackerMark, Title: "Отметка в трекере",
		Hint: "у трекеров-счётчиков прибавляет указанное число"},
	{Code: ActionNotify, Title: "Сообщение в Telegram",
		Hint: "в тексте можно подставить {subject}, {from}, {rcpt}, {date}"},
	{Code: ActionMailFlag, Title: "Пометить письмо",
		Hint: "прочитано, ★ или в архив — чтобы разобранные письма не мешались"},
}

// MailFlags — какие флаги письму правило умеет ставить.
var MailFlags = []ActionType{
	{Code: "is_read", Title: "прочитано"},
	{Code: "starred", Title: "со звёздочкой"},
	{Code: "archived", Title: "в архив"},
}

func sourceByCode(code string) (Source, bool) {
	for _, s := range Sources {
		if s.Code == code {
			return s, true
		}
	}
	return Source{}, false
}

func fieldByCode(code string) (Field, bool) {
	for _, s := range Sources {
		for _, f := range s.Fields {
			if f.Code == code {
				return f, true
			}
		}
	}
	return Field{}, false
}

func cmpByCode(code string) (Cmp, bool) {
	for _, c := range Cmps {
		if c.Code == code {
			return c, true
		}
	}
	return Cmp{}, false
}

// Meta — весь реестр одним объектом: страница запрашивает его один раз.
func Meta() map[string]any {
	return map[string]any{
		"sources":    Sources,
		"cmps":       Cmps,
		"actions":    ActionTypes,
		"mail_flags": MailFlags,
		"limits": map[string]int{
			"depth": maxDepth, "nodes": maxNodes, "actions": maxActions,
		},
	}
}
