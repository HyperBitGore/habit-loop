package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var errInvalidVerificationLink = fmt.Errorf("invalid or expired verification link")
var errInvalidPasswordReset = fmt.Errorf("invalid or expired password reset link")

type UserSummary struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	Role          string `json:"role"`
	EmailVerified bool   `json:"email_verified"`
}

type UserCursor struct {
	Name string `json:"name"`
	ID   int    `json:"id"`
}

type UserPage struct {
	Users      []UserSummary `json:"users"`
	NextCursor string        `json:"next_cursor,omitempty"`
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

func (s *Store) GetUserByName(ctx context.Context, name string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `
		SELECT id, name, COALESCE(email, ''), COALESCE(pending_email, ''),
		       password_hash, role, email_verified
		FROM users
		WHERE name = ?
	`, strings.TrimSpace(name)))
}

func (s *Store) GetUserByID(ctx context.Context, id int) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `
		SELECT id, name, COALESCE(email, ''), COALESCE(pending_email, ''),
		       password_hash, role, email_verified
		FROM users
		WHERE id = ?
	`, id))
}

func (s *Store) BootstrapAdmin(ctx context.Context, cfg Config) error {
	var exists bool
	if err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE role = 'admin')").Scan(&exists); err != nil {
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
	_, err = s.db.ExecContext(ctx, `
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

func (s *Store) ListUsers(ctx context.Context, search string, cursor UserCursor, pageSize int) (UserPage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, COALESCE(email, ''), role, email_verified
		FROM users
		WHERE (
			? = ''
			OR instr(lower(name), lower(?)) > 0
			OR instr(lower(COALESCE(email, '')), lower(?)) > 0
		)
		  AND (
			? = ''
			OR lower(name) > ?
			OR (lower(name) = ? AND id > ?)
		  )
		ORDER BY lower(name), id
		LIMIT ?
	`, search, search, search, cursor.Name, cursor.Name, cursor.Name, cursor.ID, pageSize+1)
	if err != nil {
		return UserPage{}, err
	}
	defer rows.Close()

	users := make([]UserSummary, 0, pageSize+1)
	for rows.Next() {
		var user UserSummary
		if err := rows.Scan(&user.ID, &user.Name, &user.Email, &user.Role, &user.EmailVerified); err != nil {
			return UserPage{}, err
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return UserPage{}, err
	}

	page := UserPage{Users: users}
	if len(page.Users) > pageSize {
		lastUser := page.Users[pageSize-1]
		encodedCursor, err := json.Marshal(UserCursor{
			Name: strings.ToLower(lastUser.Name),
			ID:   lastUser.ID,
		})
		if err != nil {
			return UserPage{}, err
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(encodedCursor)
		page.Users = page.Users[:pageSize]
	}
	return page, nil
}

func (s *Store) VerifyEmail(ctx context.Context, token string) error {
	tokenHash := sha256.Sum256([]byte(token))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var userID int
	var email, kind string
	err = tx.QueryRowContext(ctx, `
		SELECT user_id, email, kind
		FROM email_verifications
		WHERE token_hash = ? AND expires_at > CURRENT_TIMESTAMP
	`, tokenHash[:]).Scan(&userID, &email, &kind)
	if err == sql.ErrNoRows {
		return errInvalidVerificationLink
	}
	if err != nil {
		return err
	}

	switch kind {
	case "account", "activation":
		if _, err := tx.ExecContext(ctx, `
			UPDATE users SET email = ?, email_normalized = ?, email_verified = TRUE
			WHERE id = ?
		`, email, normalizeEmail(email), userID); err != nil {
			return err
		}
	case "email_change":
		if _, err := tx.ExecContext(ctx, `
			UPDATE users
			SET email = ?, email_normalized = ?, email_verified = TRUE,
			    pending_email = NULL, pending_email_normalized = NULL
			WHERE id = ?
		`, email, normalizeEmail(email), userID); err != nil {
			return fmt.Errorf("email is already registered")
		}
	default:
		return errInvalidVerificationLink
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM email_verifications WHERE user_id = ?", userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteUserByID(ctx context.Context, userID int) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", userID)
	return err
}

func (s *Store) FindVerifiedUserByEmail(ctx context.Context, email string) (int, string, string, bool, error) {
	var userID int
	var userName, storedEmail string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, email
		FROM users
		WHERE email_normalized = ? AND email_verified = TRUE
	`, email).Scan(&userID, &userName, &storedEmail)
	if err == sql.ErrNoRows {
		return 0, "", "", false, nil
	}
	if err != nil {
		return 0, "", "", false, err
	}
	return userID, userName, storedEmail, true, nil
}

func (s *Store) CreatePasswordReset(ctx context.Context, userID int, tokenHash []byte) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM password_resets WHERE user_id = ?", userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO password_resets (token_hash, user_id, expires_at)
		VALUES (?, ?, ?)
	`, tokenHash, userID, timestamp(time.Now().Add(time.Hour))); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeletePasswordReset(ctx context.Context, tokenHash []byte) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM password_resets WHERE token_hash = ?", tokenHash)
	return err
}

func (s *Store) ClearPendingEmail(ctx context.Context, userID int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE users SET pending_email = NULL, pending_email_normalized = NULL WHERE id = ?
	`, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM email_verifications WHERE user_id = ? AND kind = 'email_change'
	`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ResetPassword(ctx context.Context, tokenHash []byte, passwordHash []byte) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var userID int
	if err := tx.QueryRowContext(ctx, `
		SELECT user_id FROM password_resets
		WHERE token_hash = ? AND expires_at > CURRENT_TIMESTAMP
	`, tokenHash).Scan(&userID); err != nil {
		if err == sql.ErrNoRows {
			return errInvalidPasswordReset
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE users SET password_hash = ? WHERE id = ?", string(passwordHash), userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM password_resets WHERE user_id = ?", userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", userID); err != nil {
		return err
	}
	return tx.Commit()
}
