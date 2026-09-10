package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"time"
)

func hashSessionToken(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}

func userIDFromToken(ctx context.Context, db *sql.DB, token string) (int, bool, error) {
	var userID int
	err := db.QueryRowContext(ctx, `
		SELECT user_id
		FROM sessions
		WHERE token_hash = ? AND expires_at > CURRENT_TIMESTAMP
	`, hashSessionToken(token)).Scan(&userID)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return userID, true, nil
}

func CreateSessionToken(ctx context.Context, db *sql.DB, userID int) (string, error) {
	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	_, err := db.ExecContext(ctx, `
		INSERT INTO sessions (token_hash, user_id, expires_at)
		VALUES (?, ?, ?)
	`, hashSessionToken(token), userID, timestamp(time.Now().Add(sessionTimeMinutes*time.Minute)))
	if err != nil {
		return "", err
	}
	return token, nil
}

func GetUserFromToken(ctx context.Context, db *sql.DB, token string) (*User, error) {
	id, ok, err := userIDFromToken(ctx, db, token)
	if err != nil || !ok {
		return nil, err
	}
	return getUserByID(ctx, db, id)
}

func DeleteSessionToken(ctx context.Context, db *sql.DB, token string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", hashSessionToken(token))
	return err
}
