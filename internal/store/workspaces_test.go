package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func TestNewID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := NewID()
		if len(id) != 32 {
			t.Fatalf("NewID() = %q, want 32 hex chars", id)
		}
		for _, c := range id {
			if !strings.ContainsRune("0123456789abcdef", c) {
				t.Fatalf("NewID() = %q, non-hex char %q", id, c)
			}
		}
		if seen[id] {
			t.Fatalf("NewID() duplicated %q", id)
		}
		seen[id] = true
	}
}

func TestCreateGetWorkspace(t *testing.T) {
	db := openTest(t)

	w, err := CreateWorkspace(db, "alpha", "/tmp/alpha")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	if w.ID == "" || w.Name != "alpha" || w.RootPath != "/tmp/alpha" {
		t.Errorf("workspace = %+v", w)
	}
	if w.CreatedAt == "" || w.UpdatedAt == "" {
		t.Errorf("timestamps empty: %+v", w)
	}
	if w.ArchivedAt != nil {
		t.Errorf("ArchivedAt = %v, want nil", *w.ArchivedAt)
	}

	got, err := GetWorkspace(db, w.ID)
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	if got != w {
		t.Errorf("GetWorkspace = %+v, want %+v", got, w)
	}
}

func TestListWorkspacesOrderedByCreatedAt(t *testing.T) {
	db := openTest(t)
	// Consecutive inserts can share a millisecond (§3.3), so seed distinct
	// created_at values to make the ordering assertion deterministic.
	for i, name := range []string{"first", "second", "third"} {
		ts := "2099-01-01T00:00:0" + string(rune('0'+i)) + ".000"
		if _, err := db.Exec("INSERT INTO workspaces (id, name, root_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)", NewID(), name, "/tmp/"+name, ts, ts); err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
	}
	ws, err := ListWorkspaces(db)
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(ws) != 3 {
		t.Fatalf("len = %d, want 3", len(ws))
	}
	want := []string{"first", "second", "third"}
	for i, w := range ws {
		if w.Name != want[i] {
			t.Errorf("ws[%d].Name = %q, want %q", i, w.Name, want[i])
		}
	}
}

func TestMostRecentWorkspace(t *testing.T) {
	db := openTest(t)
	if _, err := MostRecentWorkspace(db); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("MostRecentWorkspace on empty = %v, want sql.ErrNoRows", err)
	}
	if _, err := CreateWorkspace(db, "older", "/tmp/older"); err != nil {
		t.Fatal(err)
	}
	// Two inserts can share a millisecond (ties are harmless per §3.3), so make
	// the later row's created_at unambiguously distinct.
	if _, err := db.Exec("INSERT INTO workspaces (id, name, root_path, created_at, updated_at) VALUES (?, 'newer', '/tmp/newer', '2099-01-01T00:00:00.000', '2099-01-01T00:00:00.000')", NewID()); err != nil {
		t.Fatalf("insert newer: %v", err)
	}
	w, err := MostRecentWorkspace(db)
	if err != nil {
		t.Fatalf("MostRecentWorkspace: %v", err)
	}
	if w.Name != "newer" {
		t.Errorf("MostRecentWorkspace = %q, want newer", w.Name)
	}
}

func TestWorkspaceByRootPath(t *testing.T) {
	db := openTest(t)
	if _, err := WorkspaceByRootPath(db, "/tmp/nope"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing root_path = %v, want sql.ErrNoRows", err)
	}
	if _, err := CreateWorkspace(db, "w", "/tmp/w"); err != nil {
		t.Fatal(err)
	}
	if _, err := WorkspaceByRootPath(db, "/tmp/w"); err != nil {
		t.Fatalf("WorkspaceByRootPath existing: %v", err)
	}
}

func TestDeleteWorkspaceCascades(t *testing.T) {
	db := openTest(t)
	w, err := CreateWorkspace(db, "w", "/tmp/w")
	if err != nil {
		t.Fatal(err)
	}
	b, err := CreateBoard(db, w.ID, "Board")
	if err != nil {
		t.Fatal(err)
	}
	cols, err := ListColumns(db, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) == 0 {
		t.Fatal("no columns seeded")
	}
	if _, err := db.Exec("INSERT INTO cards (id, column_id, board_id, workspace_id, title) VALUES (?, ?, ?, ?, 'c')", NewID(), cols[0].ID, b.ID, w.ID); err != nil {
		t.Fatalf("insert card: %v", err)
	}
	if _, err := CreateCodebase(db, w.ID, "/tmp/w/cb"); err != nil {
		t.Fatal(err)
	}

	if err := DeleteWorkspace(db, w.ID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	for _, q := range []string{"SELECT count(*) FROM workspaces", "SELECT count(*) FROM boards", "SELECT count(*) FROM columns", "SELECT count(*) FROM cards", "SELECT count(*) FROM codebases"} {
		var n int
		if err := db.QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if n != 0 {
			t.Errorf("%s = %d, want 0", q, n)
		}
	}
}

func TestWorkspaceIsNotFound(t *testing.T) {
	if !IsNotFound(sql.ErrNoRows) {
		t.Error("IsNotFound(sql.ErrNoRows) = false, want true")
	}
	if IsNotFound(nil) {
		t.Error("IsNotFound(nil) = true, want false")
	}
}
