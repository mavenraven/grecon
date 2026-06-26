package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

var global *sql.DB

func Path() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".recon", "grecon.db")
}

func Open() (*sql.DB, error) {
	path := Path()
	if path == "" {
		return nil, fmt.Errorf("could not determine home directory")
	}
	os.MkdirAll(filepath.Dir(path), 0o755)

	d, err := sql.Open("sqlite", path+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := migrate(d); err != nil {
		d.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	global = d
	return d, nil
}

func Get() *sql.DB {
	return global
}
