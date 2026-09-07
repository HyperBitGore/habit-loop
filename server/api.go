package main

// recieve api requests and send to other api file functions

// TODO
//	- add seperate users
//	- add task data saving
//	- add repeating tasks
//  - Session creation and validation.
//  - Authentication middleware on every task route.
//  - Admin middleware on account-management routes.
//	- Concurrency protection around shared  tasks  state.
//	- Explicit validation and error responses.

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"golang.org/x/term"
)

func authMiddleware (next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("auth")
		if (err != nil || cookie.Value == "") {
			    http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		token, err := strconv.ParseUint(cookie.Value, 10, 64)
		if err != nil || !CheckSessionToken(token) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}


func main() {
	if !fileExists(userPath("admin")) {
		if err := createAdmin(); err != nil {
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
	log.Println("Server listening on http://localhost:8081")
	log.Fatal(http.ListenAndServe(":8081", mux))
}

func createAdmin() error {
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

	return AddUser(adminName, string(adminPassword))
}
