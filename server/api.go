package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// REFINE
//	- Move everything to clean seperate packages
//	- Remove unneeded code
//	- Code audit for simplicity
//	- Optimizations
//	- Move frontend to React
//	- add better admin controls
//		- see users total todos/habits
//	- Calendar view
//	- Calendar list select year and month seperate
// EXTEND
//	- Repeatable todos?
//		- Roll habits into this?? (no I think having seperate habit tracker important)
//	- Todos can stay in your day if you don't complete them, so just roll over to next day??
//	- List views can be minimized
//	- Reorderable lists
//	- Zoom habit tracking out further
//	- Habits can have subtasks
//	- App version for clients
//	- Todos can sticky to habits and only appear when that habit appears
//	- Multi tiered todos
//		- multiple completeables inside a todo
//		- maybe remove goals??

var (
	appConfig            Config
	emailSender          EmailSender
	appStore             *Store
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
		user, err := appStore.GetUserFromToken(r.Context(), cookie.Value)
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
	configPath := flag.String("config", "server.cfg", "path to the server configuration file")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	appConfig = cfg

	db, err := InitDB(cfg.DatabasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	appStore = NewStore(db)
	if err := appStore.BootstrapAdmin(context.Background(), cfg); err != nil {
		log.Fatal(err)
	}
	if err := appStore.CleanupExpiredRecords(context.Background()); err != nil {
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
	go cleanupLoop(appStore, stopCleanup)

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
		if err := appStore.PingContext(r.Context()); err != nil {
			writeAPIError(w, http.StatusServiceUnavailable, "Not ready")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/app-config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"title":                    cfg.AppTitle,
			"contact_email":            cfg.ContactEmail,
			"adsense_publisher_id":     cfg.AdSensePublisherID,
			"adsense_ad_slot":          cfg.AdSenseAdSlot,
			"adsense_test_placement":   cfg.AdSenseTestPlacement,
			"adsense_authed_pages":     cfg.AdSenseAuthedPages,
			"adsense_non_personalized": cfg.AdSenseNonPersonalized,
			"public_registration":      cfg.PublicRegistration,
		})
	})
	mux.HandleFunc("/ads.txt", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		if cfg.AdSensePublisherID == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "google.com, %s, DIRECT, f08c47fec0942fa0\n", cfg.AdSensePublisherID)
	})
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, "User-agent: *\nDisallow: /api/\n")
		if cfg.AppBaseURL != nil {
			fmt.Fprintf(w, "Sitemap: %s/sitemap.xml\n", strings.TrimRight(cfg.AppBaseURL.String(), "/"))
		}
	})
	mux.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		if cfg.AppBaseURL == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		base := strings.TrimRight(cfg.AppBaseURL.String(), "/")
		publicPages := []string{
			"/",
			"/login.html",
			"/guides.html",
			"/guide-starting-habits.html",
			"/guide-streaks-and-skips.html",
			"/guide-daily-planning.html",
			"/privacy.html",
			"/terms.html",
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		fmt.Fprint(w, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
		fmt.Fprint(w, "<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")
		for _, page := range publicPages {
			file := page
			if file == "/" {
				file = "/index.html"
			}
			lastmod := ""
			if info, err := os.Stat(filepath.Join(cfg.WebRoot, filepath.FromSlash(file))); err == nil {
				lastmod = fmt.Sprintf("<lastmod>%s</lastmod>", info.ModTime().UTC().Format("2006-01-02"))
			}
			fmt.Fprintf(w, "  <url><loc>%s%s</loc>%s</url>\n", base, page, lastmod)
		}
		fmt.Fprint(w, "</urlset>\n")
	})

	mux.Handle("/api/login", rateLimitMiddleware(loginLimiter, nil, http.HandlerFunc(HandleLogin)))
	mux.Handle("/api/create_account", rateLimitMiddleware(registrationLimiter, nil, http.HandlerFunc(HandleCreateAccount)))
	mux.Handle("/api/request-password-reset", rateLimitMiddleware(resetLimiter, nil, http.HandlerFunc(HandleRequestPasswordReset)))
	mux.HandleFunc("/api/logout", HandleLogout)
	mux.HandleFunc("/api/verify-email", HandleVerifyEmail)
	mux.HandleFunc("/api/reset-password", HandleResetPassword)

	mux.Handle("/api/get_tasks", authMiddleware(http.HandlerFunc(handleGetTodos)))
	mux.Handle("/api/reorder", authMiddleware(http.HandlerFunc(HandleReorder)))
	mux.Handle("/api/add_task", authMiddleware(http.HandlerFunc(HandleAddTask)))
	mux.Handle("/api/remove_task", authMiddleware(http.HandlerFunc(HandleRemoveTask)))
	mux.Handle("/api/update_task", authMiddleware(http.HandlerFunc(HandleUpdateTask)))
	mux.Handle("/api/notes", authMiddleware(http.HandlerFunc(HandleSaveNote)))
	mux.Handle("/api/get_note", authMiddleware(http.HandlerFunc(HandleGetNote)))
	mux.Handle("/api/todo_history", authMiddleware(http.HandlerFunc(HandleTodoHistory)))
	mux.Handle("/api/goals", authMiddleware(http.HandlerFunc(HandleGoals)))
	mux.Handle("/api/delete-account", authMiddleware(http.HandlerFunc(HandleDeleteAccount)))
	mux.Handle("/api/set_password", authMiddleware(http.HandlerFunc(HandleSetPassword)))
	mux.Handle("/api/current_user", authMiddleware(http.HandlerFunc(HandleCurrentUser)))
	mux.Handle("/api/profile", authMiddleware(http.HandlerFunc(HandleUpdateProfile)))
	mux.Handle("/api/session_length", authMiddleware(http.HandlerFunc(HandleSetSessionLength)))
	mux.Handle("/api/import/uhabit", authMiddleware(http.HandlerFunc(HandleUHabitDBUpload)))
	mux.Handle("/api/export/uhabit", authMiddleware(http.HandlerFunc(HandleUHabitDBExport)))
	mux.Handle("/api/export/csv", authMiddleware(http.HandlerFunc(HandleHabitCSVExport)))
	mux.Handle("/api/get_habits", authMiddleware(http.HandlerFunc(HandleGetHabits)))
	mux.Handle("/api/habit_summary", authMiddleware(http.HandlerFunc(HandleHabitSummary)))
	mux.Handle("/api/add_habit", authMiddleware(http.HandlerFunc(HandleAddHabit)))
	mux.Handle("/api/delete_habit", authMiddleware(http.HandlerFunc(HandleDeleteHabit)))
	mux.Handle("/api/edit_habit", authMiddleware(http.HandlerFunc(HandleEditHabit)))
	mux.Handle("/api/complete_habit", authMiddleware(http.HandlerFunc(HandleCompleteHabit)))
	mux.Handle("/api/uncomplete_habit", authMiddleware(http.HandlerFunc(HandleUncompleteHabit)))
	mux.Handle("/api/skip_habit", authMiddleware(http.HandlerFunc(HandleSkipHabit)))
	mux.Handle("/api/unskip_habit", authMiddleware(http.HandlerFunc(HandleUnskipHabit)))
	mux.Handle("/api/save_metric", authMiddleware(http.HandlerFunc(HandleSaveMetric)))
	mux.Handle("/api/set_habit_status", authMiddleware(http.HandlerFunc(HandleSetHabitStatus)))

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

func cleanupLoop(store *Store, stop <-chan struct{}) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := store.CleanupExpiredRecords(context.Background()); err != nil {
				log.Printf("expired-record cleanup failed: %v", err)
			}
		case <-stop:
			return
		}
	}
}
