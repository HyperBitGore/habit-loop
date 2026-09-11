package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const turnstileTokenMaxLength = 2048

var (
	turnstileSiteVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	turnstileHTTPClient    = &http.Client{Timeout: 10 * time.Second}
	turnstileVerifier      = verifyTurnstile
)

type turnstileResponse struct {
	Success    bool     `json:"success"`
	Action     string   `json:"action"`
	Hostname   string   `json:"hostname"`
	ErrorCodes []string `json:"error-codes"`
}

func verifyTurnstile(ctx context.Context, token, expectedAction, remoteIP string) error {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > turnstileTokenMaxLength ||
		appConfig.TurnstileSecret == "" || len(appConfig.TurnstileHostnames) == 0 {
		return errors.New("turnstile verification rejected")
	}

	form := url.Values{
		"secret":   {appConfig.TurnstileSecret},
		"response": {token},
	}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		turnstileSiteVerifyURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return fmt.Errorf("create turnstile request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := turnstileHTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("verify turnstile token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("turnstile siteverify status %d", response.StatusCode)
	}

	var result turnstileResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil {
		return fmt.Errorf("decode turnstile response: %w", err)
	}
	if !result.Success || result.Action != expectedAction ||
		!containsString(appConfig.TurnstileHostnames, strings.ToLower(result.Hostname)) {
		return errors.New("turnstile verification rejected")
	}
	return nil
}

func requireTurnstile(w http.ResponseWriter, r *http.Request, token, expectedAction string) bool {
	if err := turnstileVerifier(r.Context(), token, expectedAction, clientIP(r)); err != nil {
		logRequestError(r, "turnstile verification failed", err)
		writeAPIError(w, http.StatusForbidden, "Verification failed")
		return false
	}
	return true
}
