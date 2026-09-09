package main

import (
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

func userIDFromToken(db *sql.DB, token string) (int, bool, error) {
	var userID int
	err := db.QueryRow(`
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

func CreateSessionToken(db *sql.DB, userID int) (string, error) {
	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(rawToken)

	_, err := db.Exec(`
		INSERT INTO sessions (token_hash, user_id, expires_at)
		VALUES (?, ?, ?)
	`, hashSessionToken(token), userID, time.Now().UTC().Add(sessionTimeMinutes*time.Minute).Format("2006-01-02 15:04:05"))
	if err != nil {
		return "", err
	}

	return token, nil
}

func CheckSessionToken(token string) bool {
	_, ok, err := userIDFromToken(database, token)
	return err == nil && ok
}

func GetUserFromToken(token string) (*User, error) {
	id, ok, err := userIDFromToken(database, token)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	return getUserByID(database, id)
}

func DeleteSessionToken(token string) error {
	_, err := database.Exec(
		"DELETE FROM sessions WHERE token_hash = ?",
		hashSessionToken(token),
	)
	return err
}
