package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestActiveGoalStaysOutOfTodayTodoList(t *testing.T) {
	db, err := InitDB(filepath.Join(t.TempDir(), "goals.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	result, err := db.Exec(`
		INSERT INTO users (name, email, email_normalized, password_hash, role, email_verified)
		VALUES ('goal-user', 'goal@example.com', 'goal@example.com', 'hash', 'user', TRUE)
	`)
	if err != nil {
		t.Fatal(err)
	}
	userID, _ := result.LastInsertId()
	store := NewStore(db)
	ctx := context.Background()
	if err := store.SaveGoal(ctx, int(userID), goalRequest{Name: "Ship it", Items: []string{"First step"}}); err != nil {
		t.Fatal(err)
	}

	tasks, err := store.ListTasks(ctx, int(userID), time.Now().Format("2006-01-02"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("goal tasks = %+v", tasks)
	}
}
