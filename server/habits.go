package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var errHabitNotFound = errors.New("habit not found")

type DaysOfWeek struct {
	Sunday    bool `json:"sunday"`
	Monday    bool `json:"monday"`
	Tuesday   bool `json:"tuesday"`
	Wednesday bool `json:"wednesday"`
	Thursday  bool `json:"thursday"`
	Friday    bool `json:"friday"`
	Saturday  bool `json:"saturday"`
}

type Habit struct {
	Name         string      `json:"name"`
	Completions  []time.Time `json:"completions"`
	Skips        []time.Time `json:"skips"`
	DaysOfWeek   DaysOfWeek  `json:"days_of_week"`
	DaysOfWeekID *uint64     `json:"days_of_week_id"`
	ID           uint64      `json:"id"`
	Interval     int         `json:"interval"`
	DaysMode     bool        `json:"days_mode"`
	StartDate    string      `json:"start_date"`
}

func getUserHabits(db *sql.DB, userID int) ([]Habit, error) {
	rows, err := db.Query(`
		SELECT h.id, h.name, h.interval, h.days_mode, h.start_date, h.days_of_week_id,
		       COALESCE(d.sunday, FALSE), COALESCE(d.monday, FALSE),
		       COALESCE(d.tuesday, FALSE), COALESCE(d.wednesday, FALSE),
		       COALESCE(d.thursday, FALSE), COALESCE(d.friday, FALSE),
		       COALESCE(d.saturday, FALSE), 'completion', c.date
		FROM habits h
		LEFT JOIN days_of_week d ON d.id = h.days_of_week_id
		LEFT JOIN completions c ON c.habit_id = h.id
		WHERE h.user_id = ?
		UNION ALL
		SELECT h.id, h.name, h.interval, h.days_mode, h.start_date, h.days_of_week_id,
		       COALESCE(d.sunday, FALSE), COALESCE(d.monday, FALSE),
		       COALESCE(d.tuesday, FALSE), COALESCE(d.wednesday, FALSE),
		       COALESCE(d.thursday, FALSE), COALESCE(d.friday, FALSE),
		       COALESCE(d.saturday, FALSE), 'skip', s.date
		FROM habits h
		LEFT JOIN days_of_week d ON d.id = h.days_of_week_id
		LEFT JOIN skips s ON s.habit_id = h.id
		WHERE h.user_id = ?
		ORDER BY 2, 1, 15
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
		var interval int
		var daysMode bool
		var startDate string
		var daysOfWeekID sql.NullInt64
		var daysOfWeek DaysOfWeek
		var date sql.NullString
		if err := rows.Scan(
			&id,
			&name,
			&interval,
			&daysMode,
			&startDate,
			&daysOfWeekID,
			&daysOfWeek.Sunday,
			&daysOfWeek.Monday,
			&daysOfWeek.Tuesday,
			&daysOfWeek.Wednesday,
			&daysOfWeek.Thursday,
			&daysOfWeek.Friday,
			&daysOfWeek.Saturday,
			&kind,
			&date,
		); err != nil {
			return nil, err
		}
		habit := habitsByID[id]
		if habit == nil {
			habit = &Habit{
				ID:          id,
				Name:        name,
				Interval:    interval,
				DaysMode:    daysMode,
				StartDate:   startDate,
				DaysOfWeek:  daysOfWeek,
				Completions: []time.Time{},
				Skips:       []time.Time{},
			}
			if daysOfWeekID.Valid {
				id := uint64(daysOfWeekID.Int64)
				habit.DaysOfWeekID = &id
			}
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

func (days DaysOfWeek) anyEnabled() bool {
	return days.Sunday || days.Monday || days.Tuesday || days.Wednesday ||
		days.Thursday || days.Friday || days.Saturday
}

func parseHabitSchedule(r *http.Request) (int, bool, DaysOfWeek, error) {
	interval := 1
	if value := strings.TrimSpace(r.Header.Get("X-Habit-Interval")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			return 0, false, DaysOfWeek{}, errors.New("Habit interval must be at least 1")
		}
		interval = parsed
	}

	daysMode := false
	if value := strings.TrimSpace(r.Header.Get("X-Habit-Days-Mode")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return 0, false, DaysOfWeek{}, errors.New("Invalid habit days mode")
		}
		daysMode = parsed
	}

	var daysOfWeek DaysOfWeek
	if value := strings.TrimSpace(r.Header.Get("X-Habit-Days-Of-Week")); value != "" {
		if err := json.Unmarshal([]byte(value), &daysOfWeek); err != nil {
			return 0, false, DaysOfWeek{}, errors.New("Invalid habit days of week")
		}
	}
	return interval, daysMode, daysOfWeek, nil
}

func validateHabitSchedule(daysMode bool, daysOfWeek DaysOfWeek) error {
	if daysMode && !daysOfWeek.anyEnabled() {
		return errors.New("At least one day of the week must be enabled")
	}
	return nil
}

func parseHabitStartDate(r *http.Request) (string, error) {
	value := strings.TrimSpace(r.Header.Get("X-Habit-Start-Date"))
	if value == "" {
		return time.Now().UTC().Format("2006-01-02"), nil
	}
	date, err := time.Parse("2006-01-02", value)
	if err != nil {
		return "", errors.New("Invalid habit start date")
	}
	return date.Format("2006-01-02"), nil
}

func insertDaysOfWeek(tx *sql.Tx, days DaysOfWeek) (int64, error) {
	result, err := tx.Exec(`
		INSERT INTO days_of_week (
			sunday, monday, tuesday, wednesday, thursday, friday, saturday
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, days.Sunday, days.Monday, days.Tuesday, days.Wednesday, days.Thursday, days.Friday, days.Saturday)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func updateDaysOfWeek(tx *sql.Tx, id int64, days DaysOfWeek) error {
	_, err := tx.Exec(`
		UPDATE days_of_week
		SET sunday = ?, monday = ?, tuesday = ?, wednesday = ?,
		    thursday = ?, friday = ?, saturday = ?
		WHERE id = ?
	`, days.Sunday, days.Monday, days.Tuesday, days.Wednesday, days.Thursday, days.Friday, days.Saturday, id)
	return err
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
	interval, daysMode, daysOfWeek, err := parseHabitSchedule(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateHabitSchedule(daysMode, daysOfWeek); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	startDate, err := parseHabitStartDate(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
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
	daysOfWeekID, err := insertDaysOfWeek(tx, daysOfWeek)
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	result, err := tx.ExecContext(r.Context(), `
		INSERT INTO habits (user_id, name, interval, days_mode, start_date, days_of_week_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, user.ID, name, interval, daysMode, startDate, daysOfWeekID)
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
	interval, daysMode, daysOfWeek, err := parseHabitSchedule(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
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
	var daysOfWeekID sql.NullInt64
	var storedInterval int
	var storedDaysMode bool
	var storedStartDate string
	var storedDaysOfWeek DaysOfWeek
	if err := tx.QueryRowContext(r.Context(), `
		SELECT h.interval, h.days_mode, h.start_date, h.days_of_week_id,
		       COALESCE(d.sunday, FALSE), COALESCE(d.monday, FALSE),
		       COALESCE(d.tuesday, FALSE), COALESCE(d.wednesday, FALSE),
		       COALESCE(d.thursday, FALSE), COALESCE(d.friday, FALSE),
		       COALESCE(d.saturday, FALSE)
		FROM habits h
		LEFT JOIN days_of_week d ON d.id = h.days_of_week_id
		WHERE h.id = ? AND h.user_id = ?
	`, id, user.ID).Scan(
		&storedInterval,
		&storedDaysMode,
		&storedStartDate,
		&daysOfWeekID,
		&storedDaysOfWeek.Sunday,
		&storedDaysOfWeek.Monday,
		&storedDaysOfWeek.Tuesday,
		&storedDaysOfWeek.Wednesday,
		&storedDaysOfWeek.Thursday,
		&storedDaysOfWeek.Friday,
		&storedDaysOfWeek.Saturday,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeHabitMutationError(w, errHabitNotFound)
		} else {
			writeHabitMutationError(w, err)
		}
		return
	}
	if r.Header.Get("X-Habit-Interval") == "" {
		interval = storedInterval
	}
	if r.Header.Get("X-Habit-Days-Mode") == "" {
		daysMode = storedDaysMode
	}
	if r.Header.Get("X-Habit-Days-Of-Week") == "" {
		daysOfWeek = storedDaysOfWeek
	}
	startDate := storedStartDate
	if r.Header.Get("X-Habit-Start-Date") != "" {
		startDate, err = parseHabitStartDate(r)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := validateHabitSchedule(daysMode, daysOfWeek); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !daysOfWeekID.Valid {
		insertedID, err := insertDaysOfWeek(tx, daysOfWeek)
		if err != nil {
			writeHabitMutationError(w, err)
			return
		}
		daysOfWeekID = sql.NullInt64{Int64: insertedID, Valid: true}
	} else if err := updateDaysOfWeek(tx, daysOfWeekID.Int64, daysOfWeek); err != nil {
		writeHabitMutationError(w, err)
		return
	}
	result, err := tx.ExecContext(r.Context(), `
		UPDATE habits
		SET name = ?, interval = ?, days_mode = ?, start_date = ?, days_of_week_id = ?
		WHERE id = ? AND user_id = ?
	`, name, interval, daysMode, startDate, daysOfWeekID.Int64, id, user.ID)
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
