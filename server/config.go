package main

import (
	"bufio"
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
	TurnstileSecret        string
	TurnstileHostnames     []string
	SecureCookies          bool
	TrustProxyHeaders      bool
	TrustedProxies         []*net.IPNet
	BootstrapAdminName     string
	BootstrapAdminEmail    string
	BootstrapAdminPassword string
}

var allowedConfigKeys = map[string]struct{}{
	"APP_ENV":                  {},
	"LISTEN_ADDR":              {},
	"DATABASE_PATH":            {},
	"WEB_ROOT":                 {},
	"APP_BASE_URL":             {},
	"RESEND_API_KEY":           {},
	"RESEND_FROM_EMAIL":        {},
	"TURNSTILE_SECRET":         {},
	"TURNSTILE_HOSTNAMES":      {},
	"SECURE_COOKIES":           {},
	"TRUST_PROXY_HEADERS":      {},
	"TRUSTED_PROXY_CIDRS":      {},
	"BOOTSTRAP_ADMIN_NAME":     {},
	"BOOTSTRAP_ADMIN_EMAIL":    {},
	"BOOTSTRAP_ADMIN_PASSWORD": {},
}

func LoadConfig(path string) (Config, error) {
	values, err := readConfigFile(path)
	if err != nil {
		return Config{}, err
	}

	environment := strings.TrimSpace(configValue(values, "APP_ENV", "development"))
	if environment != "development" && environment != "production" && environment != "test" {
		return Config{}, fmt.Errorf("APP_ENV must be development, production, or test")
	}

	cfg := Config{
		Environment:            environment,
		ListenAddr:             strings.TrimSpace(configValue(values, "LISTEN_ADDR", ":8081")),
		DatabasePath:           strings.TrimSpace(configValue(values, "DATABASE_PATH", "storage.db")),
		WebRoot:                strings.TrimSpace(configValue(values, "WEB_ROOT", "../web")),
		ResendAPIKey:           strings.TrimSpace(configValue(values, "RESEND_API_KEY", "")),
		ResendFromEmail:        strings.TrimSpace(configValue(values, "RESEND_FROM_EMAIL", "")),
		TurnstileSecret:        strings.TrimSpace(configValue(values, "TURNSTILE_SECRET", "")),
		BootstrapAdminName:     strings.TrimSpace(configValue(values, "BOOTSTRAP_ADMIN_NAME", "")),
		BootstrapAdminEmail:    normalizeEmail(configValue(values, "BOOTSTRAP_ADMIN_EMAIL", "")),
		BootstrapAdminPassword: configValue(values, "BOOTSTRAP_ADMIN_PASSWORD", ""),
	}

	hostnameValue := strings.TrimSpace(configValue(values, "TURNSTILE_HOSTNAMES", ""))
	if hostnameValue == "" && environment != "production" {
		hostnameValue = "localhost,127.0.0.1"
	}
	for _, hostname := range strings.Split(hostnameValue, ",") {
		hostname = strings.ToLower(strings.TrimSpace(hostname))
		if hostname == "" {
			continue
		}
		if strings.Contains(hostname, "://") || strings.ContainsAny(hostname, "/?#") {
			return Config{}, fmt.Errorf("TURNSTILE_HOSTNAMES must contain hostnames without schemes or paths")
		}
		cfg.TurnstileHostnames = append(cfg.TurnstileHostnames, hostname)
	}
	if environment != "test" {
		if cfg.TurnstileSecret == "" {
			return Config{}, fmt.Errorf("TURNSTILE_SECRET is required")
		}
		if len(cfg.TurnstileHostnames) == 0 {
			return Config{}, fmt.Errorf("TURNSTILE_HOSTNAMES is required")
		}
	}

	cfg.SecureCookies, err = configBool(values, "SECURE_COOKIES", environment == "production")
	if err != nil {
		return Config{}, err
	}
	cfg.TrustProxyHeaders, err = configBool(values, "TRUST_PROXY_HEADERS", false)
	if err != nil {
		return Config{}, err
	}
	if cfg.TrustProxyHeaders {
		proxyValues := strings.Split(configValue(values, "TRUSTED_PROXY_CIDRS", ""), ",")
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

	baseURL := strings.TrimSpace(configValue(values, "APP_BASE_URL", ""))
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
		for _, hostname := range cfg.TurnstileHostnames {
			if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
				return Config{}, fmt.Errorf("TURNSTILE_HOSTNAMES must not include local hostnames in production")
			}
		}
		appHostname := strings.ToLower(cfg.AppBaseURL.Hostname())
		if !containsString(cfg.TurnstileHostnames, appHostname) {
			return Config{}, fmt.Errorf("TURNSTILE_HOSTNAMES must include the APP_BASE_URL hostname")
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

	cfg.DatabasePath = resolveConfigPath(path, cfg.DatabasePath)
	cfg.WebRoot = resolveConfigPath(path, cfg.WebRoot)
	return cfg, nil
}

func readConfigFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config file %q: %w", path, err)
	}
	defer file.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		key, rawValue, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			return nil, fmt.Errorf("config line %d must use KEY=value", lineNumber)
		}
		if _, allowed := allowedConfigKeys[key]; !allowed {
			return nil, fmt.Errorf("config line %d contains unknown key %q", lineNumber, key)
		}
		if _, duplicate := values[key]; duplicate {
			return nil, fmt.Errorf("config line %d repeats key %q", lineNumber, key)
		}
		value, err := parseConfigValue(rawValue)
		if err != nil {
			return nil, fmt.Errorf("config line %d for %s: %w", lineNumber, key, err)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}
	return values, nil
}

func parseConfigValue(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if value[0] == '"' {
		if len(value) < 2 || value[len(value)-1] != '"' {
			return "", fmt.Errorf("unterminated quoted value")
		}
		parsed, err := strconv.Unquote(value)
		if err != nil {
			return "", fmt.Errorf("invalid quoted value")
		}
		return parsed, nil
	}
	if value[0] == '\'' {
		if len(value) < 2 || value[len(value)-1] != '\'' {
			return "", fmt.Errorf("unterminated quoted value")
		}
		return value[1 : len(value)-1], nil
	}
	return value, nil
}

func configValue(values map[string]string, name, fallback string) string {
	value, exists := values[name]
	if !exists || strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func configBool(values map[string]string, name string, fallback bool) (bool, error) {
	value, exists := values[name]
	if !exists || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return parsed, nil
}

func resolveConfigPath(configPath, value string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	absolute, err := filepath.Abs(filepath.Join(filepath.Dir(configPath), value))
	if err != nil {
		return filepath.Clean(filepath.Join(filepath.Dir(configPath), value))
	}
	return absolute
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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
