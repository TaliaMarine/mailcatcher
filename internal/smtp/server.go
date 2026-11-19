package smtp

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"time"

	"github.com/TaliaMarine/mailcatcher/internal/database"
	ws "github.com/TaliaMarine/mailcatcher/internal/websocket"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

const maxMessageSize = 10 * 1024 * 1024 // 10 MB

type Server struct {
	server *smtp.Server
	db     *database.DB
	hub    *ws.Hub
	logger *slog.Logger
}

// NewServer creates an SMTP server. A non-nil tlsConfig enables STARTTLS and
// allows ListenAndServeTLS (implicit TLS).
func NewServer(addr string, db *database.DB, hub *ws.Hub, logger *slog.Logger, tlsConfig *tls.Config) *Server {
	s := &Server{db: db, hub: hub, logger: logger}

	srv := smtp.NewServer(&backend{handler: s})
	srv.Addr = addr
	srv.MaxMessageBytes = maxMessageSize
	srv.MaxRecipients = 1000
	srv.ReadTimeout = 2 * time.Minute
	srv.WriteTimeout = 2 * time.Minute
	srv.TLSConfig = tlsConfig
	// A catcher accepts any credentials, so plain-text AUTH is harmless and
	// lets applications that insist on authenticating work without TLS.
	srv.AllowInsecureAuth = true

	s.server = srv
	return s
}

func (s *Server) ListenAndServe() error {
	s.logger.Info("SMTP server starting", "addr", s.server.Addr)
	return s.server.ListenAndServe()
}

// ListenAndServeTLS serves implicit-TLS connections (SMTPS).
func (s *Server) ListenAndServeTLS() error {
	s.logger.Info("SMTPS server starting", "addr", s.server.Addr)
	return s.server.ListenAndServeTLS()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) handleMessage(from string, to []string, rawData []byte) error {
	msg, err := mail.ReadMessage(bytes.NewReader(rawData))
	if err != nil {
		return fmt.Errorf("parse email: %w", err)
	}

	fromAddr, fromName := parseAddress(msg.Header.Get("From"))
	if fromAddr == "" {
		fromAddr = from
	}

	subject := decodeHeader(msg.Header.Get("Subject"))
	messageID := msg.Header.Get("Message-Id")

	recipients := collectRecipients(msg.Header, to)

	bodyText, bodyHTML, attachments, err := parseBody(msg)
	if err != nil {
		return fmt.Errorf("parse body: %w", err)
	}

	input := database.StoreMessageInput{
		MessageID:   messageID,
		FromAddress: fromAddr,
		FromName:    fromName,
		Subject:     subject,
		BodyText:    bodyText,
		BodyHTML:    bodyHTML,
		Raw:         rawData,
		Size:        len(rawData),
		Recipients:  recipients,
		Attachments: attachments,
	}

	storedMsg, mailboxIDs, err := s.db.StoreMessage(context.Background(), input)
	if err != nil {
		return fmt.Errorf("store message: %w", err)
	}

	s.logger.Info("message received",
		"message_id", storedMsg.ID,
		"from", fromAddr,
		"subject", subject,
		"recipients", len(recipients),
		"mailboxes", mailboxIDs,
	)

	s.hub.Broadcast(ws.Event{
		Type: "message:new",
		Data: map[string]any{
			"message_id":  storedMsg.ID,
			"mailbox_ids": mailboxIDs,
		},
	})

	return nil
}

func collectRecipients(header mail.Header, envelopeTo []string) []database.Recipient {
	seen := make(map[string]bool)
	var recipients []database.Recipient

	addRecipients := func(field, recipientType string) {
		addrs, err := header.AddressList(field)
		if err != nil {
			return
		}
		for _, a := range addrs {
			addr := strings.ToLower(a.Address)
			if seen[addr] {
				continue
			}
			seen[addr] = true
			recipients = append(recipients, database.Recipient{
				Address: addr,
				Name:    a.Name,
				Type:    recipientType,
			})
		}
	}

	addRecipients("To", "to")
	addRecipients("Cc", "cc")
	addRecipients("Bcc", "bcc")

	// Fall back to SMTP envelope if no headers parsed
	if len(recipients) == 0 {
		for _, addr := range envelopeTo {
			addr = strings.ToLower(addr)
			if seen[addr] {
				continue
			}
			seen[addr] = true
			recipients = append(recipients, database.Recipient{
				Address: addr,
				Name:    "",
				Type:    "to",
			})
		}
	}

	return recipients
}

func parseAddress(raw string) (string, string) {
	if raw == "" {
		return "", ""
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return raw, ""
	}
	return addr.Address, addr.Name
}

func decodeHeader(raw string) string {
	dec := new(mime.WordDecoder)
	decoded, err := dec.DecodeHeader(raw)
	if err != nil {
		return raw
	}
	return decoded
}

func parseBody(msg *mail.Message) (text string, html *string, attachments []database.StoreAttachment, err error) {
	contentType := msg.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain"
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		// Treat as plain text
		body, _ := io.ReadAll(msg.Body)
		return string(body), nil, nil, nil
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		return parseMultipart(msg.Body, params["boundary"])
	}

	body, err := io.ReadAll(msg.Body)
	if err != nil {
		return "", nil, nil, fmt.Errorf("read body: %w", err)
	}

	switch {
	case strings.Contains(mediaType, "text/html"):
		h := string(body)
		return "", &h, nil, nil
	default:
		return string(body), nil, nil, nil
	}
}

func parseMultipart(r io.Reader, boundary string) (text string, html *string, attachments []database.StoreAttachment, err error) {
	mr := multipart.NewReader(r, boundary)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return text, html, attachments, fmt.Errorf("next part: %w", err)
		}

		partContentType := part.Header.Get("Content-Type")
		disposition := part.Header.Get("Content-Disposition")
		mediaType, params, _ := mime.ParseMediaType(partContentType)

		body, err := io.ReadAll(part)
		if err != nil {
			return text, html, attachments, fmt.Errorf("read part: %w", err)
		}

		// Nested multipart
		if strings.HasPrefix(mediaType, "multipart/") {
			nestedText, nestedHTML, nestedAttachments, err := parseMultipart(bytes.NewReader(body), params["boundary"])
			if err == nil {
				if nestedText != "" && text == "" {
					text = nestedText
				}
				if nestedHTML != nil && html == nil {
					html = nestedHTML
				}
				attachments = append(attachments, nestedAttachments...)
			}
			continue
		}

		// Attachment
		if strings.HasPrefix(disposition, "attachment") || (part.FileName() != "") {
			ct := partContentType
			if ct == "" {
				ct = "application/octet-stream"
			}
			attachments = append(attachments, database.StoreAttachment{
				Filename:    part.FileName(),
				ContentType: ct,
				Content:     body,
			})
			continue
		}

		// Body parts
		switch {
		case strings.Contains(mediaType, "text/plain"):
			if text == "" {
				text = string(body)
			}
		case strings.Contains(mediaType, "text/html"):
			if html == nil {
				h := string(body)
				html = &h
			}
		}
	}
	return text, html, attachments, nil
}

// -- go-smtp backend/session --

type backend struct {
	handler *Server
}

func (b *backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &session{handler: b.handler}, nil
}

type session struct {
	handler *Server
	from    string
	to      []string
}

// AuthMechanisms advertises AUTH so mail clients that refuse to send without
// it (Apple Mail, Outlook) work. Any credentials are accepted: this is a catcher.
func (s *session) AuthMechanisms() []string { return []string{sasl.Plain, sasl.Login} }

func (s *session) Auth(mech string) (sasl.Server, error) {
	switch mech {
	case sasl.Plain:
		return sasl.NewPlainServer(func(identity, username, password string) error { return nil }), nil
	case sasl.Login:
		return &loginServer{}, nil
	}
	return nil, smtp.ErrAuthUnknownMechanism
}
func (s *session) Mail(from string, opts *smtp.MailOptions) error {
	s.from = from
	return nil
}
func (s *session) Rcpt(to string, opts *smtp.RcptOptions) error {
	s.to = append(s.to, to)
	return nil
}
func (s *session) Data(r io.Reader) error {
	data, err := io.ReadAll(r) // go-smtp enforces MaxMessageBytes
	if err != nil {
		return fmt.Errorf("read data: %w", err)
	}
	return s.handler.handleMessage(s.from, s.to, data)
}
func (s *session) Reset()        {}
func (s *session) Logout() error { return nil }

// loginServer implements the legacy LOGIN mechanism (username, then password
// prompts) and accepts anything.
type loginServer struct{ step int }

func (l *loginServer) Next(response []byte) (challenge []byte, done bool, err error) {
	l.step++
	switch l.step {
	case 1:
		return []byte("Username:"), false, nil
	case 2:
		return []byte("Password:"), false, nil
	}
	return nil, true, nil
}
