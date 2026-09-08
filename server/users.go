package main

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"slices"
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

func getUser(name string) User {
	user, ok := userSnapshotByName(name)
	if !ok {
		return User{ID: -1}
	}
	return user
}

func AddUser(name string, password string, role string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("username is required")
	}
	if role != "user" && role != "admin" {
		return fmt.Errorf("invalid role %q", role)
	}
	password_bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("Failed to generate password hash! %v", err)
	}
	return withUserDataWrite(func(users map[int]User, names map[string]int, nextUserID *int) error {
		if _, exists := names[name]; exists {
			return fmt.Errorf("username %q is already taken", name)
		}
		user := createUser(name, password_bytes, role)
		user.ID = *nextUserID
		*nextUserID = *nextUserID + 1
		names[user.Name] = user.ID
		users[user.ID] = user
		return nil
	})
}

func DeleteUser(name string, id int) error {
	err := withUserDataWrite(func(users map[int]User, names map[string]int, nextUserID *int) error {
		user, ok := users[id]
		if !ok || user.Name != name {
			return fmt.Errorf("user %q with ID %d doesn't exist", name, id)
		}
		delete(names, user.Name)
		delete(users, id)
		return nil
	})
	if err != nil {
		return err
	}

	sessionMu.Lock()
	if session, ok := user_sessions[id]; ok {
		delete(token_map, session.Token)
		delete(user_sessions, id)
	}
	sessionMu.Unlock()
	return nil
}

func EditUser(name string, id int, role string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("username is required")
	}
	if role != "user" && role != "admin" {
		return fmt.Errorf("invalid role %q", role)
	}
	return withUserDataWrite(func(users map[int]User, names map[string]int, nextUserID *int) error {
		user, ok := users[id]
		if !ok {
			return fmt.Errorf("user with ID %d doesn't exist", id)
		}
		if existingID, exists := names[name]; exists && existingID != id {
			return fmt.Errorf("username %q is already taken", name)
		}

		if user.Name != name {
			delete(names, user.Name)
		}
		user.Name = name
		user.Role = role
		names[name] = id
		users[id] = user
		return nil
	})
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
	originalHash := slices.Clone(user.Password)
	return updateUser(user.ID, func(storedUser *User) error {
		if !bytes.Equal(storedUser.Password, originalHash) {
			return fmt.Errorf("password changed; retry with the current password")
		}
		storedUser.Password = passwordHash
		return nil
	})
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
	userSnapshots := allUserSnapshots()
	users := make([]userSummary, 0, len(userSnapshots))
	for _, user := range userSnapshots {
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
	_, ok := userIDFromToken(token)
	return ok
}

func userIDFromToken(token uint64) (int, bool) {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	userID, ok := token_map[token]
	if ok {
		session, ok := user_sessions[userID]
		if !ok {
			return 0, false
		}
		now := time.Now()
		if now.After(session.Expiration) {
			delete(token_map, session.Token)
			delete(user_sessions, userID)
			return 0, false
		}
		return userID, true
	}
	return 0, false
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
	id, ok := userIDFromToken(token)
	if !ok {
		return &User{ID: -1}
	}
	user, ok := userSnapshotByID(id)
	if !ok {
		return &User{ID: -1}
	}
	return &user
}

func UserExists(name string) bool {
	_, ok := userSnapshotByName(name)
	return ok
}
