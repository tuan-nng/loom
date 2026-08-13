// Package session's Manager owns the card-session lifecycle: ensure-launch,
// attach handoff, kill, the status poll, and reconcile-on-startup. Only
// ensure touches the agent driver (DESIGN-002 §10.2); everything else is
// byte-for-byte ADR-001 §4.1 behavior over session names and trace rows.
package session

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"loom/internal/agent"
	"loom/internal/config"
	"loom/internal/store"
	"loom/internal/trace"
)

// runRecorder is the trace surface the manager drives for one run;
// *trace.Recorder satisfies it. The seam exists so a failing StartRun/Watch
// is injectable at unit level (TASKS §16 post-create error path) without a
// second store implementation.
type runRecorder interface {
	StartRun(cardID, root string, baseline trace.Baseline) (string, error)
	Watch(runID, root string) error
	LiveChanges(runID string) ([]trace.Change, error)
	EndRun(runID string, durationMs, filesChanged int) error
	AbortRun(runID string) error
}

// SessionStatus is one card's session state as rendered in the board: ●
// running, ◉ attached (ADR-001 §3.5).
type SessionStatus struct {
	Running  bool
	Attached bool
}

// Manager launches and tracks one detached tmux session per card on the
// configured `-L <server>`, recording trace events through the recorder.
type Manager struct {
	tm   Tmux
	cfg  *config.Config
	db   *sql.DB
	rec  runRecorder
	logf func(format string, args ...any) // informational notices (launch mode)
}

const (
	probeDelay      = 500 * time.Millisecond // startup probe window (ADR-001 §4.1 step 1)
	probeRetryDelay = 100 * time.Millisecond // transient missing-server retry (invariant 3)
)

// NewManager returns a Manager writing trace events to a fresh recorder over
// db and logging launch-mode notices via log.Printf.
func NewManager(tm Tmux, cfg *config.Config, db *sql.DB) *Manager {
	return newManager(tm, cfg, db, trace.NewRecorder(db), log.Printf)
}

// newManager is the test seam: rec and logf are injectable so a failing
// StartRun/Watch and quiet test logging are possible.
func newManager(tm Tmux, cfg *config.Config, db *sql.DB, rec runRecorder, logf func(string, ...any)) *Manager {
	if logf == nil {
		logf = log.Printf
	}
	return &Manager{tm: tm, cfg: cfg, db: db, rec: rec, logf: logf}
}

// Ensure creates-or-reuses the card's session, exactly per DESIGN-002 §10.2:
// reuse if live; else resolve → launch → snapshot baseline → new-session →
// probe → record AFTER the probe → watch → sendkeys. Every post-create error
// path is closed by the deferred KillSession + AbortRun, so a session is never
// left alive without its run (invariants 1–2).
func (m *Manager) Ensure(ctx context.Context, c store.Card) error {
	name := SessionName(c.ID)
	ok, err := m.tm.HasSession(name)
	if err != nil {
		if !MissingServer(err) {
			return fmt.Errorf("session: %w", err)
		}
		ok = false // cold -L server (exit-empty): absence, not failure (invariant 3)
	}
	if ok {
		m.tm.configureServer() // ensure loom-owned settings on reuse too (ADR-001 §4.4)
		return nil             // reuse: no new run
	}

	ac := m.cardForAgent(c)
	driver, err := agent.Get(ac.Agent)
	if err != nil {
		return err // CHECK'd at the store; programmer error
	}

	exe, err := driver.Resolve(m.cfg) // fail the open, no trace_start (C7)
	if err != nil {
		return fmt.Errorf("%s: not found in PATH (install it or set [agent.%s] binary)", ac.Agent, ac.Agent)
	}
	spec, err := driver.Launch(exe, ac, m.cfg)
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	m.logf("session: agent %s launch mode %s", ac.Agent, driver.LaunchMode()) // informational

	root, err := m.watchRoot(c)
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	baseline, err := trace.SnapshotBaseline(root) // git pair captured BEFORE launch, held
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	if err := m.tm.NewSession(name, root, agent.CommandLine(spec.Argv)); err != nil {
		return fmt.Errorf("session: %w", err)
	}

	var runID string
	success := false
	defer func() { // close the leak on ANY post-create error (invariant 1)
		if !success {
			_ = m.tm.KillSession(name) // idempotent; no-op if already gone
			if runID != "" {
				_ = m.rec.AbortRun(runID) // DELETE whole run + stop watcher; idempotent
			}
		}
	}()

	probePane := m.tm.CapturePane(name) // pane text at probe start, for a useful failure message
	alive := m.probeAlive(name)         // waits the window; a transient missing-server is retried once
	if !alive {
		if p := m.tm.CapturePane(name); p != "" {
			probePane = p // prefer scrollback captured after the probe window
		}
		return fmt.Errorf("session: %s session failed to start: %s", ac.Agent, probePane) // defer kills; no row ever written
	}

	runID, err = m.rec.StartRun(c.ID, root, baseline) // recorded AFTER the probe (C7)
	if err != nil {
		return fmt.Errorf("session: %w", err) // defer kills; no partial run
	}
	if err := m.rec.Watch(runID, root); err != nil {
		return fmt.Errorf("session: %w", err) // defer aborts the just-written run
	}

	if s := spec.SendKeys; s != "" {
		if err := m.tm.SendKeys(name, s); err != nil {
			return fmt.Errorf("session: %w", err) // defer aborts run + kills
		}
	}
	success = true
	return nil
}

// cardForAgent projects the store card into the agent.Card the driver
// consumes, resolving Agent via §6 (NULL → config default, late-bound).
func (m *Manager) cardForAgent(c store.Card) agent.Card {
	return agent.Card{
		ID:                 c.ID,
		Title:              c.Title,
		Description:        deref(c.Description),
		Objective:          deref(c.Objective),
		AcceptanceCriteria: deref(c.AcceptanceCriteria),
		Agent:              c.AgentOrDefault(m.cfg.Agent.Default),
	}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// watchRoot returns the card's watch scope: its codebase path if set, else the
// workspace root (ADR-001 §4.6). A vanished codebase row falls through to the
// workspace root; both missing returns an error that still classifies as
// store.IsNotFound so reconcile can finalize blind.
func (m *Manager) watchRoot(c store.Card) (string, error) {
	if c.CodebaseID != nil && *c.CodebaseID != "" {
		cb, err := store.GetCodebase(m.db, *c.CodebaseID)
		if err == nil {
			return cb.Path, nil
		}
		if !store.IsNotFound(err) {
			return "", err
		}
	}
	ws, err := store.GetWorkspace(m.db, c.WorkspaceID)
	if err != nil {
		if store.IsNotFound(err) {
			return "", fmt.Errorf("watch scope for card %s not found: %w", c.ID, err)
		}
		return "", err
	}
	return ws.RootPath, nil
}

// probeAlive waits the probe window then re-checks that the session survived
// (invariant 3). A transient missing-server error is retried once before
// declaring failure, so a one-off tmux hiccup cannot kill a booting session.
func (m *Manager) probeAlive(name string) bool {
	time.Sleep(probeDelay)
	ok, err := m.tm.HasSession(name)
	if err != nil {
		if !MissingServer(err) {
			return false
		}
		time.Sleep(probeRetryDelay)
		ok, err = m.tm.HasSession(name)
		if err != nil {
			return false
		}
	}
	return ok
}

// Attach hands the terminal to the card's session client (ADR-001 §4.1
// step 2). It does not finalize on return — completion is the poll's job.
func (m *Manager) Attach(ctx context.Context, c store.Card) error {
	name := SessionName(c.ID)
	ok, err := m.tm.HasSession(name)
	if err != nil {
		if !MissingServer(err) {
			return fmt.Errorf("session: %w", err)
		}
		ok = false
	}
	if !ok {
		return fmt.Errorf("session: card %s has no live session", c.ID)
	}
	m.tm.configureServer() // settings survive on the server; cheap idempotent ensure
	cmd := m.tm.AttachCommand(name)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("session: attach %s: %w", name, err)
	}
	return nil
}

// Kill kills the card's session and finalizes its open run's trace (ADR-001
// §4.1 step 4). Killing an already-absent session is a no-op.
func (m *Manager) Kill(ctx context.Context, c store.Card) error {
	name := SessionName(c.ID)
	err := m.tm.KillSession(name)
	if te, ok := err.(*tmuxError); ok && te.code == 1 {
		err = nil // already absent = fine
	}
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	open, err := store.OpenRuns(m.db)
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	for _, r := range open {
		if r.CardID == c.ID {
			if err := m.completeRun(r); err != nil {
				return fmt.Errorf("session: %w", err)
			}
			return nil
		}
	}
	return nil
}

// Status returns each open run's card → session state, finalizing any run
// whose session has disappeared (ADR-001 §4.1 step 3). One synchronous tick;
// the caller owns the cadence (TUI 2s poll, one-shot CLI).
func (m *Manager) Status(ctx context.Context) (map[string]SessionStatus, error) {
	states, err := m.tm.Sessions()
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(states))
	for _, s := range states {
		present[s.Name] = s.Attached
	}
	open, err := store.OpenRuns(m.db)
	if err != nil {
		return nil, err
	}
	out := make(map[string]SessionStatus)
	for _, r := range open {
		name := SessionName(r.CardID)
		attached, alive := present[name]
		if !alive {
			if err := m.completeRun(r); err != nil {
				return nil, err
			}
			continue
		}
		out[r.CardID] = SessionStatus{Running: true, Attached: attached}
	}
	return out, nil
}

// ReconcileOnStartup finalizes every open run whose session is absent
// (ADR-001 §4.1 step 5): the correctness backstop for runs that ended while
// no loom process was watching.
func (m *Manager) ReconcileOnStartup(ctx context.Context) error {
	open, err := store.OpenRuns(m.db)
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	return m.finalizeDisappeared(open)
}

// finalizeDisappeared completes each open run whose session no longer exists.
// A dead server means all sessions are gone; any other failure aborts the pass
// (idempotent — the run stays open and is retried next startup).
func (m *Manager) finalizeDisappeared(open []store.OpenRun) error {
	for _, r := range open {
		ok, err := m.tm.HasSession(SessionName(r.CardID))
		if err != nil {
			if !MissingServer(err) {
				return fmt.Errorf("session: %w", err)
			}
			ok = false
		}
		if ok {
			continue // live run: leave open
		}
		if err := m.completeRun(r); err != nil {
			return fmt.Errorf("session: %w", err)
		}
	}
	return nil
}

// completeRun is the single finalize path for status/kill/reconcile: git-
// reconcile the run's changes against its stored baseline, emit missing
// file_change rows, write trace_end (ADR-001 §4.1 step 3/5). durationMs is 0:
// OpenRun carries no start timestamp, so the value is undetermined.
func (m *Manager) completeRun(run store.OpenRun) error {
	card, err := store.GetCard(m.db, run.CardID)
	if store.IsNotFound(err) {
		return m.endRunBlind(run.RunID) // card deleted (cascade already wiped the run)
	}
	if err != nil {
		return err
	}
	root, err := m.watchRoot(card)
	if store.IsNotFound(err) {
		return m.endRunBlind(run.RunID) // scope rows gone: finalize without reconcile (decision 3)
	}
	if err != nil {
		return err
	}
	current, err := trace.SnapshotBaseline(root)
	if err != nil {
		return err
	}
	diffOut := ""
	if run.BaseHead != "" && current.BaseHead != "" {
		if d, err := trace.GitDiffNameStatus(root, run.BaseHead, current.BaseHead); err == nil {
			diffOut = d
		} // repo destroyed mid-run → over-attribute the baseline set; the run still ends
	}
	changes, err := trace.Reconcile(trace.Baseline{BaseHead: run.BaseHead, Porcelain: run.Porcelain}, current, diffOut)
	if err != nil {
		return err
	}
	live, err := m.rec.LiveChanges(run.RunID)
	if err != nil {
		return err
	}
	missing := trace.Dedup(live, changes)
	for _, c := range missing {
		// store, not recorder: Recorder.RecordChange drops when no live
		// watcher exists (reconcile-on-startup), and paths already live must
		// not be re-emitted (DESIGN-002 §10.2).
		if err := store.RecordChange(m.db, run.RunID, c.Path, c.Operation); err != nil {
			return err
		}
	}
	// filesChanged aggregates unique paths per run_id: the watcher-recorded
	// set plus the reconcile-emitted set (ADR-001 §5, DESIGN-002 §10.2).
	return m.rec.EndRun(run.RunID, 0, trace.FilesChanged(append(live, missing...)))
}

// endRunBlind writes trace_end with no reconcile for runs whose card or watch
// scope no longer exists. A run already cascaded away (EndRun can't resolve
// card_id) is treated as done.
func (m *Manager) endRunBlind(runID string) error {
	err := m.rec.EndRun(runID, 0, 0)
	if store.IsNotFound(err) {
		return nil
	}
	return err
}
