package main

import (
	"context"
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
	if appConfig.PublicRegistrationSet && !appConfig.PublicRegistration {
		writeAPIError(w, http.StatusForbidden, "Public registration is disabled")
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
	userID, token, err := appStore.CreateUnverifiedUser(r.Context(), account.Name, account.Email, account.Password, "user", "account")
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
		if cleanupErr := appStore.DeleteUserByID(r.Context(), int(userID)); cleanupErr != nil {
			logRequestError(r, "cleanup failed registration", cleanupErr)
		}
		writeAPIError(w, http.StatusBadGateway, "Verification email could not be sent")
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Store) CreateUnverifiedUser(
	ctx context.Context,
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

	tx, err := s.db.BeginTx(ctx, nil)
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
	if err := appStore.VerifyEmail(r.Context(), request.Token); err != nil {
		if err == errInvalidVerificationLink || err.Error() == "email is already registered" {
			writeAPIError(w, http.StatusBadRequest, err.Error())
		} else {
			writeAPIError(w, http.StatusInternalServerError, "Unable to verify email")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
