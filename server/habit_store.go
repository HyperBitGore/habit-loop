package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (s *Store) ListHabits(ctx context.Context, userID int) ([]Habit, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT h.id, h.name, h.interval, h.days_mode, h.start_date, h.days_of_week_id,
		       COALESCE(d.sunday, FALSE), COALESCE(d.monday, FALSE),
		       COALESCE(d.tuesday, FALSE), COALESCE(d.wednesday, FALSE),
		       COALESCE(d.thursday, FALSE), COALESCE(d.friday, FALSE),
		       COALESCE(d.saturday, FALSE), 'completion', c.date
		FROM habits h
		LEFT JOIN days_of_week d ON d.id = h.days_of_week_id
		LEFT JOIN completions c ON c.habit_id = h.id
		WHERE h.user_id = ?
		UNION ALL
		SELECT h.id, h.name, h.interval, h.days_mode, h.start_date, h.days_of_week_id,
		       COALESCE(d.sunday, FALSE), COALESCE(d.monday, FALSE),
		       COALESCE(d.tuesday, FALSE), COALESCE(d.wednesday, FALSE),
		       COALESCE(d.thursday, FALSE), COALESCE(d.friday, FALSE),
		       COALESCE(d.saturday, FALSE), 'skip', s.date
		FROM habits h
		LEFT JOIN days_of_week d ON d.id = h.days_of_week_id
		LEFT JOIN skips s ON s.habit_id = h.id
		WHERE h.user_id = ?
		ORDER BY 2, 1, 15
	`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	habitsByID := make(map[uint64]*Habit)
	order := make([]uint64, 0)
	for rows.Next() {
		var id uint64
		var name, kind string
		var interval int
		var daysMode bool
		var startDate string
		var daysOfWeekID sql.NullInt64
		var daysOfWeek DaysOfWeek
		var date sql.NullString
		if err := rows.Scan(
			&id,
			&name,
			&interval,
			&daysMode,
			&startDate,
			&daysOfWeekID,
			&daysOfWeek.Sunday,
			&daysOfWeek.Monday,
			&daysOfWeek.Tuesday,
			&daysOfWeek.Wednesday,
			&daysOfWeek.Thursday,
			&daysOfWeek.Friday,
			&daysOfWeek.Saturday,
			&kind,
			&date,
		); err != nil {
			return nil, err
		}
		habit := habitsByID[id]
		if habit == nil {
			habit = &Habit{
				ID:          id,
				Name:        name,
				Interval:    interval,
				DaysMode:    daysMode,
				StartDate:   startDate,
				DaysOfWeek:  daysOfWeek,
				Completions: []time.Time{},
				Skips:       []time.Time{},
			}
			if daysOfWeekID.Valid {
				id := uint64(daysOfWeekID.Int64)
				habit.DaysOfWeekID = &id
			}
			habitsByID[id] = habit
			order = append(order, id)
		}
		if !date.Valid {
			continue
		}
		parsed, err := parseTaskDate(date.String)
		if err != nil {
			return nil, err
		}
		if kind == "completion" {
			habit.Completions = append(habit.Completions, parsed)
		} else {
			habit.Skips = append(habit.Skips, parsed)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	habits := make([]Habit, 0, len(order))
	for _, id := range order {
		habits = append(habits, *habitsByID[id])
	}
	return habits, nil
}

func (s *Store) AddHabit(ctx context.Context, userID int, daysOfWeek DaysOfWeek, name string, interval int, daysMode bool, startDate string, completions []time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	daysOfWeekID, err := insertDaysOfWeek(tx, daysOfWeek)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO habits (user_id, name, interval, days_mode, start_date, days_of_week_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, userID, name, interval, daysMode, startDate, daysOfWeekID)
	if err != nil {
		return err
	}
	habitID, err := result.LastInsertId()
	if err != nil {
		return err
	}
	for _, completion := range completions {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO completions (habit_id, date) VALUES (?, ?)
			ON CONFLICT(habit_id, date) DO NOTHING
		`, habitID, completion.Format("2006-01-02")); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func insertDaysOfWeek(tx *sql.Tx, days DaysOfWeek) (int64, error) {
	result, err := tx.Exec(`
		INSERT INTO days_of_week (
			sunday, monday, tuesday, wednesday, thursday, friday, saturday
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, days.Sunday, days.Monday, days.Tuesday, days.Wednesday, days.Thursday, days.Friday, days.Saturday)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func updateDaysOfWeek(tx *sql.Tx, id int64, days DaysOfWeek) error {
	_, err := tx.Exec(`
		UPDATE days_of_week
		SET sunday = ?, monday = ?, tuesday = ?, wednesday = ?,
		    thursday = ?, friday = ?, saturday = ?
		WHERE id = ?
	`, days.Sunday, days.Monday, days.Tuesday, days.Wednesday, days.Thursday, days.Friday, days.Saturday, id)
	return err
}

func (s *Store) DeleteHabit(ctx context.Context, userID int, habitID uint64) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM habits
		WHERE id = ? AND user_id = ?
	`, habitID, userID)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errHabitNotFound
	}
	return nil
}

func (s *Store) EditHabit(
	ctx context.Context,
	userID int,
	habitID uint64,
	name string,
	completions []time.Time,
	interval *int,
	daysMode *bool,
	daysOfWeek *DaysOfWeek,
	startDate *string,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var (
		storedInterval     int
		storedDaysMode     bool
		storedStartDate    string
		storedDaysOfWeekID sql.NullInt64
		storedDaysOfWeek   DaysOfWeek
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT h.interval, h.days_mode, h.start_date, h.days_of_week_id,
		       COALESCE(d.sunday, FALSE), COALESCE(d.monday, FALSE),
		       COALESCE(d.tuesday, FALSE), COALESCE(d.wednesday, FALSE),
		       COALESCE(d.thursday, FALSE), COALESCE(d.friday, FALSE),
		       COALESCE(d.saturday, FALSE)
		FROM habits h
		LEFT JOIN days_of_week d ON d.id = h.days_of_week_id
		WHERE h.id = ? AND h.user_id = ?
	`, habitID, userID).Scan(
		&storedInterval,
		&storedDaysMode,
		&storedStartDate,
		&storedDaysOfWeekID,
		&storedDaysOfWeek.Sunday,
		&storedDaysOfWeek.Monday,
		&storedDaysOfWeek.Tuesday,
		&storedDaysOfWeek.Wednesday,
		&storedDaysOfWeek.Thursday,
		&storedDaysOfWeek.Friday,
		&storedDaysOfWeek.Saturday,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errHabitNotFound
		}
		return err
	}

	resolvedInterval := storedInterval
	if interval != nil {
		resolvedInterval = *interval
	}
	resolvedDaysMode := storedDaysMode
	if daysMode != nil {
		resolvedDaysMode = *daysMode
	}
	resolvedDaysOfWeek := storedDaysOfWeek
	if daysOfWeek != nil {
		resolvedDaysOfWeek = *daysOfWeek
	}
	resolvedStartDate := storedStartDate
	if startDate != nil {
		resolvedStartDate = *startDate
	}
	if err := validateHabitSchedule(resolvedDaysMode, resolvedDaysOfWeek); err != nil {
		return err
	}

	var daysOfWeekID int64
	if storedDaysOfWeekID.Valid {
		daysOfWeekID = storedDaysOfWeekID.Int64
		if err := updateDaysOfWeek(tx, daysOfWeekID, resolvedDaysOfWeek); err != nil {
			return err
		}
	} else {
		daysOfWeekID, err = insertDaysOfWeek(tx, resolvedDaysOfWeek)
		if err != nil {
			return err
		}
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE habits
		SET name = ?, interval = ?, days_mode = ?, start_date = ?, days_of_week_id = ?
		WHERE id = ? AND user_id = ?
	`, name, resolvedInterval, resolvedDaysMode, resolvedStartDate, daysOfWeekID, habitID, userID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errHabitNotFound
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM completions
		WHERE habit_id = ?
	`, habitID); err != nil {
		return err
	}
	for _, completion := range completions {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO completions (habit_id, date)
			VALUES (?, ?)
			ON CONFLICT(habit_id, date) DO NOTHING
		`, habitID, completion.Format("2006-01-02")); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) SetHabitDate(
	ctx context.Context,
	userID int,
	habitID uint64,
	date string,
	insertTable string,
	deleteTable string,
) error {
	if !validHabitDateTable(insertTable) || !validHabitDateTable(deleteTable) {
		return fmt.Errorf("invalid habit date table")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM habits WHERE id = ? AND user_id = ?)
	`, habitID, userID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return errHabitNotFound
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM %s
		WHERE habit_id = ? AND date = ?
	`, deleteTable), habitID, date); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (habit_id, date)
		VALUES (?, ?)
		ON CONFLICT(habit_id, date) DO NOTHING
	`, insertTable), habitID, date); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) RemoveHabitDate(
	ctx context.Context,
	userID int,
	habitID uint64,
	date string,
	table string,
) error {
	if !validHabitDateTable(table) {
		return fmt.Errorf("invalid habit date table")
	}

	result, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM %s
		WHERE habit_id IN (SELECT id FROM habits WHERE id = ? AND user_id = ?)
		  AND date = ?
	`, table), habitID, userID, date)
	if err != nil {
		return err
	}
	if _, err := result.RowsAffected(); err != nil {
		return err
	}
	return nil
}

func validHabitDateTable(table string) bool {
	return table == "completions" || table == "skips"
}
