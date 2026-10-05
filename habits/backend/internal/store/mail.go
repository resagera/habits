package store

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// MailAddress — адрес или одноразовый алиас, на который принимаем почту.
type MailAddress struct {
	ID       int64      `json:"id"`
	UserID   int64      `json:"user_id"`
	Address  string     `json:"address"`
	Label    string     `json:"label"`
	Kind     string     `json:"kind"`
	OnlyFrom string     `json:"only_from"`
	Enabled  bool       `json:"enabled"`
	Received int        `json:"received"`
	Rejected int        `json:"rejected"`
	LastAt   *time.Time `json:"last_at"`
	Note     string     `json:"note"`
	// разбор писем как чеков магазина ('' — не разбирать)
	Parser           string `json:"parser"`
	ParserAccountID  *int64 `json:"parser_account_id"`
	ParserCategoryID *int64 `json:"parser_category_id"`
}

const mailAddressCols = `id, user_id, address, label, kind, only_from, enabled,
	received, rejected, last_at, note, parser, parser_account_id, parser_category_id`

func (s *Store) ListMailAddresses(ctx context.Context, userID int64) ([]MailAddress, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+mailAddressCols+`
		FROM mail_addresses WHERE user_id = $1
		ORDER BY kind, lower(address)`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[MailAddress])
}

// MailAddressByAddress — поиск получателя для SMTP. Пользователь не указан:
// приёмник работает от лица сервера, до авторизации дело не доходит.
func (s *Store) MailAddressByAddress(ctx context.Context, addr string) (MailAddress, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+mailAddressCols+`
		FROM mail_addresses WHERE lower(address) = lower($1)`, addr)
	if err != nil {
		return MailAddress{}, err
	}
	a, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[MailAddress])
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (s *Store) CreateMailAddress(ctx context.Context, userID int64, a MailAddress) (MailAddress, error) {
	rows, err := s.pool.Query(ctx, `
		INSERT INTO mail_addresses (user_id, address, label, kind, only_from, enabled, note)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING `+mailAddressCols,
		userID, a.Address, a.Label, a.Kind, a.OnlyFrom, a.Enabled, a.Note)
	if err != nil {
		return a, uniqueAsConflict(err)
	}
	out, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[MailAddress])
	return out, uniqueAsConflict(err)
}

func (s *Store) UpdateMailAddress(ctx context.Context, userID int64, a MailAddress) (MailAddress, error) {
	rows, err := s.pool.Query(ctx, `
		UPDATE mail_addresses SET label = $3, only_from = $4, enabled = $5,
			note = $6, updated_at = now()
		WHERE user_id = $1 AND id = $2
		RETURNING `+mailAddressCols,
		userID, a.ID, a.Label, a.OnlyFrom, a.Enabled, a.Note)
	if err != nil {
		return a, err
	}
	out, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[MailAddress])
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, err
}

func (s *Store) DeleteMailAddress(ctx context.Context, userID, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM mail_addresses WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// BumpMailAddress — счётчики адреса: сколько принято и сколько отбито.
func (s *Store) BumpMailAddress(ctx context.Context, id int64, accepted bool) error {
	col := "rejected = rejected + 1"
	if accepted {
		col = "received = received + 1, last_at = now()"
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE mail_addresses SET `+col+` WHERE id = $1`, id)
	return err
}

// --- письма ---

// MailMessage — принятое письмо. Тела хранятся в базе (они текстовые и
// небольшие), вложения и исходник — на диске.
type MailMessage struct {
	ID          int64      `json:"id"`
	AddressID   *int64     `json:"address_id"`
	Rcpt        string     `json:"rcpt"`
	MailFrom    string     `json:"mail_from"`
	FromName    string     `json:"from_name"`
	FromAddr    string     `json:"from_addr"`
	Subject     string     `json:"subject"`
	MessageID   string     `json:"message_id"`
	SentAt      *time.Time `json:"sent_at"`
	ReceivedAt  time.Time  `json:"received_at"`
	SizeBytes   int        `json:"size_bytes"`
	TextBody    string     `json:"text_body"`
	HTMLBody    string     `json:"html_body"`
	RemoteIP    string     `json:"remote_ip"`
	Helo        string     `json:"helo"`
	PTR         string     `json:"ptr"`
	TLS         bool       `json:"tls"`
	SPF         string     `json:"spf"`
	SpamScore   int        `json:"spam_score"`
	SpamReasons string     `json:"spam_reasons"`
	IsSpam      bool       `json:"is_spam"`
	IsRead      bool       `json:"is_read"`
	Starred     bool       `json:"starred"`
	ArchivedAt  *time.Time `json:"archived_at"`
	RawPath     string     `json:"-"`
}

// в списке тела не нужны: письмо магазина в HTML — это сотни килобайт на строку
const mailListCols = `id, address_id, rcpt, mail_from, from_name, from_addr,
	subject, message_id, sent_at, received_at, size_bytes, '' AS text_body,
	'' AS html_body, remote_ip, helo, ptr, tls, spf, spam_score, spam_reasons,
	is_spam, is_read, starred, archived_at, '' AS raw_path`

const mailFullCols = `id, address_id, rcpt, mail_from, from_name, from_addr,
	subject, message_id, sent_at, received_at, size_bytes, text_body,
	html_body, remote_ip, helo, ptr, tls, spf, spam_score, spam_reasons,
	is_spam, is_read, starred, archived_at, raw_path`

// MailFilter — параметры списка писем.
type MailFilter struct {
	Box       string // inbox | spam | archive | starred
	AddressID int64
	Query     string
	Limit     int
	Offset    int
}

func (s *Store) ListMailMessages(ctx context.Context, userID int64, f MailFilter) ([]MailMessage, int, error) {
	where := []string{"user_id = $1"}
	args := []any{userID}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	switch f.Box {
	case "spam":
		where = append(where, "is_spam AND archived_at IS NULL")
	case "archive":
		where = append(where, "archived_at IS NOT NULL")
	case "starred":
		where = append(where, "starred")
	default:
		where = append(where, "NOT is_spam AND archived_at IS NULL")
	}
	if f.AddressID > 0 {
		add("address_id = ?", f.AddressID)
	}
	if f.Query != "" {
		add("(subject ILIKE ? OR from_addr ILIKE ? OR text_body ILIKE ?)", "%"+f.Query+"%")
	}
	cond := strings.Join(where, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM mail_messages WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args = append(args, limit, f.Offset)
	rows, err := s.pool.Query(ctx, `
		SELECT `+mailListCols+`
		FROM mail_messages WHERE `+cond+`
		ORDER BY received_at DESC, id DESC
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[MailMessage])
	return list, total, err
}

func (s *Store) MailMessageByID(ctx context.Context, userID, id int64) (MailMessage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+mailFullCols+`
		FROM mail_messages WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return MailMessage{}, err
	}
	m, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[MailMessage])
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// MailAttachment — вложение. Отдаётся только через API с проверкой владельца:
// в отличие от фонов, содержимое писем публичным быть не должно.
type MailAttachment struct {
	ID          int64  `json:"id"`
	MessageID   int64  `json:"message_id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int    `json:"size_bytes"`
	Path        string `json:"-"`
}

func (s *Store) ListMailAttachments(ctx context.Context, messageID int64) ([]MailAttachment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, message_id, filename, content_type, size_bytes, path
		FROM mail_attachments WHERE message_id = $1 ORDER BY id`, messageID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[MailAttachment])
}

func (s *Store) MailAttachmentByID(ctx context.Context, userID, id int64) (MailAttachment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.message_id, a.filename, a.content_type, a.size_bytes, a.path
		FROM mail_attachments a
		JOIN mail_messages m ON m.id = a.message_id AND m.user_id = $1
		WHERE a.id = $2`, userID, id)
	if err != nil {
		return MailAttachment{}, err
	}
	a, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[MailAttachment])
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// SaveMailMessage — письмо целиком, вместе с вложениями, одной транзакцией.
func (s *Store) SaveMailMessage(ctx context.Context, userID int64, m MailMessage, atts []MailAttachment) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO mail_messages (user_id, address_id, rcpt, mail_from, from_name,
			from_addr, subject, message_id, sent_at, size_bytes, text_body, html_body,
			remote_ip, helo, ptr, tls, spf, spam_score, spam_reasons, is_spam, raw_path)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
		RETURNING id`,
		userID, m.AddressID, m.Rcpt, m.MailFrom, m.FromName, m.FromAddr, m.Subject,
		m.MessageID, m.SentAt, m.SizeBytes, m.TextBody, m.HTMLBody, m.RemoteIP,
		m.Helo, m.PTR, m.TLS, m.SPF, m.SpamScore, m.SpamReasons, m.IsSpam,
		m.RawPath).Scan(&id); err != nil {
		return 0, err
	}
	for _, a := range atts {
		if _, err := tx.Exec(ctx, `
			INSERT INTO mail_attachments (message_id, filename, content_type, size_bytes, path)
			VALUES ($1,$2,$3,$4,$5)`,
			id, a.Filename, a.ContentType, a.SizeBytes, a.Path); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit(ctx)
}

func (s *Store) SetMailFlag(ctx context.Context, userID, id int64, flag string, v bool) error {
	if flag != "is_read" && flag != "starred" && flag != "is_spam" {
		return ErrNotFound
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE mail_messages SET `+flag+` = $3 WHERE user_id = $1 AND id = $2`,
		userID, id, v)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ArchiveMailMessage(ctx context.Context, userID, id int64, archived bool) error {
	var at any
	if archived {
		at = time.Now()
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE mail_messages SET archived_at = $3 WHERE user_id = $1 AND id = $2`,
		userID, id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteMailMessage возвращает пути файлов, чтобы вызывающий убрал их с диска.
func (s *Store) DeleteMailMessage(ctx context.Context, userID, id int64) ([]string, error) {
	var raw string
	err := s.pool.QueryRow(ctx,
		`SELECT raw_path FROM mail_messages WHERE user_id = $1 AND id = $2`,
		userID, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	paths := []string{}
	if raw != "" {
		paths = append(paths, raw)
	}
	atts, err := s.ListMailAttachments(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, a := range atts {
		if a.Path != "" {
			paths = append(paths, a.Path)
		}
	}
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM mail_messages WHERE user_id = $1 AND id = $2`, userID, id); err != nil {
		return nil, err
	}
	return paths, nil
}

func (s *Store) MailUnreadCount(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM mail_messages
		WHERE user_id = $1 AND NOT is_read AND NOT is_spam AND archived_at IS NULL`,
		userID).Scan(&n)
	return n, err
}

// --- статистика по IP и блокировки ---

// MailIPStat — сколько раз с адреса приходили и за что его закрыли.
type MailIPStat struct {
	IP           string     `json:"ip"`
	FirstSeen    time.Time  `json:"first_seen"`
	LastSeen     time.Time  `json:"last_seen"`
	Connections  int        `json:"connections"`
	Accepted     int        `json:"accepted"`
	Rejected     int        `json:"rejected"`
	BlockedUntil *time.Time `json:"blocked_until"`
	LastReason   string     `json:"last_reason"`
	PTR          string     `json:"ptr"`
}

// TouchMailIP — один UPSERT на подключение: счётчики, а не журнал событий.
// Порт 25 перебирают круглосуточно, построчный лог занял бы больше места, чем
// сама почта.
func (s *Store) TouchMailIP(ctx context.Context, ip, ptr string, accepted, rejected int, reason string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO mail_ip_stats (ip, ptr, connections, accepted, rejected, last_reason)
		VALUES ($1,$2,1,$3,$4,$5)
		ON CONFLICT (ip) DO UPDATE SET
			last_seen = now(),
			connections = mail_ip_stats.connections + 1,
			accepted = mail_ip_stats.accepted + EXCLUDED.accepted,
			rejected = mail_ip_stats.rejected + EXCLUDED.rejected,
			ptr = CASE WHEN EXCLUDED.ptr <> '' THEN EXCLUDED.ptr ELSE mail_ip_stats.ptr END,
			last_reason = CASE WHEN EXCLUDED.last_reason <> '' THEN EXCLUDED.last_reason
			                   ELSE mail_ip_stats.last_reason END`,
		ip, ptr, accepted, rejected, reason)
	return err
}

func (s *Store) BlockMailIP(ctx context.Context, ip string, until time.Time, reason string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO mail_ip_stats (ip, blocked_until, last_reason, rejected)
		VALUES ($1,$2,$3,1)
		ON CONFLICT (ip) DO UPDATE SET
			blocked_until = EXCLUDED.blocked_until,
			last_reason = EXCLUDED.last_reason,
			last_seen = now()`, ip, until, reason)
	return err
}

func (s *Store) UnblockMailIP(ctx context.Context, ip string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE mail_ip_stats SET blocked_until = NULL WHERE ip = $1`, ip)
	return err
}

// BlockedMailIPs — действующие блокировки: приёмник поднимает их в память при
// старте, чтобы не ходить в базу на каждое подключение.
func (s *Store) BlockedMailIPs(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT ip, blocked_until FROM mail_ip_stats WHERE blocked_until > now()`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var ip string
		var until time.Time
		if err := rows.Scan(&ip, &until); err != nil {
			return nil, err
		}
		out[ip] = until
	}
	return out, rows.Err()
}

func (s *Store) ListMailIPStats(ctx context.Context, limit int) ([]MailIPStat, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT ip, first_seen, last_seen, connections, accepted, rejected,
		       blocked_until, last_reason, ptr
		FROM mail_ip_stats
		ORDER BY (blocked_until > now()) DESC NULLS LAST, last_seen DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[MailIPStat])
}

// MailTotals — цифры для шапки страницы.
type MailTotals struct {
	Messages  int `json:"messages"`
	Spam      int `json:"spam"`
	Unread    int `json:"unread"`
	IPs       int `json:"ips"`
	Blocked   int `json:"blocked"`
	Rejected  int `json:"rejected"`
	Addresses int `json:"addresses"`
}

func (s *Store) MailTotals(ctx context.Context, userID int64) (MailTotals, error) {
	var t MailTotals
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM mail_messages WHERE user_id = $1 AND NOT is_spam),
			(SELECT count(*) FROM mail_messages WHERE user_id = $1 AND is_spam),
			(SELECT count(*) FROM mail_messages
			   WHERE user_id = $1 AND NOT is_read AND NOT is_spam AND archived_at IS NULL),
			(SELECT count(*) FROM mail_ip_stats),
			(SELECT count(*) FROM mail_ip_stats WHERE blocked_until > now()),
			(SELECT COALESCE(sum(rejected), 0) FROM mail_ip_stats),
			(SELECT count(*) FROM mail_addresses WHERE user_id = $1)`, userID).
		Scan(&t.Messages, &t.Spam, &t.Unread, &t.IPs, &t.Blocked, &t.Rejected, &t.Addresses)
	return t, err
}

// MailOwner — кому показывать письма: владелец адреса. Приёмник работает вне
// сессии, поэтому пользователя берём из самой записи адреса.
func (s *Store) MailNotifyEnabled(ctx context.Context, userID int64) (bool, error) {
	var v bool
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(mail_notify, true) FROM user_settings WHERE user_id = $1`,
		userID).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	return v, err
}

func (s *Store) SetMailNotify(ctx context.Context, userID int64, v bool) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_settings (user_id, mail_notify) VALUES ($1,$2)
		ON CONFLICT (user_id) DO UPDATE SET mail_notify = EXCLUDED.mail_notify,
			updated_at = now()`, userID, v)
	return err
}
