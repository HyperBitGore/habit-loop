package main

import (
	"database/sql"
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
	ID       uint64    `json:"id"`
}

func getUserTasks(db *sql.DB, user *User) ([]Task, error) {
	rows, err := db.Query(`
		SELECT id, name, date, complete
		FROM todos
		WHERE user_name = ?
		ORDER BY date, id
	`, user.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		var task Task
		var date string

		if err := rows.Scan(&task.ID, &task.Name, &date, &task.Complete); err != nil {
			return nil, err
		}
		task.Date, err = parseTaskDate(date)
		if err != nil {
			return nil, fmt.Errorf("parse task %d date: %w", task.ID, err)
		}

		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return tasks, nil
}

func handleGetTodos(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Recieved a todo list request")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	tasks, err := getUserTasks(database, user)
	if err != nil {
		http.Error(w, "Failed to load tasks", http.StatusInternalServerError)
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

func addTask(tasks *[]Task, nextTaskID *uint64, name string, date time.Time, complete bool) {
	*nextTaskID = *nextTaskID + 1
	task := Task{Name: name, Date: date, Complete: complete, ID: *nextTaskID}
	*tasks = append(*tasks, task)
}

func parseTaskDate(value string) (time.Time, error) {
	if date, err := time.Parse(time.RFC3339, value); err == nil {
		return date, nil
	}
	if date, err := time.Parse("2006-01-02 15:04:05", value); err == nil {
		return date, nil
	}
	return time.Parse("2006-01-02", value)
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
	user := requestUser(w, r)
	if user == nil {
		return
	}

	_, err = database.Exec(`
		INSERT INTO todos (user_name, name, date, complete)
		VALUES (?, ?, ?, ?)
	`, user.Name, name, date.Format(time.RFC3339), complete)
	if err != nil {
		http.Error(w, "Failed to save task", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
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
	user := requestUser(w, r)
	if user == nil {
		return
	}

	result, err := database.Exec(
		"DELETE FROM todos WHERE id = ? AND user_name = ?",
		id,
		user.Name,
	)
	if err != nil {
		http.Error(w, "Failed to save task", http.StatusInternalServerError)
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Failed to verify task deletion", http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
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
	user := requestUser(w, r)
	if user == nil {
		return
	}

	result, err := database.Exec(`
		UPDATE todos
		SET name = ?, date = ?, complete = ?
		WHERE id = ? AND user_name = ?
	`, name, date.Format(time.RFC3339), complete, id, user.Name)
	if err != nil {
		http.Error(w, "Failed to save task", http.StatusInternalServerError)
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Failed to verify task update", http.StatusInternalServerError)
		return
	}
	if rowsAffected == 0 {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
