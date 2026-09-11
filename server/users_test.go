package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type fakeEmailSender struct {
	verificationURL string
	activationURL   string
	resetURL        string
}

func (sender *fakeEmailSender) SendVerification(_ context.Context, _, _, url string) error {
	sender.verificationURL = url
	return nil
}

func (sender *fakeEmailSender) SendPasswordReset(_ context.Context, _, _, url string) error {
	sender.resetURL = url
	return nil
}

func (sender *fakeEmailSender) SendActivation(_ context.Context, _, _, url string) error {
	sender.activationURL = url
	return nil
}

func setupTestApplication(t *testing.T) *fakeEmailSender {
	t.Helper()
	db, err := InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	appConfig, err = LoadConfigForTest()
	if err != nil {
		t.Fatal(err)
	}
	sender := &fakeEmailSender{}
	emailSender = sender
	loginLimiter = newRateLimiter(100, time.Minute)
	loginAccountLimiter = newRateLimiter(100, time.Minute)
	registrationLimiter = newRateLimiter(100, time.Minute)
	accountCreateLimiter = newRateLimiter(100, time.Minute)
	resetLimiter = newRateLimiter(100, time.Minute)
	resetAccountLimiter = newRateLimiter(100, time.Minute)
	previousVerifier := turnstileVerifier
	turnstileVerifier = func(context.Context, string, string, string) error {
		return nil
	}
	t.Cleanup(func() {
		turnstileVerifier = previousVerifier
	})
	return sender
}

func LoadConfigForTest() (Config, error) {
	config := Config{
		Environment:        "test",
		ListenAddr:         ":0",
		DatabasePath:       ":memory:",
		WebRoot:            "../web",
		SecureCookies:      true,
		TurnstileSecret:    "test-secret",
		TurnstileHostnames: []string{"example.test"},
	}
	baseURL, err := url.Parse("https://example.test")
	config.AppBaseURL = baseURL
	return config, err
}

func TestPublicRegistrationCannotCreateAdmin(t *testing.T) {
	setupTestApplication(t)
	request := httptest.NewRequest(http.MethodPost, "/api/create_account", strings.NewReader(
		`{"name":"alice","email":"alice@example.com","password":"password123","role":"admin"}`,
	))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	HandleCreateAccount(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("public registration created %d users", count)
	}
}

func TestRegistrationLinkIgnoresRequestHost(t *testing.T) {
	sender := setupTestApplication(t)
	request := httptest.NewRequest(http.MethodPost, "http://attacker.example/api/create_account", strings.NewReader(
		`{"name":"alice","email":"alice@example.com","password":"password123"}`,
	))
	request.Host = "attacker.example"
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	HandleCreateAccount(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if !strings.HasPrefix(sender.verificationURL, "https://example.test/") {
		t.Fatalf("verification URL = %q", sender.verificationURL)
	}
}

func TestLoginSetsSecureCookie(t *testing.T) {
	setupTestApplication(t)
	insertTestUser(t, "alice", "alice@example.com", "user")
	request := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(
		`{"name":"alice","password":"password123"}`,
	))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	HandleLogin(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("insecure session cookie: %#v", cookies)
	}
}

func TestUnverifiedLoginUsesGenericFailure(t *testing.T) {
	setupTestApplication(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO users (name, email, email_normalized, password_hash, role, email_verified)
		VALUES ('alice', 'alice@example.com', 'alice@example.com', ?, 'user', FALSE)
	`, string(hash)); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(
		`{"name":"alice","password":"password123"}`,
	))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	HandleLogin(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "Invalid login credentials") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBootstrapAdminRunsOnlyWhenNoAdminExists(t *testing.T) {
	setupTestApplication(t)
	config := appConfig
	config.BootstrapAdminName = "owner"
	config.BootstrapAdminEmail = "owner@example.com"
	config.BootstrapAdminPassword = "password123"
	if err := bootstrapAdmin(context.Background(), database, config); err != nil {
		t.Fatal(err)
	}
	config.BootstrapAdminName = "attacker"
	config.BootstrapAdminEmail = "attacker@example.com"
	if err := bootstrapAdmin(context.Background(), database, config); err != nil {
		t.Fatal(err)
	}
	var admins int
	if err := database.QueryRow("SELECT COUNT(*) FROM users WHERE role = 'admin'").Scan(&admins); err != nil {
		t.Fatal(err)
	}
	if admins != 1 {
		t.Fatalf("admin count = %d, want 1", admins)
	}
}

func TestDeletingUserCascadesOwnedData(t *testing.T) {
	setupTestApplication(t)
	adminID := insertTestUser(t, "admin", "admin@example.com", "admin")
	userID := insertTestUser(t, "alice", "alice@example.com", "user")
	if _, err := database.Exec("INSERT INTO todos (user_id, name, date) VALUES (?, 'task', '2026-09-09')", userID); err != nil {
		t.Fatal(err)
	}
	result, err := database.Exec("INSERT INTO habits (user_id, name) VALUES (?, 'habit')", userID)
	if err != nil {
		t.Fatal(err)
	}
	habitID, _ := result.LastInsertId()
	if _, err := database.Exec("INSERT INTO completions (habit_id, date) VALUES (?, '2026-09-09')", habitID); err != nil {
		t.Fatal(err)
	}
	if err := DeleteUser(context.Background(), database, adminID, userID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"todos", "habits", "completions"} {
		var count int
		if err := database.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d after delete", table, count)
		}
	}
}

func TestFinalAdminCannotBeDeletedOrDemoted(t *testing.T) {
	setupTestApplication(t)
	adminID := insertTestUser(t, "admin", "admin@example.com", "admin")
	otherID := insertTestUser(t, "alice", "alice@example.com", "user")
	if err := DeleteUser(context.Background(), database, otherID, adminID); err == nil {
		t.Fatal("final admin deletion succeeded")
	}
	if err := EditUser(context.Background(), database, otherID, adminID, "admin", "user"); err == nil {
		t.Fatal("final admin demotion succeeded")
	}
}

func TestPasswordChangeInvalidatesSessions(t *testing.T) {
	setupTestApplication(t)
	userID := insertTestUser(t, "alice", "alice@example.com", "user")
	user, err := getUserByID(context.Background(), database, userID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateSessionToken(context.Background(), database, userID); err != nil {
		t.Fatal(err)
	}
	if err := SetUserPassword(context.Background(), database, user, "password123", "newpassword123"); err != nil {
		t.Fatal(err)
	}
	var sessions int
	if err := database.QueryRow("SELECT COUNT(*) FROM sessions WHERE user_id = ?", userID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("sessions = %d after password change", sessions)
	}
}

func TestEmailChangeRequiresPasswordAndStaysPending(t *testing.T) {
	setupTestApplication(t)
	userID := insertTestUser(t, "alice", "alice@example.com", "user")
	user, err := getUserByID(context.Background(), database, userID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := updateUserProfile(context.Background(), database, user, "alice", "new@example.com", "wrong"); err == nil {
		t.Fatal("email change succeeded with wrong password")
	}
	token, changed, err := updateUserProfile(context.Background(), database, user, "alice", "new@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if !changed || token == "" {
		t.Fatal("email change did not create a verification token")
	}
	var email, pending string
	if err := database.QueryRow(`
		SELECT email, pending_email FROM users WHERE id = ?
	`, userID).Scan(&email, &pending); err != nil {
		t.Fatal(err)
	}
	if email != "alice@example.com" || pending != "new@example.com" {
		t.Fatalf("email=%q pending=%q", email, pending)
	}
}

func TestPasswordResetTokenIsSingleUse(t *testing.T) {
	setupTestApplication(t)
	userID := insertTestUser(t, "alice", "alice@example.com", "user")
	token := "reset-token"
	tokenHash := sha256.Sum256([]byte(token))
	if _, err := database.Exec(`
		INSERT INTO password_resets (token_hash, user_id, expires_at)
		VALUES (?, ?, ?)
	`, tokenHash[:], userID, timestamp(time.Now().Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	reset := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/reset-password", strings.NewReader(
			`{"token":"reset-token","new_password":"newpassword123"}`,
		))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		HandleResetPassword(response, request)
		return response
	}
	if response := reset(); response.Code != http.StatusNoContent {
		t.Fatalf("first reset status=%d body=%s", response.Code, response.Body.String())
	}
	if response := reset(); response.Code != http.StatusBadRequest {
		t.Fatalf("second reset status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCrossUserTaskMutationIsRejected(t *testing.T) {
	setupTestApplication(t)
	ownerID := insertTestUser(t, "owner", "owner@example.com", "user")
	otherID := insertTestUser(t, "other", "other@example.com", "user")
	result, err := database.Exec(`
		INSERT INTO todos (user_id, name, date) VALUES (?, 'private', '2026-09-09')
	`, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	taskID, _ := result.LastInsertId()
	other, err := getUserByID(context.Background(), database, otherID)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/update_task", nil)
	request = request.WithContext(context.WithValue(request.Context(), userContextKey, other))
	request.Header.Set("X-Task-ID", strconv.FormatInt(taskID, 10))
	request.Header.Set("X-Task-Name", "stolen")
	request.Header.Set("X-Task-Date", "2026-09-09")
	request.Header.Set("X-Task-Complete", "false")
	response := httptest.NewRecorder()
	HandleUpdateTask(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var name string
	if err := database.QueryRow("SELECT name FROM todos WHERE id = ?", taskID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "private" {
		t.Fatalf("cross-user mutation changed task to %q", name)
	}
}

func TestDuplicateHabitCompletionIsIdempotent(t *testing.T) {
	setupTestApplication(t)
	userID := insertTestUser(t, "alice", "alice@example.com", "user")
	result, err := database.Exec("INSERT INTO habits (user_id, name) VALUES (?, 'habit')", userID)
	if err != nil {
		t.Fatal(err)
	}
	habitID, _ := result.LastInsertId()
	user, err := getUserByID(context.Background(), database, userID)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		request := httptest.NewRequest(http.MethodPost, "/api/complete_habit", strings.NewReader(
			fmt.Sprintf(`{"habit_id":%d,"date":"2026-09-09"}`, habitID),
		))
		request.Header.Set("Content-Type", "application/json")
		request = request.WithContext(context.WithValue(request.Context(), userContextKey, user))
		response := httptest.NewRecorder()
		HandleCompleteHabit(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
		}
	}
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM completions WHERE habit_id = ?", habitID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("completion count = %d", count)
	}
}

func TestGetUsersSupportsSearchAndCursorPagination(t *testing.T) {
	setupTestApplication(t)
	tx, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 205; index++ {
		name := fmt.Sprintf("user%03d", index)
		email := fmt.Sprintf("%s@example.com", name)
		if index == 150 {
			email = "matching-search@example.com"
		}
		if _, err := tx.Exec(`
			INSERT INTO users (
				name, email, email_normalized, password_hash, role, email_verified
			)
			VALUES (?, ?, ?, 'hash', 'user', TRUE)
		`, name, email, normalizeEmail(email)); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	type userSummary struct {
		ID    int    `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	type userPage struct {
		Users      []userSummary `json:"users"`
		NextCursor string        `json:"next_cursor"`
	}
	readPage := func(target string) userPage {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		response := httptest.NewRecorder()
		HandleGetUsers(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
		}
		var page userPage
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}

	firstPage := readPage("/api/get_users")
	if len(firstPage.Users) != 100 || firstPage.NextCursor == "" {
		t.Fatalf("first page users=%d cursor=%q", len(firstPage.Users), firstPage.NextCursor)
	}
	secondPage := readPage("/api/get_users?cursor=" + url.QueryEscape(firstPage.NextCursor))
	if len(secondPage.Users) != 100 || secondPage.NextCursor == "" {
		t.Fatalf("second page users=%d cursor=%q", len(secondPage.Users), secondPage.NextCursor)
	}
	thirdPage := readPage("/api/get_users?cursor=" + url.QueryEscape(secondPage.NextCursor))
	if len(thirdPage.Users) != 5 || thirdPage.NextCursor != "" {
		t.Fatalf("third page users=%d cursor=%q", len(thirdPage.Users), thirdPage.NextCursor)
	}
	if firstPage.Users[99].Name >= secondPage.Users[0].Name ||
		secondPage.Users[99].Name >= thirdPage.Users[0].Name {
		t.Fatal("cursor pages are not strictly ordered")
	}

	searchPage := readPage("/api/get_users?search=matching-search")
	if len(searchPage.Users) != 1 || searchPage.Users[0].Name != "user150" {
		t.Fatalf("search results = %+v", searchPage.Users)
	}
}

func insertTestUser(t *testing.T, name, email, role string) int {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	result, err := database.Exec(`
		INSERT INTO users (name, email, email_normalized, password_hash, role, email_verified)
		VALUES (?, ?, ?, ?, ?, TRUE)
	`, name, email, normalizeEmail(email), string(hash), role)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	return int(id)
}
