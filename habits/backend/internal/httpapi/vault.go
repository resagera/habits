package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/notify"
	"streaks-backend/internal/store"
)

// Сейф. Сервер хранит шифротекст и конверты ключей и не может ничего
// расшифровать: пароль остаётся в браузере. Отсюда особенности:
//   - тип файла не проверить (внутри случайные байты), остаются только лимиты;
//   - имя файла приходит уже зашифрованным (meta_env), сервер его не читает;
//   - блобы отдаются через API, а не статикой: нужна проверка доступа.
type vaultHandlers struct {
	store   *store.Store
	bot     *notify.Bot
	dataDir string

	mu      sync.Mutex
	uploads map[string]*vaultUpload
}

// vaultUpload — незавершённая загрузка. Живёт в памяти: перезапуск сервера
// обрывает загрузку, клиент начинает заново (файлы небольшие, это дешевле
// хранения состояния в базе).
type vaultUpload struct {
	UserID    int64
	FolderID  int64
	OwnerID   int64
	PlainSize int64
	ChunkSize int32
	Path      string
	Written   int64
	Started   time.Time
}

const (
	maxVaultChunk     = 8 << 20 // потолок одного чанка (клиент шлёт 4 МБ)
	maxVaultThumb     = 512 << 10
	uploadTTL         = time.Hour
	vaultEnvelopeMax  = 4096 // конверты ключей и метаданных — base64, всегда мелкие
	maxAutoDeleteDays = 3650
	maxLinkTTL        = 30 * 24 * time.Hour
	vaultSweepEvery   = time.Hour
)

// Токен временной ссылки: 32 случайных байта в base64url. Перебирать нечего,
// поэтому отдельного ограничения по частоте на публичной ручке нет.
var linkTokenRe = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func (h *vaultHandlers) dir() string { return filepath.Join(h.dataDir, "vault") }

func (h *vaultHandlers) tmpDir() string { return filepath.Join(h.dir(), "tmp") }

func randomName(prefix string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(buf) + ".bin", nil
}

func validEnvelope(s string) bool {
	if s == "" || len(s) > vaultEnvelopeMax {
		return false
	}
	_, err := base64.StdEncoding.DecodeString(s)
	return err == nil
}

// GET /vault — дерево папок, файлы и квота одним ответом: страница всё равно
// показывает их вместе, а отдельные запросы дали бы мигание пустого экрана.
func (h *vaultHandlers) tree(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	folders, err := h.store.ListVaultFolders(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	files, err := h.store.ListVaultFiles(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	quota, err := h.store.VaultQuotaFor(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	if folders == nil {
		folders = []store.VaultFolder{}
	}
	if files == nil {
		files = []store.VaultFile{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": folders, "files": files, "quota": quota})
}

func (h *vaultHandlers) createFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		ParentID   *int64 `json:"parent_id"`
		Name       string `json:"name"`
		Hint       string `json:"hint"`
		Thumbs     bool   `json:"thumbs"`
		KdfSalt    string `json:"kdf_salt"`
		KdfIter    int32  `json:"kdf_iter"`
		WrappedKey string `json:"wrapped_key"`
		WrapIV     string `json:"wrap_iv"`
		// косметика и срок жизни — необязательные
		HideChildren   bool  `json:"hide_children"`
		AutoDeleteDays int32 `json:"auto_delete_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if n := utf8.RuneCountInString(req.Name); n < 1 || n > 100 {
		badRequest(w, "name must be 1-100 characters")
		return
	}
	if utf8.RuneCountInString(req.Hint) > 200 {
		badRequest(w, "hint is too long")
		return
	}
	if !validEnvelope(req.WrappedKey) || !validEnvelope(req.WrapIV) || !validEnvelope(req.KdfSalt) {
		badRequest(w, "invalid key material")
		return
	}
	if req.KdfIter < 100_000 || req.KdfIter > 2_000_000 {
		badRequest(w, "kdf_iter out of range")
		return
	}
	if req.AutoDeleteDays < 0 || req.AutoDeleteDays > maxAutoDeleteDays {
		badRequest(w, "auto_delete_days out of range")
		return
	}
	folder, err := h.store.CreateVaultFolder(r.Context(), user.ID, store.VaultFolder{
		ParentID: req.ParentID, Name: req.Name, Hint: req.Hint, Thumbs: req.Thumbs,
		HideChildren: req.HideChildren, AutoDeleteDays: req.AutoDeleteDays,
		KdfSalt: req.KdfSalt, KdfIter: req.KdfIter, WrappedKey: req.WrappedKey, WrapIV: req.WrapIV,
	})
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"folder": folder})
}

func (h *vaultHandlers) updateFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid folder id")
		return
	}
	var req struct {
		Name *string `json:"name"`
		Hint *string `json:"hint"`
		// смена пароля: обёртка ключа приходит целиком
		KdfSalt        *string `json:"kdf_salt"`
		KdfIter        *int32  `json:"kdf_iter"`
		WrappedKey     *string `json:"wrapped_key"`
		WrapIV         *string `json:"wrap_iv"`
		HideChildren   *bool   `json:"hide_children"`
		AutoDeleteDays *int32  `json:"auto_delete_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if n := utf8.RuneCountInString(name); n < 1 || n > 100 {
			badRequest(w, "name must be 1-100 characters")
			return
		}
		req.Name = &name
	}
	var wrap *store.VaultFolder
	if req.WrappedKey != nil || req.KdfSalt != nil || req.WrapIV != nil || req.KdfIter != nil {
		if req.WrappedKey == nil || req.KdfSalt == nil || req.WrapIV == nil || req.KdfIter == nil {
			badRequest(w, "key material must be sent together")
			return
		}
		if !validEnvelope(*req.WrappedKey) || !validEnvelope(*req.WrapIV) || !validEnvelope(*req.KdfSalt) {
			badRequest(w, "invalid key material")
			return
		}
		wrap = &store.VaultFolder{
			KdfSalt: *req.KdfSalt, KdfIter: *req.KdfIter,
			WrappedKey: *req.WrappedKey, WrapIV: *req.WrapIV,
		}
	}
	if req.AutoDeleteDays != nil && (*req.AutoDeleteDays < 0 || *req.AutoDeleteDays > maxAutoDeleteDays) {
		badRequest(w, "auto_delete_days out of range")
		return
	}
	if req.Name == nil && req.Hint == nil && wrap == nil && req.HideChildren == nil && req.AutoDeleteDays == nil {
		badRequest(w, "nothing to update")
		return
	}
	folder, err := h.store.UpdateVaultFolder(r.Context(), user.ID, id, req.Name, req.Hint,
		req.HideChildren, req.AutoDeleteDays, wrap)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "folder not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"folder": folder})
	}
}

func (h *vaultHandlers) deleteFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid folder id")
		return
	}
	names, err := h.store.DeleteVaultFolder(r.Context(), user.ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "folder not found")
		return
	case err != nil:
		internalError(w)
		return
	}
	for _, n := range names {
		_ = os.Remove(filepath.Join(h.dir(), n))
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /vault/uploads — начало загрузки: проверяем лимиты ДО того, как на
// диск попадёт хоть байт, и заводим временный файл.
func (h *vaultHandlers) initUpload(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		FolderID  int64 `json:"folder_id"`
		PlainSize int64 `json:"plain_size"`
		ChunkSize int32 `json:"chunk_size"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.PlainSize <= 0 || req.ChunkSize <= 0 || req.ChunkSize > maxVaultChunk {
		badRequest(w, "invalid size")
		return
	}
	ok, ownerID, err := h.store.CanWriteVaultFolder(r.Context(), user.ID, req.FolderID)
	if err != nil {
		internalError(w)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "folder not found")
		return
	}
	// квота считается владельцу папки: расшаренная папка расходует его место
	quota, err := h.store.VaultQuotaFor(r.Context(), ownerID)
	if err != nil {
		internalError(w)
		return
	}
	if req.PlainSize > quota.FileLimit {
		writeError(w, http.StatusRequestEntityTooLarge, "file_too_large",
			"файл больше лимита на один файл")
		return
	}
	if quota.Used+req.PlainSize > quota.TotalLimit {
		writeError(w, http.StatusRequestEntityTooLarge, "quota_exceeded",
			"не хватает места в сейфе")
		return
	}

	if err := os.MkdirAll(h.tmpDir(), 0o755); err != nil {
		internalError(w)
		return
	}
	h.sweepUploads()
	name, err := randomName("u_")
	if err != nil {
		internalError(w)
		return
	}
	path := filepath.Join(h.tmpDir(), name)
	f, err := os.Create(path)
	if err != nil {
		internalError(w)
		return
	}
	f.Close()

	idBuf := make([]byte, 12)
	if _, err := rand.Read(idBuf); err != nil {
		internalError(w)
		return
	}
	id := hex.EncodeToString(idBuf)
	h.mu.Lock()
	h.uploads[id] = &vaultUpload{
		UserID: user.ID, FolderID: req.FolderID, OwnerID: ownerID,
		PlainSize: req.PlainSize, ChunkSize: req.ChunkSize, Path: path, Started: time.Now(),
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"upload_id": id})
}

// sweepUploads выбрасывает брошенные загрузки: клиент мог закрыть страницу.
func (h *vaultHandlers) sweepUploads() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, u := range h.uploads {
		if time.Since(u.Started) > uploadTTL {
			_ = os.Remove(u.Path)
			delete(h.uploads, id)
		}
	}
}

func (h *vaultHandlers) upload(r *http.Request) (*vaultUpload, bool) {
	user := auth.UserFromContext(r.Context())
	h.mu.Lock()
	defer h.mu.Unlock()
	u, ok := h.uploads[r.PathValue("uid")]
	if !ok || u.UserID != user.ID {
		return nil, false
	}
	return u, true
}

// POST /vault/uploads/{uid}/chunk — сырые байты шифроблока в тело запроса.
func (h *vaultHandlers) uploadChunk(w http.ResponseWriter, r *http.Request) {
	u, ok := h.upload(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "upload not found")
		return
	}
	// потолок на один чанк: шифротекст длиннее исходного на тег GCM
	r.Body = http.MaxBytesReader(w, r.Body, int64(u.ChunkSize)+64<<10)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		badRequest(w, "chunk is too large")
		return
	}
	// общий потолок: шифротекст не может быть заметно больше исходника
	if u.Written+int64(len(data)) > u.PlainSize+16<<20 {
		badRequest(w, "upload exceeds declared size")
		return
	}
	f, err := os.OpenFile(u.Path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		internalError(w)
		return
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		internalError(w)
		return
	}
	h.mu.Lock()
	u.Written += int64(len(data))
	written := u.Written
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"written": written})
}

// POST /vault/uploads/{uid}/finish — конверты ключа и метаданных, превью.
func (h *vaultHandlers) finishUpload(w http.ResponseWriter, r *http.Request) {
	u, ok := h.upload(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "upload not found")
		return
	}
	var req struct {
		KeyEnv  string `json:"key_env"`
		MetaEnv string `json:"meta_env"`
		Thumb   string `json:"thumb"` // base64 шифроблоба превью, необязательно
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if !validEnvelope(req.KeyEnv) || !validEnvelope(req.MetaEnv) {
		badRequest(w, "invalid envelopes")
		return
	}
	// повторная проверка квоты: между init и finish могли добавиться файлы
	quota, err := h.store.VaultQuotaFor(r.Context(), u.OwnerID)
	if err != nil {
		internalError(w)
		return
	}
	if quota.Used+u.PlainSize > quota.TotalLimit {
		h.dropUpload(r.PathValue("uid"))
		writeError(w, http.StatusRequestEntityTooLarge, "quota_exceeded", "не хватает места в сейфе")
		return
	}

	blobName, err := randomName("v_")
	if err != nil {
		internalError(w)
		return
	}
	if err := os.Rename(u.Path, filepath.Join(h.dir(), blobName)); err != nil {
		internalError(w)
		return
	}
	thumbName := ""
	if req.Thumb != "" {
		raw, err := base64.StdEncoding.DecodeString(req.Thumb)
		if err != nil || len(raw) > maxVaultThumb {
			badRequest(w, "invalid thumb")
			return
		}
		thumbName, err = randomName("t_")
		if err != nil {
			internalError(w)
			return
		}
		if err := os.WriteFile(filepath.Join(h.dir(), thumbName), raw, 0o644); err != nil {
			thumbName = "" // без превью переживём, файл важнее
		}
	}
	file, err := h.store.CreateVaultFile(r.Context(), u.OwnerID, store.VaultFile{
		FolderID: u.FolderID, Size: u.Written, PlainSize: u.PlainSize,
		KeyEnv: req.KeyEnv, MetaEnv: req.MetaEnv, ChunkSize: u.ChunkSize,
	}, blobName, thumbName)
	if err != nil {
		internalError(w)
		return
	}
	h.dropUpload(r.PathValue("uid"))
	writeJSON(w, http.StatusCreated, map[string]any{"file": file})
}

func (h *vaultHandlers) dropUpload(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if u, ok := h.uploads[id]; ok {
		_ = os.Remove(u.Path)
		delete(h.uploads, id)
	}
}

// GET /vault/files/{id}/blob и /thumb — шифротекст. Через API, а не статикой:
// доступ проверяется, а имя файла на диске случайное и наружу не светится.
func (h *vaultHandlers) blob(thumb bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			badRequest(w, "invalid file id")
			return
		}
		file, blobName, thumbName, err := h.store.VaultFileAccess(r.Context(), user.ID, id)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "file not found")
			return
		}
		if err != nil {
			internalError(w)
			return
		}
		name := blobName
		if thumb {
			name = thumbName
		}
		if name == "" {
			writeError(w, http.StatusNotFound, "not_found", "file not found")
			return
		}
		// журнал — только чужие обращения и только к самому файлу: превью
		// подгружается пачкой при открытии папки и засорило бы журнал
		if !thumb && file.OwnerID != user.ID {
			uid := user.ID
			if err := h.store.LogVaultAccess(r.Context(), id, &uid, "share"); err != nil {
				// журнал не повод не отдать файл
				_ = err
			}
		}
		f, err := os.Open(filepath.Join(h.dir(), name))
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "file not found")
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			internalError(w)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		http.ServeContent(w, r, "", st.ModTime(), f)
	}
}

func (h *vaultHandlers) updateFile(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid file id")
		return
	}
	var req struct {
		MetaEnv  *string `json:"meta_env"`
		KeyEnv   *string `json:"key_env"`
		FolderID *int64  `json:"folder_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if req.MetaEnv == nil && req.FolderID == nil {
		badRequest(w, "nothing to update")
		return
	}
	if req.MetaEnv != nil && !validEnvelope(*req.MetaEnv) {
		badRequest(w, "invalid meta envelope")
		return
	}
	// перенос в другую папку: ключ содержимого должен приехать перевёрнутым
	// под ключ новой папки, иначе файл станет нечитаемым
	if req.FolderID != nil {
		if req.KeyEnv == nil || !validEnvelope(*req.KeyEnv) {
			badRequest(w, "key_env is required when moving a file")
			return
		}
		ok, _, err := h.store.CanWriteVaultFolder(r.Context(), user.ID, *req.FolderID)
		if err != nil {
			internalError(w)
			return
		}
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "folder not found")
			return
		}
	}
	file, err := h.store.UpdateVaultFile(r.Context(), user.ID, id, req.MetaEnv, req.KeyEnv, req.FolderID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "file not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"file": file})
	}
}

// POST /vault/files/delete — удаление пачкой (и одного файла тоже).
// Корзины нет: место в сейфе освобождается сразу.
func (h *vaultHandlers) deleteFiles(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		badRequest(w, "ids are required")
		return
	}
	if len(req.IDs) > 500 {
		badRequest(w, "too many ids")
		return
	}
	names, deleted, err := h.store.DeleteVaultFiles(r.Context(), user.ID, req.IDs)
	if err != nil {
		internalError(w)
		return
	}
	for _, n := range names {
		_ = os.Remove(filepath.Join(h.dir(), n))
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

// --- шаринг ---

func (h *vaultHandlers) share(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			badRequest(w, "invalid id")
			return
		}
		var req struct {
			To string `json:"to"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.To) == "" {
			badRequest(w, "to (user id or @username) is required")
			return
		}
		recipient, err := h.store.FindUserExact(r.Context(), strings.TrimSpace(req.To))
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		if err != nil {
			internalError(w)
			return
		}
		if recipient.ID == user.ID {
			badRequest(w, "cannot share with yourself")
			return
		}
		queued, name, err := deliverShare(r.Context(), h.store, h.bot, user, recipient.ID, "vault_"+kind, id)
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "not found")
		case err != nil:
			internalError(w)
		default:
			writeJSON(w, http.StatusOK, map[string]any{
				"queued": queued, "name": name, "shared_with": recipient})
		}
	}
}

func (h *vaultHandlers) listShares(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			badRequest(w, "invalid id")
			return
		}
		users, err := h.store.ListVaultShares(r.Context(), user.ID, kind, id)
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "not found")
		case err != nil:
			internalError(w)
		default:
			if users == nil {
				users = []store.AccessUser{}
			}
			writeJSON(w, http.StatusOK, map[string]any{"users": users})
		}
	}
}

func (h *vaultHandlers) revokeShare(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			badRequest(w, "invalid id")
			return
		}
		target, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
		if err != nil {
			badRequest(w, "invalid user id")
			return
		}
		switch err := h.store.RevokeVaultShare(r.Context(), user.ID, kind, id, target); {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "share not found")
		case err != nil:
			internalError(w)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// --- копирование, срок жизни, журнал ---

// POST /vault/files/{id}/copy — копия файла в другой папке. Байты на диске
// копируются как есть: ключ содержимого приезжает уже перевёрнутым под ключ
// целевой папки, перешифровывать нечего.
func (h *vaultHandlers) copyFile(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid file id")
		return
	}
	var req struct {
		FolderID int64  `json:"folder_id"`
		KeyEnv   string `json:"key_env"`
		MetaEnv  string `json:"meta_env"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if !validEnvelope(req.KeyEnv) || !validEnvelope(req.MetaEnv) {
		badRequest(w, "invalid envelopes")
		return
	}
	src, blobName, thumbName, err := h.store.VaultFileAccess(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "file not found")
		return
	}
	if err != nil {
		internalError(w)
		return
	}
	ok, ownerID, err := h.store.CanWriteVaultFolder(r.Context(), user.ID, req.FolderID)
	if err != nil {
		internalError(w)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "folder not found")
		return
	}
	// копия занимает место заново — квота владельца целевой папки
	quota, err := h.store.VaultQuotaFor(r.Context(), ownerID)
	if err != nil {
		internalError(w)
		return
	}
	if quota.Used+src.PlainSize > quota.TotalLimit {
		writeError(w, http.StatusRequestEntityTooLarge, "quota_exceeded", "не хватает места в сейфе")
		return
	}
	newBlob, err := randomName("v_")
	if err != nil {
		internalError(w)
		return
	}
	if err := copyFileOnDisk(filepath.Join(h.dir(), blobName), filepath.Join(h.dir(), newBlob)); err != nil {
		internalError(w)
		return
	}
	newThumb := ""
	if thumbName != "" {
		if n, err := randomName("t_"); err == nil {
			if err := copyFileOnDisk(filepath.Join(h.dir(), thumbName), filepath.Join(h.dir(), n)); err == nil {
				newThumb = n
			}
		}
	}
	file, err := h.store.CopyVaultFile(r.Context(), ownerID, src, req.FolderID,
		req.KeyEnv, req.MetaEnv, newBlob, newThumb)
	if err != nil {
		_ = os.Remove(filepath.Join(h.dir(), newBlob))
		if newThumb != "" {
			_ = os.Remove(filepath.Join(h.dir(), newThumb))
		}
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"file": file})
}

func copyFileOnDisk(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		_ = os.Remove(to)
		return err
	}
	return dst.Close()
}

// POST /vault/files/expiry — самоуничтожение через N дней (0 — снять срок).
func (h *vaultHandlers) setExpiry(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		IDs  []int64 `json:"ids"`
		Days int32   `json:"days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		badRequest(w, "ids are required")
		return
	}
	if len(req.IDs) > 500 || req.Days < 0 || req.Days > maxAutoDeleteDays {
		badRequest(w, "invalid request")
		return
	}
	n, err := h.store.SetVaultFileExpiry(r.Context(), user.ID, req.IDs, req.Days)
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": n})
}

// GET /vault/files/{id}/access — кто открывал файл. Только владельцу.
func (h *vaultHandlers) accessLog(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid file id")
		return
	}
	entries, err := h.store.ListVaultAccess(r.Context(), user.ID, id)
	if err != nil {
		internalError(w)
		return
	}
	if entries == nil {
		entries = []store.VaultAccessEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

// GET /vault/uploads/{uid} — сколько байт уже принято: клиент так продолжает
// прерванную загрузку. Чанк пишется целиком или никак (тело читается в
// память до записи), поэтому счётчик всегда стоит на границе чанка.
func (h *vaultHandlers) uploadStatus(w http.ResponseWriter, r *http.Request) {
	u, ok := h.upload(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "upload not found")
		return
	}
	h.mu.Lock()
	written := u.Written
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"written": written})
}

// --- временные ссылки ---

// POST /vault/files/{id}/links — ссылка на один файл. Токен виден один раз:
// в базе только его SHA-256.
func (h *vaultHandlers) createLink(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid file id")
		return
	}
	var req struct {
		KdfSalt   string `json:"kdf_salt"`
		KdfIter   int32  `json:"kdf_iter"`
		KeyEnv    string `json:"key_env"`
		MetaEnv   string `json:"meta_env"`
		TTLMinute int32  `json:"ttl_minutes"`
		MaxViews  int32  `json:"max_views"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	if !validEnvelope(req.KdfSalt) || !validEnvelope(req.KeyEnv) || !validEnvelope(req.MetaEnv) {
		badRequest(w, "invalid key material")
		return
	}
	if req.KdfIter < 100_000 || req.KdfIter > 2_000_000 {
		badRequest(w, "kdf_iter out of range")
		return
	}
	ttl := time.Duration(req.TTLMinute) * time.Minute
	if ttl <= 0 || ttl > maxLinkTTL {
		badRequest(w, "ttl_minutes out of range")
		return
	}
	if req.MaxViews < 0 || req.MaxViews > 1000 {
		badRequest(w, "max_views out of range")
		return
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		internalError(w)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(token))
	link, err := h.store.CreateVaultLink(r.Context(), user.ID, hex.EncodeToString(sum[:]), store.VaultLink{
		FileID: id, KdfSalt: req.KdfSalt, KdfIter: req.KdfIter,
		KeyEnv: req.KeyEnv, MetaEnv: req.MetaEnv,
		ExpiresAt: time.Now().Add(ttl), MaxViews: req.MaxViews,
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "file not found")
	case err != nil:
		internalError(w)
	default:
		// путь, а не полный URL: домен знает фронтенд, а мы можем стоять за прокси
		writeJSON(w, http.StatusCreated, map[string]any{"link": link, "path": "l/" + token})
	}
}

func (h *vaultHandlers) listLinks(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid file id")
		return
	}
	links, err := h.store.ListVaultLinks(r.Context(), user.ID, id)
	if err != nil {
		internalError(w)
		return
	}
	if links == nil {
		links = []store.VaultLink{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links})
}

func (h *vaultHandlers) revokeLink(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid link id")
		return
	}
	switch err := h.store.RevokeVaultLink(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "link not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// linkFromPath — общая часть публичных ручек. Просроченная, исчерпанная,
// отозванная и несуществующая ссылка дают одинаковый 404: по ответу нельзя
// понять, был ли когда-нибудь такой файл.
func (h *vaultHandlers) linkFromPath(r *http.Request, consume bool) (store.VaultLink, store.VaultFile, string, bool) {
	token := r.PathValue("token")
	if !linkTokenRe.MatchString(token) {
		return store.VaultLink{}, store.VaultFile{}, "", false
	}
	sum := sha256.Sum256([]byte(token))
	link, file, blob, err := h.store.VaultLinkByToken(r.Context(), hex.EncodeToString(sum[:]), consume)
	if err != nil {
		return link, file, "", false
	}
	return link, file, blob, true
}

// GET /vault/public/links/{token} — вне авторизации: параметры расшифровки
// для страницы ссылки. Без пароля они бесполезны, сервер его не знает.
func (h *vaultHandlers) publicLink(w http.ResponseWriter, r *http.Request) {
	link, file, _, ok := h.linkFromPath(r, false)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "link not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kdf_salt": link.KdfSalt, "kdf_iter": link.KdfIter,
		"key_env": link.KeyEnv, "meta_env": link.MetaEnv,
		"plain_size": file.PlainSize, "chunk_size": file.ChunkSize,
		"expires_at": link.ExpiresAt,
	})
}

// GET /vault/public/links/{token}/blob — шифротекст по ссылке. Открытие
// засчитывается здесь, а не на загрузке страницы: иначе счётчик тратился бы
// на тех, кто пароля так и не ввёл.
func (h *vaultHandlers) publicLinkBlob(w http.ResponseWriter, r *http.Request) {
	link, _, blobName, ok := h.linkFromPath(r, true)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "link not found")
		return
	}
	if err := h.store.LogVaultAccess(r.Context(), link.FileID, nil, "link"); err != nil {
		_ = err // журнал не повод не отдать файл
	}
	f, err := os.Open(filepath.Join(h.dir(), blobName))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "link not found")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		internalError(w)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// sweep — уборщик: просроченные файлы, ссылки и старые записи журнала.
// Запускается фоном при старте приложения.
func (h *vaultHandlers) sweep(ctx context.Context) {
	for {
		names, _, err := h.store.SweepVaultFiles(ctx)
		if err == nil {
			for _, n := range names {
				_ = os.Remove(filepath.Join(h.dir(), n))
			}
		}
		_ = h.store.SweepVaultLinks(ctx)
		h.sweepUploads()
		select {
		case <-ctx.Done():
			return
		case <-time.After(vaultSweepEvery):
		}
	}
}
