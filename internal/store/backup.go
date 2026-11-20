package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	sqlite "modernc.org/sqlite"
)

type onlineBackuper interface {
	NewBackup(string) (*sqlite.Backup, error)
}

// BackupDatabase uses SQLite's online backup API without migrating the source.
// The destination is a new file in the caller's private staging directory.
func BackupDatabase(ctx context.Context, source, dest string) error {
	sourceURI, err := sqliteFileURI(source, "mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	destURI, err := sqliteFileURI(dest, "mode=rwc")
	if err != nil {
		return err
	}
	db, err := sql.Open("sqlite", sourceURI)
	if err != nil {
		return err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open source database read-only: %w", err)
	}
	defer conn.Close()
	return conn.Raw(func(raw any) (err error) {
		backuper, ok := raw.(onlineBackuper)
		if !ok {
			return errors.New("SQLite driver does not support online backup")
		}
		snapshot, err := backuper.NewBackup(destURI)
		if err != nil {
			return fmt.Errorf("start SQLite backup: %w", err)
		}
		defer func() { err = errors.Join(err, snapshot.Finish()) }()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			more, err := snapshot.Step(128)
			if err != nil {
				return fmt.Errorf("copy SQLite backup pages: %w", err)
			}
			if !more {
				return nil
			}
		}
	})
}

// ValidateBackupDatabase verifies a restored snapshot without migrating or
// otherwise modifying it.
func ValidateBackupDatabase(ctx context.Context, path string) error {
	dsn, err := sqliteFileURI(path, "mode=ro")
	if err != nil {
		return err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open restored database: %w", err)
	}
	defer db.Close()
	var version, tables int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read snapshot schema version: %w", err)
	}
	if version < 1 || version > len(migrations) {
		return fmt.Errorf("snapshot schema version %d is not supported by this executable", version)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name IN
		('users', 'clients', 'settings', 'signing_keys')`).Scan(&tables); err != nil {
		return fmt.Errorf("read snapshot schema: %w", err)
	}
	if tables != 4 {
		return errors.New("snapshot is missing required XAuth tables")
	}
	var integrity string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check(1)").Scan(&integrity); err != nil {
		return fmt.Errorf("check snapshot integrity: %w", err)
	}
	if integrity != "ok" {
		return errors.New("snapshot failed SQLite integrity_check")
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("check snapshot references: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("snapshot failed SQLite foreign_key_check")
	}
	return rows.Err()
}

func sqliteFileURI(path, query string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // Windows drive paths are URI paths, not authorities.
	}
	return (&url.URL{Scheme: "file", Path: p, RawQuery: query}).String(), nil
}
