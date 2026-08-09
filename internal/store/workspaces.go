package store

import (
	"database/sql"
	"errors"
)

type Workspace struct {
	ID         string
	Name       string
	RootPath   string
	ArchivedAt *string
	CreatedAt  string
	UpdatedAt  string
}

func CreateWorkspace(db *sql.DB, name, rootPath string) (Workspace, error) {
	w := Workspace{ID: NewID(), Name: name, RootPath: rootPath}
	_, err := db.Exec(
		"INSERT INTO workspaces (id, name, root_path) VALUES (?, ?, ?)",
		w.ID, w.Name, w.RootPath,
	)
	if err != nil {
		return Workspace{}, err
	}
	return GetWorkspace(db, w.ID)
}

func ListWorkspaces(db *sql.DB) ([]Workspace, error) {
	rows, err := db.Query(
		"SELECT id, name, root_path, archived_at, created_at, updated_at FROM workspaces ORDER BY created_at",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ws []Workspace
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		ws = append(ws, w)
	}
	return ws, rows.Err()
}

func GetWorkspace(db *sql.DB, id string) (Workspace, error) {
	row := db.QueryRow(
		"SELECT id, name, root_path, archived_at, created_at, updated_at FROM workspaces WHERE id = ?",
		id,
	)
	return scanWorkspace(row)
}

func WorkspaceByRootPath(db *sql.DB, rootPath string) (Workspace, error) {
	row := db.QueryRow(
		"SELECT id, name, root_path, archived_at, created_at, updated_at FROM workspaces WHERE root_path = ?",
		rootPath,
	)
	return scanWorkspace(row)
}

func MostRecentWorkspace(db *sql.DB) (Workspace, error) {
	row := db.QueryRow(
		"SELECT id, name, root_path, archived_at, created_at, updated_at FROM workspaces ORDER BY created_at DESC LIMIT 1",
	)
	return scanWorkspace(row)
}

func DeleteWorkspace(db *sql.DB, id string) error {
	_, err := db.Exec("DELETE FROM workspaces WHERE id = ?", id)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanWorkspace(row rowScanner) (Workspace, error) {
	var w Workspace
	var archived sql.NullString
	err := row.Scan(&w.ID, &w.Name, &w.RootPath, &archived, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return Workspace{}, err
	}
	w.ArchivedAt = nullToPtr(archived)
	return w, nil
}

func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
