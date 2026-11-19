// Package imap serves the captured mail over IMAP. It is read-mostly: clients
// can list, fetch, search and delete messages, but not upload or move them.
//
// Folders: INBOX holds every message; each recipient mailbox is also a folder
// named after its address. Message UIDs are the database ids, which are never
// reused. Flags (\Seen etc.) live only as long as the IMAP session; deleting a
// message (\Deleted + EXPUNGE) removes it for everyone.
package imap

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/TaliaMarine/mailcatcher/internal/credentials"
	"github.com/TaliaMarine/mailcatcher/internal/database"
	ws "github.com/TaliaMarine/mailcatcher/internal/websocket"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
)

const (
	delim = '/'
	inbox = "INBOX"

	maxFailures   = 10
	failureWindow = 15 * time.Minute
)

// Options configure the server.
type Options struct {
	DB     *database.DB
	Store  credentials.Store
	Hub    *ws.Hub
	TLS    *tls.Config
	Logger *slog.Logger
}

// Server is the IMAP server.
type Server struct {
	db      *database.DB
	store   credentials.Store
	hub     *ws.Hub
	tls     *tls.Config
	logger  *slog.Logger
	srv     *imapserver.Server
	limiter *failureLimiter
}

// New creates an IMAP server. Call Serve for each listener.
func New(o Options) *Server {
	s := &Server{
		db:      o.DB,
		store:   o.Store,
		hub:     o.Hub,
		tls:     o.TLS,
		logger:  o.Logger,
		limiter: newFailureLimiter(maxFailures, failureWindow),
	}
	s.srv = imapserver.New(&imapserver.Options{
		NewSession: func(c *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return &session{srv: s, remote: remoteIP(c.NetConn()), flags: map[int64]map[imap.Flag]struct{}{}}, nil, nil
		},
		Caps:      imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapUIDPlus: {}},
		TLSConfig: o.TLS, // enables STARTTLS on plain listeners
		Logger:    slogPrinter{o.Logger},
		// InsecureAuth stays false: LOGIN is refused until the connection is
		// protected by TLS (implicit or STARTTLS).
	})
	return s
}

// Serve serves plain connections that must upgrade with STARTTLS before login.
func (s *Server) Serve(ln net.Listener) error { return s.srv.Serve(ln) }

// ServeTLS serves implicit-TLS connections (IMAPS).
func (s *Server) ServeTLS(ln net.Listener) error { return s.srv.Serve(tls.NewListener(ln, s.tls)) }

// Close stops the listeners and drops open connections.
func (s *Server) Close() error { return s.srv.Close() }

type slogPrinter struct{ l *slog.Logger }

func (p slogPrinter) Printf(format string, args ...any) {
	p.l.Warn(fmt.Sprintf(format, args...), "component", "imap")
}

func remoteIP(c net.Conn) string {
	if c == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(c.RemoteAddr().String())
	if err != nil {
		return c.RemoteAddr().String()
	}
	return host
}

// failureLimiter blocks an address after too many failed logins in a window.
type failureLimiter struct {
	max    int
	window time.Duration

	mu       sync.Mutex
	failures map[string][]time.Time
}

func newFailureLimiter(max int, window time.Duration) *failureLimiter {
	return &failureLimiter{max: max, window: window, failures: map[string][]time.Time{}}
}

func (l *failureLimiter) prune(ip string, now time.Time) []time.Time {
	kept := l.failures[ip][:0]
	for _, t := range l.failures[ip] {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.failures, ip)
	} else {
		l.failures[ip] = kept
	}
	return kept
}

func (l *failureLimiter) blocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(ip, time.Now())) >= l.max
}

func (l *failureLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.prune(ip, now)
	l.failures[ip] = append(l.failures[ip], now)
}

func (l *failureLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, ip)
}

func (s *Server) ctx() context.Context { return context.Background() }
