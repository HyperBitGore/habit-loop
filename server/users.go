package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const sessionTimeMinutes = 30

var dummyPasswordHash, _ = bcrypt.GenerateFromPassword([]byte("invalid-password"), bcrypt.DefaultCost)

type User struct {
	ID            int
	Name          string
	Email         string
	PendingEmail  string
	Password      []byte
	Role          string
	EmailVerified bool
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validateUsername(name string) error {
	name = strings.TrimSpace(name)
	length := utf8.RuneCountInString(name)
	if length < 3 || length > 50 {
		return fmt.Errorf("username must be between 3 and 50 characters")
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return fmt.Errorf("username contains invalid characters")
		}
	}
	return nil
}

func validateEmail(email string) error {
	normalized := normalizeEmail(email)
	if len(normalized) > 254 {
		return fmt.Errorf("email is too long")
	}
	address, err := mail.ParseAddress(normalized)
	if err != nil || normalizeEmail(address.Address) != normalized {
		return fmt.Errorf("a valid email is required")
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if len(password) > 72 {
		return fmt.Errorf("password must be at most 72 bytes")
	}
	return nil
}

func requestUser(w http.ResponseWriter, r *http.Request) *User {
	if user := userFromContext(r.Context()); user != nil {
		return user
	}
	writeAPIError(w, http.StatusUnauthorized, "Unauthorized")
	return nil
}

func scanUser(row *sql.Row) (*User, error) {
	var user User
	err := row.Scan(
		&user.ID,
		&user.Name,
		&user.Email,
		&user.PendingEmail,
		&user.Password,
		&user.Role,
		&user.EmailVerified,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func getUserByName(ctx context.Context, db *sql.DB, name string) (*User, error) {
	return scanUser(db.QueryRowContext(ctx, `
		SELECT id, name, COALESCE(email, ''), COALESCE(pending_email, ''),
		       password_hash, role, email_verified
		FROM users
		WHERE name = ?
	`, strings.TrimSpace(name)))
}

func getUserByID(ctx context.Context, db *sql.DB, id int) (*User, error) {
	return scanUser(db.QueryRowContext(ctx, `
		SELECT id, name, COALESCE(email, ''), COALESCE(pending_email, ''),
		       password_hash, role, email_verified
		FROM users
		WHERE id = ?
	`, id))
}

func adminExists(ctx context.Context, db *sql.DB) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE role = 'admin')").Scan(&exists)
	return exists, err
}

func bootstrapAdmin(ctx context.Context, db *sql.DB, cfg Config) error {
	exists, err := adminExists(ctx, db)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if cfg.BootstrapAdminName == "" {
		return fmt.Errorf("no administrator exists; set BOOTSTRAP_ADMIN_NAME, BOOTSTRAP_ADMIN_EMAIL, and BOOTSTRAP_ADMIN_PASSWORD")
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(cfg.BootstrapAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO users (
			name, email, email_normalized, password_hash, role, email_verified
		)
		VALUES (?, ?, ?, ?, 'admin', TRUE)
	`, cfg.BootstrapAdminName, cfg.BootstrapAdminEmail, normalizeEmail(cfg.BootstrapAdminEmail), string(passwordHash))
	if err != nil {
		return fmt.Errorf("create bootstrap administrator: %w", err)
	}
	return nil
}

func DeleteUser(ctx context.Context, db *sql.DB, actorID int, targetID int) error {
	if actorID == targetID {
		return fmt.Errorf("you cannot delete your own account from user administration")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var role string
	if err := tx.QueryRowContext(ctx, "SELECT role FROM users WHERE id = ?", targetID).Scan(&role); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("user does not exist")
		}
		return err
	}
	if role == "admin" {
		var adminCount int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE role = 'admin'").Scan(&adminCount); err != nil {
			return err
		}
		if adminCount <= 1 {
			return fmt.Errorf("the final administrator cannot be deleted")
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM users WHERE id = ?", targetID); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return tx.Commit()
}

func EditUser(ctx context.Context, db *sql.DB, actorID int, targetID int, name string, role string) error {
	name = strings.TrimSpace(name)
	if err := validateUsername(name); err != nil {
		return err
	}
	if role != "user" && role != "admin" {
		return fmt.Errorf("invalid role")
	}
	if actorID == targetID && role != "admin" {
		return fmt.Errorf("you cannot demote your own administrator account")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var oldRole string
	if err := tx.QueryRowContext(ctx, "SELECT role FROM users WHERE id = ?", targetID).Scan(&oldRole); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("user does not exist")
		}
		return err
	}
	if oldRole == "admin" && role != "admin" {
		var adminCount int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE role = 'admin'").Scan(&adminCount); err != nil {
			return err
		}
		if adminCount <= 1 {
			return fmt.Errorf("the final administrator cannot be demoted")
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE users SET name = ?, role = ? WHERE id = ?", name, role, targetID); err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	return tx.Commit()
}

func SetUserPassword(ctx context.Context, db *sql.DB, user *User, currentPassword string, newPassword string) error {
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword(user.Password, []byte(currentPassword)); err != nil {
		return fmt.Errorf("current password is incorrect")
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE users SET password_hash = ? WHERE id = ?", string(passwordHash), user.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", user.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var credentials struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &credentials); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !loginAccountLimiter.allow(strings.ToLower(strings.TrimSpace(credentials.Name))) {
		writeAPIError(w, http.StatusTooManyRequests, "Too many requests")
		return
	}

	user, err := getUserByName(r.Context(), database, credentials.Name)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to log in")
		return
	}
	hash := dummyPasswordHash
	if user != nil {
		hash = user.Password
	}
	passwordMatches := bcrypt.CompareHashAndPassword(hash, []byte(credentials.Password)) == nil
	if user == nil || !passwordMatches || !user.EmailVerified {
		writeAPIError(w, http.StatusUnauthorized, "Invalid login credentials")
		return
	}
	token, err := CreateSessionToken(r.Context(), database, user.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to log in")
		return
	}
	http.SetCookie(w, sessionCookie(token, sessionTimeMinutes*60))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func sessionCookie(value string, maxAge int) *http.Cookie {
	cookie := &http.Cookie{
		Name:     "auth",
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   appConfig.SecureCookies,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   maxAge,
	}
	if maxAge > 0 {
		cookie.Expires = time.Now().Add(time.Duration(maxAge) * time.Second)
	} else {
		cookie.Expires = time.Unix(1, 0)
	}
	return cookie
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	cookie, err := r.Cookie("auth")
	if err == nil && cookie.Value != "" {
		if err := DeleteSessionToken(r.Context(), database, cookie.Value); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "Unable to log out")
			return
		}
	}
	http.SetCookie(w, sessionCookie("", -1))
	w.WriteHeader(http.StatusNoContent)
}

func HandleRegisterUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var account struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := decodeJSON(w, r, &account); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !accountCreateLimiter.allow(normalizeEmail(account.Email)) {
		writeAPIError(w, http.StatusTooManyRequests, "Too many requests")
		return
	}
	if account.Role != "user" && account.Role != "admin" {
		writeAPIError(w, http.StatusBadRequest, "Invalid role")
		return
	}
	userID, token, err := createUnverifiedUser(r.Context(), database, account.Name, account.Email, account.Password, account.Role, "activation")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, publicAccountError(err))
		return
	}
	verificationURL := verificationPageURL(token)
	if err := emailSender.SendActivation(r.Context(), normalizeEmail(account.Email), strings.TrimSpace(account.Name), verificationURL); err != nil {
		if _, cleanupErr := database.ExecContext(r.Context(), "DELETE FROM users WHERE id = ?", userID); cleanupErr != nil {
			logRequestError(r, "cleanup failed admin-created user", cleanupErr)
		}
		writeAPIError(w, http.StatusBadGateway, "Activation email could not be sent")
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func HandleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	actor := requestUser(w, r)
	if actor == nil {
		return
	}
	var request struct {
		ID int `json:"id"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := DeleteUser(r.Context(), database, actor.ID, request.ID); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func HandleEditUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	actor := requestUser(w, r)
	if actor == nil {
		return
	}
	var request struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Role string `json:"role"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := EditUser(r.Context(), database, actor.ID, request.ID, request.Name, request.Role); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func HandleGetUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	type userSummary struct {
		ID            int    `json:"id"`
		Name          string `json:"name"`
		Email         string `json:"email"`
		Role          string `json:"role"`
		EmailVerified bool   `json:"email_verified"`
	}
	rows, err := database.QueryContext(r.Context(), `
		SELECT id, name, COALESCE(email, ''), role, email_verified
		FROM users
		ORDER BY name
	`)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to load users")
		return
	}
	defer rows.Close()
	users := make([]userSummary, 0)
	for rows.Next() {
		var user userSummary
		if err := rows.Scan(&user.ID, &user.Name, &user.Email, &user.Role, &user.EmailVerified); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "Unable to load users")
			return
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to load users")
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func HandleSetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	var passwords struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(w, r, &passwords); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := SetUserPassword(r.Context(), database, user, passwords.CurrentPassword, passwords.NewPassword); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	http.SetCookie(w, sessionCookie("", -1))
	w.WriteHeader(http.StatusNoContent)
}

func HandleRequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var request struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	normalizedEmail := normalizeEmail(request.Email)
	if !resetAccountLimiter.allow(normalizedEmail) {
		writeAPIError(w, http.StatusTooManyRequests, "Too many requests")
		return
	}
	var userID int
	var userName, email string
	err := database.QueryRowContext(r.Context(), `
		SELECT id, name, email
		FROM users
		WHERE email_normalized = ? AND email_verified = TRUE
	`, normalizedEmail).Scan(&userID, &userName, &email)
	if err == sql.ErrNoRows {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to request password reset")
		return
	}
	token, tokenHash, err := newToken()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to request password reset")
		return
	}
	tx, err := database.BeginTx(r.Context(), nil)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to request password reset")
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), "DELETE FROM password_resets WHERE user_id = ?", userID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to request password reset")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO password_resets (token_hash, user_id, expires_at)
		VALUES (?, ?, ?)
	`, tokenHash, userID, timestamp(time.Now().Add(time.Hour))); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to request password reset")
		return
	}
	if err := tx.Commit(); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to request password reset")
		return
	}
	resetURL := appConfig.AppBaseURL.String() + "/reset-password.html?token=" + url.QueryEscape(token)
	if err := emailSender.SendPasswordReset(r.Context(), email, userName, resetURL); err != nil {
		if _, cleanupErr := database.ExecContext(r.Context(), "DELETE FROM password_resets WHERE token_hash = ?", tokenHash); cleanupErr != nil {
			logRequestError(r, "cleanup password reset token", cleanupErr)
		}
		writeAPIError(w, http.StatusBadGateway, "Password reset email could not be sent")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func HandleResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var request struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.Token == "" {
		writeAPIError(w, http.StatusBadRequest, "Invalid or expired password reset link")
		return
	}
	if err := validatePassword(request.NewPassword); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	tokenHash := sha256.Sum256([]byte(request.Token))
	tx, err := database.BeginTx(r.Context(), nil)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to reset password")
		return
	}
	defer tx.Rollback()
	var userID int
	err = tx.QueryRowContext(r.Context(), `
		SELECT user_id FROM password_resets
		WHERE token_hash = ? AND expires_at > CURRENT_TIMESTAMP
	`, tokenHash[:]).Scan(&userID)
	if err == sql.ErrNoRows {
		writeAPIError(w, http.StatusBadRequest, "Invalid or expired password reset link")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to reset password")
		return
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(request.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to reset password")
		return
	}
	if _, err := tx.ExecContext(r.Context(), "UPDATE users SET password_hash = ? WHERE id = ?", string(passwordHash), userID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to reset password")
		return
	}
	if _, err := tx.ExecContext(r.Context(), "DELETE FROM password_resets WHERE user_id = ?", userID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to reset password")
		return
	}
	if _, err := tx.ExecContext(r.Context(), "DELETE FROM sessions WHERE user_id = ?", userID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to reset password")
		return
	}
	if err := tx.Commit(); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to reset password")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func HandleCurrentUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":           user.Name,
		"email":          user.Email,
		"pending_email":  user.PendingEmail,
		"role":           user.Role,
		"email_verified": user.EmailVerified,
	})
}

func HandleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	var profile struct {
		Name            string `json:"name"`
		Email           string `json:"email"`
		CurrentPassword string `json:"current_password"`
	}
	if err := decodeJSON(w, r, &profile); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	token, changedEmail, err := updateUserProfile(r.Context(), database, user, profile.Name, profile.Email, profile.CurrentPassword)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if changedEmail {
		verificationURL := verificationPageURL(token)
		if err := emailSender.SendVerification(r.Context(), normalizeEmail(profile.Email), strings.TrimSpace(profile.Name), verificationURL); err != nil {
			tx, cleanupErr := database.BeginTx(r.Context(), nil)
			if cleanupErr == nil {
				if _, cleanupErr = tx.ExecContext(r.Context(), `
					UPDATE users SET pending_email = NULL, pending_email_normalized = NULL WHERE id = ?
				`, user.ID); cleanupErr == nil {
					_, cleanupErr = tx.ExecContext(r.Context(), `
						DELETE FROM email_verifications WHERE user_id = ? AND kind = 'email_change'
					`, user.ID)
				}
				if cleanupErr == nil {
					cleanupErr = tx.Commit()
				} else {
					_ = tx.Rollback()
				}
			}
			if cleanupErr != nil {
				logRequestError(r, "cleanup pending email", cleanupErr)
			}
			writeAPIError(w, http.StatusBadGateway, "Verification email could not be sent")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"email_verification_required": changedEmail})
}

func updateUserProfile(ctx context.Context, db *sql.DB, user *User, name, email, currentPassword string) (string, bool, error) {
	name = strings.TrimSpace(name)
	email = normalizeEmail(email)
	if err := validateUsername(name); err != nil {
		return "", false, err
	}
	if err := validateEmail(email); err != nil {
		return "", false, err
	}
	emailChanged := normalizeEmail(user.Email) != email
	if emailChanged && bcrypt.CompareHashAndPassword(user.Password, []byte(currentPassword)) != nil {
		return "", false, fmt.Errorf("current password is required to change email")
	}

	var token string
	var tokenHash []byte
	var err error
	if emailChanged {
		token, tokenHash, err = newToken()
		if err != nil {
			return "", false, err
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE users SET name = ? WHERE id = ?", name, user.ID); err != nil {
		return "", false, fmt.Errorf("update profile: %w", err)
	}
	if emailChanged {
		if _, err := tx.ExecContext(ctx, `
			UPDATE users SET pending_email = ?, pending_email_normalized = ? WHERE id = ?
		`, email, normalizeEmail(email), user.ID); err != nil {
			return "", false, fmt.Errorf("email is already registered")
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM email_verifications WHERE user_id = ? AND kind = 'email_change'", user.ID); err != nil {
			return "", false, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO email_verifications (token_hash, user_id, email, kind, expires_at)
			VALUES (?, ?, ?, 'email_change', ?)
		`, tokenHash, user.ID, email, timestamp(time.Now().Add(24*time.Hour))); err != nil {
			return "", false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", false, err
	}
	return token, emailChanged, nil
}

func newToken() (string, []byte, error) {
	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	hash := sha256.Sum256([]byte(token))
	return token, hash[:], nil
}

func timestamp(value time.Time) string {
	return value.UTC().Format("2006-01-02 15:04:05")
}

func publicAccountError(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "username"):
		return message
	case strings.Contains(message, "email"):
		return message
	case strings.Contains(message, "password"):
		return message
	default:
		return "Unable to create account"
	}
}

func logRequestError(r *http.Request, message string, err error) {
	fmt.Printf("%s request_id=%s error=%v\n", message, requestIDFromContext(r.Context()), err)
}
