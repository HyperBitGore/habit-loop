package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type rateLimitEntry struct {
	count       int
	windowStart time.Time
	lastSeen    time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	entries map[string]rateLimitEntry
	limit   int
	window  time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		entries: make(map[string]rateLimitEntry),
		limit:   limit,
		window:  window,
	}
}

func (limiter *rateLimiter) allow(key string) bool {
	now := time.Now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	entry := limiter.entries[key]
	if entry.windowStart.IsZero() || now.Sub(entry.windowStart) >= limiter.window {
		entry.count = 0
		entry.windowStart = now
	}
	entry.count++
	entry.lastSeen = now
	limiter.entries[key] = entry

	if len(limiter.entries) > 10000 {
		for storedKey, storedEntry := range limiter.entries {
			if now.Sub(storedEntry.lastSeen) > 2*limiter.window {
				delete(limiter.entries, storedKey)
			}
		}
	}
	return entry.count <= limiter.limit
}

func rateLimitMiddleware(limiter *rateLimiter, accountKey func(*http.Request) string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := clientIP(r)
		if accountKey != nil {
			if account := strings.TrimSpace(strings.ToLower(accountKey(r))); account != "" {
				key += ":" + account
			}
		}
		if !limiter.allow(key) {
			w.Header().Set("Retry-After", "60")
			writeAPIError(w, http.StatusTooManyRequests, "Too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	if appConfig.TrustProxyHeaders && appConfig.isTrustedProxy(r.RemoteAddr) {
		forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		if len(forwarded) > 0 {
			if candidate := strings.TrimSpace(forwarded[0]); net.ParseIP(candidate) != nil {
				return candidate
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
