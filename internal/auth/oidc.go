package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
)

const (
	sessionCookieName = "mc_session"
	stateCookieName   = "mc_oauth_state"
	sessionDuration   = 24 * time.Hour
	stateDuration     = 5 * time.Minute
	callbackPath      = "/auth/callback"
	loginPath         = "/auth/login"
	logoutPath        = "/auth/logout"
)

type contextKey struct{}

// User is the authenticated person behind a request.
type User struct {
	Subject           string `json:"sub"`
	Email             string `json:"email"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
}

// WithUser returns ctx carrying u.
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, contextKey{}, u)
}

// UserFromContext returns the user stored by the auth middleware.
func UserFromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(contextKey{}).(User)
	return u, ok
}

// session is stored, signed, in the session cookie.
type session struct {
	User
	ExpiresAt int64 `json:"exp"`
}

// loginState is stored, signed, in the short-lived state cookie while the user
// is at the identity provider.
type loginState struct {
	State     string `json:"state"`
	Nonce     string `json:"nonce"`
	Verifier  string `json:"verifier"`
	ReturnURL string `json:"return_url"`
	ExpiresAt int64  `json:"exp"`
}

// Options configure Auth.
type Options struct {
	Issuer       string
	ClientID     string
	ClientSecret string // optional: empty means a public client (PKCE only)
	BaseURL      string
	SessionKey   []byte
	Logger       *slog.Logger
}

// Auth provides OIDC authentication middleware and handlers.
//
// The login flow is always Authorization Code with PKCE (S256), so the client
// can be a public client without a secret.
type Auth struct {
	verifier   *oidc.IDTokenVerifier
	oauth2Cfg  oauth2.Config
	signingKey []byte
	secure     bool
	logger     *slog.Logger
}

// New discovers the OIDC provider and builds an Auth.
func New(ctx context.Context, o Options) (*Auth, error) {
	if len(o.SessionKey) < 32 {
		return nil, errors.New("session key must be at least 32 bytes")
	}
	provider, err := oidc.NewProvider(ctx, o.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc provider discovery: %w", err)
	}

	base := strings.TrimRight(o.BaseURL, "/")
	cfg := oauth2.Config{
		ClientID:     o.ClientID,
		ClientSecret: o.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  base + callbackPath,
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}
	o.Logger.Debug("OIDC provider configured",
		"issuer", o.Issuer, "client_id", o.ClientID,
		"public_client", o.ClientSecret == "", "redirect_url", cfg.RedirectURL)

	return &Auth{
		verifier:   provider.Verifier(&oidc.Config{ClientID: o.ClientID}),
		oauth2Cfg:  cfg,
		signingKey: o.SessionKey,
		secure:     strings.HasPrefix(strings.ToLower(base), "https://"),
		logger:     o.Logger,
	}, nil
}

// Register adds the login, callback and logout routes.
func (a *Auth) Register(r gin.IRoutes) {
	r.GET(loginPath, a.LoginHandler)
	r.GET(callbackPath, a.CallbackHandler)
	r.POST(logoutPath, a.LogoutHandler)
}

// LoginHandler redirects the user to the OIDC provider.
func (a *Auth) LoginHandler(c *gin.Context) {
	state, err1 := randomString(16)
	nonce, err2 := randomString(16)
	if err := errors.Join(err1, err2); err != nil {
		a.logger.Error("failed to generate login state", "error", err)
		c.String(http.StatusInternalServerError, "internal error")
		return
	}
	verifier := oauth2.GenerateVerifier()

	sealed, err := a.seal(loginState{
		State:     state,
		Nonce:     nonce,
		Verifier:  verifier,
		ReturnURL: safeReturnURL(c.Query("return_url")),
		ExpiresAt: time.Now().Add(stateDuration).Unix(),
	})
	if err != nil {
		a.logger.Error("failed to seal login state", "error", err)
		c.String(http.StatusInternalServerError, "internal error")
		return
	}
	a.setCookie(c, stateCookieName, sealed, int(stateDuration.Seconds()))

	c.Redirect(http.StatusFound, a.oauth2Cfg.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oidc.Nonce(nonce),
	))
}

// CallbackHandler handles the OIDC provider callback.
func (a *Auth) CallbackHandler(c *gin.Context) {
	cookie, err := c.Request.Cookie(stateCookieName)
	if err != nil {
		c.String(http.StatusBadRequest, "missing login state; start again at "+loginPath)
		return
	}
	var ls loginState
	if err := a.open(cookie.Value, &ls); err != nil || time.Now().Unix() > ls.ExpiresAt {
		c.String(http.StatusBadRequest, "invalid or expired login state; start again at "+loginPath)
		return
	}
	a.setCookie(c, stateCookieName, "", -1) // single use

	if !hmac.Equal([]byte(c.Query("state")), []byte(ls.State)) {
		c.String(http.StatusBadRequest, "state mismatch")
		return
	}
	if errParam := c.Query("error"); errParam != "" {
		a.logger.Warn("OIDC provider returned error", "error", errParam, "description", c.Query("error_description"))
		c.String(http.StatusForbidden, "authentication failed: %s", errParam)
		return
	}

	token, err := a.oauth2Cfg.Exchange(c.Request.Context(), c.Query("code"), oauth2.VerifierOption(ls.Verifier))
	if err != nil {
		a.logger.Error("token exchange failed", "error", err)
		c.String(http.StatusBadGateway, "token exchange failed")
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		c.String(http.StatusBadGateway, "no id_token in response")
		return
	}
	idToken, err := a.verifier.Verify(c.Request.Context(), rawIDToken)
	if err != nil {
		a.logger.Error("id_token verification failed", "error", err)
		c.String(http.StatusForbidden, "token verification failed")
		return
	}
	if !hmac.Equal([]byte(idToken.Nonce), []byte(ls.Nonce)) {
		a.logger.Error("id_token nonce mismatch")
		c.String(http.StatusForbidden, "token verification failed")
		return
	}

	var claims struct {
		Email             string `json:"email"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := idToken.Claims(&claims); err != nil {
		a.logger.Error("failed to parse claims", "error", err)
		c.String(http.StatusInternalServerError, "failed to parse claims")
		return
	}

	sealed, err := a.seal(session{
		User: User{
			Subject:           idToken.Subject,
			Email:             claims.Email,
			Name:              claims.Name,
			PreferredUsername: claims.PreferredUsername,
		},
		ExpiresAt: time.Now().Add(sessionDuration).Unix(),
	})
	if err != nil {
		a.logger.Error("failed to seal session", "error", err)
		c.String(http.StatusInternalServerError, "internal error")
		return
	}
	a.setCookie(c, sessionCookieName, sealed, int(sessionDuration.Seconds()))

	a.logger.Info("user authenticated", "subject", idToken.Subject, "email", claims.Email)
	c.Redirect(http.StatusFound, ls.ReturnURL)
}

// LogoutHandler clears the local session. POST only, so a cross-site link or
// image cannot sign the user out.
func (a *Auth) LogoutHandler(c *gin.Context) {
	a.setCookie(c, sessionCookieName, "", -1)
	c.Status(http.StatusNoContent)
}

// Middleware enforces OIDC authentication. Unauthenticated API requests get
// 401; browser requests are redirected to the login page.
func (a *Auth) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Request.Cookie(sessionCookieName)
		if err != nil {
			a.handleUnauthenticated(c)
			return
		}
		var s session
		if err := a.open(cookie.Value, &s); err != nil || time.Now().Unix() > s.ExpiresAt || s.Subject == "" {
			a.handleUnauthenticated(c)
			return
		}
		c.Request = c.Request.WithContext(WithUser(c.Request.Context(), s.User))
		c.Next()
	}
}

func (a *Auth) handleUnauthenticated(c *gin.Context) {
	path := c.Request.URL.Path

	if strings.HasPrefix(path, "/api/") || path == "/ws" {
		a.logger.Info("OIDC authentication failed", "path", path, "remote_addr", c.ClientIP())
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	// Browser request: send to login, preserving the original URL.
	c.Redirect(http.StatusFound, loginPath+"?return_url="+url.QueryEscape(c.Request.URL.RequestURI()))
	c.Abort()
}

func (a *Auth) setCookie(c *gin.Context, name, value string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   a.secure,
	})
}

// seal serializes v as "base64(payload).base64(hmac)".
func (a *Auth) seal(v any) (string, error) {
	payload, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, a.signingKey)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// open verifies the signature of a sealed value and decodes it into v.
func (a *Auth) open(value string, v any) error {
	p, s, ok := strings.Cut(value, ".")
	if !ok {
		return errors.New("malformed value")
	}
	payload, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	mac := hmac.New(sha256.New, a.signingKey)
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return errors.New("invalid signature")
	}
	return json.Unmarshal(payload, v)
}

// safeReturnURL only lets through local paths, so the login flow cannot be
// used as an open redirect.
func safeReturnURL(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\\\r\n") {
		return "/"
	}
	return raw
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
