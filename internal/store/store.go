package store

import (
	"context"
	"database/sql"
	"io/fs"

	"github.com/pressly/goose/v3"
	"modernc.org/sqlite"

	"loom/internal/store/migrate"
)

var pragmas = []string{
	"PRAGMA journal_mode = WAL",
	"PRAGMA foreign_keys = ON",
	"PRAGMA busy_timeout = 5000",
	"PRAGMA synchronous = NORMAL",
}

func init() {
	// The three non-WAL pragmas are per-connection; journal_mode persists in
	// the file header but is re-asserted for clarity (ADR-001 §3.3). The hook
	// runs on every physical connection, so a recycled pooled connection never
	// silently loses foreign_keys enforcement.
	sqlite.RegisterConnectionHook(func(conn sqlite.ExecQuerierContext, _ string) error {
		for _, q := range pragmas {
			if _, err := conn.ExecContext(context.Background(), q, nil); err != nil {
				return err
			}
		}
		return nil
	})
}

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := migrateUp(db, migrate.FS); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrateUp(db *sql.DB, fsys fs.FS) error {
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, fsys, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return err
	}
	_, err = provider.Up(context.Background())
	return err
}
