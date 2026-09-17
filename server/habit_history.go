package main

import (
	"context"
	"net/http"
	"strconv"
)

type HabitSummaryEntry struct {
	Date      string `json:"date"`
	Completed bool   `json:"completed"`
	Skipped   bool   `json:"skipped"`
	Note      string `json:"note,omitempty"`
}

func (s *Store) ListHabitSummary(ctx context.Context, userID int, habitID uint64) ([]HabitSummaryEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT event_date, MAX(completed), MAX(skipped), note
		FROM (
			SELECT c.date AS event_date, 1 AS completed, 0 AS skipped,
			       COALESCE(n.body, '') AS note
			FROM completions c
			JOIN habits h ON h.id = c.habit_id AND h.user_id = ?
			LEFT JOIN notes n ON n.habit_id = c.habit_id AND n.date = c.date
			WHERE c.habit_id = ?
			UNION ALL
			SELECT s.date AS event_date, 0 AS completed, 1 AS skipped,
			       COALESCE(n.body, '') AS note
			FROM skips s
			JOIN habits h ON h.id = s.habit_id AND h.user_id = ?
			LEFT JOIN notes n ON n.habit_id = s.habit_id AND n.date = s.date
			WHERE s.habit_id = ?
		)
		GROUP BY event_date
		ORDER BY event_date DESC
	`, userID, habitID, userID, habitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []HabitSummaryEntry
	for rows.Next() {
		var entry HabitSummaryEntry
		var completed, skipped int
		if err := rows.Scan(&entry.Date, &completed, &skipped, &entry.Note); err != nil {
			return nil, err
		}
		entry.Completed = completed != 0
		entry.Skipped = skipped != 0
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func HandleHabitSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	habitID, err := strconv.ParseUint(r.URL.Query().Get("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid habit ID")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	entries, err := appStore.ListHabitSummary(r.Context(), user.ID, habitID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to load habit summary")
		return
	}
	writeJSON(w, http.StatusOK, entries)
}
