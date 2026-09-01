package main

// recieve api requests and send to other api file functions

// TODO
//	- serve tasks data
//	- add and remove tasks endpoints

import (
	"log"
	"net/http"
)

type User struct {
	Name     string
	ID       int
	Password int // hash
}

func main() {
	initTasks()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/get_tasks", handleGetTodos)
	mux.Handle("/", http.FileServer(http.Dir("../web")))

	log.Println("Server listening on http://localhost:8081")
	log.Fatal(http.ListenAndServe(":8081", mux))
}
