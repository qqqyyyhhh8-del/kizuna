package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

const defaultBusyTimeoutMS = 5000

func Open(path string) (*sql.DB, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("sqlite path is required")
	}
	sqlitevec.Auto()
	if !isMemoryPath(path) && !isSQLiteURI(path) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := configure(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func OpenInMemory() (*sql.DB, error) {
	return Open(":memory:")
}

func configure(db *sql.DB) error {
	pragmas := []string{
		fmt.Sprintf("PRAGMA busy_timeout = %d", defaultBusyTimeoutMS),
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
	}
	for _, statement := range pragmas {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}

	var version string
	if err := db.QueryRow("select vec_version()").Scan(&version); err != nil {
		return fmt.Errorf("sqlite-vec is unavailable: %w", err)
	}
	return nil
}

func isMemoryPath(path string) bool {
	path = strings.TrimSpace(path)
	return path == ":memory:" || strings.Contains(path, ":memory:")
}

func isSQLiteURI(path string) bool {
	return strings.HasPrefix(strings.TrimSpace(path), "file:")
}
