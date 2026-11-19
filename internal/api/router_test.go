package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/TaliaMarine/mailcatcher/internal/config"
	"github.com/TaliaMarine/mailcatcher/internal/credentials"
	"github.com/TaliaMarine/mailcatcher/internal/database"
	ws "github.com/TaliaMarine/mailcatcher/internal/websocket"
)

func testAPI(t *testing.T) (*API, *database.DB) {
	t.Helper()
	return testAPIWithConfig(t, &config.Config{
		AuthDisabled: true,
		LogFormat:    "json",
	})
}

func testAPIWithToken(t *testing.T, token string) (*API, *database.DB) {
	t.Helper()
	return testAPIWithConfig(t, &config.Config{
		AuthDisabled: false,
		AuthToken:    token,
		LogFormat:    "json",
	})
}

func testAPIWithConfig(t *testing.T, cfg *config.Config) (*API, *database.DB) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	hub := ws.NewHub(logger, nil)

	a, err := New(cfg, Deps{DB: db, Hub: hub, Logger: logger})
	if err != nil {
		t.Fatalf("New API: %v", err)
	}

	return a, db
}

func storeTestMessage(t *testing.T, db *database.DB, subject string, recipients []database.Recipient) (*database.Message, []int64) {
	t.Helper()
	html := "<p>Hello</p>"
	input := database.StoreMessageInput{
		MessageID:   "test-msg-id@example.com",
		FromAddress: "sender@example.com",
		FromName:    "Sender",
		Subject:     subject,
		BodyText:    "Hello, World!",
		BodyHTML:    &html,
		Raw:         []byte("raw email content"),
		Size:        100,
		Recipients:  recipients,
	}
	msg, mailboxIDs, err := db.StoreMessage(t.Context(), input)
	if err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}
	return msg, mailboxIDs
}

// --- Health endpoints ---

func TestHealthz(t *testing.T) {
	a, _ := testAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if body := w.Body.String(); body != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestReadyz(t *testing.T) {
	a, _ := testAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// --- Mailbox endpoints ---

func TestListMailboxes_Empty(t *testing.T) {
	a, _ := testAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mailboxes", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body []database.MailboxWithStats
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("expected 0 mailboxes, got %d", len(body))
	}
}

func TestListMailboxes_WithData(t *testing.T) {
	a, db := testAPI(t)

	storeTestMessage(t, db, "Test", []database.Recipient{
		{Address: "alice@example.com", Name: "Alice", Type: "to"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mailboxes", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body []database.MailboxWithStats
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("expected 1 mailbox, got %d", len(body))
	}
	if body[0].Address != "alice@example.com" {
		t.Errorf("address = %q, want %q", body[0].Address, "alice@example.com")
	}
}

func TestGetMailbox_NotFound(t *testing.T) {
	a, _ := testAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mailboxes/99999", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestGetMailbox_Found(t *testing.T) {
	a, db := testAPI(t)

	_, mbIDs := storeTestMessage(t, db, "Test", []database.Recipient{
		{Address: "found@example.com", Name: "Found", Type: "to"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mailboxes/"+itoa(mbIDs[0]), nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestDeleteMailbox(t *testing.T) {
	a, db := testAPI(t)

	_, mbIDs := storeTestMessage(t, db, "Del", []database.Recipient{
		{Address: "del@example.com", Name: "Del", Type: "to"},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/mailboxes/"+itoa(mbIDs[0]), nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent && w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 or 204", w.Code)
	}
}

func TestDeleteAllMailboxes(t *testing.T) {
	a, db := testAPI(t)

	storeTestMessage(t, db, "A", []database.Recipient{{Address: "a@example.com", Name: "A", Type: "to"}})
	storeTestMessage(t, db, "B", []database.Recipient{{Address: "b@example.com", Name: "B", Type: "to"}})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/mailboxes", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent && w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 or 204", w.Code)
	}
}

// --- Message endpoints ---

func TestListAllMessages(t *testing.T) {
	a, db := testAPI(t)

	storeTestMessage(t, db, "Msg 1", []database.Recipient{{Address: "a@example.com", Name: "A", Type: "to"}})
	storeTestMessage(t, db, "Msg 2", []database.Recipient{{Address: "b@example.com", Name: "B", Type: "to"}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body []database.MessageSummary
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(body) != 2 {
		t.Errorf("expected 2 messages, got %d", len(body))
	}
}

func TestListMessages_ByMailbox(t *testing.T) {
	a, db := testAPI(t)

	_, mbIDs := storeTestMessage(t, db, "Inbox", []database.Recipient{{Address: "inbox@example.com", Name: "Inbox", Type: "to"}})
	storeTestMessage(t, db, "Other", []database.Recipient{{Address: "other@example.com", Name: "Other", Type: "to"}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mailboxes/"+itoa(mbIDs[0])+"/messages", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body []database.MessageSummary
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(body) != 1 {
		t.Errorf("expected 1 message, got %d", len(body))
	}
}

func TestGetMessage_NotFound(t *testing.T) {
	a, _ := testAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages/99999", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestGetMessage_Found(t *testing.T) {
	a, db := testAPI(t)

	msg, _ := storeTestMessage(t, db, "Detail", []database.Recipient{{Address: "d@example.com", Name: "D", Type: "to"}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages/"+itoa(msg.ID), nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body MessageDetail
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if body.Subject != "Detail" {
		t.Errorf("subject = %q, want %q", body.Subject, "Detail")
	}
	if len(body.Recipients) != 1 {
		t.Errorf("recipients = %d, want 1", len(body.Recipients))
	}
}

func TestGetMessageRaw(t *testing.T) {
	a, db := testAPI(t)

	msg, _ := storeTestMessage(t, db, "Raw", []database.Recipient{{Address: "r@example.com", Name: "R", Type: "to"}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages/"+itoa(msg.ID)+"/raw", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body, _ := io.ReadAll(w.Body)
	if string(body) != "raw email content" {
		t.Errorf("raw body = %q, want %q", string(body), "raw email content")
	}
}

func TestGetMessageRaw_NotFound(t *testing.T) {
	a, _ := testAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages/99999/raw", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestDeleteMessage(t *testing.T) {
	a, db := testAPI(t)

	msg, _ := storeTestMessage(t, db, "Delete", []database.Recipient{{Address: "del@example.com", Name: "Del", Type: "to"}})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/messages/"+itoa(msg.ID), nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent && w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 or 204", w.Code)
	}

	// Verify deleted
	req = httptest.NewRequest(http.MethodGet, "/api/v1/messages/"+itoa(msg.ID), nil)
	w = httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", w.Code)
	}
}

func TestDeleteMessage_NotFound(t *testing.T) {
	a, _ := testAPI(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/messages/99999", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestBulkDeleteMessages(t *testing.T) {
	a, db := testAPI(t)

	msg1, _ := storeTestMessage(t, db, "Bulk 1", []database.Recipient{{Address: "b@example.com", Name: "B", Type: "to"}})
	msg2, _ := storeTestMessage(t, db, "Bulk 2", []database.Recipient{{Address: "b@example.com", Name: "B", Type: "to"}})

	body := `{"ids":[` + itoa(msg1.ID) + `,` + itoa(msg2.ID) + `]}`
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent && w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 or 204", w.Code)
	}
}

// --- Attachments ---

func TestListAttachments(t *testing.T) {
	a, db := testAPI(t)

	msg, _ := storeTestMessage(t, db, "No Attach", []database.Recipient{{Address: "a@example.com", Name: "A", Type: "to"}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages/"+itoa(msg.ID)+"/attachments", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body []database.AttachmentMeta
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("expected 0 attachments, got %d", len(body))
	}
}

// --- Search ---

func TestSearchMessages(t *testing.T) {
	a, db := testAPI(t)

	storeTestMessage(t, db, "Welcome aboard", []database.Recipient{{Address: "s@example.com", Name: "S", Type: "to"}})
	storeTestMessage(t, db, "Invoice #42", []database.Recipient{{Address: "s@example.com", Name: "S", Type: "to"}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=invoice", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body []database.MessageSummary
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(body) != 1 {
		t.Errorf("expected 1 search result, got %d", len(body))
	}
}

func TestSearchMessages_MissingQuery(t *testing.T) {
	a, _ := testAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	// Huma should return 422 for missing required query param
	if w.Code != http.StatusUnprocessableEntity && w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 422 or 400", w.Code)
	}
}

// --- Metrics ---

func TestMetrics(t *testing.T) {
	a, _ := testAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// --- User ---

func TestGetMe_AuthDisabledReturnsDevUser(t *testing.T) {
	a, _ := testAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "Local developer") {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestGetMe_TokenAuthHasNoUser(t *testing.T) {
	a, _ := testAPIWithToken(t, "tok")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// --- CSRF ---

func TestCrossOriginWriteRejected(t *testing.T) {
	a, _ := testAPI(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/mailboxes", nil)
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

// --- IMAP credentials ---

func imapAPI(t *testing.T) *API {
	t.Helper()
	cfg := &config.Config{AuthDisabled: true, LogFormat: "json", IMAPEnabled: true, IMAPPublicHost: "mail.example", IMAPPublicTLSPort: 993, SMTPPublicPort: 25, SMTPPublicTLSPort: 465}
	db, err := database.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, err := New(cfg, Deps{DB: db, Hub: ws.NewHub(logger, nil), Logger: logger, Credentials: credentials.NewMemory()})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func doJSON(a *API, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)
	return w
}

func TestIMAPCredentialsLifecycle(t *testing.T) {
	a := imapAPI(t)

	w := doJSON(a, http.MethodGet, "/api/v1/imap/credentials", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"configured":false`) {
		t.Fatalf("initial GET = %d %s", w.Code, w.Body.String())
	}

	w = doJSON(a, http.MethodPut, "/api/v1/imap/credentials", `{"username":"dev.user","password":"a long enough password"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"username":"dev.user"`) || strings.Contains(w.Body.String(), "long enough") {
		t.Fatalf("PUT = %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"smtp_port":25`) || !strings.Contains(w.Body.String(), `"smtp_tls_port":465`) {
		t.Errorf("smtp ports missing: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"host":"mail.example"`) {
		t.Errorf("host missing: %s", w.Body.String())
	}

	w = doJSON(a, http.MethodPut, "/api/v1/imap/credentials", `{"username":"Bad Name","password":"a long enough password"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid username status = %d", w.Code)
	}

	w = doJSON(a, http.MethodDelete, "/api/v1/imap/credentials", "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d", w.Code)
	}
	w = doJSON(a, http.MethodGet, "/api/v1/imap/credentials", "")
	if !strings.Contains(w.Body.String(), `"configured":false`) {
		t.Errorf("still configured: %s", w.Body.String())
	}
}

func TestIMAPCredentialsRequireOIDCUser(t *testing.T) {
	cfg := &config.Config{AuthToken: "tok", LogFormat: "json", IMAPEnabled: true}
	db, _ := database.Open(filepath.Join(t.TempDir(), "t.db"))
	_ = db.Migrate()
	t.Cleanup(func() { _ = db.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, err := New(cfg, Deps{DB: db, Hub: ws.NewHub(logger, nil), Logger: logger, Credentials: credentials.NewMemory()})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/imap/credentials", nil)
	req.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("token client status = %d, want 403", w.Code)
	}
}

func TestMessageHTMLHasSandboxCSP(t *testing.T) {
	a, db := testAPI(t)
	h := "<b>hi</b><script>alert(1)</script>"
	msg, _, err := db.StoreMessage(context.Background(), database.StoreMessageInput{
		Subject: "s", BodyHTML: &h, Raw: []byte("x"), Recipients: []database.Recipient{{Address: "a@b.c", Type: "to"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	w := doJSON(a, http.MethodGet, "/api/v1/messages/"+strconv.FormatInt(msg.ID, 10)+"/html", "")
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "sandbox") || !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("CSP = %q", csp)
	}
}

// --- Token auth ---

func TestTokenAuth_ValidToken(t *testing.T) {
	a, db := testAPIWithToken(t, "test-secret-token")

	storeTestMessage(t, db, "Token Test", []database.Recipient{
		{Address: "t@example.com", Name: "T", Type: "to"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestTokenAuth_InvalidToken(t *testing.T) {
	a, _ := testAPIWithToken(t, "test-secret-token")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestTokenAuth_NoToken(t *testing.T) {
	a, _ := testAPIWithToken(t, "test-secret-token")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestTokenAuth_DocsPublic(t *testing.T) {
	a, _ := testAPIWithToken(t, "test-secret-token")

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestTokenAuth_HealthPublic(t *testing.T) {
	a, _ := testAPIWithToken(t, "test-secret-token")

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// --- External messages endpoint ---

func TestListExternalMessages_Empty(t *testing.T) {
	a, _ := testAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/message", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body []ExternalMessageResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("expected 0 messages, got %d", len(body))
	}
}

func TestListExternalMessages_WithData(t *testing.T) {
	a, db := testAPI(t)

	storeTestMessage(t, db, "Test email", []database.Recipient{
		{Address: "recipient@example.com", Name: "Recipient", Type: "to"},
	})
	storeTestMessage(t, db, "Test email 2", []database.Recipient{
		{Address: "recipient2@example.com", Name: "Recipient2", Type: "to"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/message", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body []ExternalMessageResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(body))
	}

	// Messages ordered by created_at DESC
	msg := body[len(body)-1] // oldest
	if msg.Sender != "<sender@example.com> size=100" {
		t.Errorf("sender = %q, want %q", msg.Sender, "<sender@example.com> size=100")
	}
	if len(msg.Recipients) != 1 || msg.Recipients[0] != "<recipient@example.com>" {
		t.Errorf("recipients = %v, want [<recipient@example.com>]", msg.Recipients)
	}
	if msg.Subject != "Test email" {
		t.Errorf("subject = %q, want %q", msg.Subject, "Test email")
	}
	if msg.Size != "100" {
		t.Errorf("size = %q, want %q", msg.Size, "100")
	}
}

func TestListExternalMessages_MultipleRecipients(t *testing.T) {
	a, db := testAPI(t)

	storeTestMessage(t, db, "Multi", []database.Recipient{
		{Address: "a@example.com", Name: "A", Type: "to"},
		{Address: "b@example.com", Name: "B", Type: "cc"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/message", nil)
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body []ExternalMessageResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("expected 1 message, got %d", len(body))
	}
	if len(body[0].Recipients) != 2 {
		t.Errorf("expected 2 recipients, got %d", len(body[0].Recipients))
	}
}

func TestListExternalMessages_WithTokenAuth(t *testing.T) {
	a, db := testAPIWithToken(t, "my-token")

	storeTestMessage(t, db, "Auth Test", []database.Recipient{
		{Address: "x@example.com", Name: "X", Type: "to"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/message", nil)
	req.Header.Set("Authorization", "Bearer my-token")
	w := httptest.NewRecorder()
	a.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body []ExternalMessageResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(body) != 1 {
		t.Errorf("expected 1 message, got %d", len(body))
	}
}

// --- helper ---

func itoa(i int64) string {
	return strconv.FormatInt(i, 10)
}
