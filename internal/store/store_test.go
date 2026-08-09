package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"

	"loom/internal/store/migrate"
)

func openTest(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "loom.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func providerFor(t *testing.T, db *sql.DB) *goose.Provider {
	t.Helper()
	p, err := goose.NewProvider(goose.DialectSQLite3, db, migrate.FS, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return p
}

func pragmaValue(t *testing.T, db *sql.DB, stmt string) string {
	t.Helper()
	var v string
	if err := db.QueryRow(stmt).Scan(&v); err != nil {
		t.Fatalf("query %q: %v", stmt, err)
	}
	return v
}

func seedBoardTree(t *testing.T, db *sql.DB, wsID, boardID, col1ID, col2ID string) {
	t.Helper()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	exec("INSERT INTO workspaces (id, name, root_path) VALUES (?, 'ws', '/tmp/ws')", wsID)
	exec("INSERT INTO boards (id, workspace_id, name) VALUES (?, ?, 'Board')", boardID, wsID)
	exec("INSERT INTO columns (id, board_id, name, stage, position) VALUES (?, ?, 'To Do', 'todo', 1000)", col1ID, boardID)
	exec("INSERT INTO columns (id, board_id, name, stage, position) VALUES (?, ?, 'Done', 'done', 4000)", col2ID, boardID)
	exec("INSERT INTO cards (id, column_id, board_id, workspace_id, title) VALUES ('card1', ?, ?, ?, 'Card A')", col1ID, boardID, wsID)
	exec("INSERT INTO cards (id, column_id, board_id, workspace_id, title) VALUES ('card2', ?, ?, ?, 'Card B')", col2ID, boardID, wsID)
}

func TestOpenPragmas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loom.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	tests := []struct {
		name string
		stmt string
		want string
	}{
		{"journal_mode", "PRAGMA journal_mode", "wal"},
		{"foreign_keys", "PRAGMA foreign_keys", "1"},
		{"busy_timeout", "PRAGMA busy_timeout", "5000"},
		{"synchronous", "PRAGMA synchronous", "1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pragmaValue(t, db, tt.stmt); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("reopen", func(t *testing.T) {
		if err := db.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		db2, err := Open(path)
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		defer db2.Close()
		for _, tt := range tests {
			if got := pragmaValue(t, db2, tt.stmt); got != tt.want {
				t.Errorf("reopen %s: got %q, want %q", tt.name, got, tt.want)
			}
		}
	})
}

func TestPragmasEveryConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loom.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(4)

	ctx := context.Background()
	conns := make([]*sql.Conn, 4)
	for i := range conns {
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("Conn %d: %v", i, err)
		}
		conns[i] = conn
	}
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	for i, conn := range conns {
		var fk string
		if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
			t.Fatalf("conn %d foreign_keys: %v", i, err)
		}
		if fk != "1" {
			t.Errorf("conn %d foreign_keys = %q, want 1", i, fk)
		}
	}
}

func TestMigrationCreatesSchema(t *testing.T) {
	db := openTest(t)

	for _, tbl := range []string{"workspaces", "boards", "columns", "codebases", "cards", "traces", "ui_state"} {
		var n int
		if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", tbl).Scan(&n); err != nil {
			t.Fatalf("query %s: %v", tbl, err)
		}
		if n != 1 {
			t.Errorf("table %s not found", tbl)
		}
	}
	for _, idx := range []string{"idx_traces_card_run", "idx_traces_run_lifecycle", "idx_traces_open_runs"} {
		var n int
		if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?", idx).Scan(&n); err != nil {
			t.Fatalf("query %s: %v", idx, err)
		}
		if n != 1 {
			t.Errorf("index %s not found", idx)
		}
	}
	if _, err := db.Exec("INSERT INTO ui_state (id) VALUES (1)"); err == nil {
		t.Error("second ui_state row accepted, want single-row enforcement")
	}
}

func TestBoardDeleteCascades(t *testing.T) {
	db := openTest(t)
	seedBoardTree(t, db, "w1", "b1", "c1", "c2")

	if _, err := db.Exec("DELETE FROM boards WHERE id = 'b1'"); err != nil {
		t.Fatalf("delete board: %v", err)
	}
	for _, q := range []string{"SELECT count(*) FROM columns", "SELECT count(*) FROM cards"} {
		var n int
		if err := db.QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("query %q: %v", q, err)
		}
		if n != 0 {
			t.Errorf("%s = %d, want 0", q, n)
		}
	}
}

func TestAgentColumn(t *testing.T) {
	db := openTest(t)
	seedBoardTree(t, db, "w1", "b1", "c1", "c2")

	if _, err := db.Exec("INSERT INTO cards (id, column_id, board_id, workspace_id, title, agent) VALUES ('bad', 'c1', 'b1', 'w1', 'Bad', 'bogus')"); err == nil || !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Fatalf("agent='bogus': err = %v, want CHECK constraint failed", err)
	}
	if _, err := db.Exec("INSERT INTO cards (id, column_id, board_id, workspace_id, title) VALUES ('ok', 'c1', 'b1', 'w1', 'OK')"); err != nil {
		t.Fatalf("insert without agent: %v", err)
	}
	var agent sql.NullString
	if err := db.QueryRow("SELECT agent FROM cards WHERE id = 'ok'").Scan(&agent); err != nil {
		t.Fatalf("select agent: %v", err)
	}
	if agent.Valid {
		t.Errorf("agent = %q, want NULL", agent.String)
	}
	if _, err := db.Exec("INSERT INTO cards (id, column_id, board_id, workspace_id, title, agent) VALUES ('cl', 'c1', 'b1', 'w1', 'Cl', 'claude')"); err != nil {
		t.Fatalf("insert agent='claude': %v", err)
	}
}

func TestExistingCardsMigrateAgentNull(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loom.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	prov := providerFor(t, db)

	if _, err := prov.UpTo(context.Background(), 1); err != nil {
		t.Fatalf("UpTo(1): %v", err)
	}
	seedBoardTree(t, db, "w1", "b1", "c1", "c2")
	if _, err := prov.UpTo(context.Background(), 2); err != nil {
		t.Fatalf("UpTo(2): %v", err)
	}

	var agent sql.NullString
	if err := db.QueryRow("SELECT agent FROM cards WHERE id = 'card1'").Scan(&agent); err != nil {
		t.Fatalf("select agent: %v", err)
	}
	if agent.Valid {
		t.Errorf("agent = %q, want NULL", agent.String)
	}
}

func TestDownRestoresInitialSchema(t *testing.T) {
	db := openTest(t)
	prov := providerFor(t, db)

	if _, err := prov.DownTo(context.Background(), 1); err != nil {
		t.Fatalf("DownTo(1): %v", err)
	}
	var agent sql.NullString
	err := db.QueryRow("SELECT agent FROM cards LIMIT 1").Scan(&agent)
	if err == nil || !strings.Contains(err.Error(), "no such column") {
		t.Fatalf("SELECT agent: err = %v, want no such column", err)
	}
	var n int
	if err := db.QueryRow("SELECT count(*) FROM workspaces").Scan(&n); err != nil {
		t.Fatalf("workspaces after down: %v", err)
	}
}

func TestUpIdempotent(t *testing.T) {
	db := openTest(t)
	prov := providerFor(t, db)

	results, err := prov.Up(context.Background())
	if err != nil {
		t.Fatalf("second Up: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("second Up applied %d migrations, want 0", len(results))
	}
}

func TestOpenNonExistentDirFails(t *testing.T) {
	_, err := Open(filepath.Join(t.TempDir(), "no", "such", "dir", "loom.db"))
	if err == nil {
		t.Error("Open with missing parent dir succeeded, want error")
	}
	if !strings.Contains(fmt.Sprint(err), "unable to open database file") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMigrateUpEmptyFSFails(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "loom.db"))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if err := migrateUp(db, os.DirFS(t.TempDir())); err == nil {
		t.Error("migrateUp with empty FS succeeded, want error")
	}
}
