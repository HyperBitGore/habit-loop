package main

import (
	"database/sql"
	"fmt"
)

const currentSchemaVersion = 6

func runMigrations(db *sql.DB) error {
	hasMigrationsTable, err := tableExists(db, "schema_migrations")
	if err != nil {
		return err
	}
	if !hasMigrationsTable {
		hasExistingSchema, err := applicationSchemaExists(db)
		if err != nil {
			return err
		}
		if hasExistingSchema {
			return fmt.Errorf("unversioned database schema detected; remove the database file at DATABASE_PATH and restart")
		}
		return createCurrentSchema(db, true)
	}

	var version int
	if err := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > currentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", version, currentSchemaVersion)
	}
	if version < currentSchemaVersion {
		if version == 5 {
			return migrateToV6(db)
		}
		if version == 4 {
			return migrateToV5(db)
		}

		if version == 3 {
			return migrateToV4(db)
		}

		if version == 0 {
			hasExistingSchema, err := applicationSchemaExists(db)
			if err != nil {
				return err
			}
			if hasExistingSchema {
				return fmt.Errorf("unversioned database schema detected; remove the database file at DATABASE_PATH and restart")
			}
			return createCurrentSchema(db, false)
		}
		return fmt.Errorf(
			"database schema version %d is no longer supported; migrate it to version %d before using this build",
			version,
			currentSchemaVersion,
		)
	}
	return nil
}

func migrateToV6(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`ALTER TABLE habits ADD COLUMN status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive'))`); err != nil {
		return fmt.Errorf("add habit status: %w", err)
	}
	if _, err := tx.Exec(`CREATE INDEX habits_user_status_idx ON habits(user_id, status)`); err != nil {
		return fmt.Errorf("index habit status: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations(version) VALUES (6)"); err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	return tx.Commit()
}

func migrateToV5(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`CREATE TABLE goals (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
					name TEXT NOT NULL,
					status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'complete', 'inactive')),
					created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
				)`,
		`CREATE TABLE goal_items (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					goal_id INTEGER NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
					name TEXT NOT NULL,
					position INTEGER NOT NULL DEFAULT 0,
					completed BOOLEAN NOT NULL DEFAULT FALSE,
					available_date TEXT NOT NULL DEFAULT CURRENT_DATE
				)`,
		`CREATE TABLE habit_metrics (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					habit_id INTEGER NOT NULL UNIQUE REFERENCES habits(id) ON DELETE CASCADE,
					name TEXT NOT NULL,
					goal_amount REAL NOT NULL CHECK (goal_amount > 0)
				)`,
		`CREATE TABLE habit_metric_values (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					metric_id INTEGER NOT NULL REFERENCES habit_metrics(id) ON DELETE CASCADE,
					date TEXT NOT NULL,
					amount REAL NOT NULL CHECK (amount >= 0),
					UNIQUE(metric_id, date)
				)`,
		`CREATE TABLE notes (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
					todo_id INTEGER REFERENCES todos(id) ON DELETE CASCADE,
					habit_id INTEGER REFERENCES habits(id) ON DELETE CASCADE,
					date TEXT NOT NULL,
					body TEXT NOT NULL,
					CHECK ((todo_id IS NOT NULL AND habit_id IS NULL) OR (todo_id IS NULL AND habit_id IS NOT NULL)),
					UNIQUE(todo_id, date),
					UNIQUE(habit_id, date)
				)`,
		`ALTER TABLE todos ADD COLUMN goal_item_id INTEGER REFERENCES goal_items(id) ON DELETE CASCADE`,
		`CREATE UNIQUE INDEX todos_goal_item_date_idx ON todos(goal_item_id, date) WHERE goal_item_id IS NOT NULL`,
		`CREATE INDEX goals_user_status_idx ON goals(user_id, status)`,
		`CREATE INDEX goal_items_goal_idx ON goal_items(goal_id, position)`,
		`CREATE INDEX metric_values_date_idx ON habit_metric_values(metric_id, date)`,
		`CREATE INDEX notes_user_date_idx ON notes(user_id, date)`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("apply schema version 5: %w", err)
		}
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations(version) VALUES (5)"); err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	return tx.Commit()
}

func migrateToV4(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("ALTER TABLE todos ADD COLUMN position INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("add todo position: %w", err)
	}
	if _, err := tx.Exec("ALTER TABLE habits ADD COLUMN position INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("add habit position: %w", err)
	}

	rows, err := tx.Query("SELECT id, user_id, date FROM todos ORDER BY user_id, date, id")
	if err != nil {
		return fmt.Errorf("read todo positions: %w", err)
	}
	type todoPosition struct {
		id     int64
		userID int64
		date   string
	}
	var todos []todoPosition
	for rows.Next() {
		var todo todoPosition
		if err := rows.Scan(&todo.id, &todo.userID, &todo.date); err != nil {
			rows.Close()
			return fmt.Errorf("scan todo position: %w", err)
		}
		todos = append(todos, todo)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read todo positions: %w", err)
	}
	rows.Close()
	todoPositions := make(map[string]int)
	for _, todo := range todos {
		date := todo.date
		if len(date) > 10 {
			date = date[:10]
		}
		key := fmt.Sprintf("%d:%s", todo.userID, date)
		position := todoPositions[key]
		if _, err := tx.Exec("UPDATE todos SET position = ? WHERE id = ?", position, todo.id); err != nil {
			return fmt.Errorf("set todo position: %w", err)
		}
		todoPositions[key] = position + 1
	}

	rows, err = tx.Query("SELECT id, user_id FROM habits ORDER BY user_id, name, id")
	if err != nil {
		return fmt.Errorf("read habit positions: %w", err)
	}
	type habitPosition struct {
		id     int64
		userID int64
	}
	var habits []habitPosition
	for rows.Next() {
		var habit habitPosition
		if err := rows.Scan(&habit.id, &habit.userID); err != nil {
			rows.Close()
			return fmt.Errorf("scan habit position: %w", err)
		}
		habits = append(habits, habit)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read habit positions: %w", err)
	}
	rows.Close()
	habitPositions := make(map[int64]int)
	for _, habit := range habits {
		position := habitPositions[habit.userID]
		if _, err := tx.Exec("UPDATE habits SET position = ? WHERE id = ?", position, habit.id); err != nil {
			return fmt.Errorf("set habit position: %w", err)
		}
		habitPositions[habit.userID] = position + 1
	}

	if _, err := tx.Exec("INSERT INTO schema_migrations(version) VALUES (4)"); err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	return tx.Commit()
}

func createCurrentSchema(db *sql.DB, createMigrationsTable bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if createMigrationsTable {
		if _, err := tx.Exec(`
			CREATE TABLE schema_migrations (
				version INTEGER PRIMARY KEY,
				applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)
		`); err != nil {
			return fmt.Errorf("create schema migrations table: %w", err)
		}
	}
	if err := createSchema(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(
		"INSERT INTO schema_migrations(version) VALUES (?)",
		currentSchemaVersion,
	); err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	return tx.Commit()
}

func createSchema(tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			email TEXT,
			email_normalized TEXT UNIQUE,
			pending_email TEXT,
			pending_email_normalized TEXT UNIQUE,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL CHECK (role IN ('user', 'admin')),
			email_verified BOOLEAN NOT NULL DEFAULT FALSE,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE goals (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'complete', 'inactive')),
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE goal_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			goal_id INTEGER NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			position INTEGER NOT NULL DEFAULT 0,
			completed BOOLEAN NOT NULL DEFAULT FALSE,
			available_date TEXT NOT NULL DEFAULT CURRENT_DATE
		)`,
		`CREATE TABLE todos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			date TEXT NOT NULL,
			complete BOOLEAN NOT NULL DEFAULT FALSE,
			position INTEGER NOT NULL DEFAULT 0,
			goal_item_id INTEGER REFERENCES goal_items(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE days_of_week (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			sunday BOOLEAN NOT NULL DEFAULT FALSE CHECK (sunday IN (0, 1)),
			monday BOOLEAN NOT NULL DEFAULT FALSE CHECK (monday IN (0, 1)),
			tuesday BOOLEAN NOT NULL DEFAULT FALSE CHECK (tuesday IN (0, 1)),
			wednesday BOOLEAN NOT NULL DEFAULT FALSE CHECK (wednesday IN (0, 1)),
			thursday BOOLEAN NOT NULL DEFAULT FALSE CHECK (thursday IN (0, 1)),
			friday BOOLEAN NOT NULL DEFAULT FALSE CHECK (friday IN (0, 1)),
			saturday BOOLEAN NOT NULL DEFAULT FALSE CHECK (saturday IN (0, 1))
		)`,
		`CREATE TABLE habits (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			interval INTEGER NOT NULL DEFAULT 1 CHECK (interval > 0),
			days_mode BOOLEAN NOT NULL DEFAULT FALSE CHECK (days_mode IN (0, 1)),
			start_date TEXT NOT NULL DEFAULT CURRENT_DATE,
			days_of_week_id INTEGER UNIQUE REFERENCES days_of_week(id) ON DELETE SET NULL,
			position INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive'))
		)`,
		`CREATE TABLE completions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			habit_id INTEGER NOT NULL REFERENCES habits(id) ON DELETE CASCADE,
			date TEXT NOT NULL,
			UNIQUE(habit_id, date)
		)`,
		`CREATE TABLE skips (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			habit_id INTEGER NOT NULL REFERENCES habits(id) ON DELETE CASCADE,
			date TEXT NOT NULL,
			UNIQUE(habit_id, date)
		)`,
		`CREATE TABLE habit_metrics (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			habit_id INTEGER NOT NULL UNIQUE REFERENCES habits(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			goal_amount REAL NOT NULL CHECK (goal_amount > 0)
		)`,
		`CREATE TABLE habit_metric_values (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			metric_id INTEGER NOT NULL REFERENCES habit_metrics(id) ON DELETE CASCADE,
			date TEXT NOT NULL,
			amount REAL NOT NULL CHECK (amount >= 0),
			UNIQUE(metric_id, date)
		)`,
		`CREATE TABLE notes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			todo_id INTEGER REFERENCES todos(id) ON DELETE CASCADE,
			habit_id INTEGER REFERENCES habits(id) ON DELETE CASCADE,
			date TEXT NOT NULL,
			body TEXT NOT NULL,
			CHECK ((todo_id IS NOT NULL AND habit_id IS NULL) OR (todo_id IS NULL AND habit_id IS NOT NULL)),
			UNIQUE(todo_id, date),
			UNIQUE(habit_id, date)
		)`,
		`CREATE TABLE sessions (
			token_hash BLOB PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at DATETIME NOT NULL
		)`,
		`CREATE TABLE email_verifications (
			token_hash BLOB PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			email TEXT NOT NULL,
			kind TEXT NOT NULL CHECK (kind IN ('account', 'email_change', 'activation')),
			expires_at DATETIME NOT NULL
		)`,
		`CREATE TABLE password_resets (
			token_hash BLOB PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at DATETIME NOT NULL
		)`,
		`CREATE INDEX todos_user_date_idx ON todos(user_id, date)`,
		`CREATE INDEX habits_user_idx ON habits(user_id)`,
		`CREATE INDEX habits_user_status_idx ON habits(user_id, status)`,
		`CREATE INDEX sessions_user_idx ON sessions(user_id)`,
		`CREATE INDEX sessions_expiry_idx ON sessions(expires_at)`,
		`CREATE INDEX verification_expiry_idx ON email_verifications(expires_at)`,
		`CREATE INDEX reset_expiry_idx ON password_resets(expires_at)`,
		`CREATE UNIQUE INDEX todos_goal_item_date_idx ON todos(goal_item_id, date) WHERE goal_item_id IS NOT NULL`,
		`CREATE INDEX goals_user_status_idx ON goals(user_id, status)`,
		`CREATE INDEX goal_items_goal_idx ON goal_items(goal_id, position)`,
		`CREATE INDEX metric_values_date_idx ON habit_metric_values(metric_id, date)`,
		`CREATE INDEX notes_user_date_idx ON notes(user_id, date)`,
		`CREATE TRIGGER habits_days_of_week_cleanup
			AFTER DELETE ON habits
			WHEN OLD.days_of_week_id IS NOT NULL
			BEGIN
				DELETE FROM days_of_week WHERE id = OLD.days_of_week_id;
			END`,
		`CREATE TRIGGER users_email_insert_guard
			BEFORE INSERT ON users
			WHEN NEW.email_normalized IS NOT NULL
			  AND EXISTS (
				SELECT 1 FROM users
				WHERE pending_email_normalized = NEW.email_normalized
			  )
			BEGIN
				SELECT RAISE(ABORT, 'email is already registered');
			END`,
		`CREATE TRIGGER users_pending_email_insert_guard
			BEFORE INSERT ON users
			WHEN NEW.pending_email_normalized IS NOT NULL
			  AND EXISTS (
				SELECT 1 FROM users
				WHERE email_normalized = NEW.pending_email_normalized
			  )
			BEGIN
				SELECT RAISE(ABORT, 'email is already registered');
			END`,
		`CREATE TRIGGER users_email_update_guard
			BEFORE UPDATE OF email_normalized ON users
			WHEN NEW.email_normalized IS NOT NULL
			  AND EXISTS (
				SELECT 1 FROM users
				WHERE id != NEW.id AND pending_email_normalized = NEW.email_normalized
			  )
			BEGIN
				SELECT RAISE(ABORT, 'email is already registered');
			END`,
		`CREATE TRIGGER users_pending_email_update_guard
			BEFORE UPDATE OF pending_email_normalized ON users
			WHEN NEW.pending_email_normalized IS NOT NULL
			  AND EXISTS (
				SELECT 1 FROM users
				WHERE id != NEW.id AND email_normalized = NEW.pending_email_normalized
			  )
			BEGIN
				SELECT RAISE(ABORT, 'email is already registered');
			END`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("create production schema: %w", err)
		}
	}
	return nil
}

func tableExists(db *sql.DB, name string) (bool, error) {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
		name,
	).Scan(&count)
	return count != 0, err
}

func applicationSchemaExists(db *sql.DB) (bool, error) {
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table'
		  AND name IN (
			'users', 'todos', 'habits', 'days_of_week', 'completions', 'skips',
			'sessions', 'email_verifications', 'password_resets'
		  )
	`).Scan(&count)
	return count != 0, err
}
