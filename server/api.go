package main

// recieve api requests and send to other api file functions

// TODO
//	- add seperate users
//	- add task data saving
//	- add repeating tasks

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"

	"golang.org/x/term"
)

func main() {
	if !fileExists(userPath("admin")) {
		if err := createAdmin(); err != nil {
			log.Fatal(err)
		}
	}

	initTasks()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/get_tasks", handleGetTodos)
	mux.Handle("/", http.FileServer(http.Dir("../web")))
	mux.HandleFunc("/api/add_task", HandleAddTask)
	mux.HandleFunc("/api/remove_task", HandleRemoveTask)
	mux.HandleFunc("/api/update_task", HandleUpdateTask)
	mux.HandleFunc("/api/login", HandleLogin)
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
