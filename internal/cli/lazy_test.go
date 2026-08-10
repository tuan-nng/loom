package cli

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"loom/internal/board"
	"loom/internal/config"
	"loom/internal/session"
	"loom/internal/store"
)

// TestLazySessionMaterializesOnce verifies the proxy builds the manager on the
// first session-touching call, caches it, and never re-invokes the builder.
func TestLazySessionMaterializesOnce(t *testing.T) {
	cfg := config.Default()
	db := openCLIDB(t)
	calls := 0
	s := &lazySession{cfg: cfg, db: db, newManager: func(server string, c *config.Config, d *sql.DB) (*session.Manager, error) {
		_ = server
		_ = c
		_ = d
		calls++
		return session.NewManager(session.Tmux{Server: "inert"}, c, d), nil
	}}
	// ReconcileOnStartup over an empty db touches materialize but not tmux.
	if err := s.ReconcileOnStartup(context.Background()); err != nil {
		t.Fatalf("ReconcileOnStartup: %v", err)
	}
	if err := s.probe(); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if calls != 1 {
		t.Errorf("builder calls = %d, want 1 (cached)", calls)
	}
}

// TestLazySessionCachesConstructionError verifies a failed materialization is
// cached: the same error is returned on every later call, the builder runs
// exactly once.
func TestLazySessionCachesConstructionError(t *testing.T) {
	cfg := config.Default()
	db := openCLIDB(t)
	calls := 0
	boom := errors.New("session: tmux not found in PATH (install it)")
	s := &lazySession{cfg: cfg, db: db, newManager: func(server string, c *config.Config, d *sql.DB) (*session.Manager, error) {
		calls++
		return nil, boom
	}}
	if err := s.probe(); !errors.Is(err, boom) {
		t.Fatalf("probe() = %v, want %v", err, boom)
	}
	if err := s.probe(); !errors.Is(err, boom) {
		t.Fatalf("second probe() = %v, want cached %v", err, boom)
	}
	if err := s.Ensure(context.Background(), store.Card{}); !errors.Is(err, boom) {
		t.Fatalf("Ensure() = %v, want cached %v", err, boom)
	}
	if calls != 1 {
		t.Errorf("builder calls = %d, want 1", calls)
	}
}

// TestLazySessionSatisfiesBoardSeam is a compile-time + wiring check that the
// proxy can be handed to board.NewService.
func TestLazySessionSatisfiesBoardSeam(t *testing.T) {
	cfg := config.Default()
	db := openCLIDB(t)
	s := newLazySession(cfg, db)
	svc := board.NewService(db, s)
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
}

// TestLazySessionDelegatesSuccess drives the success path: with a builder that
// returns a real manager (inert tmux binary — never invoked on an empty db),
// session calls delegate rather than returning the construction error.
func TestLazySessionDelegatesSuccess(t *testing.T) {
	cfg := config.Default()
	db := openCLIDB(t)
	s := &lazySession{cfg: cfg, db: db, newManager: func(server string, c *config.Config, d *sql.DB) (*session.Manager, error) {
		return session.NewManager(session.Tmux{Server: "inert"}, c, d), nil
	}}
	// ReconcileOnStartup over an empty db returns nil without touching tmux.
	if err := s.ReconcileOnStartup(context.Background()); err != nil {
		t.Fatalf("ReconcileOnStartup: %v", err)
	}
	// probe() reports availability (materialization succeeded).
	if err := s.probe(); err != nil {
		t.Fatalf("probe: %v", err)
	}
	// Status over an empty db: the manager's Status calls tmux.Sessions on the
	// inert binary, which fails — proving delegation happened (the error is
	// not the construction error).
	if _, err := s.Status(context.Background()); err == nil {
		t.Error("Status succeeded with inert tmux, want error")
	}
	// Kill on an absent session: KillSession runs exec against the inert bin.
	if err := s.Kill(context.Background(), store.Card{}); err == nil {
		t.Error("Kill succeeded with inert tmux, want error")
	}
	// Attach likewise delegates.
	if err := s.Attach(context.Background(), store.Card{}); err == nil {
		t.Error("Attach succeeded with inert tmux, want error")
	}
	// Ensure: driver resolution fails before tmux (default agent "claude" not
	// on a stub PATH) but still proves Ensure delegated, not short-circuited.
	if err := s.Ensure(context.Background(), store.Card{}); err == nil {
		t.Error("Ensure succeeded with empty card, want error")
	}
}
