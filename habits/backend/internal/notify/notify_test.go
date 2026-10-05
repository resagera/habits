package notify

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Файл уезжает в Telegram как multipart: chat_id, подпись и сам документ с
// именем — по этому имени его потом и видно в переписке.
func TestSendDocument(t *testing.T) {
	var gotChat, gotCaption, gotName, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/sendDocument") {
			t.Errorf("не тот метод API: %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("не multipart: %v", err)
		}
		gotChat = r.FormValue("chat_id")
		gotCaption = r.FormValue("caption")
		f, head, err := r.FormFile("document")
		if err != nil {
			t.Fatalf("нет документа: %v", err)
		}
		defer f.Close()
		gotName = head.Filename
		data, _ := io.ReadAll(f)
		gotBody = string(data)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	b := &Bot{Token: "test-token", APIBase: srv.URL, Logger: slog.Default()}
	err := b.SendDocument(context.Background(), 4242, "копия.zip", "вот копия",
		strings.NewReader("содержимое архива"))
	if err != nil {
		t.Fatalf("отправка: %v", err)
	}
	if gotChat != "4242" || gotCaption != "вот копия" || gotName != "копия.zip" || gotBody != "содержимое архива" {
		t.Fatalf("ушло не то: chat=%q caption=%q name=%q body=%q", gotChat, gotCaption, gotName, gotBody)
	}

	// отказ Telegram должен дойти до зовущего, а не потеряться
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"ok":false,"description":"file is too big"}`, http.StatusBadRequest)
	}))
	defer bad.Close()
	b.APIBase = bad.URL
	if err := b.SendDocument(context.Background(), 1, "x.zip", "", strings.NewReader("x")); err == nil {
		t.Fatal("отказ Telegram проглочен")
	}

	// без токена (dev) ничего не шлём, но и не падаем
	b2 := &Bot{Logger: slog.Default()}
	if err := b2.SendDocument(context.Background(), 1, "x.zip", "", strings.NewReader("x")); err != nil {
		t.Fatalf("dev-режим: %v", err)
	}
}
