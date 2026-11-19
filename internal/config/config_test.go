package config

import (
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear any env vars that might interfere
	for _, key := range []string{
		"SMTP_PORT", "HTTP_PORT", "DATABASE_PATH", "RETENTION_DAYS",
		"AUTH_DISABLED", "OIDC_ISSUER", "OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET",
		"BASE_URL", "LOG_LEVEL", "LOG_FORMAT",
	} {
		t.Setenv(key, "")
	}

	cfg := Load()

	if cfg.SMTPPort != 1025 {
		t.Errorf("SMTPPort = %d, want 1025", cfg.SMTPPort)
	}
	if cfg.HTTPPort != 1080 {
		t.Errorf("HTTPPort = %d, want 1080", cfg.HTTPPort)
	}
	if cfg.DatabasePath != "mailcatcher.db" {
		t.Errorf("DatabasePath = %q, want %q", cfg.DatabasePath, "mailcatcher.db")
	}
	if cfg.RetentionDays != 100 {
		t.Errorf("RetentionDays = %d, want 100", cfg.RetentionDays)
	}
	if cfg.AuthDisabled {
		t.Error("AuthDisabled = true, want false")
	}
	if cfg.OIDCIssuer != "" {
		t.Errorf("OIDCIssuer = %q, want empty", cfg.OIDCIssuer)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, "json")
	}
}

func TestLoad_FromEnv(t *testing.T) {
	t.Setenv("SMTP_PORT", "2525")
	t.Setenv("HTTP_PORT", "8080")
	t.Setenv("DATABASE_PATH", "/tmp/test.db")
	t.Setenv("RETENTION_DAYS", "30")
	t.Setenv("AUTH_DISABLED", "true")
	t.Setenv("OIDC_ISSUER", "https://auth.example.com")
	t.Setenv("OIDC_CLIENT_ID", "my-client")
	t.Setenv("OIDC_CLIENT_SECRET", "secret123")
	t.Setenv("BASE_URL", "https://mail.example.com")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "plain")

	cfg := Load()

	if cfg.SMTPPort != 2525 {
		t.Errorf("SMTPPort = %d, want 2525", cfg.SMTPPort)
	}
	if cfg.HTTPPort != 8080 {
		t.Errorf("HTTPPort = %d, want 8080", cfg.HTTPPort)
	}
	if cfg.DatabasePath != "/tmp/test.db" {
		t.Errorf("DatabasePath = %q, want %q", cfg.DatabasePath, "/tmp/test.db")
	}
	if cfg.RetentionDays != 30 {
		t.Errorf("RetentionDays = %d, want 30", cfg.RetentionDays)
	}
	if !cfg.AuthDisabled {
		t.Error("AuthDisabled = false, want true")
	}
	if cfg.OIDCIssuer != "https://auth.example.com" {
		t.Errorf("OIDCIssuer = %q, want %q", cfg.OIDCIssuer, "https://auth.example.com")
	}
	if cfg.OIDCClientID != "my-client" {
		t.Errorf("OIDCClientID = %q, want %q", cfg.OIDCClientID, "my-client")
	}
	if cfg.OIDCClientSecret != "secret123" {
		t.Errorf("OIDCClientSecret = %q, want %q", cfg.OIDCClientSecret, "secret123")
	}
	if cfg.BaseURL != "https://mail.example.com" {
		t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, "https://mail.example.com")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if cfg.LogFormat != "plain" {
		t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, "plain")
	}
}

func TestGetEnvInt_InvalidFallsBack(t *testing.T) {
	t.Setenv("TEST_INT", "notanumber")
	got := getEnvInt("TEST_INT", 42)
	if got != 42 {
		t.Errorf("getEnvInt with invalid value = %d, want 42", got)
	}
}

func TestGetEnvBool_InvalidFallsBack(t *testing.T) {
	t.Setenv("TEST_BOOL", "notabool")
	got := getEnvBool("TEST_BOOL", true)
	if !got {
		t.Error("getEnvBool with invalid value = false, want true")
	}
}

func TestGetEnvBool_Values(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"true", true},
		{"false", false},
		{"1", true},
		{"0", false},
		{"TRUE", true},
		{"FALSE", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Setenv("TEST_BOOL", tt.input)
			got := getEnvBool("TEST_BOOL", !tt.want)
			if got != tt.want {
				t.Errorf("getEnvBool(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
