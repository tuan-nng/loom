package store

import (
	"database/sql"
)

// UIState mirrors the single-row ui_state table. LastWorkspaceID/LastBoardID
// are *string because ON DELETE SET NULL degrades a deleted selection to "no
// selection" rather than a dangling reference (ADR-001 §5).
type UIState struct {
	LastWorkspaceID *string
	LastBoardID     *string
	UpdatedAt       string
}

func GetUIState(db *sql.DB) (UIState, error) {
	var s UIState
	var ws, bd sql.NullString
	err := db.QueryRow(
		"SELECT last_workspace_id, last_board_id, updated_at FROM ui_state WHERE id = 1",
	).Scan(&ws, &bd, &s.UpdatedAt)
	if err != nil {
		return UIState{}, err
	}
	s.LastWorkspaceID = nullToPtr(ws)
	s.LastBoardID = nullToPtr(bd)
	return s, nil
}

// SetUIState updates the single row in place; it never inserts (the CHECK
// id = 1 constraint would reject a second row anyway).
func SetUIState(db *sql.DB, lastWorkspaceID, lastBoardID *string) error {
	_, err := db.Exec(
		"UPDATE ui_state SET last_workspace_id = ?, last_board_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%f','now') WHERE id = 1",
		ptrToNull(lastWorkspaceID), ptrToNull(lastBoardID),
	)
	return err
}
