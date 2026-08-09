package store

import (
	"testing"
)

func TestGetUIStateEmpty(t *testing.T) {
	db := openTest(t)
	s, err := GetUIState(db)
	if err != nil {
		t.Fatalf("GetUIState: %v", err)
	}
	if s.LastWorkspaceID != nil || s.LastBoardID != nil {
		t.Errorf("initial ui_state = %+v, want nil selections", s)
	}
}

func TestSetUIStateRoundTrip(t *testing.T) {
	db := openTest(t)
	w, _ := CreateWorkspace(db, "w", "/tmp/w")
	b, _ := CreateBoard(db, w.ID, "Board")

	if err := SetUIState(db, &w.ID, &b.ID); err != nil {
		t.Fatalf("SetUIState: %v", err)
	}
	s, err := GetUIState(db)
	if err != nil {
		t.Fatal(err)
	}
	if s.LastWorkspaceID == nil || *s.LastWorkspaceID != w.ID {
		t.Errorf("LastWorkspaceID = %v, want %q", s.LastWorkspaceID, w.ID)
	}
	if s.LastBoardID == nil || *s.LastBoardID != b.ID {
		t.Errorf("LastBoardID = %v, want %q", s.LastBoardID, b.ID)
	}

	// Update, then clear the board selection.
	if err := SetUIState(db, &w.ID, nil); err != nil {
		t.Fatal(err)
	}
	s, err = GetUIState(db)
	if err != nil {
		t.Fatal(err)
	}
	if s.LastBoardID != nil {
		t.Errorf("LastBoardID = %v, want nil after clear", s.LastBoardID)
	}
}

func TestSetUIStateDeletesSelectionOnWorkspaceDelete(t *testing.T) {
	db := openTest(t)
	w, _ := CreateWorkspace(db, "w", "/tmp/w")
	b, _ := CreateBoard(db, w.ID, "Board")
	if err := SetUIState(db, &w.ID, &b.ID); err != nil {
		t.Fatal(err)
	}

	if err := DeleteWorkspace(db, w.ID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	s, err := GetUIState(db)
	if err != nil {
		t.Fatal(err)
	}
	if s.LastWorkspaceID != nil || s.LastBoardID != nil {
		t.Errorf("after delete ui_state = %+v, want nil (ON DELETE SET NULL)", s)
	}
}

func TestSetUIStateSingleRow(t *testing.T) {
	db := openTest(t)
	// The CHECK (id = 1) constraint rejects a second row (T5 acceptance).
	if _, err := db.Exec("INSERT INTO ui_state (id, last_workspace_id) VALUES (2, NULL)"); err == nil {
		t.Error("second ui_state row accepted, want single-row enforcement")
	}
}
