package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

type metricRequest struct {
	HabitID uint64   `json:"habit_id"`
	Name    string   `json:"name"`
	Goal    float64  `json:"goal"`
	Date    string   `json:"date"`
	Value   *float64 `json:"value"`
}

func (s *Store) loadHabitMetrics(ctx context.Context, userID int, date string, habits []Habit) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.habit_id, m.name, m.goal_amount, COALESCE(v.amount, 0)
		FROM habit_metrics m
		JOIN habits h ON h.id = m.habit_id AND h.user_id = ?
		LEFT JOIN habit_metric_values v ON v.metric_id = m.id AND v.date = ?
	`, userID, date)
	if err != nil {
		return err
	}
	defer rows.Close()
	byID := make(map[uint64]*Habit, len(habits))
	for index := range habits {
		byID[habits[index].ID] = &habits[index]
	}
	for rows.Next() {
		var habitID uint64
		var name string
		var goal, value float64
		if err := rows.Scan(&habitID, &name, &goal, &value); err != nil {
			return err
		}
		if habit := byID[habitID]; habit != nil {
			habit.MetricName, habit.MetricGoal, habit.MetricValue = name, goal, value
		}
	}
	return rows.Err()
}

func (s *Store) SaveMetric(ctx context.Context, userID int, request metricRequest) error {
	name := strings.TrimSpace(request.Name)
	if name == "" {
		_, err := s.db.ExecContext(ctx, `
			DELETE FROM habit_metrics
			WHERE habit_id IN (SELECT id FROM habits WHERE id = ? AND user_id = ?)
		`, request.HabitID, userID)
		return err
	}
	if len(name) > 100 || request.Goal <= 0 {
		return errors.New("metric name and goal are required")
	}
	var date time.Time
	if request.Value != nil {
		var err error
		date, err = time.Parse("2006-01-02", request.Date)
		if err != nil || *request.Value < 0 {
			return errors.New("invalid metric value")
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var metricExists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM habit_metrics m
			JOIN habits h ON h.id = m.habit_id
			WHERE m.habit_id = ? AND h.user_id = ?
		)
	`, request.HabitID, userID).Scan(&metricExists); err != nil {
		return err
	}
	var metricID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO habit_metrics (habit_id, name, goal_amount)
		SELECT ?, ?, ? WHERE EXISTS (SELECT 1 FROM habits WHERE id = ? AND user_id = ?)
		ON CONFLICT(habit_id) DO UPDATE SET name = excluded.name, goal_amount = excluded.goal_amount
		RETURNING id
	`, request.HabitID, name, request.Goal, request.HabitID, userID).Scan(&metricID)
	if err != nil {
		return errors.New("habit not found")
	}
	if !metricExists {
		if _, err := tx.ExecContext(ctx, "DELETE FROM completions WHERE habit_id = ?", request.HabitID); err != nil {
			return err
		}
	}
	if request.Value != nil {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO habit_metric_values (metric_id, date, amount) VALUES (?, ?, ?)
			ON CONFLICT(metric_id, date) DO UPDATE SET amount = excluded.amount
		`, metricID, date.Format("2006-01-02"), *request.Value); err != nil {
			return err
		}
		if *request.Value >= request.Goal {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO completions (habit_id, date)
				VALUES (?, ?)
				ON CONFLICT(habit_id, date) DO NOTHING
			`, request.HabitID, date.Format("2006-01-02"))
		} else {
			_, err = tx.ExecContext(ctx, `
				DELETE FROM completions
				WHERE habit_id = ? AND date = ?
			`, request.HabitID, date.Format("2006-01-02"))
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func HandleSaveMetric(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeAPIError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var request metricRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	user := requestUser(w, r)
	if user == nil {
		return
	}
	if err := appStore.SaveMetric(r.Context(), user.ID, request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
