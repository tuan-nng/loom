package store

import (
	"context"
	"database/sql"
)

// DefaultColumns is the fixed set seeded by loom init and every board create
// (ADR-001 §6): five columns in order, one per stage, positions 0,1000,2000,
// 3000,4000. There is no empty-board template in v0.1.
var DefaultColumns = []struct {
	Name     string
	Stage    string
	Position int
}{
	{Name: "Backlog", Stage: "backlog", Position: 0},
	{Name: "To Do", Stage: "todo", Position: 1000},
	{Name: "In Progress", Stage: "dev", Position: 2000},
	{Name: "Review", Stage: "review", Position: 3000},
	{Name: "Done", Stage: "done", Position: 4000},
}

// ValidStages is the closed stage set enforced by the columns.stage CHECK
// (ADR-001 §3.3), derived from DefaultColumns so it can never drift from the
// seeded template (one stage per default column, in display order).
var ValidStages = func() []string {
	stages := make([]string, len(DefaultColumns))
	for i, dc := range DefaultColumns {
		stages[i] = dc.Stage
	}
	return stages
}()

type Board struct {
	ID          string
	WorkspaceID string
	Name        string
	Description *string
	Position    int
	CreatedAt   string
	UpdatedAt   string
}

// execer abstracts *sql.DB and *sql.Tx so a multi-statement seed can run
// atomically inside a caller-chosen transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// CreateBoard creates the board and seeds its five default columns in one
// transaction, so a board never exists with an empty template (ADR-001 §6).
func CreateBoard(db *sql.DB, workspaceID, name string) (Board, error) {
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return Board{}, err
	}
	defer tx.Rollback()

	b, err := createBoard(tx, workspaceID, name)
	if err != nil {
		return Board{}, err
	}
	if err := tx.Commit(); err != nil {
		return Board{}, err
	}
	return GetBoard(db, b.ID)
}

func createBoard(e execer, workspaceID, name string) (Board, error) {
	var pos int
	if err := e.QueryRowContext(context.Background(),
		"SELECT COALESCE(MAX(position), 0) FROM boards WHERE workspace_id = ?",
		workspaceID,
	).Scan(&pos); err != nil {
		return Board{}, err
	}
	b := Board{ID: NewID(), WorkspaceID: workspaceID, Name: name, Position: pos + 1000}
	if _, err := e.ExecContext(context.Background(),
		"INSERT INTO boards (id, workspace_id, name, position) VALUES (?, ?, ?, ?)",
		b.ID, b.WorkspaceID, b.Name, b.Position,
	); err != nil {
		return Board{}, err
	}
	if err := seedDefaultColumns(e, b.ID); err != nil {
		return Board{}, err
	}
	return b, nil
}

func seedDefaultColumns(e execer, boardID string) error {
	for _, dc := range DefaultColumns {
		if _, err := e.ExecContext(context.Background(),
			"INSERT INTO columns (id, board_id, name, stage, position) VALUES (?, ?, ?, ?, ?)",
			NewID(), boardID, dc.Name, dc.Stage, dc.Position,
		); err != nil {
			return err
		}
	}
	return nil
}

func ListBoards(db *sql.DB, workspaceID string) ([]Board, error) {
	rows, err := db.Query(
		"SELECT id, workspace_id, name, description, position, created_at, updated_at FROM boards WHERE workspace_id = ? ORDER BY position",
		workspaceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bs []Board
	for rows.Next() {
		b, err := scanBoard(rows)
		if err != nil {
			return nil, err
		}
		bs = append(bs, b)
	}
	return bs, rows.Err()
}

func GetBoard(db *sql.DB, id string) (Board, error) {
	row := db.QueryRow(
		"SELECT id, workspace_id, name, description, position, created_at, updated_at FROM boards WHERE id = ?",
		id,
	)
	return scanBoard(row)
}

// FirstBoard returns the board with the lowest position in a workspace — the
// fallback target when ui_state has no selection (ADR-001 §6).
func FirstBoard(db *sql.DB, workspaceID string) (Board, error) {
	row := db.QueryRow(
		"SELECT id, workspace_id, name, description, position, created_at, updated_at FROM boards WHERE workspace_id = ? ORDER BY position LIMIT 1",
		workspaceID,
	)
	return scanBoard(row)
}

func DeleteBoard(db *sql.DB, id string) error {
	_, err := db.Exec("DELETE FROM boards WHERE id = ?", id)
	return err
}

func scanBoard(row rowScanner) (Board, error) {
	var b Board
	var desc sql.NullString
	err := row.Scan(&b.ID, &b.WorkspaceID, &b.Name, &desc, &b.Position, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return Board{}, err
	}
	b.Description = nullToPtr(desc)
	return b, nil
}
