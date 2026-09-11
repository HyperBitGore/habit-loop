package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func HandleCreateAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var account struct {
		Name           string `json:"name"`
		Email          string `json:"email"`
		Password       string `json:"password"`
		TurnstileToken string `json:"turnstile_token"`
	}
	if err := decodeJSON(w, r, &account); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !requireTurnstile(w, r, account.TurnstileToken, "signup") {
		return
	}
	userID, token, err := createUnverifiedUser(r.Context(), database, account.Name, account.Email, account.Password, "user", "account")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, publicAccountError(err))
		return
	}
	if err := emailSender.SendVerification(
		r.Context(),
		normalizeEmail(account.Email),
		strings.TrimSpace(account.Name),
		verificationPageURL(token),
	); err != nil {
		if _, cleanupErr := database.ExecContext(r.Context(), "DELETE FROM users WHERE id = ?", userID); cleanupErr != nil {
			logRequestError(r, "cleanup failed registration", cleanupErr)
		}
		writeAPIError(w, http.StatusBadGateway, "Verification email could not be sent")
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func createUnverifiedUser(
	ctx context.Context,
	db *sql.DB,
	name string,
	email string,
	password string,
	role string,
	verificationKind string,
) (int64, string, error) {
	name = strings.TrimSpace(name)
	email = normalizeEmail(email)
	if err := validateUsername(name); err != nil {
		return 0, "", err
	}
	if err := validateEmail(email); err != nil {
		return 0, "", err
	}
	if err := validatePassword(password); err != nil {
		return 0, "", err
	}
	if role != "user" && role != "admin" {
		return 0, "", fmt.Errorf("invalid role")
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, "", err
	}
	token, tokenHash, err := newToken()
	if err != nil {
		return 0, "", err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO users (
			name, email, email_normalized, password_hash, role, email_verified
		)
		VALUES (?, ?, ?, ?, ?, FALSE)
	`, name, email, normalizeEmail(email), string(passwordHash), role)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "name") {
			return 0, "", fmt.Errorf("username is already taken")
		}
		if strings.Contains(strings.ToLower(err.Error()), "email") {
			return 0, "", fmt.Errorf("email is already registered")
		}
		return 0, "", err
	}
	userID, err := result.LastInsertId()
	if err != nil {
		return 0, "", err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO email_verifications (token_hash, user_id, email, kind, expires_at)
		VALUES (?, ?, ?, ?, ?)
	`, tokenHash, userID, email, verificationKind, timestamp(time.Now().Add(24*time.Hour))); err != nil {
		return 0, "", err
	}
	if err := tx.Commit(); err != nil {
		return 0, "", err
	}
	return userID, token, nil
}

func verificationPageURL(token string) string {
	return appConfig.AppBaseURL.String() + "/verify-email.html?token=" + url.QueryEscape(token)
}

func HandleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var request struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	tokenHash := sha256.Sum256([]byte(request.Token))
	tx, err := database.BeginTx(r.Context(), nil)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to verify email")
		return
	}
	defer tx.Rollback()

	var userID int
	var email, kind string
	err = tx.QueryRowContext(r.Context(), `
		SELECT user_id, email, kind
		FROM email_verifications
		WHERE token_hash = ? AND expires_at > CURRENT_TIMESTAMP
	`, tokenHash[:]).Scan(&userID, &email, &kind)
	if err == sql.ErrNoRows {
		writeAPIError(w, http.StatusBadRequest, "Invalid or expired verification link")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to verify email")
		return
	}
	switch kind {
	case "account", "activation":
		if _, err := tx.ExecContext(r.Context(), `
			UPDATE users SET email = ?, email_normalized = ?, email_verified = TRUE
			WHERE id = ?
		`, email, normalizeEmail(email), userID); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "Unable to verify email")
			return
		}
	case "email_change":
		if _, err := tx.ExecContext(r.Context(), `
			UPDATE users
			SET email = ?, email_normalized = ?, email_verified = TRUE,
			    pending_email = NULL, pending_email_normalized = NULL
			WHERE id = ?
		`, email, normalizeEmail(email), userID); err != nil {
			writeAPIError(w, http.StatusBadRequest, "Email is already registered")
			return
		}
	default:
		writeAPIError(w, http.StatusBadRequest, "Invalid verification link")
		return
	}
	if _, err := tx.ExecContext(r.Context(), "DELETE FROM email_verifications WHERE user_id = ?", userID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to verify email")
		return
	}
	if err := tx.Commit(); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to verify email")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
