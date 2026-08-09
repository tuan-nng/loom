package store

import (
	"database/sql"
)

type Column struct {
	ID        string
	BoardID   string
	Name      string
	Stage     string
	Position  int
	CreatedAt string
	UpdatedAt string
}

// CreateColumn appends a column at max(position)+1000 within the board. Stage
// is passed through untouched: the schema's CHECK (backlog|todo|dev|review|
// done) is the only validator — a bogus stage surfaces as a CHECK error.
func CreateColumn(db *sql.DB, boardID, name, stage string) (Column, error) {
	var pos int
	if err := db.QueryRow(
		"SELECT COALESCE(MAX(position), 0) FROM columns WHERE board_id = ?", boardID,
	).Scan(&pos); err != nil {
		return Column{}, err
	}

	c := Column{ID: NewID(), BoardID: boardID, Name: name, Stage: stage, Position: pos + 1000}
	if _, err := db.Exec(
		"INSERT INTO columns (id, board_id, name, stage, position) VALUES (?, ?, ?, ?, ?)",
		c.ID, c.BoardID, c.Name, c.Stage, c.Position,
	); err != nil {
		return Column{}, err
	}
	return GetColumn(db, c.ID)
}

func ListColumns(db *sql.DB, boardID string) ([]Column, error) {
	rows, err := db.Query(
		"SELECT id, board_id, name, stage, position, created_at, updated_at FROM columns WHERE board_id = ? ORDER BY position",
		boardID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cs []Column
	for rows.Next() {
		c, err := scanColumn(rows)
		if err != nil {
			return nil, err
		}
		cs = append(cs, c)
	}
	return cs, rows.Err()
}

func GetColumn(db *sql.DB, id string) (Column, error) {
	row := db.QueryRow(
		"SELECT id, board_id, name, stage, position, created_at, updated_at FROM columns WHERE id = ?",
		id,
	)
	return scanColumn(row)
}

func DeleteColumn(db *sql.DB, id string) error {
	_, err := db.Exec("DELETE FROM columns WHERE id = ?", id)
	return err
}

func scanColumn(row rowScanner) (Column, error) {
	var c Column
	err := row.Scan(&c.ID, &c.BoardID, &c.Name, &c.Stage, &c.Position, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}
