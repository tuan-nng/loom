package cli

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"loom/internal/config"
	"loom/internal/session"
	"loom/internal/store"
)

func openCLIDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "loom.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// stubSess is a fake sessionManager recording session calls and injecting
// canned results, mirroring board's fakeManager.
type stubSess struct {
	reconcileCalls int
	probeErr       error
	statusRes      map[string]session.SessionStatus
	statusErr      error
	ensureErr      error
	ensureCalls    int
	attachErr      error
	attachCalls    int
	killCalls      int
}

func (s *stubSess) Ensure(ctx context.Context, c store.Card) error {
	s.ensureCalls++
	return s.ensureErr
}
func (s *stubSess) Attach(ctx context.Context, c store.Card) error {
	s.attachCalls++
	return s.attachErr
}
func (s *stubSess) Kill(ctx context.Context, c store.Card) error {
	s.killCalls++
	return nil
}
func (s *stubSess) Status(ctx context.Context) (map[string]session.SessionStatus, error) {
	return s.statusRes, s.statusErr
}
func (s *stubSess) ReconcileOnStartup(ctx context.Context) error {
	s.reconcileCalls++
	return nil
}
func (s *stubSess) probe() error { return s.probeErr }

// newTestApp builds an App with a temp db, the default config, a stub session
// manager, and buffer writers so handlers are assertable without tmux.
func newTestApp(t *testing.T, sess sessionManager) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	cfg := config.Default()
	home := t.TempDir()
	cfg.Database.Path = filepath.Join(home, "loom.db")
	db := openCLIDB(t)
	out := &bytes.Buffer{}
	errw := &bytes.Buffer{}
	return newApp(cfg, db, sess, out, errw), out, errw
}
