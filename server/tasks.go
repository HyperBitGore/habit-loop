package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"sync/atomic"
	"time"
)

type Task struct {
	Name     string    `json:"name"`
	Date     time.Time `json:"date"`
	Complete bool      `json:"complete"`
	ID       uint64    `json:"id"`
}

var tasks []Task
var nextTaskID uint64

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
	task := Task{Name: name, Date: date, Complete: complete, ID: atomic.AddUint64(&nextTaskID, 1)}
	tasks = append(tasks, task)
}

func parseTaskDate(value string) (time.Time, error) {
	if date, err := time.Parse(time.RFC3339, value); err == nil {
		return date, nil
	}
	return time.Parse("2006-01-02 15:04:05", value)
}

func HandleAddTask(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Adding task!")
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := r.Header.Get("X-Task-Name")
	date, err := parseTaskDate(r.Header.Get("X-Task-Date"))
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

func removeTask(id uint64) {
	idx := slices.IndexFunc(tasks, func(n Task) bool {
		return id == n.ID
	})
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
	id, err := strconv.ParseUint(r.Header.Get("X-Task-ID"), 10, 64)
	if err != nil {
		fmt.Println("Error parsing ID: ", err)
	}
	removeTask(id)
}

func editTask(id uint64, name string, date time.Time, complete bool) {
	idx := slices.IndexFunc(tasks, func(n Task) bool {
		return id == n.ID
	})
	fmt.Printf("ID: %d", idx)
	if idx != -1 {
		tasks[idx] = Task{Name: name, Date: date, Complete: complete, ID: id}
	}
}

func HandleUpdateTask(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Updating Task!")
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseUint(r.Header.Get("X-Task-ID"), 10, 64)
	if err != nil {
		fmt.Println("Error parsing ID: ", err)
	}
	name := r.Header.Get("X-Task-Name")
	date, err := parseTaskDate(r.Header.Get("X-Task-Date"))
	if err != nil {
		fmt.Println("Error parsing time:", err)
		return
	}
	complete, err := strconv.ParseBool(r.Header.Get("X-Task-Complete"))
	if err != nil {
		fmt.Println("Error parsing complete: ", err)
	}
	editTask(id, name, date, complete)
}
