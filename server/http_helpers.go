package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const maxJSONBodySize = 64 << 10

type contextKey string

const (
	userContextKey      contextKey = "user"
	requestIDContextKey contextKey = "request-id"
)

type errorResponse struct {
	Error string `json:"error"`
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])); mediaType != "application/json" {
		return errors.New("Content-Type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return errors.New("invalid JSON request body")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if value != nil {
		if err := json.NewEncoder(w).Encode(value); err != nil {
			log.Printf("encode response error=%v", err)
		}
	}
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := make([]byte, 12)
		if _, err := rand.Read(raw); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		requestID := hex.EncodeToString(raw)
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDContextKey, requestID)))
	})
}

func requestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDContextKey).(string)
	return requestID
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("panic request_id=%s method=%s path=%s error=%v", requestIDFromContext(r.Context()), r.Method, r.URL.Path, recovered)
				writeAPIError(w, http.StatusInternalServerError, "Internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		log.Printf(
			"request request_id=%s method=%s path=%s duration=%s",
			requestIDFromContext(r.Context()),
			r.Method,
			r.URL.Path,
			time.Since(started).Round(time.Millisecond),
		)
	})
}

func securityHeadersMiddleware(cfg Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		nonceBytes := make([]byte, 32)
		if _, err := rand.Read(nonceBytes); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		nonce := base64.RawStdEncoding.EncodeToString(nonceBytes)
		w.Header().Set("Content-Security-Policy", "object-src 'none'; script-src 'nonce-"+nonce+"' 'unsafe-inline' 'unsafe-eval' 'strict-dynamic' https: http:; script-src-elem 'self' https: http:; style-src 'self' 'unsafe-inline'; img-src 'self' data: https://*.googlesyndication.com https://*.doubleclick.net https://*.adtrafficquality.google; frame-src https://challenges.cloudflare.com https://googleads.g.doubleclick.net https://*.adtrafficquality.google https://www.google.com; connect-src 'self' https://challenges.cloudflare.com https://static.cloudflareinsights.com https://pagead2.googlesyndication.com https://googleads.g.doubleclick.net https://*.adtrafficquality.google https://csi.gstatic.com; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		if cfg.Environment == "production" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		recorder := &nonceResponseWriter{header: w.Header()}
		next.ServeHTTP(recorder, r)
		recorder.flush(w, nonce)
	})
}

type nonceResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *nonceResponseWriter) Header() http.Header {
	return w.header
}

func (w *nonceResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *nonceResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(body)
}

func (w *nonceResponseWriter) flush(destination http.ResponseWriter, nonce string) {
	body := w.body.Bytes()
	if bytes.Contains(body, []byte("<script")) {
		body = bytes.ReplaceAll(body, []byte("<script"), []byte(`<script nonce="`+nonce+`"`))
		w.header.Del("Content-Length")
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	destination.WriteHeader(w.status)
	_, _ = destination.Write(body)
}

func userFromContext(ctx context.Context) *User {
	user, _ := ctx.Value(userContextKey).(*User)
	return user
}
