package main

// recieve api requests and send to other api file functions

// TODO
//	- implement security fixes
//	- change css to actually look good
//		- prob switch to react, just use fable with a detailed doc
//	- need privacy policy, terms, cookie notice?, email policy
//	- cloudflare turnstile
//	- write tests

import (
	"bufio"
	"database/sql"
	"fmt"
	"golang.org/x/term"
	"log"
	"net/http"
	"os"
)

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("auth")
		if err != nil || cookie.Value == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if !CheckSessionToken(cookie.Value) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func adminMiddleware(next http.Handler) http.Handler {
	return authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := requestUser(w, r)
		if user == nil {
			return
		}
		if user.Role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func main() {
	InitDB()
	adminExists, err := UserExists(database, "admin")
	if err != nil {
		log.Fatal(err)
	}
	if !adminExists {
		if err := createAdmin(database); err != nil {
			log.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	mux.Handle("/api/get_tasks", authMiddleware(
		http.HandlerFunc(handleGetTodos),
	))
	mux.Handle("/", http.FileServer(http.Dir("../web")))
	mux.Handle("/api/add_task", authMiddleware(
		http.HandlerFunc(HandleAddTask),
	))

	mux.Handle("/api/remove_task", authMiddleware(
		http.HandlerFunc(HandleRemoveTask),
	))

	mux.Handle("/api/update_task", authMiddleware(
		http.HandlerFunc(HandleUpdateTask),
	))
	mux.HandleFunc("/api/login", HandleLogin)
	mux.HandleFunc("/api/logout", HandleLogout)
	mux.HandleFunc("/api/create_account", HandleCreateAccount)
	mux.HandleFunc("/api/verify-email", HandleVerifyEmail)
	mux.HandleFunc("/api/request-password-reset", HandleRequestPasswordReset)
	mux.HandleFunc("/api/reset-password", HandleResetPassword)
	mux.Handle("/api/register_user", adminMiddleware(
		http.HandlerFunc(HandleRegisterUser),
	))
	mux.Handle("/api/get_users", adminMiddleware(
		http.HandlerFunc(HandleGetUsers),
	))
	mux.Handle("/api/edit_user", adminMiddleware(
		http.HandlerFunc(HandleEditUser),
	))
	mux.Handle("/api/delete_user", adminMiddleware(
		http.HandlerFunc(HandleDeleteUser),
	))
	mux.Handle("/api/set_password", authMiddleware(
		http.HandlerFunc(HandleSetPassword),
	))
	mux.Handle("/api/current_user", authMiddleware(
		http.HandlerFunc(HandleCurrentUser),
	))
	mux.Handle("/api/profile", authMiddleware(
		http.HandlerFunc(HandleUpdateProfile),
	))
	mux.Handle("/api/get_habits", authMiddleware(
		http.HandlerFunc(HandleGetHabits),
	))
	mux.Handle("/api/add_habit", authMiddleware(
		http.HandlerFunc(HandleAddHabit),
	))
	mux.Handle("/api/delete_habit", authMiddleware(
		http.HandlerFunc(HandleDeleteHabit),
	))
	mux.Handle("/api/edit_habit", authMiddleware(
		http.HandlerFunc(HandleEditHabit),
	))
	mux.Handle("/api/complete_habit", authMiddleware(
		http.HandlerFunc(HandleCompleteHabit),
	))
	mux.Handle("/api/uncomplete_habit", authMiddleware(
		http.HandlerFunc(HandleUncompleteHabit),
	))
	mux.Handle("/api/skip_habit", authMiddleware(
		http.HandlerFunc(HandleSkipHabit),
	))
	mux.Handle("/api/unskip_habit", authMiddleware(
		http.HandlerFunc(HandleUnskipHabit),
	))
	log.Println("Server listening on http://localhost:8081")
	log.Fatal(http.ListenAndServe(":8081", mux))
}

func createAdmin(db *sql.DB) error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Print("Input admin name: ")
	if !scanner.Scan() {
		return fmt.Errorf("failed to read admin name: %w", scanner.Err())
	}
	adminName := scanner.Text()
	fmt.Printf("Welcome, %s!\n", adminName)

	fmt.Print("Input admin password: ")
	adminPassword, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("failed to read admin password: %w", err)
	}
	fmt.Println()

	return AddUser(db, adminName, string(adminPassword), "admin")
}
