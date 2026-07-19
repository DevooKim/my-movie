package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func migrate(ctx context.Context, database *sql.DB) error {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()

	if _, err := transaction.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return err
	}

	var count int
	if err := transaction.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = 1").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		migration, err := migrationFiles.ReadFile("migrations/001_initial.sql")
		if err != nil {
			return err
		}
		if _, err := transaction.ExecContext(ctx, string(migration)); err != nil {
			return fmt.Errorf("apply migration 1: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, "INSERT INTO schema_migrations(version, applied_at) VALUES(1, datetime('now'))"); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

func LatestMigrationVersion(ctx context.Context, database *sql.DB) (int, error) {
	var version int
	err := database.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version)
	return version, err
}
