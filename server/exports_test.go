package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestUHabitExportMatchesCurrentSchema(t *testing.T) {
	setupTestApplication(t)
	userID := insertTestUser(t, "exporter", "exporter@example.com", "user")
	if err := appStore.AddHabit(
		context.Background(),
		userID,
		DaysOfWeek{},
		"Read",
		2,
		false,
		"2026-01-01",
		nil,
	); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "uhabits.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := createUHabitExport(context.Background(), db, appStore, userID); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	source, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()

	var schemaVersion int
	if err := source.QueryRow("PRAGMA user_version").Scan(&schemaVersion); err != nil {
		t.Fatal(err)
	}
	if schemaVersion != uHabitSchemaVersion {
		t.Fatalf("schema version = %d, want %d", schemaVersion, uHabitSchemaVersion)
	}
	var eventsTable int
	if err := source.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name IN ('Events', 'android_metadata')
	`).Scan(&eventsTable); err != nil {
		t.Fatal(err)
	}
	if eventsTable != 2 {
		t.Fatalf("supporting table count = %d, want 2", eventsTable)
	}

	var habitCount int
	if err := source.QueryRow(`
		SELECT COUNT(*)
		FROM Habits
		WHERE name IS NOT NULL AND freq_num > 0 AND freq_den > 0
	`).Scan(&habitCount); err != nil {
		t.Fatal(err)
	}
	if habitCount != 1 {
		t.Fatalf("habit count = %d, want 1", habitCount)
	}
	var (
		id                                              int64
		name, description                               string
		freqNum, freqDen, color, position, reminderDays int
		reminderHour, reminderMin                       sql.NullInt64
		highlight, archived, habitType, targetType      int
		targetValue                                     float64
		unit                                            string
	)
	if err := source.QueryRow(`
		SELECT id, name, description, freq_num, freq_den,
		       color, position, reminder_hour, reminder_min, reminder_days,
		       highlight, archived, type, target_value, target_type, unit
		FROM Habits ORDER BY position
	`).Scan(
		&id, &name, &description, &freqNum, &freqDen,
		&color, &position, &reminderHour, &reminderMin, &reminderDays,
		&highlight, &archived, &habitType, &targetValue, &targetType, &unit,
	); err != nil {
		t.Fatal(err)
	}

	var repetitionCount int
	if err := source.QueryRow(`
		SELECT COUNT(*) FROM Repetitions
		WHERE habit IS NOT NULL AND timestamp IS NOT NULL AND value > 0
	`).Scan(&repetitionCount); err != nil {
		t.Fatal(err)
	}
}
