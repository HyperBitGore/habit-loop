package main

import (
	"fmt"
	"net/http"
	"time"
)

type Task struct {
	Name     string
	Date     time.Time
	Complete bool
}

var tasks []Task

func initTasks() {
	fmt.Println("Initing Tasks!")
	tasks = make([]Task, 1024, 4096)
}

func handleGetTodos (w http.ResponseWriter, r *http.Request) {
	fmt.Println("Recieved a todo list request")
	if r.Method != http.MethodGet {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }
	fmt.Fprintln(w, "Hello from Go")
}