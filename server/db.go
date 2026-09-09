package main

import (
	"database/sql"
	"log"
	_ "modernc.org/sqlite"
)

var database *sql.DB

func InitDB() {
	db, err := sql.Open("sqlite", "storage.db")
	if err != nil {
		log.Fatal(err)
	}
	database = db
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS todos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_name TEXT NOT NULL,
            name TEXT NOT NULL,
			date TEXT NOT NULL,
            complete BOOLEAN NOT NULL DEFAULT FALSE
        )
    `)
	if err != nil {
		log.Fatal(err)
	}
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS users (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
            next_task_id INTEGER NOT NULL DEFAULT 0,
			role TEXT NOT NULL,
			next_habit_id INTEGER NOT NULL DEFAULT 0,
			email TEXT,
			email_verified BOOLEAN NOT NULL DEFAULT FALSE
        )
    `)
	if err != nil {
		log.Fatal(err)
	}
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS habits (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
            user_name TEXT NOT NULL,
            name TEXT NOT NULL
        )
    `)
	if err != nil {
		log.Fatal(err)
	}
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS completions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
            habit_id INTEGER NOT NULL,
            date TEXT NOT NULL
        )
    `)
	if err != nil {
		log.Fatal(err)
	}
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS skips (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
            habit_id INTEGER NOT NULL,
            date TEXT NOT NULL
        )
    `)
	if err != nil {
		log.Fatal(err)
	}
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS sessions (
			token_hash BLOB PRIMARY KEY,
            user_id INTEGER NOT NULL,
            expires_at DATETIME NOT NULL
        )
    `)
	if err != nil {
		log.Fatal(err)
	}
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS email_verifications (
			token_hash BLOB PRIMARY KEY,
			user_id INTEGER NOT NULL,
			expires_at DATETIME NOT NULL
        )
    `)
	if err != nil {
		log.Fatal(err)
	}
}