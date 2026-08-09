package trace

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"loom/internal/store"
)

// Recorder writes a run's trace events — trace_start, file_change,
// trace_end — through the store, and owns the run-scoped fsnotify watchers
// (ADR-001 §3.3, §4.6; DESIGN-002 §10.2). A run is one open→complete cycle
// of a card session; the recorder-generated runID scopes both the trace rows
// and the live watcher. All writes funnel through the recorder, and the store
// remains the only operation-validation point (store/traces.go).
type Recorder struct {
	db       *sql.DB
	mu       sync.Mutex          // guards watchers
	watchers map[string]*watcher // runID → live watcher; a key means the run is being recorded
}

// fileChangeData mirrors store/traces.go's unexported fileChangeData — the
// JSON shape of a file_change row's data_json. The shape parity is pinned by
// TestRecorderLiveChanges against rows the store itself wrote.
type fileChangeData struct {
	Path      string `json:"path"`
	Operation string `json:"operation"`
}

// NewRecorder returns a Recorder writing trace events to db.
func NewRecorder(db *sql.DB) *Recorder {
	return &Recorder{db: db, watchers: make(map[string]*watcher)}
}

// StartRun opens a run for cardID and returns its runID (ADR-001 §3.3,
// DESIGN-002 §10.2 step 5). It records the trace_start immediately — the
// manager's contract is that the launch probe already passed. baseline is the
// git pair captured before launch (ADR-001 §5); store.StartRun omits the git
// key when BaseHead is empty (not inside a git repo). root is accepted for
// contract parity with the manager, which passes the watch scope; Watch
// consumes it.
func (r *Recorder) StartRun(cardID, root string, baseline Baseline) (string, error) {
	runID := store.NewID()
	if err := store.StartRun(r.db, cardID, runID, baseline.BaseHead, baseline.Porcelain); err != nil {
		return "", err
	}
	return runID, nil
}

// Watch starts the live fsnotify watcher for runID over root (ADR-001 §4.6):
// recursive registration, ignore rules, on-the-fly watches for created
// directories. The run must already be started — the manager calls Watch
// immediately after StartRun. Returns an error if runID is already watched.
func (r *Recorder) Watch(runID, root string) error {
	w, err := newWatcher(r, runID, root)
	if err != nil {
		return err
	}
	r.mu.Lock()
	if _, dup := r.watchers[runID]; dup {
		r.mu.Unlock()
		_ = w.fw.Close()
		return fmt.Errorf("trace: run %s already watched", runID)
	}
	r.watchers[runID] = w
	r.mu.Unlock()
	go w.loop()
	return nil
}

// RecordChange writes one file_change event (created|modified|deleted) for
// runID (ADR-001 §3.3). operation is validated by the store — the only
// enforcement point for the opaque data_json set. A run with no live watcher
// (unknown or already stopped) is dropped silently: after the run's watcher
// stops, git reconciliation is the completion authority, so late live events
// are deliberately not written (ADR-001 §5, DESIGN-002 §10.2).
func (r *Recorder) RecordChange(runID, path, operation string) error {
	r.mu.Lock()
	_, live := r.watchers[runID]
	r.mu.Unlock()
	if !live {
		return nil
	}
	return store.RecordChange(r.db, runID, path, operation)
}

// LiveChanges returns the path→operation map of file_change events recorded
// so far for runID, deduped by path (last write wins) and sorted by path
// (ADR-001 §5 step 3). This is the "live" set handed to trace.Dedup, and its
// unique-path count feeds trace_end.files_changed. The store is the source
// of truth; no in-memory mirror is kept.
func (r *Recorder) LiveChanges(runID string) ([]Change, error) {
	rows, err := r.db.Query(
		"SELECT data_json FROM traces WHERE run_id = ? AND event_type = 'file_change' ORDER BY seq",
		runID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byPath := make(map[string]string)
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var fc fileChangeData
		if err := json.Unmarshal([]byte(data), &fc); err != nil {
			return nil, err
		}
		byPath[fc.Path] = fc.Operation
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	changes := make([]Change, 0, len(byPath))
	for path, op := range byPath {
		changes = append(changes, Change{Path: path, Operation: op})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

// EndRun stops the run's watcher, draining queued events, then writes
// trace_end (ADR-001 §3.3). Not idempotent: idx_traces_run_lifecycle permits
// at most one trace_end per (card_id, run_id), so a second call surfaces the
// store's UNIQUE error.
func (r *Recorder) EndRun(runID string, durationMs, filesChanged int) error {
	r.stopWatcher(runID)
	return store.EndRun(r.db, runID, durationMs, filesChanged)
}

// AbortRun stops the run's watcher, then deletes the whole run — every trace
// row for runID — never leaving an orphaned trace_end or file_change behind
// (DESIGN-002 §10.2 invariant 4). Idempotent: an unknown run is a no-op.
func (r *Recorder) AbortRun(runID string) error {
	r.stopWatcher(runID)
	return store.AbortRun(r.db, runID)
}

// stopWatcher removes runID from the registry and blocks until its event
// loop has drained. The watcher goroutine is never leaked: once EndRun or
// AbortRun returns, no further live events can be written for the run.
// Deadlock-free: the registry is taken under mu but w.stop() waits outside
// the lock, so a loop blocked in RecordChange never deadlocks against it.
func (r *Recorder) stopWatcher(runID string) {
	r.mu.Lock()
	w := r.watchers[runID]
	delete(r.watchers, runID)
	r.mu.Unlock()
	if w != nil {
		w.stop()
	}
}
