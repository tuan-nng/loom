package store

import (
	"path/filepath"
	"testing"
)

func TestInitWorkspaceCreatesTree(t *testing.T) {
	db := openTest(t)
	dir := filepath.Join(t.TempDir(), "proj")

	w, err := InitWorkspace(db, dir)
	if err != nil {
		t.Fatalf("InitWorkspace: %v", err)
	}
	if w.Name != "proj" {
		t.Errorf("name = %q, want dir basename proj", w.Name)
	}
	if w.RootPath != filepath.Clean(dir) {
		t.Errorf("root_path = %q, want %q", w.RootPath, filepath.Clean(dir))
	}

	bs, err := ListBoards(db, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 1 || bs[0].Name != "Board" {
		t.Fatalf("boards = %+v, want single 'Board'", bs)
	}
	cols, err := ListColumns(db, bs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 5 {
		t.Fatalf("columns = %d, want 5 defaults", len(cols))
	}
}

func TestInitWorkspaceIdempotent(t *testing.T) {
	db := openTest(t)
	dir := filepath.Join(t.TempDir(), "proj")

	w1, err := InitWorkspace(db, dir)
	if err != nil {
		t.Fatal(err)
	}
	w2, err := InitWorkspace(db, dir)
	if err != nil {
		t.Fatalf("second InitWorkspace: %v", err)
	}
	if w1.ID != w2.ID {
		t.Errorf("idempotent init returned different workspace: %q vs %q", w1.ID, w2.ID)
	}

	ws, err := ListWorkspaces(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 1 {
		t.Errorf("workspaces = %d, want 1", len(ws))
	}
	bs, _ := ListBoards(db, w1.ID)
	if len(bs) != 1 {
		t.Errorf("boards = %d, want 1 after re-init", len(bs))
	}
	cols, _ := ListColumns(db, bs[0].ID)
	if len(cols) != 5 {
		t.Errorf("columns = %d, want 5 after re-init", len(cols))
	}
}

func TestInitWorkspacePathNormalized(t *testing.T) {
	db := openTest(t)
	dir := filepath.Join(t.TempDir(), "proj")
	withSlash := dir + string(filepath.Separator)

	if _, err := InitWorkspace(db, withSlash); err != nil {
		t.Fatal(err)
	}
	// Same directory expressed differently resolves to the same workspace.
	if _, err := InitWorkspace(db, dir); err != nil {
		t.Fatal(err)
	}
	ws, err := ListWorkspaces(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 1 {
		t.Errorf("workspaces = %d, want 1 (path normalization)", len(ws))
	}
}
