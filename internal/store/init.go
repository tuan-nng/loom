package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
)

// InitWorkspace initializes loom for a directory (ADR-001 §6): a workspace
// named after the directory, a default board "Board", and the five default
// columns — all in one transaction. It is idempotent keyed on root_path: an
// already-registered directory returns the existing workspace untouched.
func InitWorkspace(db *sql.DB, rootPath string) (Workspace, error) {
	rootPath = filepath.Clean(rootPath)

	if w, err := WorkspaceByRootPath(db, rootPath); err == nil {
		return w, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, err
	}

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return Workspace{}, err
	}
	defer tx.Rollback()

	w := Workspace{ID: NewID(), Name: filepath.Base(rootPath), RootPath: rootPath}
	if _, err := tx.Exec(
		"INSERT INTO workspaces (id, name, root_path) VALUES (?, ?, ?)",
		w.ID, w.Name, w.RootPath,
	); err != nil {
		return Workspace{}, err
	}
	if _, err := createBoard(tx, w.ID, "Board"); err != nil {
		return Workspace{}, err
	}
	if err := tx.Commit(); err != nil {
		return Workspace{}, err
	}
	return GetWorkspace(db, w.ID)
}
