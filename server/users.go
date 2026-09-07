package main

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"golang.org/x/crypto/bcrypt"
)
type UserSession struct {
	Token uint64 `json:"token"`
	Expiration time.Time `json:"expiration"`
}

const usersDir = "users"
const sessionTimeMinutes = 30
var user_sessions = map[int]UserSession{}
var token_map = map[uint64]int{}

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
		token := GetUserSessionCookie(&user)
		token_cookie := http.Cookie{ Name: "auth", Value: strconv.FormatUint(token, 10), HttpOnly: true, Secure: false, SameSite: http.SameSiteStrictMode }
		http.SetCookie(w, &token_cookie)
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

func new_token () uint64 {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	token := binary.BigEndian.Uint64(b)
	return token
}

func CheckUserSessionToken (user *User) bool {
	session, ok := user_sessions[user.ID]
	now := time.Now()
	if ok && now.Before(session.Expiration) {
		return true
	} 
	if now.After(session.Expiration) {
		delete(token_map, session.Token)
		delete(user_sessions, user.ID)
	}
	return false
}

func CheckSessionToken (token uint64) bool {
	t, ok := token_map[token]
	if ok {
		session := user_sessions[t]
		now := time.Now()
		if now.After(session.Expiration) {
			return false
		}
		return true
	}
	return false
}

// just not gonna worry abt collisions such a low chance
func GetUserSessionCookie (user *User) uint64 {
	if (CheckUserSessionToken(user)) {
		return user_sessions[user.ID].Token
	}
	now := time.Now()
	token := new_token()
	expire := now.Add(sessionTimeMinutes * time.Minute)
	user_sessions[user.ID] = UserSession{ Token: token, Expiration: expire}
	token_map[token] = user.ID
	return token
}
