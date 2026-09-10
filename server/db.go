package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

var database *sql.DB

func InitDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("configure database: %w", err)
		}
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, err
	}
	database = db
	return db, nil
}

func cleanupExpiredRecords(db *sql.DB) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"sessions", "email_verifications", "password_resets"} {
		if _, err := tx.Exec("DELETE FROM "+table+" WHERE expires_at <= ?", now); err != nil {
			return fmt.Errorf("clean expired %s: %w", table, err)
		}
	}
	return tx.Commit()
}
