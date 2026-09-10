package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// TODO
//	- habits can appear on set interval or only certain days of the week
//	- admin page, infinite/searchable user list
//	- cloudflare turnstile
//	- adsense
//	- update readme

var (
	appConfig   Config
	emailSender EmailSender

	loginLimiter         = newRateLimiter(10, time.Minute)
	loginAccountLimiter  = newRateLimiter(10, time.Minute)
	registrationLimiter  = newRateLimiter(5, time.Hour)
	accountCreateLimiter = newRateLimiter(3, time.Hour)
	resetLimiter         = newRateLimiter(5, time.Hour)
	resetAccountLimiter  = newRateLimiter(3, time.Hour)
)

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("auth")
		if err != nil || cookie.Value == "" {
			writeAPIError(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		user, err := GetUserFromToken(r.Context(), database, cookie.Value)
		if err != nil {
			logRequestError(r, "load authenticated user", err)
			writeAPIError(w, http.StatusInternalServerError, "Unable to authenticate")
			return
		}
		if user == nil {
			http.SetCookie(w, sessionCookie("", -1))
			writeAPIError(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func adminMiddleware(next http.Handler) http.Handler {
	return authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := requestUser(w, r)
		if user == nil {
			return
		}
		if user.Role != "admin" {
			writeAPIError(w, http.StatusForbidden, "Forbidden")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func requireHTTPSMiddleware(cfg Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cfg.Environment == "production" {
			secure := r.TLS != nil
			if cfg.TrustProxyHeaders && cfg.isTrustedProxy(r.RemoteAddr) &&
				r.Header.Get("X-Forwarded-Proto") == "https" {
				secure = true
			}
			if !secure {
				writeAPIError(w, http.StatusBadRequest, "HTTPS is required")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	appConfig = cfg

	db, err := InitDB(cfg.DatabasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := bootstrapAdmin(context.Background(), db, cfg); err != nil {
		log.Fatal(err)
	}
	if err := cleanupExpiredRecords(db); err != nil {
		log.Printf("initial expired-record cleanup failed: %v", err)
	}
	emailSender = newResendSender(cfg)

	handler := buildHandler(cfg)
	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	stopCleanup := make(chan struct{})
	go cleanupLoop(db, stopCleanup)

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("server listening address=%s environment=%s", cfg.ListenAddr, cfg.Environment)
		serverErrors <- server.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case signalValue := <-signals:
		log.Printf("shutdown signal=%s", signalValue)
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	}
	close(stopCleanup)
	shutdownContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}

func buildHandler(cfg Config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		if err := database.PingContext(r.Context()); err != nil {
			writeAPIError(w, http.StatusServiceUnavailable, "Not ready")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.Handle("/api/login", rateLimitMiddleware(loginLimiter, nil, http.HandlerFunc(HandleLogin)))
	mux.Handle("/api/create_account", rateLimitMiddleware(registrationLimiter, nil, http.HandlerFunc(HandleCreateAccount)))
	mux.Handle("/api/request-password-reset", rateLimitMiddleware(resetLimiter, nil, http.HandlerFunc(HandleRequestPasswordReset)))
	mux.HandleFunc("/api/logout", HandleLogout)
	mux.HandleFunc("/api/verify-email", HandleVerifyEmail)
	mux.HandleFunc("/api/reset-password", HandleResetPassword)

	mux.Handle("/api/get_tasks", authMiddleware(http.HandlerFunc(handleGetTodos)))
	mux.Handle("/api/add_task", authMiddleware(http.HandlerFunc(HandleAddTask)))
	mux.Handle("/api/remove_task", authMiddleware(http.HandlerFunc(HandleRemoveTask)))
	mux.Handle("/api/update_task", authMiddleware(http.HandlerFunc(HandleUpdateTask)))
	mux.Handle("/api/set_password", authMiddleware(http.HandlerFunc(HandleSetPassword)))
	mux.Handle("/api/current_user", authMiddleware(http.HandlerFunc(HandleCurrentUser)))
	mux.Handle("/api/profile", authMiddleware(http.HandlerFunc(HandleUpdateProfile)))
	mux.Handle("/api/get_habits", authMiddleware(http.HandlerFunc(HandleGetHabits)))
	mux.Handle("/api/add_habit", authMiddleware(http.HandlerFunc(HandleAddHabit)))
	mux.Handle("/api/delete_habit", authMiddleware(http.HandlerFunc(HandleDeleteHabit)))
	mux.Handle("/api/edit_habit", authMiddleware(http.HandlerFunc(HandleEditHabit)))
	mux.Handle("/api/complete_habit", authMiddleware(http.HandlerFunc(HandleCompleteHabit)))
	mux.Handle("/api/uncomplete_habit", authMiddleware(http.HandlerFunc(HandleUncompleteHabit)))
	mux.Handle("/api/skip_habit", authMiddleware(http.HandlerFunc(HandleSkipHabit)))
	mux.Handle("/api/unskip_habit", authMiddleware(http.HandlerFunc(HandleUnskipHabit)))

	mux.Handle("/api/register_user", adminMiddleware(http.HandlerFunc(HandleRegisterUser)))
	mux.Handle("/api/get_users", adminMiddleware(http.HandlerFunc(HandleGetUsers)))
	mux.Handle("/api/edit_user", adminMiddleware(http.HandlerFunc(HandleEditUser)))
	mux.Handle("/api/delete_user", adminMiddleware(http.HandlerFunc(HandleDeleteUser)))

	mux.Handle("/", http.FileServer(http.Dir(cfg.WebRoot)))

	var handler http.Handler = mux
	handler = securityHeadersMiddleware(cfg, handler)
	handler = requireHTTPSMiddleware(cfg, handler)
	handler = loggingMiddleware(handler)
	handler = recoveryMiddleware(handler)
	handler = requestIDMiddleware(handler)
	return handler
}

func cleanupLoop(db *sql.DB, stop <-chan struct{}) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := cleanupExpiredRecords(db); err != nil {
				log.Printf("expired-record cleanup failed: %v", err)
			}
		case <-stop:
			return
		}
	}
}
