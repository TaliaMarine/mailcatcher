package api

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/TaliaMarine/mailcatcher/internal/auth"
	"github.com/TaliaMarine/mailcatcher/internal/credentials"
	"github.com/danielgtaylor/huma/v2"
)

// IMAPAccess describes how the signed-in user can reach their mail over IMAP.
type IMAPAccess struct {
	Enabled      bool       `json:"enabled" doc:"Whether this server offers IMAP"`
	Configured   bool       `json:"configured" doc:"Whether the user has created an IMAP login"`
	Username     string     `json:"username,omitempty" doc:"The user's IMAP username"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty" doc:"When the login was last changed"`
	Host         string     `json:"host,omitempty" doc:"IMAP server host name"`
	TLSPort      int        `json:"tls_port,omitempty" doc:"Port for implicit TLS (IMAPS)"`
	StartTLSPort int        `json:"starttls_port,omitempty" doc:"Port for plain connections upgraded with STARTTLS"`
	SMTPPort     int        `json:"smtp_port,omitempty" doc:"Public SMTP port (STARTTLS when a certificate is configured)"`
	SMTPTLSPort  int        `json:"smtp_tls_port,omitempty" doc:"Public SMTP port for implicit TLS (SMTPS)"`
}

type IMAPAccessOutput struct {
	Body IMAPAccess
}

type SetIMAPCredentialsInput struct {
	Body struct {
		Username string `json:"username" minLength:"3" maxLength:"64" doc:"Lowercase letters, digits and . _ @ + -"`
		Password string `json:"password" minLength:"12" maxLength:"72" doc:"At least 12 characters"`
	}
}

// imapOwner returns the signed-in OIDC user. Bearer-token clients have no
// identity of their own and cannot own an IMAP login.
func imapOwner(ctx context.Context) (credentials.Owner, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok || u.Subject == "" {
		return credentials.Owner{}, huma.Error403Forbidden("sign in with your account to manage IMAP access")
	}
	return credentials.Owner{Subject: u.Subject, Email: u.Email}, nil
}

func (a *API) imapAccess(cred *credentials.Credential) IMAPAccess {
	acc := IMAPAccess{
		Enabled:      a.imapEnabled(),
		Host:         a.imapHost(),
		TLSPort:      a.cfg.IMAPPublicTLSPort,
		StartTLSPort: a.cfg.IMAPPublicPort,
		SMTPPort:     a.cfg.SMTPPublicPort,
		SMTPTLSPort:  a.cfg.SMTPPublicTLSPort,
	}
	if cred != nil {
		acc.Configured = true
		acc.Username = cred.Username
		t := cred.UpdatedAt
		acc.UpdatedAt = &t
	}
	return acc
}

func (a *API) imapEnabled() bool { return a.cfg.IMAPEnabled && a.creds != nil }

// imapHost is the configured public host, else the host of BASE_URL.
func (a *API) imapHost() string {
	if a.cfg.IMAPPublicHost != "" {
		return a.cfg.IMAPPublicHost
	}
	if u, err := url.Parse(a.cfg.BaseURL); err == nil {
		return u.Hostname()
	}
	return ""
}

func (a *API) getIMAPCredentials(ctx context.Context, _ *struct{}) (*IMAPAccessOutput, error) {
	if !a.imapEnabled() {
		return &IMAPAccessOutput{Body: a.imapAccess(nil)}, nil
	}
	owner, err := imapOwner(ctx)
	if err != nil {
		return nil, err
	}
	cred, err := a.creds.Get(ctx, owner.Subject)
	if errors.Is(err, credentials.ErrNotFound) {
		return &IMAPAccessOutput{Body: a.imapAccess(nil)}, nil
	}
	if err != nil {
		a.logger.Error("failed to read IMAP credential", "error", err)
		return nil, huma.Error500InternalServerError("failed to read IMAP login")
	}
	return &IMAPAccessOutput{Body: a.imapAccess(cred)}, nil
}

func (a *API) setIMAPCredentials(ctx context.Context, in *SetIMAPCredentialsInput) (*IMAPAccessOutput, error) {
	if !a.imapEnabled() {
		return nil, huma.Error404NotFound("IMAP is not enabled on this server")
	}
	owner, err := imapOwner(ctx)
	if err != nil {
		return nil, err
	}
	cred, err := a.creds.Set(ctx, owner, in.Body.Username, in.Body.Password)
	var verr *credentials.ValidationError
	switch {
	case errors.As(err, &verr):
		return nil, huma.Error422UnprocessableEntity(verr.Msg)
	case errors.Is(err, credentials.ErrUsernameTaken):
		return nil, huma.Error409Conflict("that username is already taken")
	case err != nil:
		a.logger.Error("failed to store IMAP credential", "error", err)
		return nil, huma.Error500InternalServerError("failed to save IMAP login")
	}
	a.logger.Info("IMAP login saved", "subject", owner.Subject, "username", cred.Username)
	return &IMAPAccessOutput{Body: a.imapAccess(cred)}, nil
}

func (a *API) deleteIMAPCredentials(ctx context.Context, _ *struct{}) (*struct{}, error) {
	if !a.imapEnabled() {
		return nil, huma.Error404NotFound("IMAP is not enabled on this server")
	}
	owner, err := imapOwner(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.creds.Delete(ctx, owner.Subject); err != nil {
		a.logger.Error("failed to delete IMAP credential", "error", err)
		return nil, huma.Error500InternalServerError("failed to delete IMAP login")
	}
	a.logger.Info("IMAP login deleted", "subject", owner.Subject)
	return nil, nil
}
