package main

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type UserSession struct {
	Token      uint64    `json:"token"`
	Expiration time.Time `json:"expiration"`
}

const usersDir = "users"
const sessionTimeMinutes = 30

var user_sessions = map[int]UserSession{}
var token_map = map[uint64]int{}
var sessionMu sync.Mutex
var user_map = map[int]User{}
var id_map = map[string]int{}

type User struct {
	Name        string  `json:"name"`
	ID          int     `json:"id"`
	Password    []byte  `json:"password"` // hash
	Tasks       []Task  `json:"tasks"`
	NextTaskID  uint64  `json:"next_task_id"`
	Role        string  `json:"role"`
	Habits      []Habit `json:"habits"`
	NextHabitID uint64  `json:"next_habit_id"`
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

func createUser(name string, password []byte, role string) User {
	user := User{
		Name:       name,
		ID:         0,
		Password:   password,
		Tasks:      make([]Task, 0, 4096),
		NextTaskID: 0,
		Role:       role,
		Habits:     make([]Habit, 0),
	}
	return user
}

// edits user_map
func ReadUsers() map[int]User {
	// switch this to sql eventually
	if fileExists("users/db") {
		file, err := os.ReadFile("users/db")
		if err != nil {
			log.Fatalf("Failed to read user JSON db file: %v", err)
			return nil
		}
		var temp = map[int]User{}
		err = json.Unmarshal(file, &temp)
		if err != nil {
			log.Fatalf("Failed to unmarshal JSON: %v", err)
			return nil
		}
		for key := range temp {
			user := temp[key]
			id_map[user.Name] = user.ID
		}
		return temp
	}
	return nil
}

func WriteUsers(m map[int]User) error {
	if err := os.MkdirAll(usersDir, 0700); err != nil {
		return fmt.Errorf("Failed to read make user folder: %v", err)
	}
	file, err := os.Create("users/db")
	if err != nil {
		return fmt.Errorf("Failed to create users db: %v", err)
	}
	defer file.Close()
	jsondata, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("Failed to marshal json db: %v", err)
	}
	_, err = file.Write(jsondata)
	if err != nil {
		return fmt.Errorf("Failed to write json db: %v", err)
	}
	return nil
}

func getUser(name string) User {
	id, ok := id_map[name]
	if !ok {
		return User{ID: -1}
	}
	user, ok := user_map[id]
	if !ok {
		return User{ID: -1}
	}
	return user
}

func nextUserID() int {
	nextID := 0
	for id := range user_map {
		if id >= nextID {
			nextID = id + 1
		}
	}
	return nextID
}

func AddUser(name string, password string, role string) error {
	password_bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("Failed to generate password hash! %v", err)
	}
	user := createUser(name, password_bytes, role)
	user.ID = nextUserID()
	id_map[user.Name] = user.ID
	user_map[user.ID] = user
	return WriteUsers(user_map)
}

func DeleteUser(name string, id int) error {
	if UserExists(name) {
		delete(id_map, name)
		delete(user_map, id)
		if err := WriteUsers(user_map); err != nil {
			return fmt.Errorf("failed to save edited user: %w", err)
		}
		return nil
	}
	return fmt.Errorf("User doesn't exist! %v", name)
}

func EditUser(name string, id int, role string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("username is required")
	}
	if role != "user" && role != "admin" {
		return fmt.Errorf("invalid role %q", role)
	}
	user, ok := user_map[id]
	if !ok {
		return fmt.Errorf("user with ID %d doesn't exist", id)
	}

	if existingID, exists := id_map[name]; exists && existingID != id {
		return fmt.Errorf("username %q is already taken", name)
	}

	oldName := user.Name
	user.Name = name
	user.Role = role

	if oldName != name {
		delete(id_map, oldName)
	}
	id_map[name] = id
	user_map[id] = user

	if err := WriteUsers(user_map); err != nil {
		return fmt.Errorf("failed to save edited user: %w", err)
	}
	return nil
}

func SetUserPassword(user *User, currentPassword string, newPassword string) error {
	if newPassword == "" {
		return fmt.Errorf("new password is required")
	}
	if err := bcrypt.CompareHashAndPassword(user.Password, []byte(currentPassword)); err != nil {
		return fmt.Errorf("current password is incorrect")
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to generate password hash: %w", err)
	}
	user.Password = passwordHash
	user_map[user.ID] = *user
	if err := WriteUsers(user_map); err != nil {
		return fmt.Errorf("failed to save password: %w", err)
	}
	return nil
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
		token_cookie := http.Cookie{Name: "auth", Value: strconv.FormatUint(token, 10), HttpOnly: true, Secure: false, SameSite: http.SameSiteStrictMode, Path: "/", MaxAge: sessionTimeMinutes * 60}
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

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	log.Println("Recieved a logout request")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cookie, err := r.Cookie("auth")
	if err != nil || cookie.Value == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	token_cookie := http.Cookie{Name: "auth", Value: "", HttpOnly: true, Secure: false, SameSite: http.SameSiteStrictMode, Path: "/", MaxAge: -1}
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
	defer r.Body.Close()
	var creds struct {
		Name     string `json:"name"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, "Invalid request body", http.StatusUnauthorized)
		return
	}
	// check if user name exists already
	_, ok := id_map[creds.Name]
	if ok {
		http.Error(w, "Invalid request body, name already taken!", http.StatusUnauthorized)
		return
	}
	if AddUser(creds.Name, creds.Password, creds.Role) == nil {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
		return
	}
	http.Error(w, "Invalid request body", http.StatusBadRequest)
}

func HandleDeleteUser(w http.ResponseWriter, r *http.Request) {
	log.Println("Recieved a todo list request")
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var creds struct {
		Name string `json:"name"`
		ID   int    `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, "Invalid request body", http.StatusUnauthorized)
		return
	}
	if DeleteUser(creds.Name, creds.ID) == nil {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
		return
	}
	http.Error(w, "Invalid request body", http.StatusBadRequest)
}

func HandleEditUser(w http.ResponseWriter, r *http.Request) {
	log.Println("Recieved a todo list request")
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var creds struct {
		Name string `json:"name"`
		ID   int    `json:"id"`
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, "Invalid request body", http.StatusUnauthorized)
		return
	}
	if EditUser(creds.Name, creds.ID, creds.Role) == nil {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
		return
	}
	http.Error(w, "Invalid request body", http.StatusBadRequest)
}

func HandleGetUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	type userSummary struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Role string `json:"role"`
	}
	users := make([]userSummary, 0, len(user_map))
	for _, user := range user_map {
		users = append(users, userSummary{
			ID:   user.ID,
			Name: user.Name,
			Role: user.Role,
		})
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i].Name < users[j].Name
	})
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(users); err != nil {
		http.Error(w, "Failed to encode users", http.StatusInternalServerError)
	}
}

func HandleSetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	defer r.Body.Close()
	var passwords struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&passwords); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := SetUserPassword(user, passwords.CurrentPassword, passwords.NewPassword); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func HandleCurrentUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"name": user.Name,
		"role": user.Role,
	}); err != nil {
		http.Error(w, "Failed to encode current user", http.StatusInternalServerError)
	}
}

func new_token() uint64 {
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

func CheckUserSessionToken(user *User) bool {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	return checkUserSessionTokenLocked(user)
}

func CheckSessionToken(token uint64) bool {
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
func GetUserSessionCookie(user *User) uint64 {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	if checkUserSessionTokenLocked(user) {
		return user_sessions[user.ID].Token
	}
	now := time.Now()
	token := new_token()
	expire := now.Add(sessionTimeMinutes * time.Minute)
	user_sessions[user.ID] = UserSession{Token: token, Expiration: expire}
	token_map[token] = user.ID
	return token
}

func GetUserFromToken(token uint64) *User {
	id, ok := token_map[token]
	if !ok {
		return &User{ID: -1}
	}
	user, ok := user_map[id]
	if !ok {
		return &User{ID: -1}
	}
	return &user
}

func UserExists(name string) bool {
	_, ok := id_map[name]
	if !ok {
		return false
	}
	_, ok = user_map[id_map[name]]
	return ok
}
