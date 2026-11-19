package smtp

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TaliaMarine/mailcatcher/internal/database"
	"github.com/TaliaMarine/mailcatcher/internal/tlsutil"
	ws "github.com/TaliaMarine/mailcatcher/internal/websocket"
	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"
)

type loginClient struct{ user, pass string }

func (c *loginClient) Start() (string, []byte, error) { return "LOGIN", nil, nil }
func (c *loginClient) Next(challenge []byte) ([]byte, error) {
	if strings.HasPrefix(strings.ToLower(string(challenge)), "user") {
		return []byte(c.user), nil
	}
	return []byte(c.pass), nil
}

// Mail clients such as Apple Mail refuse to send unless the server advertises
// AUTH, so the catcher must offer it and accept any credentials.
func TestSMTPSAdvertisesAndAcceptsAuth(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := database.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	tlsCfg, err := tlsutil.Config("", "", logger)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer("", db, ws.NewHub(logger, nil), logger, tlsCfg)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.server.Serve(ln) }()
	t.Cleanup(func() { _ = srv.server.Close() })

	for name, auth := range map[string]sasl.Client{
		"PLAIN": sasl.NewPlainClient("", "anyone", "anything"),
		"LOGIN": &loginClient{"anyone", "anything"},
	} {
		t.Run(name, func(t *testing.T) {
			conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{InsecureSkipVerify: true})
			if err != nil {
				t.Fatal(err)
			}
			c := gosmtp.NewClient(conn)
			defer func() { _ = c.Close() }()
			if err := c.Hello("localhost"); err != nil {
				t.Fatal(err)
			}
			if ok, _ := c.Extension("AUTH"); !ok {
				t.Fatal("AUTH not advertised")
			}
			if err := c.Auth(auth); err != nil {
				t.Fatalf("auth: %v", err)
			}
			msg := "From: a@example.com\r\nTo: b@example.com\r\nSubject: via " + name + "\r\n\r\nhi\r\n"
			if err := c.SendMail("a@example.com", []string{"b@example.com"}, strings.NewReader(msg)); err != nil {
				t.Fatalf("send: %v", err)
			}
		})
	}

	ids, err := db.ListMessageIDs(context.Background(), 0)
	if err != nil || len(ids) != 2 {
		t.Fatalf("stored messages = %v, %v", ids, err)
	}
}
