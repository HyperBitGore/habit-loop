package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type GoalItem struct {
	ID        uint64 `json:"id"`
	Name      string `json:"name"`
	Position  int    `json:"position"`
	Completed bool   `json:"completed"`
}

type Goal struct {
	ID      uint64     `json:"id"`
	Name    string     `json:"name"`
	Status  string     `json:"status"`
	Percent int        `json:"percent"`
	Items   []GoalItem `json:"items"`
}

type goalRequest struct {
	ID     uint64   `json:"id"`
	Name   string   `json:"name"`
	Status string   `json:"status"`
	Items  []string `json:"items"`
}

func (s *Store) ListGoals(ctx context.Context, userID int) ([]Goal, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.id, g.name, g.status, i.id, i.name, i.position, i.completed
		FROM goals g
		LEFT JOIN goal_items i ON i.goal_id = g.id
		WHERE g.user_id = ?
		ORDER BY CASE g.status WHEN 'active' THEN 0 WHEN 'complete' THEN 1 ELSE 2 END,
		         g.id, i.position, i.id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var goals []Goal
	byID := make(map[uint64]int)
	for rows.Next() {
		var goalID, itemID uint64
		var name, status, itemName string
		var position int
		var completed bool
		if err := rows.Scan(&goalID, &name, &status, &itemID, &itemName, &position, &completed); err != nil {
			return nil, err
		}
		index, exists := byID[goalID]
		if !exists {
			goals = append(goals, Goal{ID: goalID, Name: name, Status: status})
			index = len(goals) - 1
			byID[goalID] = index
		}
		if itemID != 0 {
			goals[index].Items = append(goals[index].Items, GoalItem{ID: itemID, Name: itemName, Position: position, Completed: completed})
		}
	}
	for index := range goals {
		completed := 0
		for _, item := range goals[index].Items {
			if item.Completed {
				completed++
			}
		}
		if len(goals[index].Items) > 0 {
			goals[index].Percent = completed * 100 / len(goals[index].Items)
		}
	}
	return goals, rows.Err()
}

func (s *Store) SaveGoal(ctx context.Context, userID int, request goalRequest) error {
	name := strings.TrimSpace(request.Name)
	if name == "" || len(request.Items) == 0 || len(request.Items) > 100 {
		return errors.New("goal name and at least one todo are required")
	}
	status := request.Status
	if status == "" {
		status = "active"
	}
	if status != "active" && status != "inactive" {
		return errors.New("invalid goal status")
	}
	availableDate := time.Now().Format("2006-01-02")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var goalID int64
	if request.ID == 0 {
		result, err := tx.ExecContext(ctx, "INSERT INTO goals (user_id, name, status) VALUES (?, ?, ?)", userID, name, status)
		if err != nil {
			return err
		}
		goalID, err = result.LastInsertId()
		if err != nil {
			return err
		}
	} else {
		goalID = int64(request.ID)
		result, err := tx.ExecContext(ctx, "UPDATE goals SET name = ?, status = ? WHERE id = ? AND user_id = ?", name, status, request.ID, userID)
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count == 0 {
			return errors.New("goal not found")
		}
	}
	for position, item := range request.Items {
		item = strings.TrimSpace(item)
		if item == "" || len(item) > 200 {
			return errors.New("goal todo names must be non-empty and at most 200 characters")
		}
		if request.ID != 0 {
			result, err := tx.ExecContext(ctx, "UPDATE goal_items SET name = ? WHERE goal_id = ? AND position = ?", item, goalID, position)
			if err != nil {
				return err
			}
			if count, _ := result.RowsAffected(); count > 0 {
				continue
			}
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO goal_items (goal_id, name, position, available_date) VALUES (?, ?, ?, ?)", goalID, item, position, availableDate); err != nil {
			return err
		}
	}
	if request.ID != 0 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM goal_items WHERE goal_id = ? AND position >= ?", goalID, len(request.Items)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteGoal(ctx context.Context, userID int, goalID uint64) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM goals WHERE id = ? AND user_id = ?", goalID, userID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return errors.New("goal not found")
	}
	return nil
}

func (s *Store) SetGoalStatus(ctx context.Context, userID int, goalID uint64, status string) error {
	if status != "active" && status != "inactive" {
		return errors.New("invalid goal status")
	}
	result, err := s.db.ExecContext(ctx, "UPDATE goals SET status = ? WHERE id = ? AND user_id = ? AND status != 'complete'", status, goalID, userID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return errors.New("goal not found or complete")
	}
	return nil
}

func (s *Store) updateGoalItem(ctx context.Context, userID int, itemID uint64, complete bool) error {
	var goalID uint64
	if err := s.db.QueryRowContext(ctx, `
		SELECT g.id FROM goal_items i JOIN goals g ON g.id = i.goal_id
		WHERE i.id = ? AND g.user_id = ?
	`, itemID, userID).Scan(&goalID); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE goal_items SET completed = ? WHERE id = ?", complete, itemID); err != nil {
		return err
	}
	if complete {
		nextDate := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
		_, err := s.db.ExecContext(ctx, `
			UPDATE goal_items SET available_date = ?
			WHERE goal_id = ? AND completed = FALSE AND position = (
				SELECT MIN(position) FROM goal_items WHERE goal_id = ? AND completed = FALSE
			)
		`, nextDate, goalID, goalID)
		if err != nil {
			return err
		}
	}
	var remaining int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM goal_items WHERE goal_id = ? AND completed = FALSE", goalID).Scan(&remaining); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, "UPDATE goals SET status = CASE WHEN ? = 0 THEN 'complete' ELSE 'active' END WHERE id = ?", remaining, goalID)
	return err
}

func HandleGoals(w http.ResponseWriter, r *http.Request) {
	user := requestUser(w, r)
	if user == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		goals, err := appStore.ListGoals(r.Context(), user.ID)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "Unable to load goals")
			return
		}
		writeJSON(w, http.StatusOK, goals)
	case http.MethodPut:
		var request goalRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := appStore.SaveGoal(r.Context(), user.ID, request); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodPatch:
		var request struct {
			ID        uint64 `json:"id"`
			ItemID    uint64 `json:"item_id"`
			Status    string `json:"status"`
			Completed *bool  `json:"completed"`
		}
		if err := decodeJSON(w, r, &request); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		if request.ItemID != 0 && request.Completed != nil {
			if err := appStore.updateGoalItem(r.Context(), user.ID, request.ItemID, *request.Completed); err != nil {
				writeAPIError(w, http.StatusBadRequest, "Unable to update goal todo")
				return
			}
		} else if err := appStore.SetGoalStatus(r.Context(), user.ID, request.ID, request.Status); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		goalID, err := strconv.ParseUint(r.URL.Query().Get("id"), 10, 64)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "Invalid goal ID")
			return
		}
		if err := appStore.DeleteGoal(r.Context(), user.ID, goalID); err != nil {
			writeAPIError(w, http.StatusNotFound, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
