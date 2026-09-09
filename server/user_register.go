package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func HandleCreateAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()

	var account struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&account); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	account.Name = strings.TrimSpace(account.Name)
	account.Email = strings.TrimSpace(account.Email)
	if account.Name == "" || account.Password == "" {
		http.Error(w, "Name and password are required", http.StatusBadRequest)
		return
	}
	address, err := mail.ParseAddress(account.Email)
	if err != nil || address.Address != account.Email {
		http.Error(w, "A valid email is required", http.StatusBadRequest)
		return
	}

	userID, token, err := createRegisteredUser(database, account.Name, account.Email, account.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	verificationURL := strings.TrimRight(os.Getenv("APP_BASE_URL"), "/") +
		"/api/verify-email?token=" + token
	if err := SendVerificationEmail(account.Email, account.Name, verificationURL); err != nil {
		if cleanupErr := DeleteUser(database, account.Name, int(userID)); cleanupErr != nil {
			http.Error(w, "Verification email failed and account cleanup also failed", http.StatusInternalServerError)
			return
		}
		http.Error(w, "Verification email could not be sent; account was not created", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func createRegisteredUser(db *sql.DB, name, email, password string) (int64, string, error) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, "", fmt.Errorf("failed to generate password hash: %w", err)
	}

	var exists bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE name = ?)", name).Scan(&exists); err != nil {
		return 0, "", err
	}
	if exists {
		return 0, "", fmt.Errorf("username %q is already taken", name)
	}
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE email = ?)", email).Scan(&exists); err != nil {
		return 0, "", err
	}
	if exists {
		return 0, "", fmt.Errorf("email is already registered")
	}

	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		return 0, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	tokenHash := sha256.Sum256([]byte(token))

	tx, err := db.Begin()
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
		INSERT INTO users (name, email, password_hash, role, email_verified)
		VALUES (?, ?, ?, 'user', FALSE)
	`, name, email, string(passwordHash))
	if err != nil {
		return 0, "", fmt.Errorf("failed to create account: %w", err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		return 0, "", err
	}
	_, err = tx.Exec(`
		INSERT INTO email_verifications (token_hash, user_id, expires_at)
		VALUES (?, ?, ?)
	`, tokenHash[:], userID, time.Now().UTC().Add(24*time.Hour).Format("2006-01-02 15:04:05"))
	if err != nil {
		return 0, "", err
	}
	if err := tx.Commit(); err != nil {
		return 0, "", err
	}

	return userID, token, nil
}

func SendVerificationEmail(targetEmail, userName, verificationURL string) error {
	apiKey := os.Getenv("RESEND_API_KEY")
	from := os.Getenv("RESEND_FROM_EMAIL")
	fmt.Printf("from: %v\n", from)
	if apiKey == "" || from == "" {
		return fmt.Errorf("RESEND_API_KEY and RESEND_FROM_EMAIL must be configured")
	}

	payload, err := json.Marshal(map[string]any{
		"from":    from,
		"to":      []string{targetEmail},
		"subject": "Verify your Habit Loop account",
		"html": fmt.Sprintf(
			"<p>Hello %s,</p><p><a href=\"%s\">Verify your account</a>.</p>",
			html.EscapeString(userName),
			html.EscapeString(verificationURL),
		),
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(
		http.MethodPost,
		"https://api.resend.com/emails",
		strings.NewReader(string(payload)),
	)
	if err != nil {
		fmt.Printf("Error from resend: %v\n", err)
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Resend returned %s", resp.Status)
	}
	return nil
}

func HandleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Verification token is required", http.StatusBadRequest)
		return
	}
	tokenHash := sha256.Sum256([]byte(token))

	var userID int
	err := database.QueryRow(`
		SELECT user_id
		FROM email_verifications
		WHERE token_hash = ? AND expires_at > CURRENT_TIMESTAMP
	`, tokenHash[:]).Scan(&userID)
	if err == sql.ErrNoRows {
		http.Error(w, "Invalid or expired verification token", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	tx, err := database.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec("UPDATE users SET email_verified = TRUE WHERE id = ?", userID); err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec("DELETE FROM email_verifications WHERE token_hash = ?", tokenHash[:]); err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
