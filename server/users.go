package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"log"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

const usersDir = "users"
const sessionTimeMinutes = 30

type User struct {
	Name        string `json:"name"`
	ID          int    `json:"id"`
	Email       string `json:"email"`
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
	user, err := GetUserFromToken(cookie.Value)
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
		SELECT id, name, COALESCE(email, ''), password_hash, next_task_id, role, next_habit_id
		FROM users
		WHERE name = ?
	`, name).Scan(&user.ID, &user.Name, &user.Email, &user.Password, &user.NextTaskID, &user.Role, &user.NextHabitID)
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
		INSERT INTO users (name, password_hash, role, email_verified)
		VALUES (?, ?, ?, ?)
	`, name, string(password_bytes), role, role == "admin")
	if err != nil {
		return fmt.Errorf("failed to create user %q: %w", name, err)
	}

	return nil
}

func DeleteUser(db *sql.DB, name string, id int) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to start deleting user %q: %w", name, err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(
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

	if _, err := tx.Exec("DELETE FROM sessions WHERE user_id = ?", id); err != nil {
		return fmt.Errorf("failed to delete sessions for user %d: %w", id, err)
	}
	if _, err := tx.Exec("DELETE FROM email_verifications WHERE user_id = ?", id); err != nil {
		return fmt.Errorf("failed to delete verification records for user %d: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit deletion of user %q: %w", name, err)
	}

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

	var oldName string
	if err := db.QueryRow("SELECT name FROM users WHERE id = ?", id).Scan(&oldName); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("user with ID %d doesn't exist", id)
		}
		return fmt.Errorf("failed to load user %d: %w", id, err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to start updating user %d: %w", id, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		"UPDATE users SET name = ?, role = ? WHERE id = ?",
		name,
		role,
		id,
	); err != nil {
		return fmt.Errorf("failed to update user %d: %w", id, err)
	}
	if err := updateUserNameReferences(tx, oldName, name); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit update of user %d: %w", id, err)
	}

	return nil
}

func updateUserNameReferences(tx *sql.Tx, oldName string, newName string) error {
	if _, err := tx.Exec(
		"UPDATE todos SET user_name = ? WHERE user_name = ?",
		newName,
		oldName,
	); err != nil {
		return fmt.Errorf("failed to update user todos: %w", err)
	}
	if _, err := tx.Exec(
		"UPDATE habits SET user_name = ? WHERE user_name = ?",
		newName,
		oldName,
	); err != nil {
		return fmt.Errorf("failed to update user habits: %w", err)
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

func UpdateUserProfile(db *sql.DB, user *User, name string, email string) error {
	name = strings.TrimSpace(name)
	email = strings.TrimSpace(email)
	if name == "" {
		return fmt.Errorf("username is required")
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return fmt.Errorf("a valid email is required")
	}

	var existingID int
	err = db.QueryRow(
		"SELECT id FROM users WHERE name = ? AND id != ?",
		name,
		user.ID,
	).Scan(&existingID)
	if err == nil {
		return fmt.Errorf("username %q is already taken", name)
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("failed to check username %q: %w", name, err)
	}

	err = db.QueryRow(
		"SELECT id FROM users WHERE email = ? AND id != ?",
		email,
		user.ID,
	).Scan(&existingID)
	if err == nil {
		return fmt.Errorf("email is already registered")
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("failed to check email: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to start updating profile: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		"UPDATE users SET name = ?, email = ? WHERE id = ?",
		name,
		email,
		user.ID,
	); err != nil {
		return fmt.Errorf("failed to update profile: %w", err)
	}
	if err := updateUserNameReferences(tx, user.Name, name); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit profile update: %w", err)
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
	verified, err := CheckEmailVerified(database, creds.Name)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if !verified {
		http.Error(w, "User not verified, can't login yet!", http.StatusUnauthorized)
		return
	}

	err = bcrypt.CompareHashAndPassword(user.Password, []byte(creds.Password))
	if err == nil {
		token, err := CreateSessionToken(database, user.ID)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		tokenCookie := http.Cookie{Name: "auth", Value: token, HttpOnly: true, Secure: false, SameSite: http.SameSiteStrictMode, Path: "/", MaxAge: sessionTimeMinutes * 60}
		http.SetCookie(w, &tokenCookie)
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

	if err := DeleteSessionToken(cookie.Value); err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
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

func HandleRequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()

	var request struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	request.Email = strings.TrimSpace(request.Email)

	var userID int
	var userName string
	err := database.QueryRow(
		"SELECT id, name FROM users WHERE email = ?",
		request.Email,
	).Scan(&userID, &userName)
	if err == sql.ErrNoRows {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		http.Error(w, "Failed to create reset token", http.StatusInternalServerError)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	tokenHash := sha256.Sum256([]byte(token))

	tx, err := database.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM password_resets WHERE user_id = ?", userID); err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(`
		INSERT INTO password_resets (token_hash, user_id, expires_at)
		VALUES (?, ?, ?)
	`, tokenHash[:], userID, time.Now().UTC().Add(time.Hour).Format("2006-01-02 15:04:05")); err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	resetURL := applicationBaseURL(r) + "/reset-password.html?token=" + url.QueryEscape(token)
	if err := SendPasswordResetEmail(request.Email, userName, resetURL); err != nil {
		if _, cleanupErr := database.Exec("DELETE FROM password_resets WHERE token_hash = ?", tokenHash[:]); cleanupErr != nil {
			log.Printf("failed to clean up password reset token: %v", cleanupErr)
		}
		http.Error(w, "Password reset email could not be sent", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func HandleResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()

	var request struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if request.Token == "" || len(request.NewPassword) < 8 {
		http.Error(w, "A valid token and password of at least 8 characters are required", http.StatusBadRequest)
		return
	}
	tokenHash := sha256.Sum256([]byte(request.Token))

	tx, err := database.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var userID int
	err = tx.QueryRow(`
		SELECT user_id
		FROM password_resets
		WHERE token_hash = ? AND expires_at > CURRENT_TIMESTAMP
	`, tokenHash[:]).Scan(&userID)
	if err == sql.ErrNoRows {
		http.Error(w, "Invalid or expired password reset link", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(request.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Failed to update password", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec("UPDATE users SET password_hash = ? WHERE id = ?", string(passwordHash), userID); err != nil {
		http.Error(w, "Failed to update password", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec("DELETE FROM password_resets WHERE user_id = ?", userID); err != nil {
		http.Error(w, "Failed to complete password reset", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec("DELETE FROM sessions WHERE user_id = ?", userID); err != nil {
		http.Error(w, "Failed to invalidate sessions", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to complete password reset", http.StatusInternalServerError)
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
		"name":  user.Name,
		"email": user.Email,
		"role":  user.Role,
	}); err != nil {
		http.Error(w, "Failed to encode current user", http.StatusInternalServerError)
	}
}

func HandleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	defer r.Body.Close()

	var profile struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := UpdateUserProfile(database, user, profile.Name, profile.Email); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func getUserByID(db *sql.DB, id int) (*User, error) {
	var user User
	err := db.QueryRow(`
		SELECT id, name, COALESCE(email, ''), password_hash, next_task_id, role, next_habit_id
		FROM users
		WHERE id = ?
	`, id).Scan(
		&user.ID,
		&user.Name,
		&user.Email,
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
