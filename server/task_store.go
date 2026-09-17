package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrTaskNotFound = fmt.Errorf("task not found")
var errReorderItems = errors.New("items must contain each item exactly once")

func (s *Store) ListTasks(ctx context.Context, userID int, date string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, date, complete, position
		FROM todos
		WHERE user_id = ? AND substr(date, 1, 10) = ? AND goal_item_id IS NULL
		ORDER BY position, id
	`, userID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]Task, 0)
	for rows.Next() {
		var task Task
		var storedDate string
		if err := rows.Scan(&task.ID, &task.Name, &storedDate, &task.Complete, &task.Position); err != nil {
			return nil, err
		}
		task.Date, err = parseTaskDate(storedDate)
		if err != nil {
			return nil, fmt.Errorf("parse task %d date: %w", task.ID, err)
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *Store) AddTask(ctx context.Context, userID int, name string, date string, complete bool) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO todos (user_id, name, date, complete, position)
		VALUES (?, ?, ?, ?, COALESCE((SELECT MAX(position) + 1 FROM todos WHERE user_id = ? AND substr(date, 1, 10) = substr(?, 1, 10)), 0))
	`, userID, name, date, complete, userID, date)

	return err
}

func (s *Store) ReorderTasks(ctx context.Context, userID int, date string, ids []uint64) error {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM todos WHERE user_id = ? AND substr(date, 1, 10) = ?", userID, date)
	if err != nil {
		return err
	}
	defer rows.Close()
	expected := make(map[uint64]struct{})
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		expected[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(expected) != len(ids) {
		return errReorderItems
	}
	for _, id := range ids {
		if _, ok := expected[id]; !ok {
			return errReorderItems
		}
		delete(expected, id)
	}
	if len(expected) != 0 {
		return errReorderItems
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for position, id := range ids {
		if _, err := tx.ExecContext(ctx, "UPDATE todos SET position = ? WHERE id = ? AND user_id = ?", position, id, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteTask(ctx context.Context, userID int, taskID uint64) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM todos WHERE id = ? AND user_id = ?", taskID, userID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrTaskNotFound
	}
	return nil
}

func (s *Store) UpdateTask(ctx context.Context, userID int, taskID uint64, name string, date string, complete bool) error {
	var goalItemID uint64
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE(goal_item_id, 0) FROM todos WHERE id = ? AND user_id = ?", taskID, userID).Scan(&goalItemID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTaskNotFound
		}
		return err
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE todos SET name = ?, date = ?, complete = ?
		WHERE id = ? AND user_id = ?
	`, name, date, complete, taskID, userID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrTaskNotFound
	}
	if goalItemID != 0 {
		if err := s.updateGoalItem(ctx, userID, goalItemID, complete); err != nil {
			return err
		}
	}
	return nil
}
