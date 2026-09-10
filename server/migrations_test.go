package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestFreshDatabaseCreatesVersionedSchema(t *testing.T) {
	db, err := InitDB(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var version int
	if err := db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, currentSchemaVersion)
	}
	for _, table := range []string{
		"users", "todos", "habits", "completions", "skips",
		"sessions", "email_verifications", "password_resets",
	} {
		exists, err := tableExists(db, table)
		if err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("table %s was not created", table)
		}
	}
}

func TestVersionedDatabaseStartupIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "versioned.db")
	db, err := InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var migrations int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrations); err != nil {
		t.Fatal(err)
	}
	if migrations != 1 {
		t.Fatalf("migration count = %d, want 1", migrations)
	}
}

func TestUnversionedDatabaseIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unversioned.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	_, err = InitDB(path)
	if err == nil {
		t.Fatal("unversioned database was accepted")
	}
	if !strings.Contains(err.Error(), "unversioned database schema") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEmptyMigrationTableWithExistingSchemaIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE todos (id INTEGER PRIMARY KEY);
	`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	_, err = InitDB(path)
	if err == nil {
		t.Fatal("partially initialized database was accepted")
	}
	if !strings.Contains(err.Error(), "unversioned database schema") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSchemaEnforcesOwnershipAndUniqueHabitDates(t *testing.T) {
	db, err := InitDB(filepath.Join(t.TempDir(), "constraints.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	result, err := db.Exec(`
		INSERT INTO users (name, email, email_normalized, password_hash, role, email_verified)
		VALUES ('alice', 'alice@example.com', 'alice@example.com', 'hash', 'user', TRUE)
	`)
	if err != nil {
		t.Fatal(err)
	}
	userID, _ := result.LastInsertId()
	result, err = db.Exec("INSERT INTO habits (user_id, name) VALUES (?, 'habit')", userID)
	if err != nil {
		t.Fatal(err)
	}
	habitID, _ := result.LastInsertId()
	if _, err := db.Exec("INSERT INTO completions (habit_id, date) VALUES (?, '2026-09-09')", habitID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO completions (habit_id, date) VALUES (?, '2026-09-09')", habitID); err == nil {
		t.Fatal("duplicate habit completion was accepted")
	}
	if _, err := db.Exec("DELETE FROM users WHERE id = ?", userID); err != nil {
		t.Fatal(err)
	}
	var habits, completions int
	if err := db.QueryRow("SELECT COUNT(*) FROM habits").Scan(&habits); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM completions").Scan(&completions); err != nil {
		t.Fatal(err)
	}
	if habits != 0 || completions != 0 {
		t.Fatalf("cascade failed: habits=%d completions=%d", habits, completions)
	}
}
