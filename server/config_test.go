package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfigFile(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "server.cfg")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	return path
}

func TestLoadConfig(t *testing.T) {
	path := writeConfigFile(t, `
APP_ENV=development
LISTEN_ADDR=:9090
DATABASE_PATH=data/storage.db
WEB_ROOT=public
APP_BASE_URL=http://localhost:9090
TURNSTILE_SECRET="secret with spaces"
TURNSTILE_HOSTNAMES=localhost,127.0.0.1
RESEND_FROM_EMAIL='Habit Loop <mail@example.com>'
SECURE_COOKIES=false
TRUST_PROXY_HEADERS=true
TRUSTED_PROXY_CIDRS=127.0.0.1,10.0.0.0/8
BOOTSTRAP_ADMIN_NAME=owner
BOOTSTRAP_ADMIN_EMAIL=owner@example.com
BOOTSTRAP_ADMIN_PASSWORD="a long bootstrap password"
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	configDir := filepath.Dir(path)
	if cfg.DatabasePath != filepath.Join(configDir, "data", "storage.db") {
		t.Errorf("DatabasePath = %q", cfg.DatabasePath)
	}
	if cfg.WebRoot != filepath.Join(configDir, "public") {
		t.Errorf("WebRoot = %q", cfg.WebRoot)
	}
	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.TurnstileSecret != "secret with spaces" {
		t.Errorf("TurnstileSecret = %q", cfg.TurnstileSecret)
	}
	if cfg.ResendFromEmail != "Habit Loop <mail@example.com>" {
		t.Errorf("ResendFromEmail = %q", cfg.ResendFromEmail)
	}
	if !cfg.TrustProxyHeaders || len(cfg.TrustedProxies) != 2 {
		t.Errorf("trusted proxies = %v, enabled = %t", cfg.TrustedProxies, cfg.TrustProxyHeaders)
	}
}

func TestLoadConfigRejectsInvalidFiles(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		want     string
	}{
		{
			name:     "unknown key",
			contents: "APP_ENV=test\nUNKNOWN=value\n",
			want:     `unknown key "UNKNOWN"`,
		},
		{
			name:     "duplicate key",
			contents: "APP_ENV=test\nAPP_ENV=test\n",
			want:     `repeats key "APP_ENV"`,
		},
		{
			name:     "invalid boolean",
			contents: "APP_ENV=test\nSECURE_COOKIES=occasionally\n",
			want:     "SECURE_COOKIES must be true or false",
		},
		{
			name:     "incomplete production config",
			contents: "APP_ENV=production\nTURNSTILE_SECRET=secret\nTURNSTILE_HOSTNAMES=todosloop.com\n",
			want:     "APP_BASE_URL is required in production",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadConfig(writeConfigFile(t, tt.contents))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadConfig() error = %v, want error containing %q", err, tt.want)
			}
		})
	}
}

func TestLoadConfigAcceptsProductionConfig(t *testing.T) {
	path := writeConfigFile(t, `
APP_ENV=production
APP_BASE_URL=https://todosloop.com
TURNSTILE_SECRET=secret
TURNSTILE_HOSTNAMES=todosloop.com
RESEND_API_KEY=resend-key
RESEND_FROM_EMAIL="Habit Loop <mail@todosloop.com>"
SECURE_COOKIES=true
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Environment != "production" || cfg.AppBaseURL.String() != "https://todosloop.com" {
		t.Errorf("production config = %#v", cfg)
	}
}

func TestLoadConfigReportsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.cfg")
	_, err := LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "open config file") {
		t.Fatalf("LoadConfig() error = %v", err)
	}
}
