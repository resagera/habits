package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"streaks-backend/internal/auth"
	"streaks-backend/internal/files"
	"streaks-backend/internal/store"

	"github.com/gorilla/websocket"
)

type filesHandlers struct {
	store   *store.Store
	hub     *files.Hub
	tickets *files.Tickets
}

var fileUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// агент — не браузер, Origin не проверяем (авторизация по токену)
	CheckOrigin: func(*http.Request) bool { return true },
}

func newMachineToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// --- CRUD машин ---

type fileMachineView struct {
	store.FileMachine
	Online bool `json:"online"`
}

func (h *filesHandlers) list(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	machines, err := h.store.ListFileMachines(r.Context(), user.ID)
	if err != nil {
		internalError(w)
		return
	}
	views := make([]fileMachineView, 0, len(machines))
	for _, m := range machines {
		views = append(views, fileMachineView{FileMachine: m, Online: h.hub.Online(m.ID)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"machines": views})
}

func (h *filesHandlers) create(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if n := len([]rune(req.Name)); n < 1 || n > 100 {
		badRequest(w, "name must be 1-100 characters")
		return
	}
	m, err := h.store.CreateFileMachine(r.Context(), user.ID, req.Name, newMachineToken())
	if err != nil {
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"machine": fileMachineView{FileMachine: m}})
}

func (h *filesHandlers) rename(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid machine id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if n := len([]rune(req.Name)); n < 1 || n > 100 {
		badRequest(w, "name must be 1-100 characters")
		return
	}
	m, err := h.store.RenameFileMachine(r.Context(), user.ID, id, req.Name)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "machine not found")
	case err != nil:
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"machine": fileMachineView{FileMachine: m, Online: h.hub.Online(m.ID)}})
	}
}

func (h *filesHandlers) delete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid machine id")
		return
	}
	switch err := h.store.DeleteFileMachine(r.Context(), user.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "machine not found")
	case err != nil:
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// ownedMachine достаёт машину пользователя по id из пути.
func (h *filesHandlers) ownedMachine(w http.ResponseWriter, r *http.Request) (store.FileMachine, bool) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "invalid machine id")
		return store.FileMachine{}, false
	}
	m, err := h.store.FileMachineOwned(r.Context(), user.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "machine not found")
		return store.FileMachine{}, false
	}
	if err != nil {
		internalError(w)
		return store.FileMachine{}, false
	}
	return m, true
}

// callAgent транслирует ошибки хаба в HTTP-ответы.
func (h *filesHandlers) callAgent(w http.ResponseWriter, r *http.Request, id int64, req files.Request) (*files.Response, bool) {
	resp, err := h.hub.Call(r.Context(), id, req)
	switch {
	case errors.Is(err, files.ErrOffline):
		writeError(w, http.StatusServiceUnavailable, "offline", "machine is offline")
	case errors.Is(err, files.ErrTimeout):
		writeError(w, http.StatusGatewayTimeout, "timeout", "agent did not respond")
	case err != nil:
		writeError(w, http.StatusBadRequest, "agent_error", err.Error())
	default:
		return resp, true
	}
	return nil, false
}

// GET /files/machines/{id}/list?path= — содержимое папки.
func (h *filesHandlers) listDir(w http.ResponseWriter, r *http.Request) {
	m, ok := h.ownedMachine(w, r)
	if !ok {
		return
	}
	resp, ok := h.callAgent(w, r, m.ID, files.Request{Op: "list", Path: r.URL.Query().Get("path")})
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp.Result)
}

// POST /files/machines/{id}/mkdir|rename|remove — операции записи.
func (h *filesHandlers) op(op string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m, ok := h.ownedMachine(w, r)
		if !ok {
			return
		}
		var body struct {
			Path  string `json:"path"`
			To    string `json:"to"`
			IsDir bool   `json:"is_dir"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid JSON body")
			return
		}
		req := files.Request{Op: op, Path: body.Path, To: body.To, IsDir: body.IsDir}
		if _, ok := h.callAgent(w, r, m.ID, req); !ok {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// POST /files/machines/{id}/upload?path=dir&name=file — загрузка файла на машину.
func (h *filesHandlers) upload(w http.ResponseWriter, r *http.Request) {
	m, ok := h.ownedMachine(w, r)
	if !ok {
		return
	}
	dir := r.URL.Query().Get("path")
	name := path.Base(r.URL.Query().Get("name"))
	if name == "" || name == "." || name == "/" {
		badRequest(w, "name is required")
		return
	}
	dst := path.Join(dir, name)
	const chunk = 512 * 1024
	buf := make([]byte, chunk)
	var offset int64
	body := io.LimitReader(r.Body, 8<<30) // 8 ГБ потолок
	for {
		n, err := io.ReadFull(body, buf)
		if n > 0 {
			req := files.Request{Op: "write", Path: dst, Offset: offset, Data: buf[:n], Trunc: false}
			if offset == 0 {
				req.Trunc = true // первая запись создаёт/очищает файл
			}
			if _, ok := h.callAgent(w, r, m.ID, req); !ok {
				return
			}
			offset += int64(n)
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "upload_error", err.Error())
			return
		}
	}
	if offset == 0 {
		// пустой файл — создаём усечением
		if _, ok := h.callAgent(w, r, m.ID, files.Request{Op: "write", Path: dst, Offset: 0, Trunc: true}); !ok {
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": dst})
}

// POST /files/machines/{id}/ticket — пропуск на стриминг файла.
func (h *filesHandlers) ticket(w http.ResponseWriter, r *http.Request) {
	m, ok := h.ownedMachine(w, r)
	if !ok {
		return
	}
	var body struct {
		Path     string `json:"path"`
		Download bool   `json:"download"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
		badRequest(w, "path is required")
		return
	}
	tok := h.tickets.Issue(m.ID, body.Path, path.Base(body.Path), body.Download)
	// стрим-URL живёт под тем же префиксом, что и API
	writeJSON(w, http.StatusOK, map[string]any{"ticket": tok})
}

// GET /files/stream/{ticket} — отдача файла с поддержкой Range. Вне
// tma-авторизации: пропуск сам является одноразово выданным секретом.
func (h *filesHandlers) stream(w http.ResponseWriter, r *http.Request) {
	tk, ok := h.tickets.Get(r.PathValue("ticket"))
	if !ok {
		http.Error(w, "ticket not found", http.StatusNotFound)
		return
	}
	// размер файла
	statResp, err := h.hub.Call(r.Context(), tk.MachineID, files.Request{Op: "stat", Path: tk.Path})
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	var st struct {
		Size  int64 `json:"size"`
		IsDir bool  `json:"is_dir"`
	}
	_ = json.Unmarshal(statResp.Result, &st)
	if st.IsDir {
		http.Error(w, "is a directory", http.StatusBadRequest)
		return
	}

	ctype := mime.TypeByExtension(strings.ToLower(path.Ext(tk.Path)))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Accept-Ranges", "bytes")
	if tk.Download {
		w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+urlEncode(tk.Name))
	}

	start, end := int64(0), st.Size-1
	status := http.StatusOK
	if rng := r.Header.Get("Range"); rng != "" && st.Size > 0 {
		s, e, ok := parseRange(rng, st.Size)
		if !ok {
			w.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(st.Size, 10))
			http.Error(w, "range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		start, end = s, e
		status = http.StatusPartialContent
		w.Header().Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(st.Size, 10))
	}
	if st.Size == 0 {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}

	const chunk = 512 * 1024
	offset := start
	for offset <= end {
		want := int64(chunk)
		if rem := end - offset + 1; rem < want {
			want = rem
		}
		resp, err := h.hub.Call(r.Context(), tk.MachineID, files.Request{Op: "read", Path: tk.Path, Offset: offset, Length: want})
		if err != nil {
			return // соединение оборвано; заголовки уже ушли
		}
		if len(resp.Binary) == 0 {
			return
		}
		if _, err := w.Write(resp.Binary); err != nil {
			return // клиент отвалился (частая ситуация при seek видео)
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		offset += int64(len(resp.Binary))
		if resp.EOF {
			return
		}
	}
}

// --- WS-приём агента (вне tma-авторизации) ---

type agentHello struct {
	Roots []store.FileRoot `json:"roots"`
}

// GET /api/v1/files/agent — апгрейд до WS, авторизация Bearer-токеном машины.
func (h *filesHandlers) agentWS(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = r.URL.Query().Get("token") // фолбэк: заголовки WS ограничены
	}
	if token == "" || len(token) > 200 {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	m, err := h.store.FileMachineByToken(r.Context(), token)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "unknown token", http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	ws, err := fileUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	// первый кадр — hello со списком доступных папок
	ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, data, err := ws.ReadMessage()
	if err != nil {
		ws.Close()
		return
	}
	var hello agentHello
	if json.Unmarshal(data, &hello) == nil {
		_ = h.store.TouchFileMachine(r.Context(), m.ID, hello.Roots)
	}
	ws.SetReadDeadline(time.Time{})
	h.hub.Serve(m.ID, ws) // блокирует до обрыва
}

// --- утилиты ---

func parseRange(header string, size int64) (start, end int64, ok bool) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(header, "bytes=")
	if strings.Contains(spec, ",") {
		return 0, 0, false // множественные диапазоны не поддерживаем
	}
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return 0, 0, false
	}
	startStr, endStr := spec[:dash], spec[dash+1:]
	switch {
	case startStr == "": // суффикс: последние N байт
		n, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true
	case endStr == "":
		s, err := strconv.ParseInt(startStr, 10, 64)
		if err != nil || s < 0 || s >= size {
			return 0, 0, false
		}
		return s, size - 1, true
	default:
		s, err1 := strconv.ParseInt(startStr, 10, 64)
		e, err2 := strconv.ParseInt(endStr, 10, 64)
		if err1 != nil || err2 != nil || s < 0 || s > e || s >= size {
			return 0, 0, false
		}
		if e >= size {
			e = size - 1
		}
		return s, e, true
	}
}

// urlEncode кодирует имя файла для Content-Disposition (RFC 5987).
func urlEncode(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			const hexd = "0123456789ABCDEF"
			b.WriteByte(hexd[c>>4])
			b.WriteByte(hexd[c&0xf])
		}
	}
	return b.String()
}
