package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// newTraceRun opens a store with a card and a started run. baseHead is a
// fixed 40-hex sha so callers don't have to spell one; the returned runID is
// generated with the same shared generator the recorder uses.
func newTraceRun(t *testing.T) (*sql.DB, string, string) {
	t.Helper()
	db, _, _, dev, _ := newCardTree(t)
	card := mustCard(t)(CreateCard(db, CardInput{ColumnID: dev.ID, Title: "Trace Card"}))
	runID := NewID()
	if err := StartRun(db, card.ID, runID, strings.Repeat("a", 40), " M file.txt\n?? new.go\n"); err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	return db, card.ID, runID
}

func countTraces(t *testing.T, db *sql.DB, runID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM traces WHERE run_id = ?", runID).Scan(&n); err != nil {
		t.Fatalf("count traces: %v", err)
	}
	return n
}

func TestRunRoundTrip(t *testing.T) {
	db, _, runID := newTraceRun(t)
	if err := RecordChange(db, runID, "a.txt", OpModified); err != nil {
		t.Fatalf("RecordChange: %v", err)
	}
	if err := RecordChange(db, runID, "b.go", OpCreated); err != nil {
		t.Fatalf("RecordChange: %v", err)
	}
	if err := EndRun(db, runID, 1500, 2); err != nil {
		t.Fatalf("EndRun: %v", err)
	}

	rows, err := db.Query("SELECT event_type, card_id, run_id, data_json FROM traces WHERE run_id = ? ORDER BY seq", runID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	type row struct {
		eventType string
		cardID    string
		runID     string
		data      string
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.eventType, &r.cardID, &r.runID, &r.data); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("rows = %d, want 4", len(got))
	}
	wantTypes := []string{"trace_start", "file_change", "file_change", "trace_end"}
	for i, w := range wantTypes {
		if got[i].eventType != w {
			t.Errorf("row %d type = %q, want %q", i, got[i].eventType, w)
		}
	}

	var start traceStartData
	if err := json.Unmarshal([]byte(got[0].data), &start); err != nil {
		t.Fatalf("trace_start data_json: %v", err)
	}
	if start.Git == nil || start.Git.BaseHead != strings.Repeat("a", 40) || start.Git.Porcelain != " M file.txt\n?? new.go\n" {
		t.Errorf("trace_start git = %+v, want baseline pair", start.Git)
	}

	var change fileChangeData
	if err := json.Unmarshal([]byte(got[1].data), &change); err != nil {
		t.Fatalf("file_change data_json: %v", err)
	}
	if change.Path != "a.txt" || change.Operation != OpModified {
		t.Errorf("file_change = %+v, want a.txt/modified", change)
	}

	var end traceEndData
	if err := json.Unmarshal([]byte(got[3].data), &end); err != nil {
		t.Fatalf("trace_end data_json: %v", err)
	}
	if end.DurationMs != 1500 || end.FilesChanged != 2 {
		t.Errorf("trace_end = %+v, want 1500/2", end)
	}
}

func TestStartRunDuplicateRejected(t *testing.T) {
	db, cardID, runID := newTraceRun(t)
	err := StartRun(db, cardID, runID, "", "")
	if err == nil || !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		t.Fatalf("second StartRun: err = %v, want UNIQUE constraint failed", err)
	}
	if n := countTraces(t, db, runID); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
}

func TestStartRunOmitsGit(t *testing.T) {
	db, cardID, _ := newTraceRun(t)
	runID2 := NewID()
	if err := StartRun(db, cardID, runID2, "", ""); err != nil {
		t.Fatalf("StartRun git-less: %v", err)
	}
	var data string
	if err := db.QueryRow("SELECT data_json FROM traces WHERE run_id = ? AND event_type = 'trace_start'", runID2).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		t.Fatalf("data_json: %v", err)
	}
	if _, ok := m["git"]; ok {
		t.Errorf("data_json = %s, want git key absent", data)
	}
	if data != "{}" {
		t.Errorf("data_json = %q, want %q", data, "{}")
	}
}

func TestStartRunUnknownCard(t *testing.T) {
	db := openTest(t)
	err := StartRun(db, NewID(), NewID(), "", "")
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
		t.Fatalf("StartRun unknown card: err = %v, want FOREIGN KEY constraint failed", err)
	}
}

func TestRecordChangeOperations(t *testing.T) {
	db, _, runID := newTraceRun(t)

	tests := []struct {
		op   string
		path string
	}{
		{OpCreated, "new.go"},
		{OpModified, "dir with space/ünïcode.txt"},
		{OpDeleted, "old.txt"},
	}
	for _, tt := range tests {
		if err := RecordChange(db, runID, tt.path, tt.op); err != nil {
			t.Fatalf("RecordChange %s: %v", tt.op, err)
		}
	}

	rows, err := db.Query("SELECT data_json FROM traces WHERE run_id = ? AND event_type = 'file_change' ORDER BY seq", runID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := make(map[string]string)
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			t.Fatal(err)
		}
		var fc fileChangeData
		if err := json.Unmarshal([]byte(data), &fc); err != nil {
			t.Fatal(err)
		}
		got[fc.Operation] = fc.Path
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		if got[tt.op] != tt.path {
			t.Errorf("op %s path = %q, want %q", tt.op, got[tt.op], tt.path)
		}
	}
}

func TestRecordChangeInvalidOperation(t *testing.T) {
	db, _, runID := newTraceRun(t)
	err := RecordChange(db, runID, "x.txt", "bogus")
	if !errors.Is(err, errInvalidOperation) {
		t.Fatalf("RecordChange bogus: err = %v, want errInvalidOperation", err)
	}
	if n := countTraces(t, db, runID); n != 1 {
		t.Errorf("rows = %d, want 1 (no change written)", n)
	}
}

func TestRecordChangeUnknownRun(t *testing.T) {
	db := openTest(t)
	err := RecordChange(db, NewID(), "x.txt", OpCreated)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("RecordChange unknown run: err = %v, want sql.ErrNoRows", err)
	}
	var n int
	if err := db.QueryRow("SELECT count(*) FROM traces").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

func TestEndRunRoundTrip(t *testing.T) {
	db, _, runID := newTraceRun(t)
	if err := EndRun(db, runID, 2500, 3); err != nil {
		t.Fatalf("EndRun: %v", err)
	}
	var data string
	if err := db.QueryRow("SELECT data_json FROM traces WHERE run_id = ? AND event_type = 'trace_end'", runID).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var end traceEndData
	if err := json.Unmarshal([]byte(data), &end); err != nil {
		t.Fatal(err)
	}
	if end.DurationMs != 2500 || end.FilesChanged != 3 {
		t.Errorf("trace_end = %+v, want 2500/3", end)
	}
}

func TestEndRunUnknownRun(t *testing.T) {
	db := openTest(t)
	err := EndRun(db, NewID(), 0, 0)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("EndRun unknown run: err = %v, want sql.ErrNoRows", err)
	}
}

func TestEndRunTwiceRejected(t *testing.T) {
	db, _, runID := newTraceRun(t)
	if err := EndRun(db, runID, 1, 0); err != nil {
		t.Fatalf("first EndRun: %v", err)
	}
	err := EndRun(db, runID, 2, 0)
	if err == nil || !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		t.Fatalf("second EndRun: err = %v, want UNIQUE constraint failed", err)
	}
}

func TestSeqTotalOrder(t *testing.T) {
	db, _, runID := newTraceRun(t)
	for _, p := range []string{"a", "b", "c"} {
		if err := RecordChange(db, runID, p, OpModified); err != nil {
			t.Fatal(err)
		}
	}
	if err := EndRun(db, runID, 0, 3); err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query("SELECT seq FROM traces WHERE run_id = ? ORDER BY seq", runID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var seqs []int64
	for rows.Next() {
		var s int64
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for i, s := range seqs {
		if int64(i+1) != s {
			t.Errorf("seq[%d] = %d, want %d", i, s, i+1)
		}
	}
}

func TestSeqBeatsTimestamp(t *testing.T) {
	db, cardID, first := newTraceRun(t)
	second := NewID()
	if err := StartRun(db, cardID, second, "", ""); err != nil {
		t.Fatal(err)
	}

	// Invert the timestamps so a timestamp ordering would return the second
	// run first. OpenRuns must still return runs in seq order: this test
	// fails if OpenRuns ever regresses to ORDER BY created_at.
	if _, err := db.Exec("UPDATE traces SET created_at = '2030-01-01T00:00:00.000' WHERE run_id = ?", second); err != nil {
		t.Fatal(err)
	}

	runs, err := OpenRuns(db)
	if err != nil {
		t.Fatalf("OpenRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("open runs = %d, want 2", len(runs))
	}
	if runs[0].RunID != first {
		t.Errorf("runs[0].RunID = %s, want %s (started first, by seq)", runs[0].RunID, first)
	}
	if runs[1].RunID != second {
		t.Errorf("runs[1].RunID = %s, want %s", runs[1].RunID, second)
	}
}

// TestSeqSurvivesVacuum proves AUTOINCREMENT protects seq across a VACUUM:
// surviving seqs are non-contiguous (1,3) after an interleaved row is
// deleted, so a renumbering pass over a bare rowid would visibly change them.
func TestSeqSurvivesVacuum(t *testing.T) {
	db, cardID, runID := newTraceRun(t)
	other := NewID()
	if err := StartRun(db, cardID, other, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := RecordChange(db, runID, "a", OpModified); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM traces WHERE run_id = ?", other); err != nil {
		t.Fatal(err)
	}

	var before []int64
	rows, err := db.Query("SELECT seq FROM traces WHERE run_id = ? ORDER BY seq", runID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s int64
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		before = append(before, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()

	if _, err := db.Exec("VACUUM"); err != nil {
		t.Fatal(err)
	}

	var after []int64
	rows, err = db.Query("SELECT seq FROM traces WHERE run_id = ? ORDER BY seq", runID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s int64
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		after = append(after, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()

	if len(before) != len(after) {
		t.Fatalf("len before=%d after=%d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("seq[%d] = %d after VACUUM, want %d", i, after[i], before[i])
		}
	}
}

func TestAbortRunDeletesWholeRun(t *testing.T) {
	db, cardID, runID := newTraceRun(t)
	if err := RecordChange(db, runID, "a", OpModified); err != nil {
		t.Fatal(err)
	}
	other := NewID()
	if err := StartRun(db, cardID, other, "", ""); err != nil {
		t.Fatalf("unrelated run: %v", err)
	}

	if err := AbortRun(db, runID); err != nil {
		t.Fatalf("AbortRun: %v", err)
	}
	if n := countTraces(t, db, runID); n != 0 {
		t.Errorf("aborted run rows = %d, want 0", n)
	}
	if n := countTraces(t, db, other); n != 1 {
		t.Errorf("unrelated run rows = %d, want 1", n)
	}
	if err := AbortRun(db, runID); err != nil {
		t.Errorf("second AbortRun: %v, want no-op", err)
	}
}

func TestOpenRunsOnlyUnfinalized(t *testing.T) {
	db, cardID, runID := newTraceRun(t)

	finished := NewID()
	if err := StartRun(db, cardID, finished, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := EndRun(db, finished, 1, 0); err != nil {
		t.Fatal(err)
	}

	open := NewID()
	if err := StartRun(db, cardID, open, strings.Repeat("b", 40), "?? x\n"); err != nil {
		t.Fatal(err)
	}

	gitless := NewID()
	if err := StartRun(db, cardID, gitless, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := EndRun(db, gitless, 2, 0); err != nil {
		t.Fatal(err)
	}

	runs, err := OpenRuns(db)
	if err != nil {
		t.Fatalf("OpenRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("open runs = %d, want 2", len(runs))
	}
	if runs[0].RunID != runID || runs[0].BaseHead != strings.Repeat("a", 40) {
		t.Errorf("runs[0] = %+v, want runID %s with baseline", runs[0], runID)
	}
	if runs[1].RunID != open || runs[1].CardID != cardID || runs[1].BaseHead != strings.Repeat("b", 40) || runs[1].Porcelain != "?? x\n" {
		t.Errorf("runs[1] = %+v", runs[1])
	}
}

func TestOpenRunsCorruptDataJSON(t *testing.T) {
	db, cardID, _ := newTraceRun(t)
	if _, err := db.Exec(
		"INSERT INTO traces (id, card_id, run_id, event_type, data_json) VALUES (?, ?, ?, 'trace_start', 'not-json')",
		NewID(), cardID, NewID(),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRuns(db); err == nil {
		t.Error("OpenRuns with corrupt data_json succeeded, want error")
	}
}

func TestRecentRunsNewestFirst(t *testing.T) {
	db, cardID, _ := newTraceRun(t)
	older := NewID()
	if err := StartRun(db, cardID, older, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := EndRun(db, older, 1200, 3); err != nil {
		t.Fatal(err)
	}
	newer := NewID()
	if err := StartRun(db, cardID, newer, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := EndRun(db, newer, 30000, 8); err != nil {
		t.Fatal(err)
	}

	runs, err := RecentRuns(db, 5)
	if err != nil {
		t.Fatalf("RecentRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("recent runs = %d, want 2", len(runs))
	}
	if runs[0].Title != "Trace Card" || runs[0].DurationMs != 30000 || runs[0].FilesChanged != 8 {
		t.Errorf("runs[0] = %+v, want newest run first", runs[0])
	}
	if runs[1].DurationMs != 1200 || runs[1].FilesChanged != 3 {
		t.Errorf("runs[1] = %+v", runs[1])
	}
}

func TestRecentRunsLimit(t *testing.T) {
	db, cardID, _ := newTraceRun(t)
	for i := 0; i < 3; i++ {
		runID := NewID()
		if err := StartRun(db, cardID, runID, "", ""); err != nil {
			t.Fatal(err)
		}
		if err := EndRun(db, runID, 1, 0); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := RecentRuns(db, 2)
	if err != nil {
		t.Fatalf("RecentRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("recent runs = %d, want 2 (limited)", len(runs))
	}
}

func TestRecentRunsEmptyAndZeroLimit(t *testing.T) {
	db, _, _ := newTraceRun(t)
	runs, err := RecentRuns(db, 5)
	if err != nil {
		t.Fatalf("RecentRuns: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("recent runs = %d, want 0 (no trace_end yet)", len(runs))
	}
	runs, err = RecentRuns(db, 0)
	if err != nil {
		t.Fatalf("RecentRuns(0): %v", err)
	}
	if runs != nil {
		t.Fatalf("RecentRuns(0) = %v, want nil", runs)
	}
}

func TestRecentRunsCorruptDataJSON(t *testing.T) {
	db, cardID, _ := newTraceRun(t)
	if _, err := db.Exec(
		"INSERT INTO traces (id, card_id, run_id, event_type, data_json) VALUES (?, ?, ?, 'trace_end', 'not-json')",
		NewID(), cardID, NewID(),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := RecentRuns(db, 5); err == nil {
		t.Error("RecentRuns with corrupt trace_end data_json succeeded, want error")
	}
}
