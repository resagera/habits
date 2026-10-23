package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/receiptimport"
	"streaks-backend/internal/receipts"
	"streaks-backend/internal/store"
)

// Страница «Почта»: чтение писем, принятых своим SMTP-приёмником, управление
// адресами и наблюдение за теми, кто ломится на порт 25.

type mailHandlers struct {
	store    *store.Store
	domains  []string
	hostname string
	unblock  func(ctx context.Context, ip string) error
	receipts *receiptimport.Importer
	authMW   *auth.Middleware
}

// receiptsAllowed — выдана ли пользователю опция «чеки магазинов». Разбор
// чужих писем в чужие финансы включается поимённо в Админке.
func (h *mailHandlers) receiptsAllowed(r *http.Request) bool {
	user := auth.UserFromContext(r.Context())
	return h.receipts.Allowed(r.Context(), user.ID)
}

// GET /mail/overview — письма, адреса, цифры и последние гости порта 25.
func (h *mailHandlers) overview(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	ctx := r.Context()

	f := store.MailFilter{
		Box:   r.URL.Query().Get("box"),
		Query: strings.TrimSpace(r.URL.Query().Get("q")),
	}
	f.AddressID, _ = strconv.ParseInt(r.URL.Query().Get("address_id"), 10, 64)
	f.Limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	f.Offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))

	messages, total, err := h.store.ListMailMessages(ctx, user.ID, f)
	if err != nil {
		internalError(w)
		return
	}
	addresses, err := h.store.ListMailAddresses(ctx, user.ID)
	if err != nil {
		internalError(w)
		return
	}
	totals, err := h.store.MailTotals(ctx, user.ID)
	if err != nil {
		internalError(w)
		return
	}
	notify, err := h.store.MailNotifyEnabled(ctx, user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if messages == nil {
		messages = []store.MailMessage{}
	}
	if addresses == nil {
		addresses = []store.MailAddress{}
	}
	out := map[string]any{
		"messages":         messages,
		"total":            total,
		"addresses":        addresses,
		"totals":           totals,
		"domains":          h.domains,
		"hostname":         h.hostname,
		"notify":           notify,
		"receipts_allowed": false,
	}
	// чеки показываем только тем, кому опция выдана: иначе в интерфейсе
	// маячили бы настройки, которые всё равно не сработают
	if h.receipts.Allowed(ctx, user.ID) {
		out["receipts_allowed"] = true
		out["parsers"] = receipts.Parsers
		list, err := h.store.ListMailReceipts(ctx, user.ID, 50)
		if err != nil {
			internalError(w)
			return
		}
		if list == nil {
			list = []store.MailReceipt{}
		}
		out["receipts"] = list
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *mailHandlers) message(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid message id")
		return
	}
	m, err := h.store.MailMessageByID(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "message not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	atts, err := h.store.ListMailAttachments(r.Context(), id)
	if err != nil {
		internalError(w)
		return
	}
	if atts == nil {
		atts = []store.MailAttachment{}
	}
	// открыли — значит прочитано
	_ = h.store.SetMailFlag(r.Context(), user.ID, id, "is_read", true)

	out := map[string]any{"message": m, "attachments": atts}
	if rec, err := h.store.MailReceiptByMessage(r.Context(), user.ID, id); err == nil {
		items, _ := h.store.ListMailReceiptItems(r.Context(), rec.ID)
		if items == nil {
			items = []store.MailReceiptItem{}
		}
		out["receipt"] = rec
		out["receipt_items"] = items
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *mailHandlers) setFlag(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid message id")
		return
	}
	var req struct {
		Flag  string `json:"flag"`
		Value bool   `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	switch req.Flag {
	case "is_read", "starred", "is_spam":
	default:
		badRequest(w, "flag must be is_read|starred|is_spam")
		return
	}
	err = h.store.SetMailFlag(r.Context(), user.ID, id, req.Flag, req.Value)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "message not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (h *mailHandlers) archive(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid message id")
		return
	}
	var req struct {
		Archived bool `json:"archived"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	err = h.store.ArchiveMailMessage(r.Context(), user.ID, id, req.Archived)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "message not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"archived": req.Archived})
	}
}

// DELETE /mail/messages/{id} — вместе с исходником и вложениями на диске.
func (h *mailHandlers) deleteMessage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid message id")
		return
	}
	paths, err := h.store.DeleteMailMessage(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "message not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	for _, p := range paths {
		_ = os.Remove(p)
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /mail/attachments/{id} — вложения не публичны (в отличие от фонов),
// поэтому отдаются через API с проверкой владельца.
func (h *mailHandlers) attachment(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid attachment id")
		return
	}
	a, err := h.store.MailAttachmentByID(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "attachment not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	f, err := os.Open(a.Path)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "file is gone")
		return
	}
	defer f.Close()
	name := filepath.Base(a.Filename)
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "attachment"
	}
	modTime := time.Time{}
	if st, err := f.Stat(); err == nil {
		modTime = st.ModTime()
	}
	// всегда как вложение: содержимое письма нельзя открывать как страницу —
	// иначе HTML из письма выполнится в контексте нашего домена
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+urlEscape(name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, modTime, f)
}

// --- адреса ---

type mailAddressRequest struct {
	Address  string `json:"address"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	OnlyFrom string `json:"only_from"`
	Enabled  *bool  `json:"enabled"`
	Note     string `json:"note"`
}

func (h *mailHandlers) createAddress(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req mailAddressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	a := store.MailAddress{
		Label:    strings.TrimSpace(req.Label),
		Kind:     req.Kind,
		OnlyFrom: strings.ToLower(strings.TrimSpace(req.OnlyFrom)),
		Enabled:  true,
		Note:     strings.TrimSpace(req.Note),
	}
	if req.Enabled != nil {
		a.Enabled = *req.Enabled
	}
	if a.Kind != "alias" {
		a.Kind = "address"
	}

	local := strings.ToLower(strings.TrimSpace(req.Address))
	local, _, _ = strings.Cut(local, "@") // домен задаём сами
	if a.Kind == "alias" && local == "" {
		// одноразовый алиас под магазин: имя генерируем, чтобы адреса не
		// повторялись и было видно, кто его продал
		base := slugify(a.Label)
		if base == "" {
			base = "alias"
		}
		local = base + "-" + randSuffix()
	}
	if !validLocalPart(local) {
		badRequest(w, "адрес: латиница, цифры, точка, дефис и подчёркивание, 1-64 символа")
		return
	}
	domain := h.domains[0]
	a.Address = local + "@" + domain

	out, err := h.store.CreateMailAddress(r.Context(), user.ID, a)
	switch {
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "такой адрес уже заведён")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"address": out})
	}
}

func (h *mailHandlers) updateAddress(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid address id")
		return
	}
	var req mailAddressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	a := store.MailAddress{
		ID:       id,
		Label:    strings.TrimSpace(req.Label),
		OnlyFrom: strings.ToLower(strings.TrimSpace(req.OnlyFrom)),
		Enabled:  true,
		Note:     strings.TrimSpace(req.Note),
	}
	if req.Enabled != nil {
		a.Enabled = *req.Enabled
	}
	out, err := h.store.UpdateMailAddress(r.Context(), user.ID, a)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "address not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"address": out})
	}
}

func (h *mailHandlers) deleteAddress(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid address id")
		return
	}
	err = h.store.DeleteMailAddress(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "address not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- кто ломится ---

// GET /mail/guard — статистика по адресам и действующие блокировки.
func (h *mailHandlers) guard(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	stats, err := h.store.ListMailIPStats(r.Context(), limit)
	if err != nil {
		internalError(w)
		return
	}
	if stats == nil {
		stats = []store.MailIPStat{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ips": stats})
}

func (h *mailHandlers) unblockIP(w http.ResponseWriter, r *http.Request) {
	ip := r.PathValue("ip")
	if ip == "" {
		badRequest(w, "invalid ip")
		return
	}
	// снимаем и в памяти приёмника, иначе он продолжит отвечать 421
	if h.unblock != nil {
		if err := h.unblock(r.Context(), ip); err != nil {
			internalError(w)
			return
		}
	} else if err := h.store.UnblockMailIP(r.Context(), ip); err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *mailHandlers) setSettings(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Notify *bool `json:"notify"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Notify != nil {
		if err := h.store.SetMailNotify(r.Context(), user.ID, *req.Notify); err != nil {
			internalError(w)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// validLocalPart — что допускаем в имени адреса. Сознательно уже RFC:
// экзотика в адресе только мешает и ломает формы магазинов.
func validLocalPart(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '.' || r == '-' || r == '_' || r == '+':
		default:
			return false
		}
	}
	return true
}

func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func randSuffix() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "x1y2z3"
	}
	return hex.EncodeToString(b)
}

func urlEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '.' || c == '-' || c == '_' {
			b.WriteByte(c)
			continue
		}
		b.WriteString("%" + strings.ToUpper(hex.EncodeToString([]byte{c})))
	}
	return b.String()
}

// --- чеки магазинов ---

// PUT /mail/addresses/{id}/parser — какой разборщик применять к письмам на
// адрес и куда писать трату.
func (h *mailHandlers) setAddressParser(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if !h.receiptsAllowed(r) {
		writeError(w, http.StatusForbidden, "forbidden", "разбор чеков не подключён")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid address id")
		return
	}
	var req struct {
		Parser     string `json:"parser"`
		CategoryID *int64 `json:"category_id"`
		AccountID  *int64 `json:"account_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Parser = strings.TrimSpace(req.Parser)
	if req.Parser != "" && !receipts.Known(req.Parser) {
		badRequest(w, "неизвестный разборщик")
		return
	}
	if req.CategoryID != nil && *req.CategoryID <= 0 {
		req.CategoryID = nil
	}
	if req.AccountID != nil && *req.AccountID <= 0 {
		req.AccountID = nil
	}
	err = h.store.SetMailAddressParser(r.Context(), user.ID, id, store.MailAddressParser{
		Parser: req.Parser, CategoryID: req.CategoryID, AccountID: req.AccountID,
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "address not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// POST /mail/messages/{id}/parse — разобрать письмо руками.
// Нужно для писем, пришедших до настройки разборщика, и после правки правил.
func (h *mailHandlers) parseMessage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if !h.receiptsAllowed(r) {
		writeError(w, http.StatusForbidden, "forbidden", "разбор чеков не подключён")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid message id")
		return
	}
	m, err := h.store.MailMessageByID(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "message not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	var req struct {
		Parser string `json:"parser"`
		// перечитать уже разобранный чек: разбор мог поумнеть с тех пор
		Refresh bool `json:"refresh"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	// разборщик берём у адреса письма, если в запросе не задан явно
	addr := store.MailAddress{UserID: user.ID, Parser: strings.TrimSpace(req.Parser)}
	if m.AddressID != nil {
		for _, a := range mustAddresses(h, r, user.ID) {
			if a.ID == *m.AddressID {
				addr = a
				break
			}
		}
	}
	if req.Parser != "" {
		if !receipts.Known(req.Parser) {
			badRequest(w, "неизвестный разборщик")
			return
		}
		addr.Parser = req.Parser
	}
	if addr.Parser == "" {
		badRequest(w, "у адреса не выбран разборщик")
		return
	}

	body := m.TextBody
	if strings.TrimSpace(body) == "" {
		body = m.HTMLBody // на крайний случай — сервер приведёт к тексту сам
	}
	if req.Refresh {
		rec, err := h.receipts.Refresh(r.Context(), addr, m.Subject, body)
		switch {
		case errors.Is(err, receipts.ErrNotReceipt):
			writeError(w, http.StatusUnprocessableEntity, "not_receipt",
				"письмо не похоже на чек этого магазина")
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "этот чек ещё не разобран")
		case err != nil:
			writeError(w, http.StatusUnprocessableEntity, "parse_failed", err.Error())
		default:
			items, _ := h.store.ListMailReceiptItems(r.Context(), rec.ID)
			if items == nil {
				items = []store.MailReceiptItem{}
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"receipt": rec, "receipt_items": items, "refreshed": true,
			})
		}
		return
	}

	rec, tx, err := h.receipts.Import(r.Context(), addr, &id, m.Subject, body, false)
	switch {
	case errors.Is(err, store.ErrReceiptExists):
		writeError(w, http.StatusConflict, "conflict", "этот заказ уже записан в траты")
	case errors.Is(err, receipts.ErrNotReceipt):
		writeError(w, http.StatusUnprocessableEntity, "not_receipt",
			"письмо не похоже на чек этого магазина")
	case err != nil:
		writeError(w, http.StatusUnprocessableEntity, "parse_failed", err.Error())
	default:
		items, _ := h.store.ListMailReceiptItems(r.Context(), rec.ID)
		if items == nil {
			items = []store.MailReceiptItem{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"receipt": rec, "receipt_items": items, "transaction": tx,
		})
	}
}

// DELETE /mail/receipts/{id} — забыть разбор. Созданная трата остаётся: за
// деньги отвечает Finance, и удаление разбора не должно их молча стирать.
func (h *mailHandlers) deleteReceipt(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid receipt id")
		return
	}
	err = h.store.DeleteMailReceipt(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "receipt not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// GET /mail/receipts/{id}/items — позиции чека.
func (h *mailHandlers) receiptItems(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid receipt id")
		return
	}
	if _, err := h.store.MailReceiptByID(r.Context(), user.ID, id); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "receipt not found")
		return
	}
	items, err := h.store.ListMailReceiptItems(r.Context(), id)
	if err != nil {
		internalError(w)
		return
	}
	if items == nil {
		items = []store.MailReceiptItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func mustAddresses(h *mailHandlers, r *http.Request, userID int64) []store.MailAddress {
	list, err := h.store.ListMailAddresses(r.Context(), userID)
	if err != nil {
		return nil
	}
	return list
}
