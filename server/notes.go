package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxNoteLength = 2000

type noteRequest struct {
	Type string `json:"type"`
	ID   uint64 `json:"id"`
	Date string `json:"date"`
	Body string `json:"body"`
}

type Note struct {
	Type string `json:"type"`
	ID   uint64 `json:"id"`
	Date string `json:"date"`
	Body string `json:"body"`
}

func (s *Store) SaveNote(ctx context.Context, userID int, note noteRequest) error {
	body := strings.TrimSpace(note.Body)
	if len(body) > maxNoteLength {
		return errors.New("note is too long")
	}
	date, err := time.Parse("2006-01-02", note.Date)
	if err != nil {
		return errors.New("invalid note date")
	}
	if body == "" {
		switch note.Type {
		case "todo":
			_, err = s.db.ExecContext(ctx, "DELETE FROM notes WHERE user_id = ? AND todo_id = ? AND date = ?", userID, note.ID, date.Format("2006-01-02"))
		case "habit":
			_, err = s.db.ExecContext(ctx, "DELETE FROM notes WHERE user_id = ? AND habit_id = ? AND date = ?", userID, note.ID, date.Format("2006-01-02"))
		default:
			return errors.New("invalid note type")
		}
		return err
	}
	switch note.Type {
	case "todo":
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO notes (user_id, todo_id, date, body)
			SELECT ?, id, ?, ? FROM todos WHERE id = ? AND user_id = ?
			ON CONFLICT(todo_id, date) DO UPDATE SET body = excluded.body
		`, userID, date.Format("2006-01-02"), body, note.ID, userID)
	case "habit":
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO notes (user_id, habit_id, date, body)
			SELECT ?, id, ?, ? FROM habits WHERE id = ? AND user_id = ?
			ON CONFLICT(habit_id, date) DO UPDATE SET body = excluded.body
		`, userID, date.Format("2006-01-02"), body, note.ID, userID)
	default:
		return errors.New("invalid note type")
	}
	return err
}

func (s *Store) GetNote(ctx context.Context, userID int, noteType string, id uint64, date string) (Note, error) {
	var note Note
	note.Type, note.ID, note.Date = noteType, id, date
	var err error
	if noteType == "todo" {
		err = s.db.QueryRowContext(ctx, "SELECT body FROM notes WHERE user_id = ? AND todo_id = ? AND date = ?", userID, id, date).Scan(&note.Body)
	} else if noteType == "habit" {
		err = s.db.QueryRowContext(ctx, "SELECT body FROM notes WHERE user_id = ? AND habit_id = ? AND date = ?", userID, id, date).Scan(&note.Body)
	} else {
		return Note{}, errors.New("invalid note type")
	}
	if err == sql.ErrNoRows {
		return note, nil
	}
	return note, err
}

func (s *Store) ListTodoHistory(ctx context.Context, userID int) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name, t.date, t.complete, t.position, COALESCE(n.body, '')
		FROM todos t
		LEFT JOIN notes n ON n.todo_id = t.id AND n.date = substr(t.date, 1, 10)
		WHERE t.user_id = ? AND t.goal_item_id IS NULL
		ORDER BY substr(t.date, 1, 10) DESC, t.position, t.id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []Task
	for rows.Next() {
		var task Task
		var storedDate string
		if err := rows.Scan(&task.ID, &task.Name, &storedDate, &task.Complete, &task.Position, &task.Note); err != nil {
			return nil, err
		}
		task.Date, err = parseTaskDate(storedDate)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func HandleSaveNote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var request noteRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	if err := appStore.SaveNote(r.Context(), user.ID, request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func HandleGetNote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	id, err := strconv.ParseUint(r.URL.Query().Get("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid item ID")
		return
	}
	date := r.URL.Query().Get("date")
	if _, err := time.Parse("2006-01-02", date); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Invalid note date")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	note, err := appStore.GetNote(r.Context(), user.ID, r.URL.Query().Get("type"), id, date)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to load note")
		return
	}
	writeJSON(w, http.StatusOK, note)
}

func HandleTodoHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	tasks, err := appStore.ListTodoHistory(r.Context(), user.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Unable to load todo history")
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}
