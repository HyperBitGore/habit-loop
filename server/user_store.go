package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

var userDataMu sync.RWMutex
var usersDBPath = filepath.Join(usersDir, "db")
var nextUserIDValue int

type persistedUserData struct {
	Users      map[int]User `json:"users"`
	NextUserID int          `json:"next_user_id"`
}

func cloneHabit(habit Habit) Habit {
	habit.Completions = slices.Clone(habit.Completions)
	habit.Skips = slices.Clone(habit.Skips)
	return habit
}

func cloneUser(user User) User {
	user.Password = slices.Clone(user.Password)
	user.Tasks = slices.Clone(user.Tasks)
	user.Habits = slices.Clone(user.Habits)
	for i := range user.Habits {
		user.Habits[i] = cloneHabit(user.Habits[i])
	}
	return user
}

func cloneUsers(users map[int]User) map[int]User {
	cloned := make(map[int]User, len(users))
	for id, user := range users {
		cloned[id] = cloneUser(user)
	}
	return cloned
}

func cloneUserNames(names map[string]int) map[string]int {
	cloned := make(map[string]int, len(names))
	for name, id := range names {
		cloned[name] = id
	}
	return cloned
}

func readUsersFile(path string) (map[int]User, map[string]int, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[int]User{}, map[string]int{}, 0, nil
		}
		return nil, nil, 0, fmt.Errorf("read user database: %w", err)
	}

	var stored persistedUserData
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, nil, 0, fmt.Errorf("decode user database: %w", err)
	}
	users := stored.Users
	nextUserID := stored.NextUserID
	if users == nil {
		users = map[int]User{}
		if err := json.Unmarshal(data, &users); err != nil {
			return nil, nil, 0, fmt.Errorf("decode legacy user database: %w", err)
		}
	}

	names := make(map[string]int, len(users))
	maximumID := -1
	for id, user := range users {
		if existingID, exists := names[user.Name]; exists && existingID != id {
			return nil, nil, 0, fmt.Errorf("duplicate username %q in user database", user.Name)
		}
		maximumID = max(maximumID, id)
		user.ID = id
		if user.Tasks == nil {
			user.Tasks = []Task{}
		}
		if user.Habits == nil {
			user.Habits = []Habit{}
		}
		for i := range user.Habits {
			if user.Habits[i].Completions == nil {
				user.Habits[i].Completions = []time.Time{}
			}
			if user.Habits[i].Skips == nil {
				user.Habits[i].Skips = []time.Time{}
			}
		}
		users[id] = user
		names[user.Name] = id
	}
	if nextUserID <= maximumID {
		nextUserID = maximumID + 1
	}
	return users, names, nextUserID, nil
}

func writeUsersFile(path string, users map[int]User, nextUserID int) error {
	data, err := json.Marshal(persistedUserData{
		Users:      users,
		NextUserID: nextUserID,
	})
	if err != nil {
		return fmt.Errorf("encode user database: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create user database directory: %w", err)
	}
	file, err := os.CreateTemp(dir, ".users-db-*")
	if err != nil {
		return fmt.Errorf("create temporary user database: %w", err)
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)

	if err := file.Chmod(0600); err != nil {
		file.Close()
		return fmt.Errorf("set temporary user database permissions: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("write temporary user database: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync temporary user database: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary user database: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace user database: %w", err)
	}
	directory, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open user database directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync user database directory: %w", err)
	}
	return nil
}

func LoadUsers() error {
	users, names, nextUserID, err := readUsersFile(usersDBPath)
	if err != nil {
		return err
	}
	userDataMu.Lock()
	user_map = users
	id_map = names
	nextUserIDValue = nextUserID
	userDataMu.Unlock()
	return nil
}

func userSnapshotByID(id int) (User, bool) {
	userDataMu.RLock()
	user, ok := user_map[id]
	if ok {
		user = cloneUser(user)
	}
	userDataMu.RUnlock()
	return user, ok
}

func userSnapshotByName(name string) (User, bool) {
	userDataMu.RLock()
	id, ok := id_map[name]
	if ok {
		user, found := user_map[id]
		ok = found
		if found {
			user = cloneUser(user)
		}
		userDataMu.RUnlock()
		return user, ok
	}
	userDataMu.RUnlock()
	return User{}, false
}

func allUserSnapshots() []User {
	userDataMu.RLock()
	users := make([]User, 0, len(user_map))
	for _, user := range user_map {
		users = append(users, cloneUser(user))
	}
	userDataMu.RUnlock()
	return users
}

func withUserDataWrite(mutate func(map[int]User, map[string]int, *int) error) error {
	userDataMu.Lock()
	defer userDataMu.Unlock()

	users := cloneUsers(user_map)
	names := cloneUserNames(id_map)
	nextUserID := nextUserIDValue
	if err := mutate(users, names, &nextUserID); err != nil {
		return err
	}
	if err := writeUsersFile(usersDBPath, users, nextUserID); err != nil {
		if loadedUsers, loadedNames, loadedNextID, loadErr := readUsersFile(usersDBPath); loadErr == nil {
			user_map = loadedUsers
			id_map = loadedNames
			nextUserIDValue = loadedNextID
		}
		return err
	}
	user_map = users
	id_map = names
	nextUserIDValue = nextUserID
	return nil
}

func updateUser(id int, mutate func(*User) error) error {
	return withUserDataWrite(func(users map[int]User, names map[string]int, nextUserID *int) error {
		user, ok := users[id]
		if !ok {
			return fmt.Errorf("user with ID %d doesn't exist", id)
		}
		if err := mutate(&user); err != nil {
			return err
		}
		users[id] = user
		return nil
	})
}
