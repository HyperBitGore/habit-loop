package main

import (
	"context"
	"path/filepath"
	"testing"
)

func TestReorderPersistsTasksAndHabits(t *testing.T) {
	db, err := InitDB(filepath.Join(t.TempDir(), "reorder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	result, err := db.Exec(`
		INSERT INTO users (name, email, email_normalized, password_hash, role, email_verified)
		VALUES ('reorder-user', 'reorder@example.com', 'reorder@example.com', 'hash', 'user', TRUE)
	`)
	if err != nil {
		t.Fatal(err)
	}
	userID, _ := result.LastInsertId()
	store := NewStore(db)
	ctx := context.Background()

	for _, name := range []string{"first", "second"} {
		if _, err := db.Exec(
			"INSERT INTO todos (user_id, name, date, position) VALUES (?, ?, '2026-09-16', ?)",
			userID, name, 0,
		); err != nil {
			t.Fatal(err)
		}
	}
	var firstID, secondID uint64
	if err := db.QueryRow("SELECT id FROM todos WHERE name = 'first'").Scan(&firstID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT id FROM todos WHERE name = 'second'").Scan(&secondID); err != nil {
		t.Fatal(err)
	}
	if err := store.ReorderTasks(ctx, int(userID), "2026-09-16", []uint64{secondID, firstID}); err != nil {
		t.Fatal(err)
	}
	tasks, err := store.ListTasks(ctx, int(userID), "2026-09-16")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 || tasks[0].ID != secondID || tasks[1].ID != firstID {
		t.Fatalf("task order = %+v", tasks)
	}

	for _, name := range []string{"first habit", "second habit"} {
		if _, err := db.Exec("INSERT INTO habits (user_id, name) VALUES (?, ?)", userID, name); err != nil {
			t.Fatal(err)
		}
	}
	var firstHabitID, secondHabitID uint64
	if err := db.QueryRow("SELECT id FROM habits WHERE name = 'first habit'").Scan(&firstHabitID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT id FROM habits WHERE name = 'second habit'").Scan(&secondHabitID); err != nil {
		t.Fatal(err)
	}
	if err := store.ReorderHabits(ctx, int(userID), []uint64{secondHabitID, firstHabitID}); err != nil {
		t.Fatal(err)
	}
	habits, err := store.ListHabits(ctx, int(userID))
	if err != nil {
		t.Fatal(err)
	}
	if len(habits) != 2 || habits[0].ID != secondHabitID || habits[1].ID != firstHabitID {
		t.Fatalf("habit order = %+v", habits)
	}
}
