package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var errHabitNotFound = errors.New("habit not found")
var errInvalidHabitSchedule = errors.New("At least one day of the week must be enabled")

type DaysOfWeek struct {
	Sunday    bool `json:"sunday"`
	Monday    bool `json:"monday"`
	Tuesday   bool `json:"tuesday"`
	Wednesday bool `json:"wednesday"`
	Thursday  bool `json:"thursday"`
	Friday    bool `json:"friday"`
	Saturday  bool `json:"saturday"`
}

type Habit struct {
	Name         string      `json:"name"`
	Completions  []time.Time `json:"completions"`
	Skips        []time.Time `json:"skips"`
	DaysOfWeek   DaysOfWeek  `json:"days_of_week"`
	DaysOfWeekID *uint64     `json:"days_of_week_id"`
	ID           uint64      `json:"id"`
	Interval     int         `json:"interval"`
	DaysMode     bool        `json:"days_mode"`
	StartDate    string      `json:"start_date"`
	Position     int         `json:"-"`
	MetricName   string      `json:"metric_name,omitempty"`
	MetricGoal   float64     `json:"metric_goal"`
	MetricValue  float64     `json:"metric_value"`
}

type reorderRequest struct {
	Type string   `json:"type"`
	Date string   `json:"date"`
	IDs  []uint64 `json:"ids"`
}

func HandleReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var request reorderRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(request.IDs) == 0 {
		writeAPIError(w, http.StatusBadRequest, "At least one item is required")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	var err error
	switch request.Type {
	case "habits":
		err = appStore.ReorderHabits(r.Context(), user.ID, request.IDs)
	case "todos":
		if _, parseErr := time.Parse("2006-01-02", request.Date); parseErr != nil {
			writeAPIError(w, http.StatusBadRequest, "Invalid task date")
			return
		}
		err = appStore.ReorderTasks(r.Context(), user.ID, request.Date, request.IDs)
	default:
		writeAPIError(w, http.StatusBadRequest, "Invalid reorder type")
		return
	}
	if err != nil {
		if errors.Is(err, errReorderItems) {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "Unable to reorder items")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (days DaysOfWeek) anyEnabled() bool {
	return days.Sunday || days.Monday || days.Tuesday || days.Wednesday ||
		days.Thursday || days.Friday || days.Saturday
}

func parseHabitSchedule(r *http.Request) (int, bool, DaysOfWeek, error) {
	interval := 1
	if value := strings.TrimSpace(r.Header.Get("X-Habit-Interval")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			return 0, false, DaysOfWeek{}, errors.New("Habit interval must be at least 1")
		}
		interval = parsed
	}

	daysMode := false
	if value := strings.TrimSpace(r.Header.Get("X-Habit-Days-Mode")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return 0, false, DaysOfWeek{}, errors.New("Invalid habit days mode")
		}
		daysMode = parsed
	}

	var daysOfWeek DaysOfWeek
	if value := strings.TrimSpace(r.Header.Get("X-Habit-Days-Of-Week")); value != "" {
		if err := json.Unmarshal([]byte(value), &daysOfWeek); err != nil {
			return 0, false, DaysOfWeek{}, errors.New("Invalid habit days of week")
		}
	}
	return interval, daysMode, daysOfWeek, nil
}

func validateHabitSchedule(daysMode bool, daysOfWeek DaysOfWeek) error {
	if daysMode && !daysOfWeek.anyEnabled() {
		return errInvalidHabitSchedule
	}
	return nil
}

func parseHabitStartDate(r *http.Request) (string, error) {
	value := strings.TrimSpace(r.Header.Get("X-Habit-Start-Date"))
	if value == "" {
		return time.Now().UTC().Format("2006-01-02"), nil
	}
	date, err := time.Parse("2006-01-02", value)
	if err != nil {
		return "", errors.New("Invalid habit start date")
	}
	return date.Format("2006-01-02"), nil
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

func writeHabitMutationError(w http.ResponseWriter, err error) {
	if errors.Is(err, errHabitNotFound) {
		writeAPIError(w, http.StatusNotFound, "Habit not found")
		return
	}
	if errors.Is(err, errInvalidHabitSchedule) {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeAPIError(w, http.StatusInternalServerError, "Unable to save habit")
}

func HandleGetHabits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	habits, err := appStore.ListHabits(r.Context(), user.ID, r.URL.Query().Get("date"))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to load habits")
		return
	}
	writeJSON(w, http.StatusOK, habits)
}

func HandleAddHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	name, err := validateItemName(r.Header.Get("X-Habit-Name"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	completions, err := parseHabitCompletions(r.Header.Get("X-Habit-Completions"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid habit completions")
		return
	}
	interval, daysMode, daysOfWeek, err := parseHabitSchedule(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateHabitSchedule(daysMode, daysOfWeek); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	startDate, err := parseHabitStartDate(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	err = appStore.AddHabit(r.Context(), user.ID, daysOfWeek, name, interval, daysMode, startDate, completions)
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func HandleDeleteHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	id, err := strconv.ParseUint(r.Header.Get("X-Habit-ID"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid habit ID")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	err = appStore.DeleteHabit(r.Context(), user.ID, id)
	if err != nil {
		writeHabitMutationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func HandleEditHabit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	id, err := strconv.ParseUint(r.Header.Get("X-Habit-ID"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid habit ID")
		return
	}
	name, err := validateItemName(r.Header.Get("X-Habit-Name"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	var completions []time.Time
	if value := r.Header.Get("X-Habit-Completions"); value != "" {
		completions, err = parseHabitCompletions(value)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "Invalid habit completions")
			return
		}
	}
	interval, daysMode, daysOfWeek, err := parseHabitSchedule(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	var intervalOverride *int
	if r.Header.Get("X-Habit-Interval") != "" {
		intervalOverride = &interval
	}
	var daysModeOverride *bool
	if r.Header.Get("X-Habit-Days-Mode") != "" {
		daysModeOverride = &daysMode
	}
	var daysOfWeekOverride *DaysOfWeek
	if r.Header.Get("X-Habit-Days-Of-Week") != "" {
		daysOfWeekOverride = &daysOfWeek
	}
	var startDateOverride *string
	if r.Header.Get("X-Habit-Start-Date") != "" {
		startDate, err := parseHabitStartDate(r)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		startDateOverride = &startDate
	}
	if err := appStore.EditHabit(
		r.Context(),
		user.ID,
		id,
		name,
		completions,
		intervalOverride,
		daysModeOverride,
		daysOfWeekOverride,
		startDateOverride,
	); err != nil {
		writeHabitMutationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type habitDateRequest struct {
	HabitID uint64 `json:"habit_id"`
	Date    string `json:"date"`
}

func decodeHabitDate(w http.ResponseWriter, r *http.Request) (habitDateRequest, string, bool) {
	var request habitDateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return request, "", false
	}
	date, err := time.Parse("2006-01-02", request.Date)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid habit date")
		return request, "", false
	}
	return request, date.Format("2006-01-02"), true
}

func mutateHabitDate(w http.ResponseWriter, r *http.Request, insertTable string, deleteTable string) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	request, date, ok := decodeHabitDate(w, r)
	if !ok {
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	if err := appStore.SetHabitDate(
		r.Context(),
		user.ID,
		request.HabitID,
		date,
		insertTable,
		deleteTable,
	); err != nil {
		writeHabitMutationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func removeHabitDate(w http.ResponseWriter, r *http.Request, table string) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	request, date, ok := decodeHabitDate(w, r)
	if !ok {
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	if err := appStore.RemoveHabitDate(
		r.Context(),
		user.ID,
		request.HabitID,
		date,
		table,
	); err != nil {
		writeHabitMutationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func HandleCompleteHabit(w http.ResponseWriter, r *http.Request) {
	mutateHabitDate(w, r, "completions", "skips")
}

func HandleUncompleteHabit(w http.ResponseWriter, r *http.Request) {
	removeHabitDate(w, r, "completions")
}

func HandleSkipHabit(w http.ResponseWriter, r *http.Request) {
	mutateHabitDate(w, r, "skips", "completions")
}

func HandleUnskipHabit(w http.ResponseWriter, r *http.Request) {
	removeHabitDate(w, r, "skips")
}
