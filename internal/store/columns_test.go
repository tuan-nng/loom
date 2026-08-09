package store

import (
	"strings"
	"testing"
)

func TestCreateColumnAppends(t *testing.T) {
	db := openTest(t)
	w, _ := CreateWorkspace(db, "w", "/tmp/w")
	b, _ := CreateBoard(db, w.ID, "Board")

	c, err := CreateColumn(db, b.ID, "Extra", "review")
	if err != nil {
		t.Fatalf("CreateColumn: %v", err)
	}
	// After the five defaults (max position 4000), the new column lands at 5000.
	if c.Position != 5000 {
		t.Errorf("position = %d, want 5000", c.Position)
	}
	cols, err := ListColumns(db, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 6 {
		t.Fatalf("len = %d, want 6", len(cols))
	}
	if cols[5].ID != c.ID {
		t.Errorf("last column = %q, want %q", cols[5].ID, c.ID)
	}
}

func TestCreateColumnStageCheck(t *testing.T) {
	db := openTest(t)
	w, _ := CreateWorkspace(db, "w", "/tmp/w")
	b, _ := CreateBoard(db, w.ID, "Board")

	if _, err := CreateColumn(db, b.ID, "Bogus", "not-a-stage"); err == nil || !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Fatalf("CreateColumn bogus stage: err = %v, want CHECK constraint failed", err)
	}
}

func TestDeleteColumnCascadesCards(t *testing.T) {
	db := openTest(t)
	w, _ := CreateWorkspace(db, "w", "/tmp/w")
	b, _ := CreateBoard(db, w.ID, "Board")
	cols, _ := ListColumns(db, b.ID)
	if _, err := db.Exec("INSERT INTO cards (id, column_id, board_id, workspace_id, title) VALUES (?, ?, ?, ?, 'c')", NewID(), cols[0].ID, b.ID, w.ID); err != nil {
		t.Fatalf("insert card: %v", err)
	}

	if err := DeleteColumn(db, cols[0].ID); err != nil {
		t.Fatalf("DeleteColumn: %v", err)
	}
	var n int
	if err := db.QueryRow("SELECT count(*) FROM cards").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("cards = %d, want 0 after column delete", n)
	}
}

func TestColumnRoundTrip(t *testing.T) {
	db := openTest(t)
	w, _ := CreateWorkspace(db, "w", "/tmp/w")
	b, _ := CreateBoard(db, w.ID, "Board")
	cols, _ := ListColumns(db, b.ID)

	got, err := GetColumn(db, cols[0].ID)
	if err != nil {
		t.Fatalf("GetColumn: %v", err)
	}
	if got != cols[0] {
		t.Errorf("GetColumn = %+v, want %+v", got, cols[0])
	}
}
