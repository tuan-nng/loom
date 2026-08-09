package trace

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"

	"loom/internal/store"
)

// watcher is one run's live file-change recorder: a single fsnotify.Watcher
// registered over every non-ignored directory of the watch scope, with
// on-the-fly registration for directories created mid-run (ADR-001 §4.6).
// fsnotify does not watch recursively, so registration is an explicit walk.
//
// The event→operation mapping is deliberately conservative. Directory events
// and Chmod never emit file_change rows: the reconcile set only names files
// (ADR-001 §5), so a dir row could dedup against nothing; and a spurious
// Chmod-derived "modified" for a path would suppress a real reconcile row
// through path-keyed Dedup (under-attribution, the failure ADR-001 §5 calls
// worse). Live recording under-reports on purpose; git reconciliation is the
// completion authority and over-attributes instead.
type watcher struct {
	rec      *Recorder
	runID    string
	root     string // absolute, cleaned watch-scope root
	matcher  *ignoreMatcher
	fw       *fsnotify.Watcher
	dirs     map[string]struct{} // watched directory paths, for dir-remove tracking
	stopCh   chan struct{}       // closed to request stop
	stopOnce sync.Once
	done     chan struct{} // closed when the event loop has exited
}

// newWatcher opens the fsnotify instance, loads .loomignore from the scope
// root (missing file → empty matcher, ADR-001 §4.6 rule 2), and registers
// watches over every non-ignored directory under root. It does not start the
// event loop — the caller registers the watcher in the run's registry and
// calls loop() in a goroutine.
func newWatcher(rec *Recorder, runID, root string) (*watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &watcher{
		rec:    rec,
		runID:  runID,
		root:   filepath.Clean(root),
		fw:     fw,
		dirs:   make(map[string]struct{}),
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}
	if m, err := loadLOOMIGNORE(root); err != nil {
		_ = fw.Close()
		return nil, err
	} else {
		w.matcher = m
	}
	if err := w.walk(root, "."); err != nil {
		_ = fw.Close()
		return nil, err
	}
	return w, nil
}

// loadLOOMIGNORE reads .loomignore at the top of the scope root; a missing
// file is the empty matcher, an unreadable file is an error (ADR-001 §4.6
// rule 2).
func loadLOOMIGNORE(root string) (*ignoreMatcher, error) {
	b, err := os.ReadFile(filepath.Join(root, ".loomignore"))
	if err != nil {
		if os.IsNotExist(err) {
			return newIgnoreMatcher(nil), nil
		}
		return nil, err
	}
	return newIgnoreMatcher(strings.Split(string(b), "\n")), nil
}

// walk registers a watch on dir (recording it in w.dirs) and recurses into
// every non-ignored subdirectory. rel is the scope-relative path used for
// ignore matching; a directory matched by the built-ins or .loomignore is
// skipped and not descended into (ADR-001 §4.6). fsnotify Add failures are
// skipped silently — a scope this large is beyond the inotify watch limit,
// and git reconciliation remains the completion authority. Called from
// newWatcher and from Create-dir events, both on the same goroutine, so
// w.dirs needs no locking.
func (w *watcher) walk(dir, rel string) error {
	if err := w.fw.Add(dir); err != nil {
		return nil
	}
	w.dirs[dir] = struct{}{}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		childRel := path.Join(rel, e.Name())
		if ignoredPath(w.matcher, childRel, true) {
			continue
		}
		w.walk(filepath.Join(dir, e.Name()), childRel)
	}
	return nil
}

// loop consumes the fsnotify channels until the watcher is closed. On stop it
// drains any events already queued before exiting, so pending events are
// never dropped and later-started watchers never observe a gap the recorder
// would misattribute.
func (w *watcher) loop() {
	defer close(w.done)
	for {
		select {
		case <-w.stopCh:
			return
		case ev, ok := <-w.fw.Events:
			if !ok {
				return
			}
			w.handle(ev)
		case _, ok := <-w.fw.Errors:
			if !ok {
				return
			}
			// A transient fsnotify error (misattribute risk, or a backend
			// hiccup) is out of the watcher's hands; git reconciliation at
			// completion is the fallback.
		}
	}
}

// stop closes the fsnotify watcher and blocks until the loop has exited.
// Safe to call before the loop runs (e.g. newWatcher error paths) and
// idempotent via stopOnce.
func (w *watcher) stop() {
	w.stopOnce.Do(func() {
		close(w.stopCh)
		_ = w.fw.Close()
	})
	<-w.done
}

// handle classifies one fsnotify event into a scope-relative (path, op) row
// or an on-the-fly directory watch registration. It never emits rows for
// directories or chmod; Created directories are watched immediately.
func (w *watcher) handle(ev fsnotify.Event) {
	if ev.Op == 0 {
		return
	}
	rel, err := filepath.Rel(w.root, ev.Name)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return // outside the watch scope — cannot happen, guard anyway
	}
	rel = filepath.ToSlash(rel)

	// For Remove/Rename the path is gone, so Lstat cannot classify it; the
	// tracked watch set (w.dirs, maintained by walk) is the authority. Events
	// on dirs that still exist are caught by the isDir branch below.
	info, err := os.Lstat(ev.Name)
	isDir := err == nil && info.IsDir()

	switch {
	case ev.Has(fsnotify.Create):
		if isDir {
			if ignoredPath(w.matcher, rel, true) {
				return
			}
			w.walk(ev.Name, rel)
			return
		}
		if ignoredPath(w.matcher, rel, false) {
			return
		}
		w.rec.RecordChange(w.runID, rel, store.OpCreated)
	case ev.Has(fsnotify.Chmod):
		// Spurious (write-setattr, touch); never a row — see the package
		// comment on the under-attribution rationale.
	case isDir:
		// Write/Remove/Rename on a watched directory that still exists: no
		// file_change row — the reconcile set only names files.
	case ev.Has(fsnotify.Rename), ev.Has(fsnotify.Remove):
		// The path is gone; classify by the watch set instead of Lstat. A
		// removed or renamed directory emits no row (its watch just died;
		// its replacement is re-walked via its own Create event). w.dirs is
		// only touched by walk and handle, both on the loop goroutine.
		if _, wasDir := w.dirs[ev.Name]; wasDir {
			delete(w.dirs, ev.Name)
			return
		}
		if ignoredPath(w.matcher, rel, false) {
			return
		}
		// fsnotify gives no destination in one event; the dest arrives as its
		// own Create. The old name is recorded modified — the op letter is
		// informational since Dedup is path-keyed and op-insensitive.
		if ev.Has(fsnotify.Rename) {
			w.rec.RecordChange(w.runID, rel, store.OpModified)
		} else {
			w.rec.RecordChange(w.runID, rel, store.OpDeleted)
		}
	case ev.Has(fsnotify.Write):
		if ignoredPath(w.matcher, rel, false) {
			return
		}
		w.rec.RecordChange(w.runID, rel, store.OpModified)
	}
}
