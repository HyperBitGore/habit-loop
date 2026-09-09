package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
)

var errHabitNotFound = errors.New("habit not found")

type Habit struct {
	Name        string      `json:"name"`
	Completions []time.Time `json:"completions"`
	Skips       []time.Time `json:"skips"`
	ID          uint64      `json:"id"`
}

func getUserHabits(db *sql.DB, user *User) ([]Habit, error) {
	rows, err := db.Query(`
		SELECT id, name
		FROM habits
		WHERE user_name = ?
		ORDER BY name, id
	`, user.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	habits := make([]Habit, 0)
	for rows.Next() {
		var habit Habit

		if err := rows.Scan(&habit.ID, &habit.Name); err != nil {
			return nil, err
		}

		habit.Completions, err = getHabitDates(db, `
			SELECT id, date
			FROM completions
			WHERE habit_id = ?
			ORDER BY date, id
		`, habit.ID)
		if err != nil {
			return nil, err
		}

		habit.Skips, err = getHabitDates(db, `
			SELECT id, date
			FROM skips
			WHERE habit_id = ?
			ORDER BY date, id
		`, habit.ID)
		if err != nil {
			return nil, err
		}

		habits = append(habits, habit)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return habits, nil
}

func getHabitDates(db *sql.DB, query string, habitID uint64) ([]time.Time, error) {
	rows, err := db.Query(query, habitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dates := make([]time.Time, 0)
	for rows.Next() {
		var (
			id   int
			date string
		)
		if err := rows.Scan(&id, &date); err != nil {
			return nil, err
		}

		parsedDate, err := parseTaskDate(date)
		if err != nil {
			return nil, err
		}
		dates = append(dates, parsedDate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return dates, nil
}

func parseHabitCompletions(value string) ([]time.Time, error) {
	if value == "" {
		return []time.Time{}, nil
	}
	var completions []time.Time
	if err := json.Unmarshal([]byte(value), &completions); err != nil {
		return nil, err
	}
	return completions, nil
}

func writeHabitMutationError(w http.ResponseWriter, err error) {
	if errors.Is(err, errHabitNotFound) {
		http.Error(w, "Habit not found", http.StatusNotFound)
		return
	}
	http.Error(w, "Failed to save habits", http.StatusInternalServerError)
}

func HandleGetHabits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	habits, err := getUserHabits(database, user)
	if err != nil {
		http.Error(w, "Failed to load habits", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(habits); err != nil {
		http.Error(w, "Failed to encode habits", http.StatusInternalServerError)
	}
}

func HandleAddHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := r.Header.Get("X-Habit-Name")
	if name == "" {
		http.Error(w, "Habit name is required", http.StatusBadRequest)
		return
	}
	completions, err := parseHabitCompletions(r.Header.Get("X-Habit-Completions"))
	if err != nil {
		http.Error(w, "Invalid habit completions", http.StatusBadRequest)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}

	tx, err := database.Begin()
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		"INSERT INTO habits (user_name, name) VALUES (?, ?)",
		user.Name,
		name,
	)
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	habitID, err := result.LastInsertId()
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	for _, completion := range completions {
		_, err = tx.Exec(
			"INSERT INTO completions (habit_id, date) VALUES (?, ?)",
			habitID,
			completion.Format("2006-01-02"),
		)
		if err != nil {
			writeHabitMutationError(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeHabitMutationError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

type habitQueryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

func habitExists(db habitQueryer, userName string, id uint64) (bool, error) {
	var exists bool
	err := db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM habits WHERE id = ? AND user_name = ?)",
		id,
		userName,
	).Scan(&exists)
	return exists, err
}

func HandleDeleteHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseUint(r.Header.Get("X-Habit-ID"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid habit ID", http.StatusBadRequest)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}

	tx, err := database.Begin()
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	defer tx.Rollback()

	exists, err := habitExists(tx, user.Name, id)
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if !exists {
		writeHabitMutationError(w, errHabitNotFound)
		return
	}
	if _, err := tx.Exec("DELETE FROM completions WHERE habit_id = ?", id); err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if _, err := tx.Exec("DELETE FROM skips WHERE habit_id = ?", id); err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if _, err := tx.Exec("DELETE FROM habits WHERE id = ?", id); err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeHabitMutationError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func HandleEditHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseUint(r.Header.Get("X-Habit-ID"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid habit ID", http.StatusBadRequest)
		return
	}
	name := r.Header.Get("X-Habit-Name")
	if name == "" {
		http.Error(w, "Habit name is required", http.StatusBadRequest)
		return
	}
	completions, err := parseHabitCompletions(r.Header.Get("X-Habit-Completions"))
	if err != nil {
		http.Error(w, "Invalid habit completions", http.StatusBadRequest)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}

	tx, err := database.Begin()
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	defer tx.Rollback()

	exists, err := habitExists(tx, user.Name, id)
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if !exists {
		writeHabitMutationError(w, errHabitNotFound)
		return
	}
	if _, err := tx.Exec(
		"UPDATE habits SET name = ? WHERE id = ?",
		name,
		id,
	); err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if _, err := tx.Exec("DELETE FROM completions WHERE habit_id = ?", id); err != nil {
		writeHabitMutationError(w, err)
		return
	}
	for _, completion := range completions {
		if _, err := tx.Exec(
			"INSERT INTO completions (habit_id, date) VALUES (?, ?)",
			id,
			completion.Format("2006-01-02"),
		); err != nil {
			writeHabitMutationError(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeHabitMutationError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func HandleCompleteHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var completion struct {
		HabitID uint64 `json:"habit_id"`
		Date    string `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&completion); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	date, err := time.Parse("2006-01-02", completion.Date)
	if err != nil {
		http.Error(w, "Invalid completion date", http.StatusBadRequest)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}

	tx, err := database.Begin()
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	defer tx.Rollback()
	exists, err := habitExists(tx, user.Name, completion.HabitID)
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if !exists {
		writeHabitMutationError(w, errHabitNotFound)
		return
	}
	dateValue := date.Format("2006-01-02")
	if _, err := tx.Exec(
		"DELETE FROM skips WHERE habit_id = ? AND date = ?",
		completion.HabitID,
		dateValue,
	); err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if _, err := tx.Exec(
		"INSERT INTO completions (habit_id, date) SELECT ?, ? WHERE NOT EXISTS (SELECT 1 FROM completions WHERE habit_id = ? AND date = ?)",
		completion.HabitID,
		dateValue,
		completion.HabitID,
		dateValue,
	); err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeHabitMutationError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func HandleUncompleteHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var completion struct {
		HabitID uint64 `json:"habit_id"`
		Date    string `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&completion); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	date, err := time.Parse("2006-01-02", completion.Date)
	if err != nil {
		http.Error(w, "Invalid completion date", http.StatusBadRequest)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}

	exists, err := habitExists(database, user.Name, completion.HabitID)
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if !exists {
		writeHabitMutationError(w, errHabitNotFound)
		return
	}
	if _, err := database.Exec(
		"DELETE FROM completions WHERE habit_id = ? AND date = ?",
		completion.HabitID,
		date.Format("2006-01-02"),
	); err != nil {
		writeHabitMutationError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func HandleSkipHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var skip struct {
		HabitID uint64 `json:"habit_id"`
		Date    string `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&skip); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	date, err := time.Parse("2006-01-02", skip.Date)
	if err != nil {
		http.Error(w, "Invalid skip date", http.StatusBadRequest)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}

	tx, err := database.Begin()
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	defer tx.Rollback()
	exists, err := habitExists(tx, user.Name, skip.HabitID)
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if !exists {
		writeHabitMutationError(w, errHabitNotFound)
		return
	}
	dateValue := date.Format("2006-01-02")
	if _, err := tx.Exec(
		"DELETE FROM completions WHERE habit_id = ? AND date = ?",
		skip.HabitID,
		dateValue,
	); err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if _, err := tx.Exec(
		"INSERT INTO skips (habit_id, date) SELECT ?, ? WHERE NOT EXISTS (SELECT 1 FROM skips WHERE habit_id = ? AND date = ?)",
		skip.HabitID,
		dateValue,
		skip.HabitID,
		dateValue,
	); err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeHabitMutationError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func HandleUnskipHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var skip struct {
		HabitID uint64 `json:"habit_id"`
		Date    string `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&skip); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	date, err := time.Parse("2006-01-02", skip.Date)
	if err != nil {
		http.Error(w, "Invalid skip date", http.StatusBadRequest)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}

	exists, err := habitExists(database, user.Name, skip.HabitID)
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if !exists {
		writeHabitMutationError(w, errHabitNotFound)
		return
	}
	if _, err := database.Exec(
		"DELETE FROM skips WHERE habit_id = ? AND date = ?",
		skip.HabitID,
		date.Format("2006-01-02"),
	); err != nil {
		writeHabitMutationError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
