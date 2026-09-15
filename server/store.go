package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) PingContext(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) CleanupExpiredRecords(ctx context.Context) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, table := range []string{"sessions", "email_verifications", "password_resets"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE expires_at <= ?", now); err != nil {
			return fmt.Errorf("clean expired %s: %w", table, err)
		}
	}
	return tx.Commit()
}
