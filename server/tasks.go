package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"
)

type Task struct {
	Name     string    `json:"name"`
	Date     time.Time `json:"date"`
	Complete bool      `json:"complete"`
}

var tasks []Task

func initTasks() {
	fmt.Println("Initing Tasks!")
	tasks = make([]Task, 0, 4096)
}

func handleGetTodos(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Recieved a todo list request")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	date := r.URL.Query().Get("date")
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	todayTasks := make([]Task, 0)
	for _, task := range tasks {
		if task.Date.Format("2006-01-02") == date {
			todayTasks = append(todayTasks, task)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(todayTasks); err != nil {
		http.Error(w, "Failed to encode tasks", http.StatusInternalServerError)
	}
}

func addTask(name string, date time.Time, complete bool) {
	task := Task{Name: name, Date: date, Complete: complete}
	tasks = append(tasks, task)
}

func HandleAddTask(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Adding task!")
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := r.Header.Get("X-Task-Name")
	layout := "2006-01-02 15:04:05"
	date, err := time.Parse(layout, r.Header.Get("X-Task-Date"))
	if err != nil {
		fmt.Println("Error parsing time:", err)
		return
	}
	complete, err := strconv.ParseBool(r.Header.Get("X-Task-Complete"))
	if err != nil {
		fmt.Println("Error parsing complete: ", err)
	}
	addTask(name, date, complete)
}

func removeTask(name string, date time.Time, complete bool) {
	idx := slices.Index(tasks, Task{Name: name, Date: date, Complete: complete})
	if idx != -1 {
		tasks = append(tasks[:idx], tasks[idx+1:]...)
	}
}

func HandleRemoveTask(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Removing Task!")
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := r.Header.Get("X-Task-Name")
	layout := "2006-01-02 15:04:05"
	date, err := time.Parse(layout, r.Header.Get("X-Task-Date"))
	if err != nil {
		fmt.Println("Error parsing time:", err)
		return
	}
	complete, err := strconv.ParseBool(r.Header.Get("X-Task-Complete"))
	if err != nil {
		fmt.Println("Error parsing complete: ", err)
	}
	removeTask(name, date, complete)
}
