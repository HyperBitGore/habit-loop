package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const maxUHabitDatabaseSize = 32 << 20

type uHabit struct {
	id          int64
	name        string
	freqNum     int64
	freqDen     int64
	startDate   string
	completions []string
}

type uHabitImportSummary struct {
	Habits      int `json:"habits"`
	Completions int `json:"completions"`
	Skipped     int `json:"skipped"`
}

func ValidateUHabitDB(db *sql.DB) error {
	for _, table := range []string{"Habits", "Repetitions"} {
		var exists int
		if err := db.QueryRow(`
			SELECT COUNT(*)
			FROM sqlite_master
			WHERE type = 'table' AND lower(name) = lower(?)
		`, table).Scan(&exists); err != nil {
			return err
		}
		if exists != 1 {
			return fmt.Errorf("uHabits database is missing the %s table", table)
		}
	}
	return nil
}

func readUHabits(ctx context.Context, db *sql.DB) ([]uHabit, uHabitImportSummary, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, TRIM(name), COALESCE(freq_num, 1), COALESCE(freq_den, 1)
		FROM Habits
		WHERE name IS NOT NULL
		  AND TRIM(name) <> ''
		  AND COALESCE(archived, 0) = 0
		ORDER BY position, id
	`)
	if err != nil {
		return nil, uHabitImportSummary{}, err
	}
	defer rows.Close()

	habits := make([]uHabit, 0)
	summary := uHabitImportSummary{}
	for rows.Next() {
		var habit uHabit
		if err := rows.Scan(&habit.id, &habit.name, &habit.freqNum, &habit.freqDen); err != nil {
			return nil, summary, err
		}
		habit.completions = make([]string, 0)
		habits = append(habits, habit)
	}
	if err := rows.Err(); err != nil {
		return nil, summary, err
	}

	byID := make(map[int64]*uHabit, len(habits))
	for index := range habits {
		byID[habits[index].id] = &habits[index]
	}

	repetitionRows, err := db.QueryContext(ctx, `
		SELECT habit, timestamp, value
		FROM Repetitions
		WHERE habit IS NOT NULL AND timestamp IS NOT NULL AND value > 0
		ORDER BY habit, timestamp
	`)
	if err != nil {
		return nil, summary, err
	}
	defer repetitionRows.Close()

	for repetitionRows.Next() {
		var (
			habitID   int64
			timestamp int64
			value     int
		)
		if err := repetitionRows.Scan(&habitID, &timestamp, &value); err != nil {
			return nil, summary, err
		}
		habit := byID[habitID]
		if habit == nil {
			summary.Skipped++
			continue
		}
		date, err := uHabitTimestampDate(timestamp)
		if err != nil {
			return nil, summary, err
		}
		habit.completions = append(habit.completions, date)
		summary.Completions++
	}
	if err := repetitionRows.Err(); err != nil {
		return nil, summary, err
	}

	for index := range habits {
		if len(habits[index].completions) > 0 {
			habits[index].startDate = habits[index].completions[0]
		} else {
			habits[index].startDate = time.Now().UTC().Format("2006-01-02")
		}
	}
	summary.Habits = len(habits)
	return habits, summary, nil
}

func uHabitInterval(freqNum, freqDen int64) (int, error) {
	if freqNum < 1 || freqDen < 1 {
		return 0, fmt.Errorf("invalid uHabits frequency %d/%d", freqNum, freqDen)
	}
	interval := (freqDen + freqNum - 1) / freqNum
	if interval < 1 {
		interval = 1
	}
	if interval > int64(^uint(0)>>1) {
		return 0, fmt.Errorf("uHabits frequency interval is too large")
	}
	return int(interval), nil
}

func uHabitTimestampDate(value int64) (string, error) {
	if value <= 0 {
		return "", fmt.Errorf("invalid uHabits repetition timestamp %d", value)
	}
	seconds := value
	if value > 100000000000 {
		seconds = value / 1000
	}
	date := time.Unix(seconds, 0).UTC()
	if date.Year() < 1970 || date.Year() > 2100 {
		return "", fmt.Errorf("uHabits repetition timestamp %d is out of range", value)
	}
	return date.Format("2006-01-02"), nil
}

func ProcessUHabitDB(ctx context.Context, source *sql.DB, destination *Store, userID int) (uHabitImportSummary, error) {
	habits, summary, err := readUHabits(ctx, source)
	if err != nil {
		return uHabitImportSummary{}, err
	}

	tx, err := destination.db.BeginTx(ctx, nil)
	if err != nil {
		return uHabitImportSummary{}, err
	}
	defer tx.Rollback()

	for _, habit := range habits {
		interval, err := uHabitInterval(habit.freqNum, habit.freqDen)
		if err != nil {
			return uHabitImportSummary{}, err
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO habits (
				user_id, name, interval, days_mode, start_date, days_of_week_id
			) VALUES (?, ?, ?, FALSE, ?, NULL)
		`, userID, habit.name, interval, habit.startDate)
		if err != nil {
			return uHabitImportSummary{}, err
		}
		habitID, err := result.LastInsertId()
		if err != nil {
			return uHabitImportSummary{}, err
		}
		for _, date := range habit.completions {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO completions (habit_id, date)
				VALUES (?, ?)
				ON CONFLICT(habit_id, date) DO NOTHING
			`, habitID, date); err != nil {
				return uHabitImportSummary{}, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return uHabitImportSummary{}, err
	}
	return summary, nil
}

func HandleUHabitDBUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUHabitDatabaseSize)
	if err := r.ParseMultipartForm(maxUHabitDatabaseSize); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid or oversized database upload")
		return
	}
	file, _, err := r.FormFile("database")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Database file is required")
		return
	}
	defer file.Close()

	tempDir, err := os.MkdirTemp("", "habit-loop-uhabit-")
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to stage database upload")
		return
	}
	defer os.RemoveAll(tempDir)
	tempPath := filepath.Join(tempDir, fmt.Sprintf("upload-%d.db", user.ID))
	tempFile, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to stage database upload")
		return
	}
	defer tempFile.Close()

	if _, err := io.Copy(tempFile, file); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Unable to read database upload")
		return
	}
	if err := tempFile.Close(); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to stage database upload")
		return
	}

	source, err := sql.Open("sqlite", "file:"+filepath.ToSlash(tempPath)+"?mode=ro")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Uploaded file is not a SQLite database")
		return
	}
	defer source.Close()
	source.SetMaxOpenConns(1)
	source.SetMaxIdleConns(1)

	if _, err := source.ExecContext(r.Context(), "PRAGMA query_only = ON"); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Unable to open uploaded database")
		return
	}
	if err := source.PingContext(r.Context()); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Uploaded file is not a readable SQLite database")
		return
	}
	if err := ValidateUHabitDB(source); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Unsupported uHabits database")
		return
	}

	summary, err := ProcessUHabitDB(r.Context(), source, appStore, user.ID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			writeAPIError(w, http.StatusRequestTimeout, "Database import timed out")
			return
		}
		writeAPIError(w, http.StatusBadRequest, "Unable to import uHabits database")
		return
	}
	writeJSON(w, http.StatusCreated, summary)
}
