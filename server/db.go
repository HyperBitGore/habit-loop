package main

import (
	"database/sql"
	"log"
	_ "modernc.org/sqlite"
)

func InitDB () {
	db, err := sql.Open("sqlite", "storage.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
}