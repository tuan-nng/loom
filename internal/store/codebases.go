package store

import (
	"database/sql"
)

type Codebase struct {
	ID          string
	WorkspaceID string
	Path        string
	Label       *string
	CreatedAt   string
}

func CreateCodebase(db *sql.DB, workspaceID, path string) (Codebase, error) {
	c := Codebase{ID: NewID(), WorkspaceID: workspaceID, Path: path}
	if _, err := db.Exec(
		"INSERT INTO codebases (id, workspace_id, path) VALUES (?, ?, ?)",
		c.ID, c.WorkspaceID, c.Path,
	); err != nil {
		return Codebase{}, err
	}
	return GetCodebase(db, c.ID)
}

func ListCodebases(db *sql.DB, workspaceID string) ([]Codebase, error) {
	rows, err := db.Query(
		"SELECT id, workspace_id, path, label, created_at FROM codebases WHERE workspace_id = ? ORDER BY created_at",
		workspaceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cs []Codebase
	for rows.Next() {
		c, err := scanCodebase(rows)
		if err != nil {
			return nil, err
		}
		cs = append(cs, c)
	}
	return cs, rows.Err()
}

func GetCodebase(db *sql.DB, id string) (Codebase, error) {
	row := db.QueryRow(
		"SELECT id, workspace_id, path, label, created_at FROM codebases WHERE id = ?",
		id,
	)
	return scanCodebase(row)
}

func DeleteCodebase(db *sql.DB, id string) error {
	_, err := db.Exec("DELETE FROM codebases WHERE id = ?", id)
	return err
}

func scanCodebase(row rowScanner) (Codebase, error) {
	var c Codebase
	var label sql.NullString
	err := row.Scan(&c.ID, &c.WorkspaceID, &c.Path, &label, &c.CreatedAt)
	if err != nil {
		return Codebase{}, err
	}
	c.Label = nullToPtr(label)
	return c, nil
}
