package api

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/TaliaMarine/mailcatcher/internal/auth"
	"github.com/TaliaMarine/mailcatcher/internal/config"
	"github.com/TaliaMarine/mailcatcher/internal/credentials"
	"github.com/TaliaMarine/mailcatcher/internal/database"
	ws "github.com/TaliaMarine/mailcatcher/internal/websocket"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type API struct {
	Router *gin.Engine
	cfg    *config.Config
	db     *database.DB
	hub    *ws.Hub
	creds  credentials.Store
	logger *slog.Logger
}

// Deps are the collaborators the API needs.
type Deps struct {
	DB          *database.DB
	Hub         *ws.Hub
	Logger      *slog.Logger
	StaticFS    fs.FS
	Credentials credentials.Store // nil disables IMAP credential management
	SessionKey  []byte            // required when OIDC is enabled
}

// devUser stands in for a signed-in user when authentication is disabled, so
// per-user features keep working in local development.
var devUser = auth.User{Subject: "local-dev", Name: "Local developer", PreferredUsername: "dev", Email: "dev@localhost"}

func New(cfg *config.Config, d Deps) (*API, error) {
	logger := d.Logger
	if cfg.LogFormat == "json" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery(), SlogMiddleware(logger), securityHeaders())

	a := &API{Router: router, cfg: cfg, db: d.DB, hub: d.Hub, creds: d.Credentials, logger: logger}

	// Health and metrics — always public, no auth
	router.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	router.GET("/readyz", func(c *gin.Context) {
		if err := d.DB.Ping(c.Request.Context()); err != nil {
			c.String(http.StatusServiceUnavailable, "not ready")
			return
		}
		c.String(http.StatusOK, "ok")
	})
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Reject cross-origin state-changing requests before anything else.
	baseHost := ""
	if u, err := url.Parse(cfg.BaseURL); err == nil {
		baseHost = u.Host
	}
	router.Use(auth.CSRFMiddleware(baseHost))
	d.Hub.SetOriginCheck(func(r *http.Request) bool { return auth.SameOrigin(r, baseHost) })

	// Authentication setup
	var oidcMiddleware gin.HandlerFunc

	if !cfg.AuthDisabled && cfg.OIDCIssuer != "" {
		oidcAuth, err := auth.New(context.Background(), auth.Options{
			Issuer:       cfg.OIDCIssuer,
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCClientSecret,
			BaseURL:      cfg.BaseURL,
			SessionKey:   d.SessionKey,
			Logger:       logger,
		})
		if err != nil {
			return nil, fmt.Errorf("oidc setup: %w", err)
		}

		oidcAuth.Register(router) // login, callback and logout are public
		oidcMiddleware = oidcAuth.Middleware()
		logger.Info("OIDC authentication enabled", "issuer", cfg.OIDCIssuer, "public_client", cfg.OIDCClientSecret == "")
	}

	// Apply combined auth middleware (OIDC + token) when any auth is configured
	if !cfg.AuthDisabled || cfg.AuthToken != "" {
		router.Use(auth.CombinedMiddleware(oidcMiddleware, cfg.AuthToken, logger))
		if cfg.AuthToken != "" {
			logger.Info("token authentication enabled for API endpoints")
		}
	} else {
		logger.Warn("authentication disabled")
		router.Use(func(c *gin.Context) {
			c.Request = c.Request.WithContext(auth.WithUser(c.Request.Context(), devUser))
			c.Next()
		})
	}

	// WebSocket — protected by the auth middleware above (registered after it)
	router.GET("/ws", func(c *gin.Context) {
		d.Hub.HandleConnect(c.Writer, c.Request)
	})

	// Huma API
	humaConfig := huma.DefaultConfig("MailCatcher API", "1.0.0")
	humaConfig.Info.Description = "SMTP email capture and browsing API"
	api := humagin.New(router, humaConfig)

	a.registerRoutes(api)

	// Serve frontend static files (SPA with fallback to index.html)
	if staticFS := d.StaticFS; staticFS != nil {
		fileServer := http.FileServer(http.FS(staticFS))
		router.NoRoute(func(c *gin.Context) {
			// Try serving the file directly
			path := c.Request.URL.Path
			f, err := staticFS.Open(path[1:]) // strip leading /
			if err == nil {
				_ = f.Close()
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
			// Fallback to index.html for SPA routing
			c.Request.URL.Path = "/"
			fileServer.ServeHTTP(c.Writer, c.Request)
		})
	}

	return a, nil
}

func (a *API) registerRoutes(api huma.API) {
	// Mailboxes
	huma.Register(api, huma.Operation{
		OperationID: "list-mailboxes",
		Method:      http.MethodGet,
		Path:        "/api/v1/mailboxes",
		Summary:     "List mailboxes",
		Tags:        []string{"Mailboxes"},
	}, a.listMailboxes)

	huma.Register(api, huma.Operation{
		OperationID: "get-mailbox",
		Method:      http.MethodGet,
		Path:        "/api/v1/mailboxes/{id}",
		Summary:     "Get mailbox",
		Tags:        []string{"Mailboxes"},
	}, a.getMailbox)

	huma.Register(api, huma.Operation{
		OperationID: "delete-mailbox",
		Method:      http.MethodDelete,
		Path:        "/api/v1/mailboxes/{id}",
		Summary:     "Delete mailbox",
		Tags:        []string{"Mailboxes"},
	}, a.deleteMailbox)

	huma.Register(api, huma.Operation{
		OperationID: "delete-all-mailboxes",
		Method:      http.MethodDelete,
		Path:        "/api/v1/mailboxes",
		Summary:     "Delete all mailboxes",
		Tags:        []string{"Mailboxes"},
	}, a.deleteAllMailboxes)

	// Messages
	huma.Register(api, huma.Operation{
		OperationID: "list-all-messages",
		Method:      http.MethodGet,
		Path:        "/api/v1/messages",
		Summary:     "List all messages across all mailboxes",
		Tags:        []string{"Messages"},
	}, a.listAllMessages)

	huma.Register(api, huma.Operation{
		OperationID: "list-messages",
		Method:      http.MethodGet,
		Path:        "/api/v1/mailboxes/{id}/messages",
		Summary:     "List messages in mailbox",
		Tags:        []string{"Messages"},
	}, a.listMessages)

	huma.Register(api, huma.Operation{
		OperationID: "get-message",
		Method:      http.MethodGet,
		Path:        "/api/v1/messages/{id}",
		Summary:     "Get message",
		Tags:        []string{"Messages"},
	}, a.getMessage)

	huma.Register(api, huma.Operation{
		OperationID: "get-message-raw",
		Method:      http.MethodGet,
		Path:        "/api/v1/messages/{id}/raw",
		Summary:     "Download raw .eml",
		Tags:        []string{"Messages"},
	}, a.getMessageRaw)

	huma.Register(api, huma.Operation{
		OperationID: "delete-message",
		Method:      http.MethodDelete,
		Path:        "/api/v1/messages/{id}",
		Summary:     "Delete message",
		Tags:        []string{"Messages"},
	}, a.deleteMessage)

	huma.Register(api, huma.Operation{
		OperationID: "delete-messages",
		Method:      http.MethodDelete,
		Path:        "/api/v1/messages",
		Summary:     "Bulk delete messages",
		Tags:        []string{"Messages"},
	}, a.deleteMessages)

	// Attachments
	huma.Register(api, huma.Operation{
		OperationID: "list-attachments",
		Method:      http.MethodGet,
		Path:        "/api/v1/messages/{id}/attachments",
		Summary:     "List attachments",
		Tags:        []string{"Attachments"},
	}, a.listAttachments)

	huma.Register(api, huma.Operation{
		OperationID: "get-attachment",
		Method:      http.MethodGet,
		Path:        "/api/v1/attachments/{id}",
		Summary:     "Download attachment",
		Tags:        []string{"Attachments"},
	}, a.getAttachment)

	// Search
	huma.Register(api, huma.Operation{
		OperationID: "search-messages",
		Method:      http.MethodGet,
		Path:        "/api/v1/search",
		Summary:     "Search messages",
		Tags:        []string{"Search"},
	}, a.searchMessages)

	// External (app-to-app)
	huma.Register(api, huma.Operation{
		OperationID: "list-external-messages",
		Method:      http.MethodGet,
		Path:        "/api/v1/message",
		Summary:     "List all messages (external format)",
		Description: "Returns all captured messages in a flat format suitable for app-to-app integration. Requires Bearer token authentication.",
		Tags:        []string{"External"},
	}, a.listExternalMessages)

	huma.Register(api, huma.Operation{
		OperationID: "get-message-html",
		Method:      http.MethodGet,
		Path:        "/api/v1/messages/{id}/html",
		Summary:     "Render message HTML",
		Description: "Returns the HTML body with a sandboxing Content-Security-Policy, meant for an iframe.",
		Tags:        []string{"Messages"},
	}, a.getMessageHTML)

	// IMAP access
	huma.Register(api, huma.Operation{
		OperationID: "get-imap-credentials",
		Method:      http.MethodGet,
		Path:        "/api/v1/imap/credentials",
		Summary:     "Get the signed-in user's IMAP access",
		Tags:        []string{"IMAP"},
	}, a.getIMAPCredentials)

	huma.Register(api, huma.Operation{
		OperationID: "set-imap-credentials",
		Method:      http.MethodPut,
		Path:        "/api/v1/imap/credentials",
		Summary:     "Create or change the signed-in user's IMAP login",
		Description: "Stores the username and a bcrypt hash of the password in a Kubernetes Secret.",
		Tags:        []string{"IMAP"},
	}, a.setIMAPCredentials)

	huma.Register(api, huma.Operation{
		OperationID:   "delete-imap-credentials",
		Method:        http.MethodDelete,
		Path:          "/api/v1/imap/credentials",
		Summary:       "Delete the signed-in user's IMAP login",
		DefaultStatus: http.StatusNoContent,
		Tags:          []string{"IMAP"},
	}, a.deleteIMAPCredentials)

	// User
	huma.Register(api, huma.Operation{
		OperationID: "get-me",
		Method:      http.MethodGet,
		Path:        "/api/v1/me",
		Summary:     "Get current user info",
		Tags:        []string{"User"},
	}, a.getMe)
}
