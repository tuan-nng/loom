package trace

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"loom/internal/store"
)

// openTraceDB opens a throwaway sqlite db through the real store.Open, so the
// recorder's integration with pragmas and migrations is exercised.
func openTraceDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "loom.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// seedCard seeds a minimal workspace→board→column→card tree and returns the
// card id, so trace events can resolve card_id from the run's trace_start.
func seedCard(t *testing.T, db *sql.DB) string {
	t.Helper()
	wsID, boardID, colID, cardID := store.NewID(), store.NewID(), store.NewID(), store.NewID()
	queries := []struct {
		q    string
		args []any
	}{
		{"INSERT INTO workspaces (id, name, root_path) VALUES (?, 'ws', '/tmp')", []any{wsID}},
		{"INSERT INTO boards (id, workspace_id, name) VALUES (?, ?, 'Board')", []any{boardID, wsID}},
		{"INSERT INTO columns (id, board_id, name, stage, position) VALUES (?, ?, 'To Do', 'todo', 1000)", []any{colID, boardID}},
		{"INSERT INTO cards (id, column_id, board_id, workspace_id, title) VALUES (?, ?, ?, ?, 'Card')", []any{cardID, colID, boardID, wsID}},
	}
	for _, q := range queries {
		if _, err := db.Exec(q.q, q.args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return cardID
}

// traceRows reads every row for a run, in seq order.
func traceRows(t *testing.T, db *sql.DB, runID string) []traceRow {
	t.Helper()
	rows, err := db.Query(
		"SELECT event_type, data_json FROM traces WHERE run_id = ? ORDER BY seq", runID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var out []traceRow
	for rows.Next() {
		var r traceRow
		if err := rows.Scan(&r.eventType, &r.data); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, r)
	}
	return out
}

type traceRow struct {
	eventType string
	data      string
}

func TestRecorderRoundTrip(t *testing.T) {
	db := openTraceDB(t)
	rec := NewRecorder(db)
	cardID := seedCard(t, db)

	root := t.TempDir()
	baseline := Baseline{BaseHead: strings.Repeat("a", 40), Porcelain: "?? newfile\n"}
	runID, err := rec.StartRun(cardID, root, baseline)
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if runID == "" {
		t.Fatal("StartRun returned empty runID")
	}
	if err := rec.Watch(runID, root); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if err := rec.RecordChange(runID, "a.go", store.OpModified); err != nil {
		t.Fatalf("RecordChange: %v", err)
	}
	if err := rec.RecordChange(runID, "b.go", store.OpCreated); err != nil {
		t.Fatalf("RecordChange: %v", err)
	}
	if err := rec.EndRun(runID, 1500, 2); err != nil {
		t.Fatalf("EndRun: %v", err)
	}

	rows := traceRows(t, db, runID)
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4 (start, 2 changes, end)\n%+v", len(rows), rows)
	}
	wantTypes := []string{"trace_start", "file_change", "file_change", "trace_end"}
	for i, r := range rows {
		if r.eventType != wantTypes[i] {
			t.Errorf("row %d event_type = %q, want %q", i, r.eventType, wantTypes[i])
		}
	}

	// trace_start carries the git baseline pair.
	var start struct {
		Git *struct {
			BaseHead  string `json:"base_head"`
			Porcelain string `json:"porcelain"`
		} `json:"git"`
	}
	if err := json.Unmarshal([]byte(rows[0].data), &start); err != nil {
		t.Fatalf("unmarshal trace_start: %v", err)
	}
	if start.Git == nil || start.Git.BaseHead != baseline.BaseHead || start.Git.Porcelain != baseline.Porcelain {
		t.Fatalf("trace_start git = %+v, want %+v", start.Git, baseline)
	}

	// trace_end carries duration and files_changed.
	var end struct {
		DurationMs   int `json:"duration_ms"`
		FilesChanged int `json:"files_changed"`
	}
	if err := json.Unmarshal([]byte(rows[3].data), &end); err != nil {
		t.Fatalf("unmarshal trace_end: %v", err)
	}
	if end.DurationMs != 1500 || end.FilesChanged != 2 {
		t.Fatalf("trace_end = %+v, want duration 1500 files 2", end)
	}
}

func TestStartRunNoGitKeyOutsideRepo(t *testing.T) {
	db := openTraceDB(t)
	rec := NewRecorder(db)
	cardID := seedCard(t, db)

	runID, err := rec.StartRun(cardID, t.TempDir(), Baseline{})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	rows := traceRows(t, db, runID)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 after StartRun alone", len(rows))
	}
	if strings.Contains(rows[0].data, "git") {
		t.Fatalf("trace_start data_json contains git key outside a repo: %s", rows[0].data)
	}
}

func TestStartRunGeneratesUniqueIDs(t *testing.T) {
	db := openTraceDB(t)
	rec := NewRecorder(db)
	cardID := seedCard(t, db)

	a, err := rec.StartRun(cardID, t.TempDir(), Baseline{})
	if err != nil {
		t.Fatalf("StartRun a: %v", err)
	}
	b, err := rec.StartRun(cardID, t.TempDir(), Baseline{})
	if err != nil {
		t.Fatalf("StartRun b: %v", err)
	}
	if a == b {
		t.Fatalf("StartRun returned identical runIDs %q", a)
	}
}

func TestRecordChangeInvalidOperation(t *testing.T) {
	db := openTraceDB(t)
	rec := NewRecorder(db)
	cardID := seedCard(t, db)

	runID, err := rec.StartRun(cardID, t.TempDir(), Baseline{})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := rec.Watch(runID, t.TempDir()); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if err := rec.RecordChange(runID, "a.go", "bogus"); err == nil {
		t.Fatal("RecordChange with bogus operation: want error")
	}
	if rows := traceRows(t, db, runID); len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (invalid op wrote nothing)", len(rows))
	}
}

func TestRecordChangeUnknownRunDropped(t *testing.T) {
	db := openTraceDB(t)
	rec := NewRecorder(db)
	if err := rec.RecordChange("nosuchrun", "a.go", store.OpModified); err != nil {
		t.Fatalf("RecordChange for unknown run = %v, want nil (dropped)", err)
	}
}

func TestEndRunUnknownRun(t *testing.T) {
	db := openTraceDB(t)
	rec := NewRecorder(db)
	if err := rec.EndRun("nosuchrun", 1, 1); err != sql.ErrNoRows {
		t.Fatalf("EndRun for unknown run = %v, want sql.ErrNoRows", err)
	}
}

func TestAbortRunDeletesWholeRun(t *testing.T) {
	db := openTraceDB(t)
	rec := NewRecorder(db)
	cardID := seedCard(t, db)

	runID, err := rec.StartRun(cardID, t.TempDir(), Baseline{})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := rec.Watch(runID, t.TempDir()); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if err := rec.RecordChange(runID, "a.go", store.OpModified); err != nil {
		t.Fatalf("RecordChange: %v", err)
	}
	if err := rec.EndRun(runID, 10, 1); err != nil {
		t.Fatalf("EndRun: %v", err)
	}
	if err := rec.AbortRun(runID); err != nil {
		t.Fatalf("AbortRun: %v", err)
	}
	if rows := traceRows(t, db, runID); len(rows) != 0 {
		t.Fatalf("rows after AbortRun = %d, want 0 (whole run deleted)", len(rows))
	}
	// Idempotent: aborting an already-aborted run is a no-op.
	if err := rec.AbortRun(runID); err != nil {
		t.Fatalf("second AbortRun: %v", err)
	}
}

func TestAbortRunLeavesOtherRunsUntouched(t *testing.T) {
	db := openTraceDB(t)
	rec := NewRecorder(db)
	cardID := seedCard(t, db)

	a, err := rec.StartRun(cardID, t.TempDir(), Baseline{})
	if err != nil {
		t.Fatalf("StartRun a: %v", err)
	}
	b, err := rec.StartRun(cardID, t.TempDir(), Baseline{})
	if err != nil {
		t.Fatalf("StartRun b: %v", err)
	}
	if err := rec.RecordChange(a, "a.go", store.OpModified); err != nil {
		t.Fatalf("RecordChange a: %v", err)
	}
	if err := rec.AbortRun(a); err != nil {
		t.Fatalf("AbortRun a: %v", err)
	}
	if rows := traceRows(t, db, b); len(rows) != 1 {
		t.Fatalf("run b rows = %d, want 1 (untouched)", len(rows))
	}
}

func TestLiveChanges(t *testing.T) {
	db := openTraceDB(t)
	rec := NewRecorder(db)
	cardID := seedCard(t, db)

	runID, err := rec.StartRun(cardID, t.TempDir(), Baseline{})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := rec.Watch(runID, t.TempDir()); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if err := rec.RecordChange(runID, "b.go", store.OpCreated); err != nil {
		t.Fatalf("RecordChange b: %v", err)
	}
	if err := rec.RecordChange(runID, "a.go", store.OpModified); err != nil {
		t.Fatalf("RecordChange a: %v", err)
	}
	// Same path written again: last op wins.
	if err := rec.RecordChange(runID, "a.go", store.OpCreated); err != nil {
		t.Fatalf("RecordChange a again: %v", err)
	}

	got, err := rec.LiveChanges(runID)
	if err != nil {
		t.Fatalf("LiveChanges: %v", err)
	}
	want := []Change{
		{Path: "a.go", Operation: store.OpCreated},
		{Path: "b.go", Operation: store.OpCreated},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LiveChanges = %#v, want %#v", got, want)
	}

	empty, err := rec.LiveChanges("nosuchrun")
	if err != nil {
		t.Fatalf("LiveChanges for unknown run: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("LiveChanges for unknown run = %v, want empty", empty)
	}
}

func TestSecondWatchSameRunRejected(t *testing.T) {
	db := openTraceDB(t)
	rec := NewRecorder(db)
	cardID := seedCard(t, db)

	runID, err := rec.StartRun(cardID, t.TempDir(), Baseline{})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := rec.Watch(runID, t.TempDir()); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if err := rec.Watch(runID, t.TempDir()); err == nil {
		t.Fatal("second Watch for same run: want error")
	}
}
