package store

import (
	"strings"
	"testing"
)

func TestCreateListCodebases(t *testing.T) {
	db := openTest(t)
	w, _ := CreateWorkspace(db, "w", "/tmp/w")

	c1, err := CreateCodebase(db, w.ID, "/tmp/w/a")
	if err != nil {
		t.Fatalf("CreateCodebase: %v", err)
	}
	if c1.Path != "/tmp/w/a" || c1.WorkspaceID != w.ID {
		t.Errorf("codebase = %+v", c1)
	}
	if c1.Label != nil {
		t.Errorf("Label = %v, want nil", *c1.Label)
	}
	if _, err := CreateCodebase(db, w.ID, "/tmp/w/b"); err != nil {
		t.Fatal(err)
	}

	cs, err := ListCodebases(db, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("len = %d, want 2", len(cs))
	}

	got, err := GetCodebase(db, c1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != c1 {
		t.Errorf("GetCodebase = %+v, want %+v", got, c1)
	}
}

func TestCreateCodebaseUniquePerWorkspace(t *testing.T) {
	db := openTest(t)
	w, _ := CreateWorkspace(db, "w", "/tmp/w")
	if _, err := CreateCodebase(db, w.ID, "/tmp/w/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateCodebase(db, w.ID, "/tmp/w/a"); err == nil || !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		t.Fatalf("duplicate path: err = %v, want UNIQUE constraint failed", err)
	}
	// Same path under a different workspace is legal.
	w2, _ := CreateWorkspace(db, "w2", "/tmp/w2")
	if _, err := CreateCodebase(db, w2.ID, "/tmp/w/a"); err != nil {
		t.Fatalf("same path other workspace: %v", err)
	}
}

func TestDeleteCodebase(t *testing.T) {
	db := openTest(t)
	w, _ := CreateWorkspace(db, "w", "/tmp/w")
	b, _ := CreateBoard(db, w.ID, "Board")
	cols, _ := ListColumns(db, b.ID)
	cb, _ := CreateCodebase(db, w.ID, "/tmp/w/a")

	// A card referencing the codebase: deleting the codebase SET NULLs the ref.
	if _, err := db.Exec("INSERT INTO cards (id, column_id, board_id, workspace_id, codebase_id, title) VALUES (?, ?, ?, ?, ?, 'c')", NewID(), cols[0].ID, b.ID, w.ID, cb.ID); err != nil {
		t.Fatalf("insert card: %v", err)
	}
	if err := DeleteCodebase(db, cb.ID); err != nil {
		t.Fatalf("DeleteCodebase: %v", err)
	}
	var codebaseID any
	if err := db.QueryRow("SELECT codebase_id FROM cards LIMIT 1").Scan(&codebaseID); err != nil {
		t.Fatal(err)
	}
	if codebaseID != nil {
		t.Errorf("codebase_id = %v, want NULL after codebase delete", codebaseID)
	}
	var n int
	if err := db.QueryRow("SELECT count(*) FROM codebases").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("codebases = %d, want 0", n)
	}
}
