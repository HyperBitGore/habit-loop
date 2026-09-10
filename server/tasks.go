package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Task struct {
	Name     string    `json:"name"`
	Date     time.Time `json:"date"`
	Complete bool      `json:"complete"`
	ID       uint64    `json:"id"`
}

func validateItemName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("name is required")
	}
	if utf8.RuneCountInString(name) > 200 {
		return "", fmt.Errorf("name must be at most 200 characters")
	}
	return name, nil
}

func getUserTasks(db *sql.DB, userID int, date string) ([]Task, error) {
	rows, err := db.Query(`
		SELECT id, name, date, complete
		FROM todos
		WHERE user_id = ? AND substr(date, 1, 10) = ?
		ORDER BY date, id
	`, userID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]Task, 0)
	for rows.Next() {
		var task Task
		var storedDate string
		if err := rows.Scan(&task.ID, &task.Name, &storedDate, &task.Complete); err != nil {
			return nil, err
		}
		task.Date, err = parseTaskDate(storedDate)
		if err != nil {
			return nil, fmt.Errorf("parse task %d date: %w", task.ID, err)
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func handleGetTodos(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	date := r.URL.Query().Get("date")
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid task date")
		return
	}
	tasks, err := getUserTasks(database, user.ID, date)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to load tasks")
		return
	}
	writeJSON(w, http.StatusOK, tasks)
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
	if r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	name, err := validateItemName(r.Header.Get("X-Task-Name"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	date, err := parseTaskDate(r.Header.Get("X-Task-Date"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid task date")
		return
	}
	complete, err := strconv.ParseBool(r.Header.Get("X-Task-Complete"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid task completion value")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	if _, err := database.ExecContext(r.Context(), `
		INSERT INTO todos (user_id, name, date, complete)
		VALUES (?, ?, ?, ?)
	`, user.ID, name, date.Format(time.RFC3339), complete); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to save task")
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func HandleRemoveTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	id, err := strconv.ParseUint(r.Header.Get("X-Task-ID"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid task ID")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	result, err := database.ExecContext(r.Context(), "DELETE FROM todos WHERE id = ? AND user_id = ?", id, user.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to delete task")
		return
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to delete task")
		return
	}
	if rowsAffected == 0 {
		writeAPIError(w, http.StatusNotFound, "Task not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func HandleUpdateTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	id, err := strconv.ParseUint(r.Header.Get("X-Task-ID"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid task ID")
		return
	}
	name, err := validateItemName(r.Header.Get("X-Task-Name"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	date, err := parseTaskDate(r.Header.Get("X-Task-Date"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid task date")
		return
	}
	complete, err := strconv.ParseBool(r.Header.Get("X-Task-Complete"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid task completion value")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	result, err := database.ExecContext(r.Context(), `
		UPDATE todos SET name = ?, date = ?, complete = ?
		WHERE id = ? AND user_id = ?
	`, name, date.Format(time.RFC3339), complete, id, user.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to update task")
		return
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to update task")
		return
	}
	if rowsAffected == 0 {
		writeAPIError(w, http.StatusNotFound, "Task not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func encodeTasks(tasks []Task) ([]byte, error) {
	return json.Marshal(tasks)
}
