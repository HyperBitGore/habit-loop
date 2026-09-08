package main

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"sync/atomic"
	"time"
)

type Habit struct {
	Name        string      `json:"name"`
	Completions []time.Time `json:"completions"`
	Skips       []time.Time `json:"skips"`
	ID          uint64      `json:"id"`
}

func requestUser(w http.ResponseWriter, r *http.Request) *User {
	cookie, err := r.Cookie("auth")
	if err != nil || cookie.Value == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil
	}
	token, err := strconv.ParseUint(cookie.Value, 10, 64)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil
	}
	user := GetUserFromToken(token)
	if user.ID == -1 {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil
	}
	return user
}

func parseHabitCompletions(value string) ([]time.Time, error) {
	if value == "" {
		return []time.Time{}, nil
	}
	var completions []time.Time
	if err := json.Unmarshal([]byte(value), &completions); err != nil {
		return nil, err
	}
	return completions, nil
}

func persistHabitUser(w http.ResponseWriter, user *User) bool {
	user_map[user.ID] = *user
	if err := WriteUsers(user_map); err != nil {
		http.Error(w, "Failed to save habits", http.StatusInternalServerError)
		return false
	}
	return true
}

func HandleGetHabits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	habits := user.Habits
	if habits == nil {
		habits = []Habit{}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(habits); err != nil {
		http.Error(w, "Failed to encode habits", http.StatusInternalServerError)
	}
}

func addHabit(habits *[]Habit, nextHabitID *uint64, name string, completions []time.Time) {
	habit := Habit{
		Name:        name,
		Completions: completions,
		Skips:       []time.Time{},
		ID:          atomic.AddUint64(nextHabitID, 1),
	}
	*habits = append(*habits, habit)
}

func HandleAddHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := r.Header.Get("X-Habit-Name")
	if name == "" {
		http.Error(w, "Habit name is required", http.StatusBadRequest)
		return
	}
	completions, err := parseHabitCompletions(r.Header.Get("X-Habit-Completions"))
	if err != nil {
		http.Error(w, "Invalid habit completions", http.StatusBadRequest)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	addHabit(&user.Habits, &user.NextHabitID, name, completions)
	persistHabitUser(w, user)
}

func deleteHabit(habits *[]Habit, id uint64) {
	idx := slices.IndexFunc(*habits, func(habit Habit) bool {
		return habit.ID == id
	})
	if idx != -1 {
		*habits = append((*habits)[:idx], (*habits)[idx+1:]...)
	}
}

func HandleDeleteHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseUint(r.Header.Get("X-Habit-ID"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid habit ID", http.StatusBadRequest)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	deleteHabit(&user.Habits, id)
	persistHabitUser(w, user)
}

func editHabit(habits *[]Habit, id uint64, name string, completions []time.Time) {
	idx := slices.IndexFunc(*habits, func(habit Habit) bool {
		return habit.ID == id
	})
	if idx != -1 {
		(*habits)[idx].Name = name
		(*habits)[idx].Completions = completions
	}
}

func HandleEditHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseUint(r.Header.Get("X-Habit-ID"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid habit ID", http.StatusBadRequest)
		return
	}
	name := r.Header.Get("X-Habit-Name")
	if name == "" {
		http.Error(w, "Habit name is required", http.StatusBadRequest)
		return
	}
	completions, err := parseHabitCompletions(r.Header.Get("X-Habit-Completions"))
	if err != nil {
		http.Error(w, "Invalid habit completions", http.StatusBadRequest)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	editHabit(&user.Habits, id, name, completions)
	persistHabitUser(w, user)
}

func addHabitCompletion(habits *[]Habit, id uint64, date time.Time) bool {
	idx := slices.IndexFunc(*habits, func(habit Habit) bool {
		return habit.ID == id
	})
	if idx == -1 {
		return false
	}
	dateValue := date.Format("2006-01-02")
	for _, completion := range (*habits)[idx].Completions {
		if completion.Format("2006-01-02") == dateValue {
			return true
		}
	}
	(*habits)[idx].Skips = slices.DeleteFunc(
		(*habits)[idx].Skips,
		func(skip time.Time) bool {
			return skip.Format("2006-01-02") == dateValue
		},
	)
	(*habits)[idx].Completions = append((*habits)[idx].Completions, date)
	return true
}

func HandleCompleteHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var completion struct {
		HabitID uint64 `json:"habit_id"`
		Date    string `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&completion); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	date, err := time.Parse("2006-01-02", completion.Date)
	if err != nil {
		http.Error(w, "Invalid completion date", http.StatusBadRequest)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	if !addHabitCompletion(&user.Habits, completion.HabitID, date) {
		http.Error(w, "Habit not found", http.StatusNotFound)
		return
	}
	persistHabitUser(w, user)
}

func removeHabitCompletion(habits *[]Habit, id uint64, date time.Time) bool {
	idx := slices.IndexFunc(*habits, func(habit Habit) bool {
		return habit.ID == id
	})
	if idx == -1 {
		return false
	}
	dateValue := date.Format("2006-01-02")
	(*habits)[idx].Completions = slices.DeleteFunc(
		(*habits)[idx].Completions,
		func(completion time.Time) bool {
			return completion.Format("2006-01-02") == dateValue
		},
	)
	return true
}

func HandleUncompleteHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var completion struct {
		HabitID uint64 `json:"habit_id"`
		Date    string `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&completion); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	date, err := time.Parse("2006-01-02", completion.Date)
	if err != nil {
		http.Error(w, "Invalid completion date", http.StatusBadRequest)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	if !removeHabitCompletion(&user.Habits, completion.HabitID, date) {
		http.Error(w, "Habit not found", http.StatusNotFound)
		return
	}
	persistHabitUser(w, user)
}

func addHabitSkip(habits *[]Habit, id uint64, date time.Time) bool {
	idx := slices.IndexFunc(*habits, func(habit Habit) bool {
		return habit.ID == id
	})
	if idx == -1 {
		return false
	}
	dateValue := date.Format("2006-01-02")
	for _, skip := range (*habits)[idx].Skips {
		if skip.Format("2006-01-02") == dateValue {
			return true
		}
	}
	(*habits)[idx].Completions = slices.DeleteFunc(
		(*habits)[idx].Completions,
		func(completion time.Time) bool {
			return completion.Format("2006-01-02") == dateValue
		},
	)
	(*habits)[idx].Skips = append((*habits)[idx].Skips, date)
	return true
}

func HandleSkipHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var skip struct {
		HabitID uint64 `json:"habit_id"`
		Date    string `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&skip); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	date, err := time.Parse("2006-01-02", skip.Date)
	if err != nil {
		http.Error(w, "Invalid skip date", http.StatusBadRequest)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	if !addHabitSkip(&user.Habits, skip.HabitID, date) {
		http.Error(w, "Habit not found", http.StatusNotFound)
		return
	}
	persistHabitUser(w, user)
}

func removeHabitSkip(habits *[]Habit, id uint64, date time.Time) bool {
	idx := slices.IndexFunc(*habits, func(habit Habit) bool {
		return habit.ID == id
	})
	if idx == -1 {
		return false
	}
	dateValue := date.Format("2006-01-02")
	(*habits)[idx].Skips = slices.DeleteFunc(
		(*habits)[idx].Skips,
		func(skip time.Time) bool {
			return skip.Format("2006-01-02") == dateValue
		},
	)
	return true
}

func HandleUnskipHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var skip struct {
		HabitID uint64 `json:"habit_id"`
		Date    string `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&skip); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	date, err := time.Parse("2006-01-02", skip.Date)
	if err != nil {
		http.Error(w, "Invalid skip date", http.StatusBadRequest)
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	if !removeHabitSkip(&user.Habits, skip.HabitID, date) {
		http.Error(w, "Habit not found", http.StatusNotFound)
		return
	}
	persistHabitUser(w, user)
}
