package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

const uHabitSchemaVersion = 21

func HandleHabitCSVExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	habits, err := appStore.ListHabits(r.Context(), user.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to export habits")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="habit-loop-habits.csv"`)
	writer := csv.NewWriter(w)
	if err := writer.Write([]string{"habit", "status", "date", "result", "interval", "days_mode", "start_date"}); err != nil {
		return
	}
	for _, habit := range habits {
		writeHabitCSVRows(writer, habit, habit.Completions, "completed")
		writeHabitCSVRows(writer, habit, habit.Skips, "skipped")
		if len(habit.Completions) == 0 && len(habit.Skips) == 0 {
			_ = writer.Write([]string{habit.Name, habit.Status, "", "", strconv.Itoa(habit.Interval), strconv.FormatBool(habit.DaysMode), habit.StartDate})
		}
	}
	writer.Flush()
}

func writeHabitCSVRows(writer *csv.Writer, habit Habit, dates []time.Time, result string) {
	for _, date := range dates {
		_ = writer.Write([]string{
			habit.Name,
			habit.Status,
			date.Format("2006-01-02"),
			result,
			strconv.Itoa(habit.Interval),
			strconv.FormatBool(habit.DaysMode),
			habit.StartDate,
		})
	}
}

func HandleUHabitDBExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	tempDir, err := os.MkdirTemp("", "habit-loop-export-")
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to create export")
		return
	}
	defer os.RemoveAll(tempDir)
	path := filepath.Join(tempDir, "habits.db")
	exportDB, err := sql.Open("sqlite", path)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to create export")
		return
	}
	defer exportDB.Close()
	if err := createUHabitExport(r.Context(), exportDB, appStore, user.ID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to export habits")
		return
	}
	if err := exportDB.Close(); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to finalize export")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="habit-loop-uhabits.db"`)
	http.ServeFile(w, r, path)
}

func createUHabitExport(ctx context.Context, db *sql.DB, store *Store, userID int) error {
	for _, statement := range []string{
		`CREATE TABLE Habits (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			archived INTEGER NOT NULL DEFAULT 0,
			color INTEGER NOT NULL DEFAULT 0,
			description TEXT NOT NULL DEFAULT '',
			freq_den INTEGER NOT NULL DEFAULT 1,
			freq_num INTEGER NOT NULL DEFAULT 1,
			highlight INTEGER NOT NULL DEFAULT 0,
			name TEXT NOT NULL,
			position INTEGER NOT NULL DEFAULT 0,
			reminder_hour INTEGER,
			reminder_min INTEGER,
			reminder_days INTEGER NOT NULL DEFAULT 127,
			type INTEGER NOT NULL DEFAULT 0,
			target_type INTEGER NOT NULL DEFAULT 0,
			target_value REAL NOT NULL DEFAULT 0,
			unit TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE Repetitions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			habit INTEGER REFERENCES Habits(id),
			timestamp INTEGER,
			value INTEGER NOT NULL DEFAULT 2
		)`,
		`CREATE INDEX idx_repetitions_habit_timestamp ON Repetitions(habit, timestamp)`,
		`CREATE TABLE Events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp INTEGER,
			message TEXT,
			server_id INTEGER
		)`,
		`CREATE TABLE android_metadata (locale TEXT)`,
		`INSERT INTO android_metadata(locale) VALUES ('en_US')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(uHabitSchemaVersion)); err != nil {
		return err
	}
	habits, err := store.ListHabits(ctx, userID)
	if err != nil {
		return err
	}
	repetitionID := 1
	for index, habit := range habits {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO Habits(id, name, freq_num, freq_den, archived, position)
			VALUES (?, ?, 1, ?, ?, ?)
		`, habit.ID, habit.Name, maxInt(habit.Interval, 1), boolInt(habit.Status == "inactive"), index); err != nil {
			return err
		}
		for _, date := range habit.Completions {
			value := time.Date(date.Year(), date.Month(), date.Day(), 12, 0, 0, 0, time.UTC).UnixMilli()
			if _, err := db.ExecContext(ctx, `
				INSERT INTO Repetitions(id, habit, timestamp, value)
				VALUES (?, ?, ?, 2)
			`, repetitionID, habit.ID, value); err != nil {
				return err
			}
			repetitionID++
		}
	}
	return nil
}

func maxInt(value, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
