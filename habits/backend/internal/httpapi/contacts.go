package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

// Страница Contacts: контакты с примечанием/фото/галочкой auto_accept и
// «входящие» шаринги. Все send-обработчики приложения доставляют через
// deliverShare: контакту с галочкой — сразу, остальным — во входящие.
type contactsHandlers struct {
	store   *store.Store
	bot     *notify.Bot
	dataDir string
}

// shareKindMsg: тексты уведомлений бота (первый %s — отправитель, второй — название).
var shareKindMsg = map[string]struct{ Applied, Queued string }{
	"checker_template":  {"📋 %s поделился с вами шаблоном «%s» — он появился на вкладке Checker в Habits.", "📋 %s хочет поделиться с вами шаблоном чек-листа «%s» — принять можно на странице Contacts в Habits."},
	"checker_group":     {"✅ %s поделился с вами списком «%s» — он появился на вкладке Checker в Habits.", "✅ %s хочет поделиться с вами списком «%s» — принять можно на странице Contacts в Habits."},
	"reminder_category": {"🔔 %s поделился с вами категорией напоминаний «%s» — она появилась на вкладке Reminders в Habits.", "🔔 %s хочет поделиться с вами категорией напоминаний «%s» — принять можно на странице Contacts в Habits."},
	"article":           {"📄 %s поделился с вами статьёй «%s» — она появилась на вкладке Articles в Habits.", "📄 %s хочет поделиться с вами статьёй «%s» — принять можно на странице Contacts в Habits."},
	"links_folder":      {"🔗 %s поделился с вами папкой ссылок «%s» — она появилась на вкладке Links в Habits.", "🔗 %s хочет поделиться с вами папкой ссылок «%s» — принять можно на странице Contacts в Habits."},
	"link":              {"🔗 %s поделился с вами ссылкой «%s» — она появилась на вкладке Links в Habits.", "🔗 %s хочет поделиться с вами ссылкой «%s» — принять можно на странице Contacts в Habits."},
	"tracker":           {"📊 %s открыл вам доступ к трекеру «%s» — он появился на вкладке Tracker в Habits.", "📊 %s хочет открыть вам доступ к трекеру «%s» — принять можно на странице Contacts в Habits."},
	"task_project":      {"🗂 %s открыл вам доступ к категории задач «%s» — она появилась на вкладке Tasks в Habits.", "🗂 %s хочет открыть вам доступ к категории задач «%s» — принять можно на странице Contacts в Habits."},
	"project":           {"📦 %s открыл вам доступ к проекту «%s» — он появился на вкладке Projects в Habits.", "📦 %s хочет открыть вам доступ к проекту «%s» — принять можно на странице Contacts в Habits."},
	"food":              {"🍽 %s открыл вам доступ к своему дневнику питания («%s») — он появился на вкладке Food → Общие в Habits.", "🍽 %s хочет открыть вам доступ к своему дневнику питания («%s») — принять можно на странице Contacts в Habits."},
}

func senderLabel(u auth.TgUser) string {
	from := u.FirstName
	if u.Username != "" {
		from += " @" + u.Username
	}
	return strings.TrimSpace(from)
}

// deliverShare — единая доставка шаринга kind/refID от sender к recipient:
// если у получателя отправитель в контактах с auto_accept — применяет сразу,
// иначе кладёт во «входящие» (применение случится при «Принять»).
// Возвращает (queued, name, err); err может быть store.ErrNotFound (источник).
func deliverShare(ctx context.Context, st *store.Store, bot *notify.Bot,
	sender auth.TgUser, recipientID int64, kind string, refID int64) (bool, string, error) {
	auto, err := st.AutoAcceptFrom(ctx, recipientID, sender.ID)
	if err != nil {
		return false, "", err
	}
	msg := shareKindMsg[kind]
	var name string
	if auto {
		if name, err = st.ApplyShare(ctx, kind, sender.ID, refID, recipientID); err != nil {
			return false, "", err
		}
	} else {
		// проверка владения + название — без применения
		if name, err = st.ShareTitle(ctx, kind, sender.ID, refID); err != nil {
			return false, "", err
		}
		if err = st.CreateIncomingShare(ctx, sender.ID, recipientID, kind, refID, name); err != nil {
			return false, "", err
		}
	}
	text := msg.Queued
	if auto {
		text = msg.Applied
	}
	go bot.SendMessage(context.Background(), recipientID, fmt.Sprintf(text, senderLabel(sender), name))
	_ = st.TouchShareRecipient(ctx, sender.ID, recipientID)
	return !auto, name, nil
}

// --- контакты ---

// GET /contacts — контакты + подсказки (с кем делился, но ещё не в контактах).
func (h *contactsHandlers) list(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	contacts, err := h.store.ListContacts(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if contacts == nil {
		contacts = []store.Contact{}
	}
	inContacts := make(map[int64]bool, len(contacts))
	for _, c := range contacts {
		if c.ContactID != nil {
			inContacts[*c.ContactID] = true
		}
	}
	recent, err := h.store.RecentRecipients(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	suggestions := []store.AccessUser{}
	for _, u := range recent {
		if !inContacts[u.ID] {
			suggestions = append(suggestions, u)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"contacts": contacts, "suggestions": suggestions})
}

var tgUsernameRe = regexp.MustCompile(`^@?[A-Za-z0-9_]{4,64}$`)

// POST /contacts {to} — добавить контакт по id или @логину. Если человека
// нет в боте — создаём «внешний» контакт: по числовому id пробуем узнать
// имя/логин через Bot API getChat (сработает, если бот его видел), по
// @логину Bot API частные аккаунты не резолвит — будет «Новый контакт».
// Привязка к пользователю случится автоматически при его первом входе.
func (h *contactsHandlers) create(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		To string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.To) == "" {
		badRequest(w, "to (user id or @username) is required")
		return
	}
	to := strings.TrimSpace(req.To)

	target, err := h.store.FindUserExact(r.Context(), to)
	switch {
	case err == nil:
		if target.ID == user.ID {
			badRequest(w, "cannot add yourself")
			return
		}
		contact, err := h.store.AddContactUser(r.Context(), user.ID, target.ID)
		if err != nil {
			internalError(w)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"contact": contact})
		return
	case !errors.Is(err, store.ErrNotFound):
		internalError(w)
		return
	}

	// в боте нет — внешний контакт
	if num, numErr := strconv.ParseInt(to, 10, 64); numErr == nil {
		if num == user.ID {
			badRequest(w, "cannot add yourself")
			return
		}
		if num <= 0 {
			badRequest(w, "invalid user id")
			return
		}
		extUsername, extName := "", ""
		if info, ok := h.bot.GetChat(r.Context(), num); ok {
			extUsername = info.Username
			extName = strings.TrimSpace(info.FirstName + " " + info.LastName)
		}
		contact, err := h.store.AddContactExternal(r.Context(), user.ID, &num, extUsername, extName)
		if err != nil {
			internalError(w)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"contact": contact})
		return
	}
	if !tgUsernameRe.MatchString(to) {
		badRequest(w, "to must be a numeric id or @username (4-64 letters/digits/_)")
		return
	}
	login := strings.TrimPrefix(to, "@")
	if strings.EqualFold(login, user.Username) {
		badRequest(w, "cannot add yourself")
		return
	}
	contact, err := h.store.AddContactExternal(r.Context(), user.ID, nil, login, "")
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"contact": contact})
}

// PATCH /contacts/{id} — примечание и/или auto_accept.
func (h *contactsHandlers) update(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid contact id")
		return
	}
	var req struct {
		Note       *string `json:"note"`
		AutoAccept *bool   `json:"auto_accept"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Note == nil && req.AutoAccept == nil {
		badRequest(w, "nothing to update")
		return
	}
	if req.Note != nil && utf8.RuneCountInString(*req.Note) > 2000 {
		badRequest(w, "note must be at most 2000 characters")
		return
	}
	contact, err := h.store.UpdateContact(r.Context(), user.ID, id, store.ContactPatch{
		Note:       req.Note,
		AutoAccept: req.AutoAccept,
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "contact not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"contact": contact})
	}
}

// DELETE /contacts/{id}
func (h *contactsHandlers) delete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid contact id")
		return
	}
	photos, err := h.store.DeleteContact(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "contact not found")
	case err != nil:
		internalError(w)
	default:
		for _, p := range photos {
			h.removePhotoFile(p)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// POST /contacts/{id}/photos — добавить фото в галерею (multipart 'file', до 5 МБ).
// Файл — под случайным именем в /uploads/contacts/, как фоны и картинки статей.
func (h *contactsHandlers) uploadPhoto(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid contact id")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		badRequest(w, "multipart field 'file' is required (max 5 MB)")
		return
	}
	defer file.Close()

	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		internalError(w)
		return
	}
	var ext string
	switch http.DetectContentType(head[:n]) {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	default:
		badRequest(w, "file must be a jpeg/png/webp image")
		return
	}

	buf := make([]byte, 16)
	rand.Read(buf)
	filename := hex.EncodeToString(buf) + ext

	dir := filepath.Join(h.dataDir, "contacts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		internalError(w)
		return
	}
	dst, err := os.Create(filepath.Join(dir, filename))
	if err != nil {
		internalError(w)
		return
	}
	defer dst.Close()
	if _, err := dst.Write(head[:n]); err != nil {
		internalError(w)
		return
	}
	if _, err := io.Copy(dst, file); err != nil {
		os.Remove(dst.Name())
		badRequest(w, "upload failed or file too large (max 5 MB)")
		return
	}

	url := "uploads/contacts/" + filename
	photo, err := h.store.AddContactPhoto(r.Context(), user.ID, id, url)
	switch {
	case errors.Is(err, store.ErrNotFound):
		os.Remove(dst.Name())
		writeError(w, http.StatusNotFound, "not_found", "contact not found")
	case errors.Is(err, store.ErrLimit):
		os.Remove(dst.Name())
		badRequest(w, "at most 20 photos per contact")
	case err != nil:
		os.Remove(dst.Name())
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"photo": photo})
	}
}

// DELETE /contacts/{id}/photos/{photoId}
func (h *contactsHandlers) deletePhoto(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid contact id")
		return
	}
	photoID, err := strconv.ParseInt(r.PathValue("photoId"), 10, 64)
	if err != nil {
		badRequest(w, "invalid photo id")
		return
	}
	old, err := h.store.DeleteContactPhoto(r.Context(), user.ID, id, photoID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "photo not found")
	case err != nil:
		internalError(w)
	default:
		h.removePhotoFile(old)
		w.WriteHeader(http.StatusNoContent)
	}
}

// removePhotoFile удаляет файл фото по хранимому пути uploads/contacts/<имя>.
func (h *contactsHandlers) removePhotoFile(url string) {
	name := filepath.Base(strings.TrimPrefix(url, "uploads/contacts/"))
	if url == "" || name == "." || name == "/" {
		return
	}
	os.Remove(filepath.Join(h.dataDir, "contacts", name))
}

// --- входящие шаринги ---

// GET /contacts/incoming
func (h *contactsHandlers) incoming(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	shares, err := h.store.ListIncomingShares(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if shares == nil {
		shares = []store.IncomingShare{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"shares": shares})
}

// POST /contacts/incoming/{id}/accept — применить шаринг (копия/доступ).
func (h *contactsHandlers) accept(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid share id")
		return
	}
	row, err := h.store.GetIncomingShare(r.Context(), id, user.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "share not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	name, err := h.store.ApplyShare(r.Context(), row.Kind, row.FromUser, row.RefID, user.ID)
	if errors.Is(err, store.ErrNotFound) {
		// источник удалён отправителем — запись больше не нужна
		_ = h.store.DeleteIncomingShare(r.Context(), id, user.ID)
		writeError(w, http.StatusNotFound, "not_found", "source was deleted by sender")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	_ = h.store.DeleteIncomingShare(r.Context(), id, user.ID)
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "kind": row.Kind})
}

// POST /contacts/incoming/{id}/decline
func (h *contactsHandlers) decline(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid share id")
		return
	}
	switch err := h.store.DeleteIncomingShare(r.Context(), id, user.ID); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "share not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
