package main

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func setupTestUserStore(t *testing.T) {
	t.Helper()

	userDataMu.Lock()
	previousUsers := user_map
	previousNames := id_map
	previousPath := usersDBPath
	previousNextUserID := nextUserIDValue
	user_map = map[int]User{}
	id_map = map[string]int{}
	nextUserIDValue = 0
	usersDBPath = filepath.Join(t.TempDir(), "users", "db")
	userDataMu.Unlock()

	sessionMu.Lock()
	previousSessions := user_sessions
	previousTokens := token_map
	user_sessions = map[int]UserSession{}
	token_map = map[uint64]int{}
	sessionMu.Unlock()

	t.Cleanup(func() {
		sessionMu.Lock()
		user_sessions = previousSessions
		token_map = previousTokens
		sessionMu.Unlock()

		userDataMu.Lock()
		user_map = previousUsers
		id_map = previousNames
		usersDBPath = previousPath
		nextUserIDValue = previousNextUserID
		userDataMu.Unlock()
	})
}

func seedTestUser(t *testing.T, id int, name string) {
	t.Helper()
	err := withUserDataWrite(func(users map[int]User, names map[string]int, nextUserID *int) error {
		user := createUser(name, []byte("password hash"), "user")
		user.ID = id
		users[id] = user
		names[name] = id
		if *nextUserID <= id {
			*nextUserID = id + 1
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func collectErrors(t *testing.T, errors <-chan error) {
	t.Helper()
	for err := range errors {
		if err != nil {
			t.Errorf("concurrent operation failed: %v", err)
		}
	}
}

func TestConcurrentTaskAndHabitMutationsPreserveUpdates(t *testing.T) {
	setupTestUserStore(t)
	seedTestUser(t, 1, "worker")

	const operationCount = 40
	errors := make(chan error, operationCount)
	var waitGroup sync.WaitGroup
	for index := 0; index < operationCount; index++ {
		index := index
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			errors <- updateUser(1, func(user *User) error {
				if index%2 == 0 {
					addTask(
						&user.Tasks,
						&user.NextTaskID,
						fmt.Sprintf("task-%d", index),
						time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC),
						false,
					)
				} else {
					addHabit(
						&user.Habits,
						&user.NextHabitID,
						fmt.Sprintf("habit-%d", index),
						[]time.Time{},
					)
				}
				return nil
			})
		}()
	}
	waitGroup.Wait()
	close(errors)
	collectErrors(t, errors)

	user, ok := userSnapshotByID(1)
	if !ok {
		t.Fatal("user disappeared after concurrent mutations")
	}
	if len(user.Tasks) != operationCount/2 {
		t.Fatalf("got %d tasks, want %d", len(user.Tasks), operationCount/2)
	}
	if len(user.Habits) != operationCount/2 {
		t.Fatalf("got %d habits, want %d", len(user.Habits), operationCount/2)
	}

	persisted, _, _, err := readUsersFile(usersDBPath)
	if err != nil {
		t.Fatalf("reload user database: %v", err)
	}
	if len(persisted[1].Tasks) != operationCount/2 ||
		len(persisted[1].Habits) != operationCount/2 {
		t.Fatal("persisted user database lost concurrent updates")
	}
}

func TestConcurrentUserCreationAndRenameRemainUnique(t *testing.T) {
	setupTestUserStore(t)

	const userCount = 6
	errors := make(chan error, userCount)
	var waitGroup sync.WaitGroup
	for index := 0; index < userCount; index++ {
		index := index
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			errors <- AddUser(fmt.Sprintf("user-%d", index), "password", "user")
		}()
	}
	waitGroup.Wait()
	close(errors)
	collectErrors(t, errors)

	users := allUserSnapshots()
	if len(users) != userCount {
		t.Fatalf("got %d users, want %d", len(users), userCount)
	}
	ids := make(map[int]struct{}, userCount)
	for _, user := range users {
		ids[user.ID] = struct{}{}
	}
	if len(ids) != userCount {
		t.Fatalf("got %d unique IDs, want %d", len(ids), userCount)
	}

	first := getUser("user-0")
	second := getUser("user-1")
	results := make(chan error, 2)
	go func() { results <- EditUser("shared", first.ID, "user") }()
	go func() { results <- EditUser("shared", second.ID, "user") }()
	firstResult := <-results
	secondResult := <-results
	if (firstResult == nil) == (secondResult == nil) {
		t.Fatalf("exactly one conflicting rename must succeed: %v, %v", firstResult, secondResult)
	}
}

func TestDeletedUserIDIsNotReusedAfterReload(t *testing.T) {
	setupTestUserStore(t)
	if err := AddUser("first", "password", "user"); err != nil {
		t.Fatalf("add first user: %v", err)
	}
	if err := AddUser("second", "password", "user"); err != nil {
		t.Fatalf("add second user: %v", err)
	}
	second := getUser("second")
	if err := DeleteUser(second.Name, second.ID); err != nil {
		t.Fatalf("delete second user: %v", err)
	}
	if err := LoadUsers(); err != nil {
		t.Fatalf("reload users: %v", err)
	}
	if err := AddUser("third", "password", "user"); err != nil {
		t.Fatalf("add third user: %v", err)
	}
	third := getUser("third")
	if third.ID <= second.ID {
		t.Fatalf("reused deleted user ID %d for new user", third.ID)
	}
}

func TestTokenReadsDuringUserWrites(t *testing.T) {
	setupTestUserStore(t)
	seedTestUser(t, 1, "reader")

	const token uint64 = 12345
	sessionMu.Lock()
	token_map[token] = 1
	user_sessions[1] = UserSession{
		Token:      token,
		Expiration: time.Now().Add(time.Hour),
	}
	sessionMu.Unlock()

	const iterations = 50
	errors := make(chan error, iterations*2)
	var waitGroup sync.WaitGroup
	for index := 0; index < iterations; index++ {
		waitGroup.Add(2)
		go func() {
			defer waitGroup.Done()
			user := GetUserFromToken(token)
			if user.ID != 1 {
				errors <- fmt.Errorf("token resolved user ID %d", user.ID)
				return
			}
			errors <- nil
		}()
		go func(index int) {
			defer waitGroup.Done()
			errors <- updateUser(1, func(user *User) error {
				user.Role = fmt.Sprintf("role-%d", index)
				return nil
			})
		}(index)
	}
	waitGroup.Wait()
	close(errors)
	collectErrors(t, errors)
}

func TestFailedPersistenceDoesNotPublishMutation(t *testing.T) {
	setupTestUserStore(t)
	seedTestUser(t, 1, "stable")

	userDataMu.Lock()
	usersDBPath = t.TempDir()
	userDataMu.Unlock()

	err := updateUser(1, func(user *User) error {
		user.Name = "changed"
		return nil
	})
	if err == nil {
		t.Fatal("expected persistence failure")
	}

	user, ok := userSnapshotByID(1)
	if !ok {
		t.Fatal("user missing after failed mutation")
	}
	if user.Name != "stable" {
		t.Fatalf("failed mutation was published with name %q", user.Name)
	}
}

func TestUserSnapshotsDoNotShareMutableSlices(t *testing.T) {
	setupTestUserStore(t)
	seedTestUser(t, 1, "snapshot")
	if err := updateUser(1, func(user *User) error {
		addTask(
			&user.Tasks,
			&user.NextTaskID,
			"original",
			time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC),
			false,
		)
		addHabit(&user.Habits, &user.NextHabitID, "habit", []time.Time{
			time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC),
		})
		return nil
	}); err != nil {
		t.Fatalf("seed user data: %v", err)
	}

	snapshot, ok := userSnapshotByID(1)
	if !ok {
		t.Fatal("user snapshot not found")
	}
	snapshot.Tasks[0].Name = "mutated"
	snapshot.Habits[0].Name = "mutated"
	snapshot.Habits[0].Completions[0] = time.Time{}

	stored, _ := userSnapshotByID(1)
	if stored.Tasks[0].Name != "original" ||
		stored.Habits[0].Name != "habit" ||
		stored.Habits[0].Completions[0].IsZero() {
		t.Fatal("snapshot mutation changed stored user data")
	}
}
