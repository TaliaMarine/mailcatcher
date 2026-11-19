package imap

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TaliaMarine/mailcatcher/internal/credentials"
	"github.com/TaliaMarine/mailcatcher/internal/database"
	"github.com/TaliaMarine/mailcatcher/internal/tlsutil"
	ws "github.com/TaliaMarine/mailcatcher/internal/websocket"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

const rawMail = "From: Alice <alice@example.com>\r\nTo: bob@example.com\r\nSubject: Hello IMAP\r\nDate: Mon, 02 Jan 2006 15:04:05 +0000\r\nMessage-ID: <1@example.com>\r\n\r\nThis is the body.\r\n"

type env struct {
	db    *database.DB
	srv   *Server
	addr  string // implicit TLS
	plain string
}

func setup(t *testing.T) *env {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	db, err := database.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, _, err := db.StoreMessage(context.Background(), database.StoreMessageInput{
		MessageID: "<1@example.com>", FromAddress: "alice@example.com", Subject: "Hello IMAP",
		BodyText: "This is the body.", Raw: []byte(rawMail), Size: len(rawMail),
		Recipients: []database.Recipient{{Address: "bob@example.com", Type: "to"}},
	}); err != nil {
		t.Fatal(err)
	}

	store := credentials.NewMemory()
	if _, err := store.Set(context.Background(), credentials.Owner{Subject: "s"}, "tester", "correct horse battery"); err != nil {
		t.Fatal(err)
	}

	tlsCfg, err := tlsutil.Config("", "", logger)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Options{DB: db, Store: store, Hub: ws.NewHub(logger, nil), TLS: tlsCfg, Logger: logger})
	t.Cleanup(func() { _ = srv.Close() })

	lnTLS, _ := net.Listen("tcp", "127.0.0.1:0")
	lnPlain, _ := net.Listen("tcp", "127.0.0.1:0")
	go func() { _ = srv.ServeTLS(lnTLS) }()
	go func() { _ = srv.Serve(lnPlain) }()
	return &env{db: db, srv: srv, addr: lnTLS.Addr().String(), plain: lnPlain.Addr().String()}
}

var insecure = &tls.Config{InsecureSkipVerify: true}

func dialTLS(t *testing.T, e *env) *imapclient.Client {
	t.Helper()
	c, err := imapclient.DialTLS(e.addr, &imapclient.Options{TLSConfig: insecure})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestLoginOverImplicitTLS(t *testing.T) {
	e := setup(t)

	bad := dialTLS(t, e)
	if err := bad.Login("tester", "wrong password!!").Wait(); err == nil {
		t.Fatal("wrong password accepted")
	}

	c := dialTLS(t, e)
	if err := c.Login("tester", "correct horse battery").Wait(); err != nil {
		t.Fatalf("login: %v", err)
	}
}

func TestPlainLoginRefusedBeforeSTARTTLS(t *testing.T) {
	e := setup(t)
	c, err := imapclient.DialInsecure(e.plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if err := c.Login("tester", "correct horse battery").Wait(); err == nil {
		t.Fatal("cleartext login accepted")
	}
}

func TestSTARTTLSLogin(t *testing.T) {
	e := setup(t)
	c, err := imapclient.DialStartTLS(e.plain, &imapclient.Options{TLSConfig: insecure})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if err := c.Login("tester", "correct horse battery").Wait(); err != nil {
		t.Fatalf("login after STARTTLS: %v", err)
	}
}

func TestListSelectFetchSearchDelete(t *testing.T) {
	e := setup(t)
	c := dialTLS(t, e)
	if err := c.Login("tester", "correct horse battery").Wait(); err != nil {
		t.Fatal(err)
	}

	boxes, err := c.List("", "*", nil).Collect()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range boxes {
		names = append(names, b.Mailbox)
	}
	if strings.Join(names, ",") != "INBOX,bob@example.com" {
		t.Fatalf("folders = %v", names)
	}

	sel, err := c.Select("INBOX", nil).Wait()
	if err != nil || sel.NumMessages != 1 {
		t.Fatalf("select = %+v, %v", sel, err)
	}

	msgs, err := c.Fetch(imap.SeqSetNum(1), &imap.FetchOptions{
		UID: true, Envelope: true, RFC822Size: true, Flags: true,
		BodySection: []*imap.FetchItemBodySection{{Peek: true}},
	}).Collect()
	if err != nil || len(msgs) != 1 {
		t.Fatalf("fetch = %v, %v", msgs, err)
	}
	m := msgs[0]
	if m.Envelope == nil || m.Envelope.Subject != "Hello IMAP" {
		t.Errorf("envelope = %+v", m.Envelope)
	}
	if got := string(m.FindBodySection(&imap.FetchItemBodySection{Peek: true})); got != rawMail {
		t.Errorf("body = %q", got)
	}
	if m.RFC822Size != int64(len(rawMail)) {
		t.Errorf("size = %d", m.RFC822Size)
	}

	// A per-recipient folder holds the same message.
	if sel, err := c.Select("bob@example.com", nil).Wait(); err != nil || sel.NumMessages != 1 {
		t.Fatalf("select recipient folder = %+v, %v", sel, err)
	}
	if _, err := c.Select("nobody@example.com", nil).Wait(); err == nil {
		t.Error("selecting an unknown folder succeeded")
	}

	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	found, err := c.Search(&imap.SearchCriteria{Body: []string{"the body"}}, nil).Wait()
	if err != nil || len(found.AllSeqNums()) != 1 {
		t.Fatalf("search body = %+v, %v", found, err)
	}
	found, _ = c.Search(&imap.SearchCriteria{Text: []string{"no such text"}}, nil).Wait()
	if len(found.AllSeqNums()) != 0 {
		t.Error("search matched nothing-text")
	}

	// \Deleted + EXPUNGE removes the message from the database.
	if err := c.Store(imap.SeqSetNum(1), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Expunge().Collect(); err != nil {
		t.Fatalf("expunge: %v", err)
	}
	ids, _ := e.db.ListMessageIDs(context.Background(), 0)
	if len(ids) != 0 {
		t.Errorf("message still in database: %v", ids)
	}
}

func TestReadOnlyOperationsRefused(t *testing.T) {
	e := setup(t)
	c := dialTLS(t, e)
	if err := c.Login("tester", "correct horse battery").Wait(); err != nil {
		t.Fatal(err)
	}
	if err := c.Create("new-folder", nil).Wait(); err == nil {
		t.Error("CREATE accepted")
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := setup(t)
	for i := 0; i < maxFailures; i++ {
		c := dialTLS(t, e)
		_ = c.Login("tester", "wrong password!!").Wait()
	}
	c := dialTLS(t, e)
	// Even the right password is refused once the address is blocked.
	if err := c.Login("tester", "correct horse battery").Wait(); err == nil {
		t.Fatal("login allowed while rate limited")
	}
}
