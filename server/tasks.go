package main

import (
	"encoding/json"
	"errors"
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
	tasks, err := appStore.ListTasks(r.Context(), user.ID, date)
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
	err = appStore.AddTask(r.Context(), user.ID, name, date.Format(time.RFC3339), complete)
	if err != nil {
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
	err = appStore.DeleteTask(r.Context(), user.ID, id)
	if err != nil {
		if errors.Is(err, ErrTaskNotFound) {
			writeAPIError(w, http.StatusNotFound, "Task not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, err.Error())
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
	err = appStore.UpdateTask(r.Context(), user.ID, id, name, date.Format(time.RFC3339), complete)
	if err != nil {
		if errors.Is(err, ErrTaskNotFound) {
			writeAPIError(w, http.StatusNotFound, "Task not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func encodeTasks(tasks []Task) ([]byte, error) {
	return json.Marshal(tasks)
}
