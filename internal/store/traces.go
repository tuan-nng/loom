package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// File-change operations recorded in a run's file_change events (ADR-001
// §3.3). They live inside opaque data_json — no schema CHECK can enforce the
// set, so the store is the only validation point.
const (
	OpCreated  = "created"
	OpModified = "modified"
	OpDeleted  = "deleted"
)

// traceStartData is the trace_start data_json. Git stays nil (and is dropped
// by omitempty) when the watch scope is not inside a git repo (ADR-001 §3.3).
type traceStartData struct {
	Git *traceGitBaseline `json:"git,omitempty"`
}

type traceGitBaseline struct {
	BaseHead  string `json:"base_head"`
	Porcelain string `json:"porcelain"`
}

type fileChangeData struct {
	Path      string `json:"path"`
	Operation string `json:"operation"`
}

type traceEndData struct {
	DurationMs   int `json:"duration_ms"`
	FilesChanged int `json:"files_changed"`
}

// marshalData serializes a recordable trace payload. Marshalling of these
// structs cannot fail; a returned error is programmer error and is surfaced
// anyway rather than silently writing a corrupt row.
func marshalData(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// errInvalidOperation rejects a RecordChange operation outside the closed
// set {created, modified, deleted}. The trace recorder derives operations
// 1:1 from fsnotify, so an invalid value is programmer error.
var errInvalidOperation = errors.New("store: trace operation must be created, modified, or deleted")

// traceCardID resolves the card_id a run's events share, looking it up from
// the run's trace_start. A missing run surfaces as sql.ErrNoRows — the store
// never records into a run that has not been started. The callers hold the
// single-collection transaction (maxopenconns=1) it runs in, so resolution
// and the follow-up insert are atomic against a concurrent AbortRun.
func traceCardID(e execer, runID string) (string, error) {
	var cardID string
	err := e.QueryRowContext(context.Background(),
		"SELECT card_id FROM traces WHERE run_id = ? AND event_type = 'trace_start'",
		runID,
	).Scan(&cardID)
	return cardID, err
}

// OpenRun is one un-finalized run as reported to reconcile-on-startup: a
// trace_start with no matching trace_end (ADR-001 §4.1 step 5).
type OpenRun struct {
	CardID    string
	RunID     string
	BaseHead  string
	Porcelain string
}

// StartRun records the trace_start of a new run for a card (ADR-001 §3.3,
// §5). baseHead and porcelain are the git baseline pair captured before the
// session launched; the git object is omitted from data_json when baseHead is
// empty (not inside a git repo). Exactly one trace_start per (card_id, run_id)
// is enforced by idx_traces_run_lifecycle; a second write is rejected.
func StartRun(db *sql.DB, cardID, runID, baseHead, porcelain string) error {
	start := traceStartData{}
	if baseHead != "" {
		start.Git = &traceGitBaseline{BaseHead: baseHead, Porcelain: porcelain}
	}
	data, err := marshalData(start)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		"INSERT INTO traces (id, card_id, run_id, event_type, data_json) VALUES (?, ?, ?, 'trace_start', ?)",
		NewID(), cardID, runID, data,
	)
	return err
}

// RecordChange records a file_change event (created|modified|deleted) for a
// run (ADR-001 §3.3). The operation set lives in opaque data_json, so it is
// validated here — the store is the only enforcement point. card_id resolves
// from the run's trace_start; a run that was never started is sql.ErrNoRows.
// Recording after trace_end is permitted: ordering of events within a settled
// run is the recorder's concern, not the store's.
func RecordChange(db *sql.DB, runID, path, operation string) error {
	if operation != OpCreated && operation != OpModified && operation != OpDeleted {
		return errInvalidOperation
	}
	data, err := marshalData(fileChangeData{Path: path, Operation: operation})
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	cardID, err := traceCardID(tx, runID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(context.Background(),
		"INSERT INTO traces (id, card_id, run_id, event_type, data_json) VALUES (?, ?, ?, 'file_change', ?)",
		NewID(), cardID, runID, data,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// EndRun closes a run with a trace_end event (ADR-001 §3.3). Note it is not
// idempotent: idx_traces_run_lifecycle permits at most one trace_end per
// (card_id, run_id), so a second call is a UNIQUE-constraint error rather than
// a silently duplicated duration row. files_changed is the run's unique-path
// count, computed by the caller.
func EndRun(db *sql.DB, runID string, durationMs, filesChanged int) error {
	data, err := marshalData(traceEndData{DurationMs: durationMs, FilesChanged: filesChanged})
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	cardID, err := traceCardID(tx, runID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(context.Background(),
		"INSERT INTO traces (id, card_id, run_id, event_type, data_json) VALUES (?, ?, ?, 'trace_end', ?)",
		NewID(), cardID, runID, data,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// OpenRuns reports every un-finalized run (a trace_start with no trace_end),
// ordered by seq, for the reconcile-on-startup pass (ADR-001 §4.1 step 5).
// Ordering is always by seq, never by timestamp. A row with corrupt data_json
// surfaces as an error rather than silently empty fields.
func OpenRuns(db *sql.DB) ([]OpenRun, error) {
	rows, err := db.Query(
		"SELECT t1.card_id, t1.run_id, t1.data_json FROM traces t1 WHERE t1.event_type = 'trace_start' AND NOT EXISTS (SELECT 1 FROM traces t2 WHERE t2.run_id = t1.run_id AND t2.event_type = 'trace_end') ORDER BY t1.seq",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []OpenRun
	for rows.Next() {
		r, err := scanOpenRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// scanOpenRun scans one trace_start row into an OpenRun, parsing the git
// baseline from data_json. BaseHead/Porcelain are empty when the git key is
// absent (not a git repo).
func scanOpenRun(row rowScanner) (OpenRun, error) {
	var r OpenRun
	var data string
	if err := row.Scan(&r.CardID, &r.RunID, &data); err != nil {
		return OpenRun{}, err
	}
	var start traceStartData
	if err := json.Unmarshal([]byte(data), &start); err != nil {
		return OpenRun{}, err
	}
	if start.Git != nil {
		r.BaseHead = start.Git.BaseHead
		r.Porcelain = start.Git.Porcelain
	}
	return r, nil
}

// AbortRun deletes the entire run — every trace row for runID — never leaving
// an orphaned trace_end or file_change behind (DESIGN-002 §10.2 invariant 4).
// Idempotent: deleting an unknown run is a no-op.
func AbortRun(db *sql.DB, runID string) error {
	_, err := db.Exec("DELETE FROM traces WHERE run_id = ?", runID)
	return err
}

// RecentRun is one finalized run as shown in `loom status`: the card title
// plus the run's duration and files-changed totals (ADR-001 §6).
type RecentRun struct {
	CardID       string
	Title        string
	DurationMs   int
	FilesChanged int
}

// RecentRuns returns the limit most-recently ended runs, newest first. The
// card_id is denormalized onto trace_end, so no trace_start join is needed;
// cards cascade with their traces, so the JOIN can never miss. Ordering is by
// seq, the only ordering key (ADR-001 §3.3). limit <= 0 returns (nil, nil).
// Corrupt trace_end data_json surfaces as an error rather than silent zeros.
func RecentRuns(db *sql.DB, limit int) ([]RecentRun, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := db.Query(
		"SELECT t.card_id, c.title, t.data_json FROM traces t JOIN cards c ON c.id = t.card_id WHERE t.event_type = 'trace_end' ORDER BY t.seq DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []RecentRun
	for rows.Next() {
		r, err := scanRecentRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func scanRecentRun(row rowScanner) (RecentRun, error) {
	var r RecentRun
	var data string
	if err := row.Scan(&r.CardID, &r.Title, &data); err != nil {
		return RecentRun{}, err
	}
	var end traceEndData
	if err := json.Unmarshal([]byte(data), &end); err != nil {
		return RecentRun{}, err
	}
	r.DurationMs = end.DurationMs
	r.FilesChanged = end.FilesChanged
	return r, nil
}
