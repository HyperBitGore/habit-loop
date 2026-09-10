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

func getUserHabits(db *sql.DB, userID int) ([]Habit, error) {
	rows, err := db.Query(`
		SELECT h.id, h.name, 'completion', c.date
		FROM habits h
		LEFT JOIN completions c ON c.habit_id = h.id
		WHERE h.user_id = ?
		UNION ALL
		SELECT h.id, h.name, 'skip', s.date
		FROM habits h
		LEFT JOIN skips s ON s.habit_id = h.id
		WHERE h.user_id = ?
		ORDER BY 2, 1, 4
	`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	habitsByID := make(map[uint64]*Habit)
	order := make([]uint64, 0)
	for rows.Next() {
		var id uint64
		var name, kind string
		var date sql.NullString
		if err := rows.Scan(&id, &name, &kind, &date); err != nil {
			return nil, err
		}
		habit := habitsByID[id]
		if habit == nil {
			habit = &Habit{ID: id, Name: name, Completions: []time.Time{}, Skips: []time.Time{}}
			habitsByID[id] = habit
			order = append(order, id)
		}
		if !date.Valid {
			continue
		}
		parsed, err := parseTaskDate(date.String)
		if err != nil {
			return nil, err
		}
		if kind == "completion" {
			habit.Completions = append(habit.Completions, parsed)
		} else {
			habit.Skips = append(habit.Skips, parsed)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	habits := make([]Habit, 0, len(order))
	for _, id := range order {
		habits = append(habits, *habitsByID[id])
	}
	return habits, nil
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
		writeAPIError(w, http.StatusNotFound, "Habit not found")
		return
	}
	writeAPIError(w, http.StatusInternalServerError, "Unable to save habit")
}

func HandleGetHabits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	habits, err := getUserHabits(database, user.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to load habits")
		return
	}
	writeJSON(w, http.StatusOK, habits)
}

func HandleAddHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	name, err := validateItemName(r.Header.Get("X-Habit-Name"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	completions, err := parseHabitCompletions(r.Header.Get("X-Habit-Completions"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid habit completions")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	tx, err := database.BeginTx(r.Context(), nil)
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), "INSERT INTO habits (user_id, name) VALUES (?, ?)", user.ID, name)
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
		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO completions (habit_id, date) VALUES (?, ?)
			ON CONFLICT(habit_id, date) DO NOTHING
		`, habitID, completion.Format("2006-01-02")); err != nil {
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

func HandleDeleteHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	id, err := strconv.ParseUint(r.Header.Get("X-Habit-ID"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid habit ID")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	result, err := database.ExecContext(r.Context(), "DELETE FROM habits WHERE id = ? AND user_id = ?", id, user.ID)
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if affected == 0 {
		writeHabitMutationError(w, errHabitNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func HandleEditHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	id, err := strconv.ParseUint(r.Header.Get("X-Habit-ID"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid habit ID")
		return
	}
	name, err := validateItemName(r.Header.Get("X-Habit-Name"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	completions, err := parseHabitCompletions(r.Header.Get("X-Habit-Completions"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid habit completions")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	tx, err := database.BeginTx(r.Context(), nil)
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), "UPDATE habits SET name = ? WHERE id = ? AND user_id = ?", name, id, user.ID)
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if affected == 0 {
		writeHabitMutationError(w, errHabitNotFound)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		DELETE FROM completions
		WHERE habit_id IN (SELECT id FROM habits WHERE id = ? AND user_id = ?)
	`, id, user.ID); err != nil {
		writeHabitMutationError(w, err)
		return
	}
	for _, completion := range completions {
		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO completions (habit_id, date)
			SELECT id, ? FROM habits WHERE id = ? AND user_id = ?
			ON CONFLICT(habit_id, date) DO NOTHING
		`, completion.Format("2006-01-02"), id, user.ID); err != nil {
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

type habitDateRequest struct {
	HabitID uint64 `json:"habit_id"`
	Date    string `json:"date"`
}

func decodeHabitDate(w http.ResponseWriter, r *http.Request) (habitDateRequest, string, bool) {
	var request habitDateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return request, "", false
	}
	date, err := time.Parse("2006-01-02", request.Date)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid habit date")
		return request, "", false
	}
	return request, date.Format("2006-01-02"), true
}

func mutateHabitDate(w http.ResponseWriter, r *http.Request, insertTable string, deleteTable string) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	request, date, ok := decodeHabitDate(w, r)
	if !ok {
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	tx, err := database.BeginTx(r.Context(), nil)
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(r.Context(), `
		SELECT EXISTS(SELECT 1 FROM habits WHERE id = ? AND user_id = ?)
	`, request.HabitID, user.ID).Scan(&exists); err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if !exists {
		writeHabitMutationError(w, errHabitNotFound)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		DELETE FROM `+deleteTable+`
		WHERE habit_id IN (SELECT id FROM habits WHERE id = ? AND user_id = ?) AND date = ?
	`, request.HabitID, user.ID, date); err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO `+insertTable+` (habit_id, date)
		SELECT id, ? FROM habits WHERE id = ? AND user_id = ?
		ON CONFLICT(habit_id, date) DO NOTHING
	`, date, request.HabitID, user.ID); err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeHabitMutationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func removeHabitDate(w http.ResponseWriter, r *http.Request, table string) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	request, date, ok := decodeHabitDate(w, r)
	if !ok {
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	result, err := database.ExecContext(r.Context(), `
		DELETE FROM `+table+`
		WHERE habit_id IN (SELECT id FROM habits WHERE id = ? AND user_id = ?) AND date = ?
	`, request.HabitID, user.ID, date)
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	if _, err := result.RowsAffected(); err != nil {
		writeHabitMutationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func HandleCompleteHabit(w http.ResponseWriter, r *http.Request) {
	mutateHabitDate(w, r, "completions", "skips")
}

func HandleUncompleteHabit(w http.ResponseWriter, r *http.Request) {
	removeHabitDate(w, r, "completions")
}

func HandleSkipHabit(w http.ResponseWriter, r *http.Request) {
	mutateHabitDate(w, r, "skips", "completions")
}

func HandleUnskipHabit(w http.ResponseWriter, r *http.Request) {
	removeHabitDate(w, r, "skips")
}
