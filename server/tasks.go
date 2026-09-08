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

func handleGetTodos(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Recieved a todo list request")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cookie, err := r.Cookie("auth")
	if err != nil || cookie.Value == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	token, err := strconv.ParseUint(cookie.Value, 10, 64)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	tasks := GetUserTasks(token)
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

func addTask(tasks *[]Task, nextTaskID *uint64, name string, date time.Time, complete bool) {
	task := Task{Name: name, Date: date, Complete: complete, ID: atomic.AddUint64(nextTaskID, 1)}
	*tasks = append(*tasks, task)
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
		http.Error(w, "Invalid task date", http.StatusBadRequest)
		return
	}
	complete, err := strconv.ParseBool(r.Header.Get("X-Task-Complete"))
	if err != nil {
		http.Error(w, "Invalid task completion value", http.StatusBadRequest)
		return
	}
	cookie, err := r.Cookie("auth")
	if err != nil || cookie.Value == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	token, err := strconv.ParseUint(cookie.Value, 10, 64)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	user := GetUserFromToken(token)
	addTask(&user.Tasks, &user.NextTaskID, name, date, complete)
	user_map[user.ID] = *user
}

func removeTask(tasks *[]Task, id uint64) {
	idx := slices.IndexFunc(*tasks, func(n Task) bool {
		return id == n.ID
	})
	if idx != -1 {
		*tasks = append((*tasks)[:idx], (*tasks)[idx+1:]...)
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
		http.Error(w, "Invalid task ID", http.StatusBadRequest)
		return
	}
	cookie, err := r.Cookie("auth")
	if err != nil || cookie.Value == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	token, err := strconv.ParseUint(cookie.Value, 10, 64)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	user := GetUserFromToken(token)
	removeTask(&user.Tasks, id)
	user_map[user.ID] = *user
}

func editTask(tasks *[]Task, id uint64, name string, date time.Time, complete bool) {
	idx := slices.IndexFunc(*tasks, func(n Task) bool {
		return id == n.ID
	})
	if idx != -1 {
		(*tasks)[idx] = Task{Name: name, Date: date, Complete: complete, ID: id}
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
		http.Error(w, "Invalid task ID", http.StatusBadRequest)
		return
	}
	name := r.Header.Get("X-Task-Name")
	date, err := parseTaskDate(r.Header.Get("X-Task-Date"))
	if err != nil {
		http.Error(w, "Invalid task date", http.StatusBadRequest)
		return
	}
	complete, err := strconv.ParseBool(r.Header.Get("X-Task-Complete"))
	if err != nil {
		http.Error(w, "Invalid task completion value", http.StatusBadRequest)
		return
	}
	cookie, err := r.Cookie("auth")
	if err != nil || cookie.Value == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	token, err := strconv.ParseUint(cookie.Value, 10, 64)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	user := GetUserFromToken(token)
	editTask(&user.Tasks, id, name, date, complete)
	user_map[user.ID] = *user
}
