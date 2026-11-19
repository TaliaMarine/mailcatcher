package config

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	SMTPPort      int
	SMTPTLSPort   int // implicit-TLS SMTP listener (SMTPS); 0 disables it
	HTTPPort      int
	DatabasePath  string
	RetentionDays int

	AuthDisabled bool
	AuthToken    string
	BaseURL      string

	// OIDC. The login flow is Authorization Code + PKCE (S256), so
	// OIDCClientSecret is optional: leave it empty for a public client.
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string

	// SessionKey signs session cookies. When empty, the key is read from (or
	// created in) the Kubernetes Secret named by SessionKeySecretName, and as a
	// last resort generated in memory (sessions then do not survive a restart).
	SessionKey           string
	SessionKeySecretName string
	SessionKeySecretKey  string

	// IMAP access to the captured mail. Credentials are created by users in
	// the web UI and kept in Kubernetes Secrets (or in memory outside a cluster).
	IMAPEnabled      bool
	IMAPPort         int // plain listener, STARTTLS required before login; 0 disables it
	IMAPTLSPort      int // implicit-TLS listener (IMAPS); 0 disables it
	CredentialStore  string
	CredentialPrefix string

	// What the UI tells users to connect to. These describe the address
	// clients reach through the Service/LoadBalancer, which usually differs
	// from the container's listen ports.
	IMAPPublicHost    string
	IMAPPublicTLSPort int
	IMAPPublicPort    int
	SMTPPublicPort    int // 0 hides it
	SMTPPublicTLSPort int // 0 hides it

	// TLS certificate shared by IMAPS, SMTPS and STARTTLS. When unset a
	// self-signed certificate is generated at startup.
	TLSCertFile string
	TLSKeyFile  string

	Namespace string

	LogLevel  string
	LogFormat string
}

func Load() *Config {
	return &Config{
		SMTPPort:      getEnvInt("SMTP_PORT", 1025),
		SMTPTLSPort:   getEnvInt("SMTP_TLS_PORT", 0),
		HTTPPort:      getEnvInt("HTTP_PORT", 1080),
		DatabasePath:  getEnv("DATABASE_PATH", "mailcatcher.db"),
		RetentionDays: getEnvInt("RETENTION_DAYS", 100),

		AuthDisabled: getEnvBool("AUTH_DISABLED", false),
		AuthToken:    getEnv("AUTH_TOKEN", ""),
		BaseURL:      getEnv("BASE_URL", ""),

		OIDCIssuer:       getEnv("OIDC_ISSUER", ""),
		OIDCClientID:     getEnv("OIDC_CLIENT_ID", ""),
		OIDCClientSecret: getEnv("OIDC_CLIENT_SECRET", ""),

		SessionKey:           getEnv("SESSION_KEY", ""),
		SessionKeySecretName: getEnv("SESSION_KEY_SECRET_NAME", ""),
		SessionKeySecretKey:  getEnv("SESSION_KEY_SECRET_KEY", "key"),

		IMAPEnabled:      getEnvBool("IMAP_ENABLED", false),
		IMAPPort:         getEnvInt("IMAP_PORT", 1143),
		IMAPTLSPort:      getEnvInt("IMAP_TLS_PORT", 1993),
		CredentialStore:  getEnv("CREDENTIAL_STORE", ""),
		CredentialPrefix: getEnv("CREDENTIAL_SECRET_PREFIX", "mailcatcher-imap"),

		IMAPPublicHost:    getEnv("IMAP_PUBLIC_HOST", ""),
		IMAPPublicTLSPort: getEnvInt("IMAP_PUBLIC_TLS_PORT", 993),
		IMAPPublicPort:    getEnvInt("IMAP_PUBLIC_PORT", 143),
		SMTPPublicPort:    getEnvInt("SMTP_PUBLIC_PORT", 0),
		SMTPPublicTLSPort: getEnvInt("SMTP_PUBLIC_TLS_PORT", 0),

		TLSCertFile: getEnv("TLS_CERT_FILE", ""),
		TLSKeyFile:  getEnv("TLS_KEY_FILE", ""),

		Namespace: getEnv("POD_NAMESPACE", ""),

		LogLevel:  getEnv("LOG_LEVEL", "info"),
		LogFormat: getEnv("LOG_FORMAT", "json"),
	}
}

// Validate reports configuration that cannot work.
func (c *Config) Validate() error {
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return errors.New("TLS_CERT_FILE and TLS_KEY_FILE must be set together")
	}
	if c.OIDCIssuer != "" && !c.AuthDisabled {
		if c.OIDCClientID == "" {
			return errors.New("OIDC_CLIENT_ID is required when OIDC_ISSUER is set")
		}
		u, err := url.Parse(c.BaseURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return errors.New("BASE_URL must be an absolute http(s) URL when OIDC is enabled")
		}
	}
	if c.IMAPEnabled && c.IMAPPort == 0 && c.IMAPTLSPort == 0 {
		return errors.New("IMAP_ENABLED is set but both IMAP_PORT and IMAP_TLS_PORT are 0")
	}
	switch c.CredentialStore {
	case "", "kubernetes", "memory":
	default:
		return errors.New("CREDENTIAL_STORE must be \"kubernetes\" or \"memory\"")
	}
	return nil
}

// SecureCookies reports whether cookies must carry the Secure attribute.
func (c *Config) SecureCookies() bool {
	return strings.HasPrefix(strings.ToLower(c.BaseURL), "https://")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
