package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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

type User struct {
	Name        string `json:"name"`
	ID          int    `json:"id"`
	Password    []byte `json:"password"` // hash
	NextTaskID  uint64 `json:"next_task_id"`
	Role        string `json:"role"`
	NextHabitID uint64 `json:"next_habit_id"`
}

func createUser(name string, password []byte, role string) User {
	user := User{
		Name:       name,
		ID:         0,
		Password:   password,
		NextTaskID: 0,
		Role:       role,
	}
	return user
}

func requestUser(w http.ResponseWriter, r *http.Request) *User {
	cookie, err := r.Cookie("auth")
	if err != nil || cookie.Value == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil
	}
	token, err := strconv.ParseUint(cookie.Value, 10, 64)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil
	}
	user, err := GetUserFromToken(token)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return nil
	}
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil
	}
	return user
}

func getUser(db *sql.DB, name string) (*User, error) {
	var user User
	err := db.QueryRow(`
		SELECT id, name, password_hash, next_task_id, role, next_habit_id
		FROM users
		where name = ?
	`, name).Scan(&user.ID, &user.Name, &user.Password, &user.NextTaskID, &user.Role, &user.NextHabitID)
	if err == sql.ErrNoRows {
		return nil, nil // user not found
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

func AddUser(db *sql.DB, name string, password string, role string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("username is required")
	}
	if role != "user" && role != "admin" {
		return fmt.Errorf("invalid role %q", role)
	}
	password_bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to generate password hash: %w", err)
	}

	_, err = db.Exec(`
		INSERT INTO users (name, password_hash, role)
		VALUES (?, ?, ?)
	`, name, string(password_bytes), role)
	if err != nil {
		return fmt.Errorf("failed to create user %q: %w", name, err)
	}

	return nil
}

func DeleteUser(db *sql.DB, name string, id int) error {
	result, err := db.Exec(
		"DELETE FROM users WHERE id = ? AND name = ?",
		id,
		name,
	)
	if err != nil {
		return fmt.Errorf("failed to delete user %q: %w", name, err)
	}

	rowsDeleted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to verify deletion of user %q: %w", name, err)
	}
	if rowsDeleted == 0 {
		return fmt.Errorf("user %q with ID %d doesn't exist", name, id)
	}

	sessionMu.Lock()
	if session, ok := user_sessions[id]; ok {
		delete(token_map, session.Token)
		delete(user_sessions, id)
	}
	sessionMu.Unlock()
	return nil
}

func EditUser(db *sql.DB, name string, id int, role string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("username is required")
	}
	if role != "user" && role != "admin" {
		return fmt.Errorf("invalid role %q", role)
	}

	var existingID int
	err := db.QueryRow(
		"SELECT id FROM users WHERE name = ? AND id != ?",
		name,
		id,
	).Scan(&existingID)
	if err == nil {
		return fmt.Errorf("username %q is already taken", name)
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("failed to check username %q: %w", name, err)
	}

	result, err := db.Exec(
		"UPDATE users SET name = ?, role = ? WHERE id = ?",
		name,
		role,
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to update user %d: %w", id, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to verify update of user %d: %w", id, err)
	}
	if rowsAffected == 0 {
		var exists bool
		if err := db.QueryRow(
			"SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)",
			id,
		).Scan(&exists); err != nil {
			return fmt.Errorf("failed to check user %d: %w", id, err)
		}
		if !exists {
			return fmt.Errorf("user with ID %d doesn't exist", id)
		}
	}

	return nil
}

func SetUserPassword(db *sql.DB, user *User, currentPassword string, newPassword string) error {
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

	result, err := db.Exec(`
		UPDATE users
		SET password_hash = ?
		WHERE id = ? AND password_hash = ?
	`, string(passwordHash), user.ID, string(user.Password))
	if err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to verify password update: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("password changed; retry with the current password")
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
	user, err := getUser(database, creds.Name)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	err = bcrypt.CompareHashAndPassword(user.Password, []byte(creds.Password))
	if err == nil {
		token := GetUserSessionCookie(user)
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
	if AddUser(database, creds.Name, creds.Password, creds.Role) == nil {
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
	if DeleteUser(database, creds.Name, creds.ID) == nil {
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
	if EditUser(database, creds.Name, creds.ID, creds.Role) == nil {
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

	rows, err := database.Query(`
		SELECT id, name, role
		FROM users
		ORDER BY name
	`)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	users := make([]userSummary, 0)
	for rows.Next() {
		var user userSummary
		if err := rows.Scan(&user.ID, &user.Name, &user.Role); err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

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
	if err := SetUserPassword(database, user, passwords.CurrentPassword, passwords.NewPassword); err != nil {
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

func GetUserFromToken(token uint64) (*User, error) {
	id, ok := userIDFromToken(token)
	if !ok {
		return nil, nil
	}

	return getUserByID(database, id)
}

func getUserByID(db *sql.DB, id int) (*User, error) {
	var user User
	err := db.QueryRow(`
		SELECT id, name, password_hash, next_task_id, role, next_habit_id
		FROM users
		WHERE id = ?
	`, id).Scan(
		&user.ID,
		&user.Name,
		&user.Password,
		&user.NextTaskID,
		&user.Role,
		&user.NextHabitID,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

func UserExists(db *sql.DB, name string) (bool, error) {
	var exists bool
	err := db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM users WHERE name = ?)",
		name,
	).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}
