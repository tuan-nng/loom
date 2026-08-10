// Package cli is the non-TUI command surface: a small stdlib-flag router
// mirroring ADR-001 §6 (no cobra — the surface is fixed and fully enumerated),
// consumed by cmd/loom. Every state mutation is scriptable (CLI/TUI parity);
// bare `loom` falls through to help until the TUI ships (T16).
package cli

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"loom/internal/config"
	"loom/internal/session"
	"loom/internal/store"
)

// sessionManager is the session surface the CLI hands to board.NewService,
// plus probe() — a tmux-availability check for status degradation. *lazySession
// is the production implementation; tests inject stubs (mirroring board's own
// unexported sessionManager seam).
type sessionManager interface {
	Ensure(ctx context.Context, c store.Card) error
	Attach(ctx context.Context, c store.Card) error
	Kill(ctx context.Context, c store.Card) error
	Status(ctx context.Context) (map[string]session.SessionStatus, error)
	ReconcileOnStartup(ctx context.Context) error
	probe() error
}

// lazySession defers *session.Manager construction to the first session-
// touching call and caches the outcome (success or failure), so store-only
// commands (workspace/board/column/init/config/version/help) never require
// tmux on PATH, and a failed probe is never re-attempted. The cached
// construction error carries tmux's install hint verbatim, so status's degrade
// notice and T15's command failures surface it for free.
type lazySession struct {
	mu  sync.Mutex
	cfg *config.Config
	db  *sql.DB
	m   *session.Manager
	err error
	// newManager is the construction seam: nil in production (session.New +
	// NewManager), stubbed in tests so the proxy is deterministic with or
	// without tmux installed.
	newManager func(server string, cfg *config.Config, db *sql.DB) (*session.Manager, error)
}

func newLazySession(cfg *config.Config, db *sql.DB) *lazySession {
	return &lazySession{cfg: cfg, db: db}
}

// materialize builds the manager once, caching both the manager and any
// construction failure; later calls return the cached value without touching
// tmux again.
func (s *lazySession) materialize() (*session.Manager, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m != nil || s.err != nil {
		return s.m, s.err
	}
	if s.newManager != nil {
		s.m, s.err = s.newManager(s.cfg.Session.TmuxServer, s.cfg, s.db)
		return s.m, s.err
	}
	tm, err := session.New(s.cfg.Session.TmuxServer)
	if err != nil {
		s.err = err
		return nil, err
	}
	s.m = session.NewManager(tm, s.cfg, s.db)
	return s.m, nil
}

// probe reports tmux availability: nil when a manager is (or was) built,
// the cached construction error otherwise.
func (s *lazySession) probe() error {
	_, err := s.materialize()
	return err
}

func (s *lazySession) Ensure(ctx context.Context, c store.Card) error {
	m, err := s.materialize()
	if err != nil {
		return fmt.Errorf("cli: %w", err)
	}
	return m.Ensure(ctx, c)
}

func (s *lazySession) Attach(ctx context.Context, c store.Card) error {
	m, err := s.materialize()
	if err != nil {
		return fmt.Errorf("cli: %w", err)
	}
	return m.Attach(ctx, c)
}

func (s *lazySession) Kill(ctx context.Context, c store.Card) error {
	m, err := s.materialize()
	if err != nil {
		return fmt.Errorf("cli: %w", err)
	}
	return m.Kill(ctx, c)
}

func (s *lazySession) Status(ctx context.Context) (map[string]session.SessionStatus, error) {
	m, err := s.materialize()
	if err != nil {
		return nil, fmt.Errorf("cli: %w", err)
	}
	st, err := m.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("cli: %w", err)
	}
	return st, nil
}

func (s *lazySession) ReconcileOnStartup(ctx context.Context) error {
	m, err := s.materialize()
	if err != nil {
		return fmt.Errorf("cli: %w", err)
	}
	if err := m.ReconcileOnStartup(ctx); err != nil {
		return fmt.Errorf("cli: %w", err)
	}
	return nil
}
