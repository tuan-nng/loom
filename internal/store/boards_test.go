package store

import (
	"database/sql"
	"errors"
	"testing"
)

func TestCreateBoardSeedsDefaultColumns(t *testing.T) {
	db := openTest(t)
	w, err := CreateWorkspace(db, "w", "/tmp/w")
	if err != nil {
		t.Fatal(err)
	}

	b, err := CreateBoard(db, w.ID, "Board")
	if err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}
	if b.WorkspaceID != w.ID || b.Name != "Board" {
		t.Errorf("board = %+v", b)
	}

	cols, err := ListColumns(db, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 5 {
		t.Fatalf("len(cols) = %d, want 5", len(cols))
	}
	want := []struct {
		name, stage string
		pos         int
	}{
		{"Backlog", "backlog", 0},
		{"To Do", "todo", 1000},
		{"In Progress", "dev", 2000},
		{"Review", "review", 3000},
		{"Done", "done", 4000},
	}
	for i, c := range cols {
		if c.BoardID != b.ID {
			t.Errorf("cols[%d].BoardID = %q, want %q", i, c.BoardID, b.ID)
		}
		if c.Name != want[i].name || c.Stage != want[i].stage || c.Position != want[i].pos {
			t.Errorf("cols[%d] = %q/%q/%d, want %q/%q/%d", i, c.Name, c.Stage, c.Position, want[i].name, want[i].stage, want[i].pos)
		}
	}
}

func TestListBoardsOrderedByPosition(t *testing.T) {
	db := openTest(t)
	w, _ := CreateWorkspace(db, "w", "/tmp/w")
	for _, name := range []string{"zeta", "alpha"} {
		if _, err := CreateBoard(db, w.ID, name); err != nil {
			t.Fatal(err)
		}
	}
	bs, err := ListBoards(db, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 2 {
		t.Fatalf("len = %d, want 2", len(bs))
	}
	if bs[0].Name != "zeta" || bs[1].Name != "alpha" {
		t.Errorf("boards = [%s, %s], want [zeta, alpha] (insertion order)", bs[0].Name, bs[1].Name)
	}
}

func TestFirstBoard(t *testing.T) {
	db := openTest(t)
	if _, err := FirstBoard(db, "nope"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("FirstBoard on empty = %v, want sql.ErrNoRows", err)
	}
	w, _ := CreateWorkspace(db, "w", "/tmp/w")
	first, _ := CreateBoard(db, w.ID, "first")
	CreateBoard(db, w.ID, "second")
	b, err := FirstBoard(db, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != first.ID {
		t.Errorf("FirstBoard = %q, want %q", b.ID, first.ID)
	}
}

func TestDeleteBoardCascades(t *testing.T) {
	db := openTest(t)
	w, _ := CreateWorkspace(db, "w", "/tmp/w")
	b, _ := CreateBoard(db, w.ID, "Board")
	cols, _ := ListColumns(db, b.ID)
	if _, err := db.Exec("INSERT INTO cards (id, column_id, board_id, workspace_id, title) VALUES (?, ?, ?, ?, 'c')", NewID(), cols[0].ID, b.ID, w.ID); err != nil {
		t.Fatalf("insert card: %v", err)
	}

	if err := DeleteBoard(db, b.ID); err != nil {
		t.Fatalf("DeleteBoard: %v", err)
	}
	for _, q := range []string{"SELECT count(*) FROM boards", "SELECT count(*) FROM columns", "SELECT count(*) FROM cards"} {
		var n int
		if err := db.QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if n != 0 {
			t.Errorf("%s = %d, want 0", q, n)
		}
	}
}

func TestGetBoardNotFound(t *testing.T) {
	db := openTest(t)
	if _, err := GetBoard(db, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetBoard missing = %v, want sql.ErrNoRows", err)
	}
}
