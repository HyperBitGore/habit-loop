package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVerifyTurnstileValidatesActionHostnameAndRequest(t *testing.T) {
	previousConfig := appConfig
	previousURL := turnstileSiteVerifyURL
	previousClient := turnstileHTTPClient
	t.Cleanup(func() {
		appConfig = previousConfig
		turnstileSiteVerifyURL = previousURL
		turnstileHTTPClient = previousClient
	})

	appConfig.TurnstileSecret = "secret-value"
	appConfig.TurnstileHostnames = []string{"todosloop.com"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Method != http.MethodPost ||
			r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" ||
			r.Form.Get("secret") != "secret-value" ||
			r.Form.Get("response") != "valid-token" ||
			r.Form.Get("remoteip") != "203.0.113.10" {
			t.Fatalf("unexpected siteverify request: method=%s form=%v", r.Method, r.Form)
		}
		json.NewEncoder(w).Encode(turnstileResponse{
			Success:  true,
			Action:   "signup",
			Hostname: "todosloop.com",
		})
	}))
	defer server.Close()
	turnstileSiteVerifyURL = server.URL
	turnstileHTTPClient = server.Client()

	if err := verifyTurnstile(
		context.Background(),
		"valid-token",
		"signup",
		"203.0.113.10",
	); err != nil {
		t.Fatal(err)
	}
	if err := verifyTurnstile(
		context.Background(),
		"valid-token",
		"login",
		"203.0.113.10",
	); err == nil {
		t.Fatal("mismatched action was accepted")
	}
}

func TestLoginRejectsFailedTurnstileBeforeAuthentication(t *testing.T) {
	setupTestApplication(t)
	turnstileVerifier = func(context.Context, string, string, string) error {
		return context.DeadlineExceeded
	}

	request := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(
		`{"name":"alice","password":"password123","turnstile_token":"invalid"}`,
	))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "203.0.113.10:1234"
	response := httptest.NewRecorder()
	HandleLogin(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}
