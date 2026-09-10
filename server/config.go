package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Environment            string
	ListenAddr             string
	DatabasePath           string
	WebRoot                string
	AppBaseURL             *url.URL
	ResendAPIKey           string
	ResendFromEmail        string
	SecureCookies          bool
	TrustProxyHeaders      bool
	TrustedProxies         []*net.IPNet
	BootstrapAdminName     string
	BootstrapAdminEmail    string
	BootstrapAdminPassword string
}

func LoadConfig() (Config, error) {
	environment := envOrDefault("APP_ENV", "development")
	if environment != "development" && environment != "production" && environment != "test" {
		return Config{}, fmt.Errorf("APP_ENV must be development, production, or test")
	}

	cfg := Config{
		Environment:            environment,
		ListenAddr:             envOrDefault("LISTEN_ADDR", ":8081"),
		DatabasePath:           envOrDefault("DATABASE_PATH", "storage.db"),
		WebRoot:                envOrDefault("WEB_ROOT", "../web"),
		ResendAPIKey:           strings.TrimSpace(os.Getenv("RESEND_API_KEY")),
		ResendFromEmail:        strings.TrimSpace(os.Getenv("RESEND_FROM_EMAIL")),
		BootstrapAdminName:     strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_NAME")),
		BootstrapAdminEmail:    normalizeEmail(os.Getenv("BOOTSTRAP_ADMIN_EMAIL")),
		BootstrapAdminPassword: os.Getenv("BOOTSTRAP_ADMIN_PASSWORD"),
	}

	var err error
	cfg.SecureCookies, err = envBool("SECURE_COOKIES", environment == "production")
	if err != nil {
		return Config{}, err
	}
	cfg.TrustProxyHeaders, err = envBool("TRUST_PROXY_HEADERS", false)
	if err != nil {
		return Config{}, err
	}
	if cfg.TrustProxyHeaders {
		proxyValues := strings.Split(os.Getenv("TRUSTED_PROXY_CIDRS"), ",")
		for _, proxyValue := range proxyValues {
			proxyValue = strings.TrimSpace(proxyValue)
			if proxyValue == "" {
				continue
			}
			if ip := net.ParseIP(proxyValue); ip != nil {
				bits := 128
				if ip.To4() != nil {
					bits = 32
				}
				cfg.TrustedProxies = append(cfg.TrustedProxies, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
				continue
			}
			_, network, err := net.ParseCIDR(proxyValue)
			if err != nil {
				return Config{}, fmt.Errorf("TRUSTED_PROXY_CIDRS contains invalid address %q", proxyValue)
			}
			cfg.TrustedProxies = append(cfg.TrustedProxies, network)
		}
		if len(cfg.TrustedProxies) == 0 {
			return Config{}, fmt.Errorf("TRUSTED_PROXY_CIDRS is required when TRUST_PROXY_HEADERS is true")
		}
	}

	baseURL := strings.TrimSpace(os.Getenv("APP_BASE_URL"))
	if baseURL == "" && environment != "production" {
		baseURL = "http://localhost:8081"
	}
	if baseURL != "" {
		parsed, err := url.Parse(baseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") {
			return Config{}, fmt.Errorf("APP_BASE_URL must be an absolute http or https URL")
		}
		parsed.Path = strings.TrimRight(parsed.Path, "/")
		cfg.AppBaseURL = parsed
	}

	if cfg.Environment == "production" {
		if cfg.AppBaseURL == nil {
			return Config{}, fmt.Errorf("APP_BASE_URL is required in production")
		}
		if cfg.AppBaseURL.Scheme != "https" {
			return Config{}, fmt.Errorf("APP_BASE_URL must use https in production")
		}
		if !cfg.SecureCookies {
			return Config{}, fmt.Errorf("SECURE_COOKIES must be true in production")
		}
		if cfg.ResendAPIKey == "" || cfg.ResendFromEmail == "" {
			return Config{}, fmt.Errorf("RESEND_API_KEY and RESEND_FROM_EMAIL are required in production")
		}
	}

	bootstrapValues := []string{
		cfg.BootstrapAdminName,
		cfg.BootstrapAdminEmail,
		cfg.BootstrapAdminPassword,
	}
	bootstrapCount := 0
	for _, value := range bootstrapValues {
		if value != "" {
			bootstrapCount++
		}
	}
	if bootstrapCount != 0 && bootstrapCount != len(bootstrapValues) {
		return Config{}, fmt.Errorf("BOOTSTRAP_ADMIN_NAME, BOOTSTRAP_ADMIN_EMAIL, and BOOTSTRAP_ADMIN_PASSWORD must be set together")
	}
	if bootstrapCount == len(bootstrapValues) {
		if err := validateUsername(cfg.BootstrapAdminName); err != nil {
			return Config{}, fmt.Errorf("invalid bootstrap admin name: %w", err)
		}
		if err := validateEmail(cfg.BootstrapAdminEmail); err != nil {
			return Config{}, fmt.Errorf("invalid bootstrap admin email: %w", err)
		}
		if err := validatePassword(cfg.BootstrapAdminPassword); err != nil {
			return Config{}, fmt.Errorf("invalid bootstrap admin password: %w", err)
		}
	}

	if absolute, err := filepath.Abs(cfg.DatabasePath); err == nil {
		cfg.DatabasePath = absolute
	}
	if absolute, err := filepath.Abs(cfg.WebRoot); err == nil {
		cfg.WebRoot = absolute
	}

	return cfg, nil
}

func envOrDefault(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func envBool(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return parsed, nil
}

func (cfg Config) isTrustedProxy(remoteAddress string) bool {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = remoteAddress
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, network := range cfg.TrustedProxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
