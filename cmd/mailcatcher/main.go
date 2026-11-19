package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/TaliaMarine/mailcatcher/internal/api"
	"github.com/TaliaMarine/mailcatcher/internal/auth"
	"github.com/TaliaMarine/mailcatcher/internal/config"
	"github.com/TaliaMarine/mailcatcher/internal/credentials"
	"github.com/TaliaMarine/mailcatcher/internal/database"
	imapsrv "github.com/TaliaMarine/mailcatcher/internal/imap"
	"github.com/TaliaMarine/mailcatcher/internal/kube"
	"github.com/TaliaMarine/mailcatcher/internal/retention"
	smtpsrv "github.com/TaliaMarine/mailcatcher/internal/smtp"
	"github.com/TaliaMarine/mailcatcher/internal/tlsutil"
	"github.com/TaliaMarine/mailcatcher/internal/websocket"
	"k8s.io/client-go/kubernetes"
)

func main() {
	cfg := config.Load()
	logger := setupLogger(cfg)
	if err := run(cfg, logger); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(cfg *config.Config, logger *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Database
	db, err := database.Open(cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Migrate(); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	logger.Info("database ready", "path", cfg.DatabasePath)

	// Kubernetes (optional): session key and IMAP credentials live in Secrets.
	var kubeClient kubernetes.Interface
	namespace := cfg.Namespace
	if kube.InCluster() {
		if kubeClient, err = kube.NewClient(); err != nil {
			return fmt.Errorf("kubernetes client: %w", err)
		}
		if namespace, err = kube.Namespace(cfg.Namespace); err != nil {
			return err
		}
	}

	oidcEnabled := !cfg.AuthDisabled && cfg.OIDCIssuer != ""
	var sessionKey []byte
	if oidcEnabled {
		sessionKey, err = auth.LoadSessionKey(ctx, cfg.SessionKey, kubeClient, namespace, cfg.SessionKeySecretName, cfg.SessionKeySecretKey, logger)
		if err != nil {
			return fmt.Errorf("session key: %w", err)
		}
	}

	var creds credentials.Store
	if cfg.IMAPEnabled {
		if creds, err = newCredentialStore(cfg, kubeClient, namespace, logger); err != nil {
			return err
		}
	}

	hub := websocket.NewHub(logger, nil)

	// TLS is shared by IMAPS, SMTPS and STARTTLS.
	var tlsCfg *tls.Config
	if cfg.TLSCertFile != "" || cfg.IMAPEnabled || cfg.SMTPTLSPort != 0 {
		if tlsCfg, err = tlsutil.Config(cfg.TLSCertFile, cfg.TLSKeyFile, logger); err != nil {
			return fmt.Errorf("tls: %w", err)
		}
	}

	errc := make(chan error, 8)
	serve := func(name string, f func() error) {
		go func() {
			if err := f(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errc <- fmt.Errorf("%s: %w", name, err)
			}
		}()
	}

	// SMTP
	smtpServer := smtpsrv.NewServer(fmt.Sprintf(":%d", cfg.SMTPPort), db, hub, logger, tlsCfg)
	serve("smtp", smtpServer.ListenAndServe)
	var smtpsServer *smtpsrv.Server
	if cfg.SMTPTLSPort != 0 {
		smtpsServer = smtpsrv.NewServer(fmt.Sprintf(":%d", cfg.SMTPTLSPort), db, hub, logger, tlsCfg)
		serve("smtps", smtpsServer.ListenAndServeTLS)
	}

	// IMAP
	var imapServer *imapsrv.Server
	if cfg.IMAPEnabled {
		imapServer = imapsrv.New(imapsrv.Options{DB: db, Store: creds, Hub: hub, TLS: tlsCfg, Logger: logger})
		if cfg.IMAPPort != 0 {
			ln, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.IMAPPort))
			if err != nil {
				return fmt.Errorf("imap listen: %w", err)
			}
			logger.Info("IMAP (STARTTLS) server starting", "addr", ln.Addr().String())
			serve("imap", func() error { return imapServer.Serve(ln) })
		}
		if cfg.IMAPTLSPort != 0 {
			ln, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.IMAPTLSPort))
			if err != nil {
				return fmt.Errorf("imaps listen: %w", err)
			}
			logger.Info("IMAPS server starting", "addr", ln.Addr().String())
			serve("imaps", func() error { return imapServer.ServeTLS(ln) })
		}
	}

	// Static frontend files
	var staticFS fs.FS
	if stat, err := os.Stat("web/dist"); err == nil && stat.IsDir() {
		staticFS = os.DirFS("web/dist")
		logger.Info("serving frontend from web/dist")
	}

	// HTTP server (Gin + Huma + WS + metrics + health + static)
	a, err := api.New(cfg, api.Deps{
		DB: db, Hub: hub, Logger: logger, StaticFS: staticFS,
		Credentials: creds, SessionKey: sessionKey,
	})
	if err != nil {
		return fmt.Errorf("initialize API: %w", err)
	}
	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           a.Router,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	logger.Info("HTTP server starting", "addr", httpServer.Addr)
	serve("http", httpServer.ListenAndServe)

	// Retention worker
	go retention.NewWorker(db, cfg.RetentionDays, logger).Start(ctx)

	select {
	case <-ctx.Done():
	case err := <-errc:
		logger.Error("server failed, shutting down", "error", err)
		defer func() { os.Exit(1) }()
	}

	logger.Info("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for name, f := range map[string]func() error{
		"smtp": func() error { return smtpServer.Shutdown(shutdownCtx) },
		"http": func() error { return httpServer.Shutdown(shutdownCtx) },
	} {
		if err := f(); err != nil {
			logger.Error("shutdown error", "server", name, "error", err)
		}
	}
	if smtpsServer != nil {
		_ = smtpsServer.Shutdown(shutdownCtx)
	}
	if imapServer != nil {
		_ = imapServer.Close()
	}
	logger.Info("shutdown complete")
	return nil
}

func newCredentialStore(cfg *config.Config, client kubernetes.Interface, namespace string, logger *slog.Logger) (credentials.Store, error) {
	kind := cfg.CredentialStore
	if kind == "" {
		kind = "memory"
		if client != nil {
			kind = "kubernetes"
		}
	}
	if kind == "kubernetes" {
		if client == nil {
			return nil, errors.New("CREDENTIAL_STORE=kubernetes requires running in a cluster")
		}
		logger.Info("IMAP credentials stored in Kubernetes Secrets", "namespace", namespace, "prefix", cfg.CredentialPrefix)
		return credentials.NewKubernetes(client, namespace, cfg.CredentialPrefix), nil
	}
	logger.Warn("IMAP credentials kept in memory only: they are lost on restart")
	return credentials.NewMemory(), nil
}

func setupLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.LogFormat == "plain" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}
