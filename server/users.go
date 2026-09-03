package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"golang.org/x/crypto/bcrypt"
)

const usersDir = "users"

type User struct {
	Name       string `json:"name"`
	ID         int    `json:"id"`
	Password   []byte `json:"password"` // hash
	Tasks      []Task `json:"tasks"`
	NextTaskID uint64 `json:"next_task_id"`
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	if err == nil {
		return true // File exists
	}
	if errors.Is(err, os.ErrNotExist) {
		return false // File explicitly does not exist
	}
	// The file may or may not exist (e.g., permission denied, disk failure)
	return false
}

func userPath(name string) string {
	return filepath.Join(usersDir, name)
}

func createUser(name string, password []byte) User {
	user := User{Name: name, ID: 0, Password: password, Tasks: make([]Task, 0, 4096), NextTaskID: 0}
	return user
}

func getUser(name string) User {
	if fileExists(userPath(name)) {
		// read file
		file, err := os.ReadFile(userPath(name))
		if err != nil {
			log.Fatalf("Failed to read JSON file for user: %v", err)
			return User{ID: -1}
		}
		var user User
		err = json.Unmarshal(file, &user)
		if err != nil {
			log.Fatalf("Failed to unmarshal JSON: %v", err)
			return User{ID: -1}
		}
		return user
	}
	return User{ID: -1}
}

func saveUser(user User) error {
	if err := os.MkdirAll(usersDir, 0700); err != nil {
		return err
	}

	file, err := os.Create(userPath(user.Name))
	if err != nil {
		return err
	}
	defer file.Close()

	jsondata, err := json.Marshal(user)
	if err != nil {
		return err
	}
	_, err = file.Write(jsondata)
	return err
}

func AddUser(name string, password string) error {
	password_bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user := createUser(name, password_bytes)
	return saveUser(user)
}

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	log.Println("Recieved a todo list request")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := r.Header.Get("X-User-Name")
	password := r.Header.Get("X-User-Password")
	user := getUser(name)
	log.Println("user, ", user)
	err := bcrypt.CompareHashAndPassword(user.Password, []byte(password)) 
	if err == nil {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
		return
	}
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{
		"error": "invalid input",
	})
}

func HandleRegisterUser(w http.ResponseWriter, r *http.Request) {
	log.Println("Recieved a todo list request")
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

}
