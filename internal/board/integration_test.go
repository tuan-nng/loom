package board

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"loom/internal/config"
	"loom/internal/session"
	"loom/internal/store"
)

const selfTestServer = "loomselftest"

// requireTmux skips the test when tmux is absent, keeping the unit suite
// runnable in minimal environments (mirrors session's tmux helpers).
func requireTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skipf("tmux not found in PATH: %v", err)
	}
}

// writeStub places an executable script named name in a fresh stub dir and
// prepends that dir to PATH so the driver's exec.LookPath finds it.
func writeStub(t *testing.T, name, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func hasSession(t *testing.T, tm session.Tmux, name string) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		ok, err := tm.HasSession(name)
		if err != nil {
			t.Fatalf("HasSession(%q): %v", name, err)
		}
		if ok {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// bootServer starts the isolated -L self-test server via a throwaway session
// so absent-session checks are deterministic (server exists, target session
// does not) rather than hitting the cold-server error.
func bootServer(t *testing.T, tm session.Tmux) {
	t.Helper()
	boot := fmt.Sprintf("loomtest-%d", time.Now().UnixNano())
	if err := tm.NewSession(boot, t.TempDir(), "cat"); err != nil {
		t.Fatalf("NewSession(boot): %v", err)
	}
	t.Cleanup(func() { tm.KillSession(boot) })
}

func traceCount(t *testing.T, db *sql.DB, cardID, eventType string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM traces WHERE card_id = ? AND event_type = ?", cardID, eventType,
	).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", eventType, err)
	}
	return n
}

// TestServiceOpenCloseRoundTrip exercises the real *session.Manager seam: a
// stubbed claude binary on PATH, an isolated -L loomselftest tmux server, and
// the production NewManager wiring. OpenCard creates the session (exactly one
// trace_start); CloseCard kills it and finalizes (exactly one trace_end).
func TestServiceOpenCloseRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	requireTmux(t)
	db, err := store.Open(filepath.Join(t.TempDir(), "loom.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ws, err := store.CreateWorkspace(db, "ws", t.TempDir())
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	board, err := store.CreateBoard(db, ws.ID, "Board")
	if err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}
	cols, err := store.ListColumns(db, board.ID)
	if err != nil {
		t.Fatalf("ListColumns: %v", err)
	}
	var todoCol store.Column
	for _, c := range cols {
		if c.Stage == "todo" {
			todoCol = c
		}
	}
	card, err := store.CreateCard(db, store.CardInput{ColumnID: todoCol.ID, Title: "Card"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}

	tm, err := session.New(selfTestServer)
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	bootServer(t, tm)
	writeStub(t, "loomstub-sleep", "echo READY\nsleep 300")
	cfg := config.Default()
	cfg.Agent.Claude.Binary = "loomstub-sleep"
	name := session.SessionName(card.ID)
	t.Cleanup(func() { tm.KillSession(name) })

	mgr := session.NewManager(tm, cfg, db)
	svc := NewService(db, mgr)

	if err := svc.OpenCard(context.Background(), card.ID, true); err != nil {
		t.Fatalf("OpenCard: %v", err)
	}
	if !hasSession(t, tm, name) {
		t.Fatalf("session %s not found after OpenCard", name)
	}
	if n := traceCount(t, db, card.ID, "trace_start"); n != 1 {
		t.Fatalf("trace_start rows = %d, want 1", n)
	}
	if n := traceCount(t, db, card.ID, "trace_end"); n != 0 {
		t.Fatalf("trace_end rows = %d, want 0 before close", n)
	}

	if err := svc.CloseCard(context.Background(), card.ID); err != nil {
		t.Fatalf("CloseCard: %v", err)
	}
	if hasSession(t, tm, name) {
		t.Fatalf("session %s still exists after CloseCard", name)
	}
	if n := traceCount(t, db, card.ID, "trace_end"); n != 1 {
		t.Fatalf("trace_end rows = %d, want exactly 1", n)
	}
}
