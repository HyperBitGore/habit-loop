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
	"sync"
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
var sessionMu sync.Mutex

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
	log.Println("Recieved a login request")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var creds struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, "Invalid request body", http.StatusUnauthorized)
		return
	}
	user := getUser(creds.Name)
	log.Println("user, ", user)
	err := bcrypt.CompareHashAndPassword(user.Password, []byte(creds.Password))
	if err == nil {
		token := GetUserSessionCookie(&user)
		token_cookie := http.Cookie{ Name: "auth", Value: strconv.FormatUint(token, 10), HttpOnly: true, Secure: false, SameSite: http.SameSiteStrictMode, Path: "/", MaxAge: sessionTimeMinutes * 60 }
		http.SetCookie(w, &token_cookie)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
		return
	}
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{
		"error": "invalid input",
	})
}

func HandleLogout (w http.ResponseWriter, r *http.Request) {
	log.Println("Recieved a logout request")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cookie, err := r.Cookie("auth")
	if (err != nil || cookie.Value == "") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	token_cookie := http.Cookie{ Name: "auth", Value: "", HttpOnly: true, Secure: false, SameSite: http.SameSiteStrictMode, Path: "/", MaxAge: -1 }
	http.SetCookie(w, &token_cookie)

	token, err := strconv.ParseUint(cookie.Value, 10, 64)
	if err == nil {
		sessionMu.Lock()
		if userID, ok := token_map[token]; ok {
			delete(token_map, token)
			delete(user_sessions, userID)
		}
		sessionMu.Unlock()
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
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

// caller must hold sessionMu
func checkUserSessionTokenLocked(user *User) bool {
	session, ok := user_sessions[user.ID]
	if !ok {
		return false
	}
	now := time.Now()
	if now.Before(session.Expiration) {
		return true
	}
	delete(token_map, session.Token)
	delete(user_sessions, user.ID)
	return false
}

func CheckUserSessionToken (user *User) bool {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	return checkUserSessionTokenLocked(user)
}

func CheckSessionToken (token uint64) bool {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	t, ok := token_map[token]
	if ok {
		session, ok := user_sessions[t]
		if !ok {
			return false
		}
		now := time.Now()
		if now.After(session.Expiration) {
			delete(token_map, session.Token)
			delete(user_sessions, t)
			return false
		}
		return true
	}
	return false
}

// just not gonna worry abt collisions such a low chance
func GetUserSessionCookie (user *User) uint64 {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	if checkUserSessionTokenLocked(user) {
		return user_sessions[user.ID].Token
	}
	now := time.Now()
	token := new_token()
	expire := now.Add(sessionTimeMinutes * time.Minute)
	user_sessions[user.ID] = UserSession{ Token: token, Expiration: expire}
	token_map[token] = user.ID
	return token
}
