package main

import (
	"context"
	"fmt"
)

var ErrTaskNotFound = fmt.Errorf("task not found")

func (s *Store) ListTasks(ctx context.Context, userID int, date string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, date, complete
		FROM todos
		WHERE user_id = ? AND substr(date, 1, 10) = ?
		ORDER BY date, id
	`, userID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]Task, 0)
	for rows.Next() {
		var task Task
		var storedDate string
		if err := rows.Scan(&task.ID, &task.Name, &storedDate, &task.Complete); err != nil {
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
		INSERT INTO todos (user_id, name, date, complete)
		VALUES (?, ?, ?, ?)
	`, userID, name, date, complete)

	return err
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
	return nil
}
