package smtp

import (
	"net/mail"
	"strings"
	"testing"

	"github.com/TaliaMarine/mailcatcher/internal/database"
)

// --- parseAddress ---

func TestParseAddress(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantAddr string
		wantName string
	}{
		{"empty", "", "", ""},
		{"bare email", "alice@example.com", "alice@example.com", ""},
		{"with name", "Alice <alice@example.com>", "alice@example.com", "Alice"},
		{"quoted name", `"Bob Smith" <bob@example.com>`, "bob@example.com", "Bob Smith"},
		{"invalid", "not-an-email", "not-an-email", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, name := parseAddress(tt.input)
			if addr != tt.wantAddr {
				t.Errorf("addr = %q, want %q", addr, tt.wantAddr)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
		})
	}
}

// --- decodeHeader ---

func TestDecodeHeader(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain", "Hello World", "Hello World"},
		{"utf8 encoded", "=?UTF-8?B?SGVsbG8gV29ybGQ=?=", "Hello World"},
		{"iso-8859-1 Q-encoding", "=?iso-8859-1?Q?Caf=E9?=", "Café"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeHeader(tt.input)
			if got != tt.want {
				t.Errorf("decodeHeader(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- collectRecipients ---

func TestCollectRecipients_FromHeaders(t *testing.T) {
	header := mail.Header{
		"To": []string{"Alice <alice@example.com>, Bob <bob@example.com>"},
		"Cc": []string{"Carol <carol@example.com>"},
	}

	recipients := collectRecipients(header, nil)

	if len(recipients) != 3 {
		t.Fatalf("expected 3 recipients, got %d", len(recipients))
	}

	types := make(map[string]int)
	for _, r := range recipients {
		types[r.Type]++
	}
	if types["to"] != 2 {
		t.Errorf("to count = %d, want 2", types["to"])
	}
	if types["cc"] != 1 {
		t.Errorf("cc count = %d, want 1", types["cc"])
	}
}

func TestCollectRecipients_Deduplication(t *testing.T) {
	header := mail.Header{
		"To": []string{"alice@example.com"},
		"Cc": []string{"alice@example.com"},
	}

	recipients := collectRecipients(header, nil)

	if len(recipients) != 1 {
		t.Errorf("expected 1 recipient (deduplicated), got %d", len(recipients))
	}
}

func TestCollectRecipients_FallbackToEnvelope(t *testing.T) {
	// Empty headers — should fall back to envelope recipients
	header := mail.Header{}
	envelopeTo := []string{"envelope@example.com", "other@example.com"}

	recipients := collectRecipients(header, envelopeTo)

	if len(recipients) != 2 {
		t.Fatalf("expected 2 recipients from envelope, got %d", len(recipients))
	}
	for _, r := range recipients {
		if r.Type != "to" {
			t.Errorf("envelope recipient type = %q, want %q", r.Type, "to")
		}
	}
}

func TestCollectRecipients_EnvelopeFallbackDedup(t *testing.T) {
	header := mail.Header{}
	envelopeTo := []string{"dup@example.com", "DUP@EXAMPLE.COM"}

	recipients := collectRecipients(header, envelopeTo)

	if len(recipients) != 1 {
		t.Errorf("expected 1 recipient (deduplicated), got %d", len(recipients))
	}
}

func TestCollectRecipients_CaseNormalization(t *testing.T) {
	header := mail.Header{
		"To": []string{"Alice@EXAMPLE.com"},
	}

	recipients := collectRecipients(header, nil)

	if len(recipients) != 1 {
		t.Fatalf("expected 1 recipient, got %d", len(recipients))
	}
	if recipients[0].Address != "alice@example.com" {
		t.Errorf("address = %q, want lowercased", recipients[0].Address)
	}
}

// --- parseBody ---

func TestParseBody_PlainText(t *testing.T) {
	raw := "From: test@example.com\r\nSubject: Test\r\nContent-Type: text/plain\r\n\r\nHello, World!"
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	text, html, attachments, err := parseBody(msg)
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	if text != "Hello, World!" {
		t.Errorf("text = %q, want %q", text, "Hello, World!")
	}
	if html != nil {
		t.Errorf("html = %v, want nil", html)
	}
	if len(attachments) != 0 {
		t.Errorf("attachments = %d, want 0", len(attachments))
	}
}

func TestParseBody_HTMLOnly(t *testing.T) {
	raw := "From: test@example.com\r\nSubject: Test\r\nContent-Type: text/html\r\n\r\n<h1>Hello</h1>"
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	text, html, _, err := parseBody(msg)
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	if text != "" {
		t.Errorf("text = %q, want empty", text)
	}
	if html == nil || *html != "<h1>Hello</h1>" {
		t.Errorf("html = %v, want %q", html, "<h1>Hello</h1>")
	}
}

func TestParseBody_NoContentType(t *testing.T) {
	raw := "From: test@example.com\r\nSubject: Test\r\n\r\nPlain body"
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	text, html, _, err := parseBody(msg)
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	if text != "Plain body" {
		t.Errorf("text = %q, want %q", text, "Plain body")
	}
	if html != nil {
		t.Errorf("html = %v, want nil", html)
	}
}

func TestParseBody_MultipartAlternative(t *testing.T) {
	raw := "From: test@example.com\r\n" +
		"Subject: Test\r\n" +
		"Content-Type: multipart/alternative; boundary=\"boundary123\"\r\n\r\n" +
		"--boundary123\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"Plain text version\r\n" +
		"--boundary123\r\n" +
		"Content-Type: text/html\r\n\r\n" +
		"<p>HTML version</p>\r\n" +
		"--boundary123--\r\n"

	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	text, html, attachments, err := parseBody(msg)
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	if text != "Plain text version" {
		t.Errorf("text = %q, want %q", text, "Plain text version")
	}
	if html == nil || *html != "<p>HTML version</p>" {
		t.Errorf("html = %v, want %q", html, "<p>HTML version</p>")
	}
	if len(attachments) != 0 {
		t.Errorf("attachments = %d, want 0", len(attachments))
	}
}

func TestParseBody_MultipartWithAttachment(t *testing.T) {
	raw := "From: test@example.com\r\n" +
		"Subject: Test\r\n" +
		"Content-Type: multipart/mixed; boundary=\"boundary456\"\r\n\r\n" +
		"--boundary456\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"Body text\r\n" +
		"--boundary456\r\n" +
		"Content-Type: application/pdf\r\n" +
		"Content-Disposition: attachment; filename=\"report.pdf\"\r\n\r\n" +
		"fake-pdf-content\r\n" +
		"--boundary456--\r\n"

	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	text, _, attachments, err := parseBody(msg)
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	if text != "Body text" {
		t.Errorf("text = %q, want %q", text, "Body text")
	}
	if len(attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(attachments))
	}
	if attachments[0].Filename != "report.pdf" {
		t.Errorf("filename = %q, want %q", attachments[0].Filename, "report.pdf")
	}
	if attachments[0].ContentType != "application/pdf" {
		t.Errorf("content_type = %q, want %q", attachments[0].ContentType, "application/pdf")
	}
}

func TestParseBody_NestedMultipart(t *testing.T) {
	raw := "From: test@example.com\r\n" +
		"Subject: Test\r\n" +
		"Content-Type: multipart/mixed; boundary=\"outer\"\r\n\r\n" +
		"--outer\r\n" +
		"Content-Type: multipart/alternative; boundary=\"inner\"\r\n\r\n" +
		"--inner\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"Nested plain text\r\n" +
		"--inner\r\n" +
		"Content-Type: text/html\r\n\r\n" +
		"<p>Nested HTML</p>\r\n" +
		"--inner--\r\n" +
		"--outer\r\n" +
		"Content-Type: image/png\r\n" +
		"Content-Disposition: attachment; filename=\"logo.png\"\r\n\r\n" +
		"png-data\r\n" +
		"--outer--\r\n"

	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	text, html, attachments, err := parseBody(msg)
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	if text != "Nested plain text" {
		t.Errorf("text = %q, want %q", text, "Nested plain text")
	}
	if html == nil || *html != "<p>Nested HTML</p>" {
		t.Errorf("html = %v, want %q", html, "<p>Nested HTML</p>")
	}
	if len(attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(attachments))
	}
	if attachments[0].Filename != "logo.png" {
		t.Errorf("filename = %q, want %q", attachments[0].Filename, "logo.png")
	}
}

// --- session (go-smtp backend) ---

func TestSession_MailAndRcpt(t *testing.T) {
	s := &session{}

	if err := s.Mail("sender@example.com", nil); err != nil {
		t.Fatalf("Mail: %v", err)
	}
	if s.from != "sender@example.com" {
		t.Errorf("from = %q, want %q", s.from, "sender@example.com")
	}

	if err := s.Rcpt("rcpt1@example.com", nil); err != nil {
		t.Fatalf("Rcpt: %v", err)
	}
	if err := s.Rcpt("rcpt2@example.com", nil); err != nil {
		t.Fatalf("Rcpt: %v", err)
	}
	if len(s.to) != 2 {
		t.Errorf("to = %v, want 2 recipients", s.to)
	}
}

func TestSession_AuthMechanisms(t *testing.T) {
	s := &session{}
	if got := strings.Join(s.AuthMechanisms(), ","); got != "PLAIN,LOGIN" {
		t.Errorf("AuthMechanisms = %q", got)
	}
	if _, err := s.Auth("CRAM-MD5"); err == nil {
		t.Error("unknown mechanism accepted")
	}
}

func TestSession_Reset(t *testing.T) {
	s := &session{from: "test", to: []string{"a"}}
	s.Reset() // Should not panic
}

func TestSession_Logout(t *testing.T) {
	s := &session{}
	if err := s.Logout(); err != nil {
		t.Errorf("Logout: %v", err)
	}
}

func TestBackend_NewSession(t *testing.T) {
	b := &backend{handler: &Server{}}
	sess, err := b.NewSession(nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if sess == nil {
		t.Error("expected non-nil session")
	}
}

// --- Attachment with no content type defaults to octet-stream ---

func TestParseBody_AttachmentNoContentType(t *testing.T) {
	raw := "From: test@example.com\r\n" +
		"Subject: Test\r\n" +
		"Content-Type: multipart/mixed; boundary=\"bound\"\r\n\r\n" +
		"--bound\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"body\r\n" +
		"--bound\r\n" +
		"Content-Disposition: attachment; filename=\"data.bin\"\r\n\r\n" +
		"binary-data\r\n" +
		"--bound--\r\n"

	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	_, _, attachments, err := parseBody(msg)
	if err != nil {
		t.Fatalf("parseBody: %v", err)
	}
	if len(attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(attachments))
	}
	// Empty content type in part should default to application/octet-stream
	if attachments[0].ContentType != "application/octet-stream" {
		t.Errorf("content_type = %q, want %q", attachments[0].ContentType, "application/octet-stream")
	}
}

// --- collectRecipients preserves names ---

func TestCollectRecipients_PreservesNames(t *testing.T) {
	header := mail.Header{
		"To": []string{`"Alice Smith" <alice@example.com>`},
	}

	recipients := collectRecipients(header, nil)
	if len(recipients) != 1 {
		t.Fatalf("expected 1 recipient, got %d", len(recipients))
	}
	if recipients[0].Name != "Alice Smith" {
		t.Errorf("name = %q, want %q", recipients[0].Name, "Alice Smith")
	}
}

// Ensure the Recipient type matches database package expectations
func TestRecipientType(t *testing.T) {
	r := database.Recipient{
		Address: "test@example.com",
		Name:    "Test",
		Type:    "to",
	}
	if r.Type != "to" {
		t.Errorf("Type = %q, want %q", r.Type, "to")
	}
}
