package trace

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// waitFor polls cond every 10ms until it is true or the timeout elapses.
// fsnotify is asynchronous, so every positive assertion reads the DB through
// this gate rather than a fixed sleep.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// countChanges returns the number of file_change rows for a run.
func countChanges(t *testing.T, db *sql.DB, runID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		"SELECT count(*) FROM traces WHERE run_id = ? AND event_type = 'file_change'", runID,
	).Scan(&n); err != nil {
		t.Fatalf("count changes: %v", err)
	}
	return n
}

// changePaths returns the scope-relative paths of a run's file_change rows.
func changePaths(t *testing.T, db *sql.DB, runID string) map[string]struct{} {
	t.Helper()
	rows, err := db.Query(
		"SELECT data_json FROM traces WHERE run_id = ? AND event_type = 'file_change'", runID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	paths := make(map[string]struct{})
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			t.Fatalf("scan: %v", err)
		}
		var fc struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(data), &fc); err != nil {
			t.Fatalf("unmarshal %s: %v", data, err)
		}
		paths[fc.Path] = struct{}{}
	}
	return paths
}

// newWatchedRun builds a root with the given directories and files, starts a
// run for a seeded card, and returns the recorder, runID, and root.
func newWatchedRun(t *testing.T, dirs, files []string) (*Recorder, string, string) {
	t.Helper()
	db := openTraceDB(t)
	rec := NewRecorder(db)
	cardID := seedCard(t, db)

	root := t.TempDir()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	for _, f := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, f)), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(f), err)
		}
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	runID, err := rec.StartRun(cardID, root, Baseline{})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := rec.Watch(runID, root); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	return rec, runID, root
}

// waitChange is waitFor specialized to a run's recorded paths.
func waitChange(t *testing.T, db *sql.DB, runID, path string) {
	t.Helper()
	waitFor(t, 3*time.Second, "file_change for "+path, func() bool {
		p := changePaths(t, db, runID)
		_, ok := p[path]
		return ok
	})
}

// ackDirWatch proves a directory's Create event was handled (and its
// on-the-fly watch registered) by writing a marker into the already-watched
// parent and waiting for its row. fsnotify delivers events for one watch in
// generation order, so the mkdir's Create is queued before the marker's.
func ackDirWatch(t *testing.T, rec *Recorder, runID, parent, marker string) {
	t.Helper()
	markerPath := filepath.Join(parent, marker)
	if err := os.WriteFile(markerPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write ack marker: %v", err)
	}
	waitChange(t, rec.db, runID, marker)
}

func TestWatchRecordsRelativeLiveEvents(t *testing.T) {
	db := openTraceDB(t)
	rec := NewRecorder(db)
	cardID := seedCard(t, db)
	root := t.TempDir()

	for _, d := range []string{"keep", "node_modules", ".git", "dist", "target"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "keep", "a.txt"), []byte("one"), 0o644); err != nil {
		t.Fatalf("write keep/a.txt: %v", err)
	}

	runID, err := rec.StartRun(cardID, root, Baseline{})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := rec.Watch(runID, root); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Write to a watched file -> a modified row, path relative to scope root.
	if err := os.WriteFile(filepath.Join(root, "keep", "a.txt"), []byte("two"), 0o644); err != nil {
		t.Fatalf("write keep/a.txt: %v", err)
	}
	waitFor(t, 3*time.Second, "modified keep/a.txt", func() bool {
		return countChanges(t, db, runID) >= 1
	})
	paths := changePaths(t, db, runID)
	if _, ok := paths["keep/a.txt"]; !ok {
		t.Fatalf("recorded paths = %q, want keep/a.txt (scope-relative)", paths)
	}

	// Create -> created. Remove -> deleted.
	if err := os.WriteFile(filepath.Join(root, "keep", "new.go"), []byte("go"), 0o644); err != nil {
		t.Fatalf("write keep/new.go: %v", err)
	}
	waitChange(t, db, runID, "keep/new.go")
	if err := os.Remove(filepath.Join(root, "keep", "a.txt")); err != nil {
		t.Fatalf("remove keep/a.txt: %v", err)
	}
	waitFor(t, 3*time.Second, "removed keep/a.txt", func() bool {
		p := changePaths(t, db, runID)
		_, ok := p["keep/a.txt"]
		return ok
	})
}

func TestWatchIgnoresBuiltinDirs(t *testing.T) {
	db := openTraceDB(t)
	rec := NewRecorder(db)
	cardID := seedCard(t, db)
	root := t.TempDir()

	for _, d := range []string{"node_modules", ".git", "dist", "target", "build", "vendor", ".venv", "__pycache__"} {
		if err := os.MkdirAll(filepath.Join(root, d, "sub"), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "keep"), 0o755); err != nil {
		t.Fatalf("mkdir keep: %v", err)
	}

	runID, err := rec.StartRun(cardID, root, Baseline{})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := rec.Watch(runID, root); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Writing inside every builtin dir must record nothing. The dirs were
	// never watched, so this is race-free: no inotify watch means no event.
	for _, d := range []string{"node_modules", ".git", "dist", "target", "build", "vendor", ".venv", "__pycache__"} {
		if err := os.WriteFile(filepath.Join(root, d, "sub", "x.go"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", d, err)
		}
	}
	// A marker in the watched scope proves we let events settle first.
	if err := os.WriteFile(filepath.Join(root, "keep", "marker.go"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	paths := changePaths(t, db, runID)
	waitFor(t, 3*time.Second, "marker.go recorded", func() bool {
		_, ok := changePaths(t, db, runID)["keep/marker.go"]
		return ok
	})
	paths = changePaths(t, db, runID)
	for _, d := range []string{"node_modules", ".git", "dist", "target", "build", "vendor", ".venv", "__pycache__"} {
		if _, ok := paths[d+"/sub/x.go"]; ok {
			t.Fatalf("recorded change for builtin-ignored %s/%s", d, "sub/x.go")
		}
	}
}

func TestWatchLoomignore(t *testing.T) {
	db := openTraceDB(t)
	rec := NewRecorder(db)
	cardID := seedCard(t, db)
	root := t.TempDir()

	for _, d := range []string{"src"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	// .loomignore at the scope root is honored. keep.log is re-included by
	// its negation; src/x.log stays dropped.
	loomignore := "# build artifacts\n*.log\n!keep.log\n"
	if err := os.WriteFile(filepath.Join(root, ".loomignore"), []byte(loomignore), 0o644); err != nil {
		t.Fatalf("write .loomignore: %v", err)
	}

	runID, err := rec.StartRun(cardID, root, Baseline{})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := rec.Watch(runID, root); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Dropped file: watch exists, event fires, but the matcher drops it. The
	// marker in the same watched dir proves the drop decision was made.
	if err := os.WriteFile(filepath.Join(root, "src", "x.log"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write src/x.log: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "marker.go"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write src/marker.go: %v", err)
	}
	waitChange(t, db, runID, "src/marker.go")

	// Re-included file records; the dropped file stays absent.
	if err := os.WriteFile(filepath.Join(root, "keep.log"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write keep.log: %v", err)
	}
	waitChange(t, db, runID, "keep.log")

	p := changePaths(t, db, runID)
	if _, ok := p["src/x.log"]; ok {
		t.Fatalf("recorded change for .loomignore-dropped src/x.log")
	}
	if _, ok := p["src/marker.go"]; !ok {
		t.Fatalf("missing src/marker.go")
	}
}

func TestWatchPicksUpNewDirs(t *testing.T) {
	db := openTraceDB(t)
	rec := NewRecorder(db)
	cardID := seedCard(t, db)
	root := t.TempDir()

	runID, err := rec.StartRun(cardID, root, Baseline{})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := rec.Watch(runID, root); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// A directory created mid-session gets a watch on the fly. fsnotify
	// delivers the mkdir's Create and the ack marker's Create (both on the
	// root watch) in generation order, so once the marker row lands the
	// mkdir's Create has been handled and `newdir` is watched.
	if err := os.MkdirAll(filepath.Join(root, "newdir"), 0o755); err != nil {
		t.Fatalf("mkdir newdir: %v", err)
	}
	ackDirWatch(t, rec, runID, root, "ack-mkdir-"+runID)
	if err := os.WriteFile(filepath.Join(root, "newdir", "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write newdir/f.txt: %v", err)
	}
	waitChange(t, db, runID, "newdir/f.txt")
}

func TestWatchNewIgnoredDirMidSession(t *testing.T) {
	db := openTraceDB(t)
	rec := NewRecorder(db)
	cardID := seedCard(t, db)
	root := t.TempDir()

	runID, err := rec.StartRun(cardID, root, Baseline{})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := rec.Watch(runID, root); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// A builtin-ignored dir created mid-session must not be watched. Write
	// into it, then a root marker to establish that the Create was processed.
	if err := os.MkdirAll(filepath.Join(root, ".venv"), 0o755); err != nil {
		t.Fatalf("mkdir .venv: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".venv", "py"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write .venv/py: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "marker.go"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	waitChange(t, db, runID, "marker.go")
	p := changePaths(t, db, runID)
	if _, ok := p[".venv/py"]; ok {
		t.Fatalf("recorded change for mid-session ignored dir .venv/py")
	}
}

func TestWatchStopStopsRecording(t *testing.T) {
	db := openTraceDB(t)
	rec := NewRecorder(db)
	cardID := seedCard(t, db)
	root := t.TempDir()

	runID, err := rec.StartRun(cardID, root, Baseline{})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := rec.Watch(runID, root); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write a.go: %v", err)
	}
	waitChange(t, db, runID, "a.go")

	// Snapshot the deduped path set. A new-file write fires both a Create and
	// a Write event, so raw row counts are mid-stream — the set is stable.
	before := changePaths(t, db, runID)
	if _, ok := before["a.go"]; !ok {
		t.Fatalf("missing a.go in %v", before)
	}
	if err := rec.EndRun(runID, 100, len(before)); err != nil {
		t.Fatalf("EndRun: %v", err)
	}
	// After EndRun returns, the watcher is stopped (kernel-close); a later
	// write must not produce a row. EndRun's drain may re-insert a.go from a
	// queued Write, so compare path sets, not counts. Deterministic — no sleep.
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write b.go: %v", err)
	}
	after := changePaths(t, db, runID)
	if _, ok := after["b.go"]; ok {
		t.Fatalf("file_change for b.go after EndRun: %v", after)
	}
	if _, ok := after["a.go"]; !ok {
		t.Fatalf("a.go dropped from %v", after)
	}
}

func TestAbortRunStopsWatcher(t *testing.T) {
	db := openTraceDB(t)
	rec := NewRecorder(db)
	cardID := seedCard(t, db)
	root := t.TempDir()

	runID, err := rec.StartRun(cardID, root, Baseline{})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := rec.Watch(runID, root); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write a.go: %v", err)
	}
	waitChange(t, db, runID, "a.go")

	if err := rec.AbortRun(runID); err != nil {
		t.Fatalf("AbortRun: %v", err)
	}
	// AbortRun deletes the whole run AND stops the watcher: even a fresh
	// write produces no rows.
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write b.go: %v", err)
	}
	if n := countChanges(t, db, runID); n != 0 {
		t.Fatalf("file_changes after AbortRun = %d, want 0", n)
	}
}
