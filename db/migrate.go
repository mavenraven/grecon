package db

import (
	"database/sql"
	"fmt"
)

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS workstreams (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		worktree TEXT,
		playwright INTEGER NOT NULL DEFAULT 0,
		active INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS tmux_sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		workstream_id INTEGER NOT NULL REFERENCES workstreams(id),
		tmux_id TEXT NOT NULL UNIQUE,
		display_name TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS claude_sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		workstream_id INTEGER NOT NULL REFERENCES workstreams(id),
		session_id TEXT NOT NULL UNIQUE,
		display_name TEXT NOT NULL DEFAULT ''
	);`,

	`ALTER TABLE claude_sessions ADD COLUMN summary TEXT NOT NULL DEFAULT '';`,
}

func migrate(d *sql.DB) error {
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`)
	if err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	var current int
	row := d.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`)
	if err := row.Scan(&current); err != nil {
		return fmt.Errorf("read version: %w", err)
	}

	for i := current; i < len(migrations); i++ {
		tx, err := d.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES (?)`, i+1); err != nil {
			tx.Rollback()
			return fmt.Errorf("update version %d: %w", i+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", i+1, err)
		}
		fmt.Printf("db: applied migration %d\n", i+1)
	}

	return nil
}
