package main

// recieve api requests and send to other api file functions

// TODO
//	- properly edit tasks (probably need to id them?)
//	- add complete end point
//	- style the frontend to display info better
//	- add seperate users
//	- add login flow
//	- add task data saving
//	- add repeating tasks

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
	mux.HandleFunc("/api/add_task", HandleAddTask)
	mux.HandleFunc("/api/remove_task", HandleRemoveTask)
	mux.HandleFunc("/api/update_task", HandleUpdateTask)
	log.Println("Server listening on http://localhost:8081")
	log.Fatal(http.ListenAndServe(":8081", mux))
}
