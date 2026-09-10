package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func habitRequestForUser(t *testing.T, method, target string, userID int) *http.Request {
	t.Helper()
	user, err := getUserByID(context.Background(), database, userID)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, target, nil)
	return request.WithContext(context.WithValue(request.Context(), userContextKey, user))
}

func TestHabitHandlersPersistAndReturnSchedule(t *testing.T) {
	setupTestApplication(t)
	userID := insertTestUser(t, "alice", "alice@example.com", "user")

	addRequest := habitRequestForUser(t, http.MethodPut, "/api/add_habit", userID)
	addRequest.Header.Set("X-Habit-Name", "Exercise")
	addRequest.Header.Set("X-Habit-Completions", "[]")
	addRequest.Header.Set("X-Habit-Interval", "3")
	addRequest.Header.Set("X-Habit-Days-Mode", "true")
	addRequest.Header.Set("X-Habit-Start-Date", "2026-09-10")
	addRequest.Header.Set(
		"X-Habit-Days-Of-Week",
		`{"monday":true,"wednesday":true,"friday":true}`,
	)
	addResponse := httptest.NewRecorder()
	HandleAddHabit(addResponse, addRequest)
	if addResponse.Code != http.StatusCreated {
		t.Fatalf("add status = %d body=%s", addResponse.Code, addResponse.Body.String())
	}

	getRequest := habitRequestForUser(t, http.MethodGet, "/api/get_habits", userID)
	getResponse := httptest.NewRecorder()
	HandleGetHabits(getResponse, getRequest)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", getResponse.Code, getResponse.Body.String())
	}
	var habits []Habit
	if err := json.Unmarshal(getResponse.Body.Bytes(), &habits); err != nil {
		t.Fatal(err)
	}
	if len(habits) != 1 {
		t.Fatalf("habit count = %d, want 1", len(habits))
	}
	habit := habits[0]
	if habit.Interval != 3 || !habit.DaysMode || habit.DaysOfWeekID == nil ||
		habit.StartDate != "2026-09-10" {
		t.Fatalf("unexpected schedule: %+v", habit)
	}
	if !habit.DaysOfWeek.Monday || !habit.DaysOfWeek.Wednesday || !habit.DaysOfWeek.Friday {
		t.Fatalf("unexpected weekdays: %+v", habit.DaysOfWeek)
	}
	if habit.DaysOfWeek.Sunday || habit.DaysOfWeek.Tuesday ||
		habit.DaysOfWeek.Thursday || habit.DaysOfWeek.Saturday {
		t.Fatalf("unexpected enabled weekdays: %+v", habit.DaysOfWeek)
	}

	editRequest := habitRequestForUser(t, http.MethodPut, "/api/edit_habit", userID)
	editRequest.Header.Set("X-Habit-ID", strconv.FormatUint(habit.ID, 10))
	editRequest.Header.Set("X-Habit-Name", "Morning exercise")
	editRequest.Header.Set("X-Habit-Completions", "[]")
	editResponse := httptest.NewRecorder()
	HandleEditHabit(editResponse, editRequest)
	if editResponse.Code != http.StatusNoContent {
		t.Fatalf("edit status = %d body=%s", editResponse.Code, editResponse.Body.String())
	}

	habits, err := getUserHabits(database, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(habits) != 1 || habits[0].Name != "Morning exercise" ||
		habits[0].Interval != 3 || !habits[0].DaysMode ||
		habits[0].StartDate != "2026-09-10" ||
		!habits[0].DaysOfWeek.Monday || !habits[0].DaysOfWeek.Wednesday ||
		!habits[0].DaysOfWeek.Friday {
		t.Fatalf("legacy edit did not preserve schedule: %+v", habits)
	}
}

func TestHabitDaysModeRequiresAnEnabledWeekday(t *testing.T) {
	setupTestApplication(t)
	userID := insertTestUser(t, "alice", "alice@example.com", "user")

	request := habitRequestForUser(t, http.MethodPut, "/api/add_habit", userID)
	request.Header.Set("X-Habit-Name", "Exercise")
	request.Header.Set("X-Habit-Completions", "[]")
	request.Header.Set("X-Habit-Days-Mode", "true")
	response := httptest.NewRecorder()
	HandleAddHabit(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	var habits int
	if err := database.QueryRow("SELECT COUNT(*) FROM habits").Scan(&habits); err != nil {
		t.Fatal(err)
	}
	if habits != 0 {
		t.Fatalf("invalid schedule created %d habits", habits)
	}
}
