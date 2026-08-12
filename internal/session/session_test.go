package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"loom/internal/agent"
	"loom/internal/config"
	"loom/internal/store"
	"loom/internal/trace"
)

// openSessionDB opens a throwaway migration-created db for one test.
func openSessionDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "loom.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// seedCardAt creates the workspace→board→column→card chain with root as the
// workspace root path (the watch scope) and returns the card.
func seedCardAt(t *testing.T, db *sql.DB, root string) store.Card {
	t.Helper()
	ws, err := store.CreateWorkspace(db, "ws", root)
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	board, err := store.CreateBoard(db, ws.ID, "Board")
	if err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}
	col, err := store.CreateColumn(db, board.ID, "To Do", "todo")
	if err != nil {
		t.Fatalf("CreateColumn: %v", err)
	}
	card, err := store.CreateCard(db, store.CardInput{ColumnID: col.ID, Title: "Card"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	return card
}

func seedCard(t *testing.T, db *sql.DB) store.Card {
	return seedCardAt(t, db, t.TempDir())
}

// seedCardWithTitleAgent is seedCardAt plus a caller-chosen title (to prove
// argv quoting round-trips embedded quotes/spaces) and agent (nil → default).
func seedCardWithTitleAgent(t *testing.T, db *sql.DB, root, title string, agentName *string) store.Card {
	t.Helper()
	ws, err := store.CreateWorkspace(db, "ws", root)
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	board, err := store.CreateBoard(db, ws.ID, "Board")
	if err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}
	col, err := store.CreateColumn(db, board.ID, "To Do", "todo")
	if err != nil {
		t.Fatalf("CreateColumn: %v", err)
	}
	card, err := store.CreateCard(db, store.CardInput{ColumnID: col.ID, Title: title, Agent: agentName})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	return card
}

// writeStub places an executable script named name in a fresh stub dir and
// prepends that dir to PATH so exec.LookPath (driver Resolve) finds it.
func writeStub(t *testing.T, name, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// fixture assembles a real tmux client on the isolated self-test server, a
// manager wired to db, and a claude binary resolved through a PATH stub.
type fixture struct {
	t  *testing.T
	m  *Manager
	tm Tmux
}

// newFixture boots the isolated self-test server, writes the stub (if named),
// and returns a manager whose claude binary is the stub. rec nil → a real
// recorder over db.
func newFixture(t *testing.T, db *sql.DB, stubName, stubBody string, rec runRecorder) *fixture {
	t.Helper()
	tm := tmuxTest(t)
	bootServer(t, tm)
	if stubName != "" {
		writeStub(t, stubName, stubBody)
	}
	cfg := config.Default()
	if stubName != "" {
		cfg.Agent.Claude.Binary = stubName
	}
	if rec == nil {
		rec = trace.NewRecorder(db)
	}
	return &fixture{t: t, m: newManager(tm, cfg, db, rec, func(string, ...any) {}), tm: tm}
}

// countEvents counts trace rows of one event type for one card.
func countEvents(t *testing.T, db *sql.DB, cardID, eventType string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM traces WHERE card_id = ? AND event_type = ?", cardID, eventType,
	).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", eventType, err)
	}
	return n
}

func countAll(t *testing.T, db *sql.DB, cardID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM traces WHERE card_id = ?", cardID,
	).Scan(&n); err != nil {
		t.Fatalf("count all: %v", err)
	}
	return n
}

// endFilesChanged reads the files_changed value the latest trace_end carries
// for a card, mirroring ADR-001 §3.3 data_json.
func endFilesChanged(t *testing.T, db *sql.DB, cardID string) int {
	t.Helper()
	var data string
	if err := db.QueryRow(
		"SELECT data_json FROM traces WHERE card_id = ? AND event_type = 'trace_end' ORDER BY seq LIMIT 1",
		cardID,
	).Scan(&data); err != nil {
		t.Fatalf("read trace_end: %v", err)
	}
	var d struct {
		FilesChanged int `json:"files_changed"`
	}
	if err := json.Unmarshal([]byte(data), &d); err != nil {
		t.Fatalf("parse trace_end %q: %v", data, err)
	}
	return d.FilesChanged
}

// gitRepo builds a throwaway repo with an initial commit (like trace's
// helper), so SnapshotBaseline returns a real BaseHead.
func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "--quiet", "-b", "main")
	git(t, dir, "config", "user.name", "loom test")
	git(t, dir, "config", "user.email", "loom@example.com")
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "--quiet", "-m", "init")
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// failRecorder wraps a real recorder with one injectable post-create failure
// (StartRun or Watch) for the invariant 1 exercises.
type failRecorder struct {
	rec       *trace.Recorder
	failRun   bool
	failWatch bool
}

func (f *failRecorder) StartRun(cardID, root string, baseline trace.Baseline) (string, error) {
	if f.failRun {
		return "", errors.New("forced: StartRun")
	}
	return f.rec.StartRun(cardID, root, baseline)
}

func (f *failRecorder) Watch(runID, root string) error {
	if f.failWatch {
		return errors.New("forced: Watch")
	}
	return f.rec.Watch(runID, root)
}

func (f *failRecorder) LiveChanges(runID string) ([]trace.Change, error) {
	return f.rec.LiveChanges(runID)
}

func (f *failRecorder) EndRun(runID string, durationMs, filesChanged int) error {
	return f.rec.EndRun(runID, durationMs, filesChanged)
}

func (f *failRecorder) AbortRun(runID string) error {
	return f.rec.AbortRun(runID)
}

func TestEnsureCreatesRun(t *testing.T) {
	db := openSessionDB(t)
	card := seedCard(t, db)
	f := newFixture(t, db, "loomstub-sleep", "echo READY\nsleep 300", nil)
	name := SessionName(card.ID)
	t.Cleanup(func() { f.tm.KillSession(name) })

	if err := f.m.Ensure(context.Background(), card); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitForSession(t, f.tm, name, true)
	if n := countEvents(t, db, card.ID, "trace_start"); n != 1 {
		t.Fatalf("trace_start rows = %d, want 1", n)
	}
	if n := countEvents(t, db, card.ID, "trace_end"); n != 0 {
		t.Fatalf("trace_end rows = %d, want 0 (run still open)", n)
	}
}

func TestEnsureReuseNoNewRun(t *testing.T) {
	db := openSessionDB(t)
	card := seedCard(t, db)
	f := newFixture(t, db, "loomstub-sleep", "echo READY\nsleep 300", nil)
	name := SessionName(card.ID)
	t.Cleanup(func() { f.tm.KillSession(name) })

	if err := f.m.Ensure(context.Background(), card); err != nil {
		t.Fatalf("Ensure #1: %v", err)
	}
	if err := f.m.Ensure(context.Background(), card); err != nil {
		t.Fatalf("Ensure #2 (reuse): %v", err)
	}
	waitForSession(t, f.tm, name, true)
	if n := countEvents(t, db, card.ID, "trace_start"); n != 1 {
		t.Fatalf("trace_start rows = %d, want exactly 1 across both ensures", n)
	}
}

func TestEnsureNonexistentBinary(t *testing.T) {
	db := openSessionDB(t)
	card := seedCard(t, db)
	f := newFixture(t, db, "", "", nil)
	f.m.cfg.Agent.Claude.Binary = "loomstub-definitely-missing"

	err := f.m.Ensure(context.Background(), card)
	if err == nil || !strings.Contains(err.Error(), "not found in PATH (install it or set [agent.claude] binary)") {
		t.Fatalf("Ensure error = %v, want not-found-in-PATH hint", err)
	}
	if ok, e := f.tm.HasSession(SessionName(card.ID)); e != nil || ok {
		t.Fatalf("session created despite resolve failure: ok=%v err=%v", ok, e)
	}
	if n := countAll(t, db, card.ID); n != 0 {
		t.Fatalf("trace rows = %d, want 0 (no trace_start before resolve)", n)
	}
}

func TestEnsureProbeFailRuntimeFailingStub(t *testing.T) {
	db := openSessionDB(t)
	card := seedCard(t, db)
	f := newFixture(t, db, "loomstub-fail", "echo boom >&2\nexit 1", nil)
	name := SessionName(card.ID)

	err := f.m.Ensure(context.Background(), card)
	if err == nil || !strings.Contains(err.Error(), "session failed to start") {
		t.Fatalf("Ensure error = %v, want session-failed-to-start", err)
	}
	waitForSession(t, f.tm, name, false)
	if n := countAll(t, db, card.ID); n != 0 {
		t.Fatalf("trace rows = %d, want 0 (probe failure leaves no run; C7)", n)
	}
}

func TestEnsureStartRunFailureAbortsSession(t *testing.T) {
	db := openSessionDB(t)
	card := seedCard(t, db)
	rec := &failRecorder{rec: trace.NewRecorder(db), failRun: true}
	f := newFixture(t, db, "loomstub-sleep", "echo READY\nsleep 300", rec)
	name := SessionName(card.ID)

	err := f.m.Ensure(context.Background(), card)
	if err == nil || !strings.Contains(err.Error(), "forced: StartRun") {
		t.Fatalf("Ensure error = %v, want forced StartRun failure", err)
	}
	waitForSession(t, f.tm, name, false) // deferred KillSession
	if n := countAll(t, db, card.ID); n != 0 {
		t.Fatalf("trace rows = %d, want 0 (no partial run)", n)
	}
}

func TestEnsureWatchFailureAbortsRun(t *testing.T) {
	db := openSessionDB(t)
	card := seedCard(t, db)
	rec := &failRecorder{rec: trace.NewRecorder(db), failWatch: true}
	f := newFixture(t, db, "loomstub-sleep", "echo READY\nsleep 300", rec)
	name := SessionName(card.ID)

	err := f.m.Ensure(context.Background(), card)
	if err == nil || !strings.Contains(err.Error(), "forced: Watch") {
		t.Fatalf("Ensure error = %v, want forced Watch failure", err)
	}
	waitForSession(t, f.tm, name, false) // deferred KillSession
	if n := countAll(t, db, card.ID); n != 0 {
		t.Fatalf("trace rows = %d, want 0 (AbortRun deleted the run; invariant 4)", n)
	}
}

func TestStatusMarkersAndFinalize(t *testing.T) {
	db := openSessionDB(t)
	c1 := seedCard(t, db)
	c2 := seedCard(t, db)
	f := newFixture(t, db, "loomstub-sleep", "echo READY\nsleep 300", nil)
	for _, c := range []store.Card{c1, c2} {
		t.Cleanup(func() { f.tm.KillSession(SessionName(c.ID)) })
		if err := f.m.Ensure(context.Background(), c); err != nil {
			t.Fatalf("Ensure %s: %v", c.ID, err)
		}
	}

	st, err := f.m.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	for _, id := range []string{c1.ID, c2.ID} {
		s, ok := st[id]
		if !ok || !s.Running || s.Attached {
			t.Fatalf("Status[%s] = %#v, want running+detached", id, s)
		}
	}

	// c1's session disappears externally; the next tick must finalize it.
	if err := f.tm.KillSession(SessionName(c1.ID)); err != nil {
		t.Fatalf("KillSession(c1): %v", err)
	}
	st, err = f.m.Status(context.Background())
	if err != nil {
		t.Fatalf("Status #2: %v", err)
	}
	if _, ok := st[c1.ID]; ok {
		t.Fatalf("c1 still in Status after session death: %v", st)
	}
	if !st[c2.ID].Running {
		t.Fatalf("c2 should still be running: %v", st)
	}
	if n := countEvents(t, db, c1.ID, "trace_end"); n != 1 {
		t.Fatalf("c1 trace_end = %d, want exactly 1", n)
	}
}

func TestStatusFinalizesCompletedRun(t *testing.T) {
	db := openSessionDB(t)
	root := gitRepo(t, map[string]string{"a.txt": "one"})
	card := seedCardAt(t, db, root)
	f := newFixture(t, db, "loomstub-git", "echo change >> changes.txt\nsleep 1\nexit 0", nil)
	name := SessionName(card.ID)

	if err := f.m.Ensure(context.Background(), card); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitForSession(t, f.tm, name, false) // stub exits after ~1s

	st, err := f.m.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(st) != 0 {
		t.Fatalf("Status = %v, want empty (run finalized on completion)", st)
	}
	if n := countEvents(t, db, card.ID, "trace_end"); n != 1 {
		t.Fatalf("trace_end rows = %d, want 1", n)
	}
	if n := countEvents(t, db, card.ID, "file_change"); n != 1 {
		t.Fatalf("file_change rows = %d, want 1 (changes.txt attributed)", n)
	}
	if fc := endFilesChanged(t, db, card.ID); fc != 1 {
		t.Fatalf("files_changed = %d, want 1", fc)
	}
}

func TestReconcileOnStartupMissedCompletion(t *testing.T) {
	db := openSessionDB(t)
	card := seedCard(t, db)
	f := newFixture(t, db, "loomstub-short", "sleep 1\nexit 0", nil)
	name := SessionName(card.ID)

	if err := f.m.Ensure(context.Background(), card); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitForSession(t, f.tm, name, false) // stub drops dead

	if err := f.m.ReconcileOnStartup(context.Background()); err != nil {
		t.Fatalf("ReconcileOnStartup: %v", err)
	}
	if n := countEvents(t, db, card.ID, "trace_end"); n != 1 {
		t.Fatalf("trace_end = %d, want exactly 1 after reconcile", n)
	}
	if err := f.m.ReconcileOnStartup(context.Background()); err != nil {
		t.Fatalf("ReconcileOnStartup #2: %v", err)
	}
	if n := countEvents(t, db, card.ID, "trace_end"); n != 1 {
		t.Fatalf("trace_end = %d after 2nd reconcile, want still 1 (idempotent)", n)
	}
}

func TestReconcileDeletedCardBlindFinalize(t *testing.T) {
	db := openSessionDB(t)
	card := seedCard(t, db)
	f := newFixture(t, db, "loomstub-sleep", "echo READY\nsleep 300", nil)
	name := SessionName(card.ID)
	t.Cleanup(func() { f.tm.KillSession(name) })

	if err := f.m.Ensure(context.Background(), card); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if err := store.DeleteCard(db, card.ID); err != nil {
		t.Fatalf("DeleteCard: %v", err)
	}
	// Cascade removed the card's trace rows; startup reconcile sees no open
	// run and must not error.
	if err := f.m.ReconcileOnStartup(context.Background()); err != nil {
		t.Fatalf("ReconcileOnStartup after delete: %v", err)
	}
	if n := countAll(t, db, card.ID); n != 0 {
		t.Fatalf("trace rows = %d, want 0 (cascade wiped the run)", n)
	}

	// Direct blind path: a run whose card no longer exists finalizes as a
	// no-op (EndRun resolves card via trace_start, which is gone).
	if err := f.m.completeRun(store.OpenRun{CardID: "missing-card", RunID: "missing-run"}); err != nil {
		t.Fatalf("completeRun(blind) = %v, want nil", err)
	}
}

func TestKillFinalizesRun(t *testing.T) {
	db := openSessionDB(t)
	card := seedCard(t, db)
	f := newFixture(t, db, "loomstub-sleep", "echo READY\nsleep 300", nil)
	name := SessionName(card.ID)

	if err := f.m.Ensure(context.Background(), card); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	waitForSession(t, f.tm, name, true)
	if err := f.m.Kill(context.Background(), card); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	waitForSession(t, f.tm, name, false)
	if n := countEvents(t, db, card.ID, "trace_end"); n != 1 {
		t.Fatalf("trace_end = %d, want 1", n)
	}
}

func TestEnsureMissingWorkspace(t *testing.T) {
	db := openSessionDB(t)
	card := seedCard(t, db)

	ws, err := store.GetWorkspace(db, card.WorkspaceID)
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	if err := store.DeleteWorkspace(db, ws.ID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}

	f := newFixture(t, db, "loomstub-sleep", "echo READY\nsleep 300", nil)
	err = f.m.Ensure(context.Background(), card)
	if err == nil || !strings.Contains(err.Error(), "watch scope for card") {
		t.Fatalf("Ensure error = %v, want watch-scope error", err)
	}
	if ok, e := f.tm.HasSession(SessionName(card.ID)); e != nil || ok {
		t.Fatalf("session created despite missing watch scope: ok=%v err=%v", ok, e)
	}
	if n := countAll(t, db, card.ID); n != 0 {
		t.Fatalf("trace rows = %d, want 0", n)
	}
}

func TestAttachNoSession(t *testing.T) {
	db := openSessionDB(t)
	card := seedCard(t, db)
	f := newFixture(t, db, "", "", nil)

	err := f.m.Attach(context.Background(), card)
	if err == nil || !strings.Contains(err.Error(), "has no live session") {
		t.Fatalf("Attach error = %v, want no-live-session", err)
	}
}

func TestNewManagerConstruction(t *testing.T) {
	db := openSessionDB(t)
	tm := tmuxTest(t)
	bootServer(t, tm)
	m := NewManager(tm, config.Default(), db)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if err := m.ReconcileOnStartup(context.Background()); err != nil {
		t.Fatalf("ReconcileOnStartup via production constructor: %v", err)
	}
}

func TestSessionsParsing(t *testing.T) {
	got := parseSessionStates("a\t1\nb\t0\n\n")
	want := []SessionState{{Name: "a", Attached: true}, {Name: "b", Attached: false}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseSessionStates = %#v, want %#v", got, want)
	}

	blank := parseSessionStates("")
	if len(blank) != 0 {
		t.Fatalf("parseSessionStates(empty) = %v, want []", blank)
	}
}

func TestSessionsRoundTrip(t *testing.T) {
	tm := tmuxTest(t)
	bootServer(t, tm)
	name := uniqueName(t)
	t.Cleanup(func() { tm.KillSession(name) })

	if err := tm.NewSession(name, t.TempDir(), "cat"); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	states, err := tm.Sessions()
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	var found bool
	for _, s := range states {
		if s.Name == name {
			found = true
			if s.Attached {
				t.Fatalf("session %s reported attached, want detached", name)
			}
		}
	}
	if !found {
		t.Fatalf("session %s not in %v", name, states)
	}
}

// strPtr is a *string constructor for CardInput.Agent (which distinguishes
// NULL, "follow [agent] default", from an explicit value, §6).
func strPtr(s string) *string { return &s }

// TestEnsurePerDriverSessionNameCwdAndArgv is the T21 "Integration — per
// driver" row (DESIGN-002 §16): parametrized stub claude + stub opencode
// (mini and full interface) scripts, against real tmux, asserting the
// loom-<id> session name, the launch cwd, and that every argv element —
// including the prompt, which carries a space and an embedded single quote —
// survives the §4.4 single-quote escaping intact through tmux's
// `$SHELL -c` re-parse.
func TestEnsurePerDriverSessionNameCwdAndArgv(t *testing.T) {
	const title = `Fix loom's quoting edge case`

	cases := []struct {
		name      string
		stub      string
		agent     *string // nil = NULL -> [agent] default ("claude")
		wantArgs  []string
		configure func(cfg *config.Config, stub string)
	}{
		{
			name:  "claude",
			stub:  "loomstub-driver-claude",
			agent: nil,
			configure: func(cfg *config.Config, stub string) {
				cfg.Agent.Claude.Binary = stub
			},
		},
		{
			name:     "opencode-mini",
			stub:     "loomstub-driver-oc-mini",
			agent:    strPtr("opencode"),
			wantArgs: []string{"--mini", "--prompt"},
			configure: func(cfg *config.Config, stub string) {
				cfg.Agent.Opencode.Binary = stub
				cfg.Agent.Opencode.Interface = "mini"
			},
		},
		{
			name:     "opencode-full",
			stub:     "loomstub-driver-oc-full",
			agent:    strPtr("opencode"),
			wantArgs: []string{"--prompt"},
			configure: func(cfg *config.Config, stub string) {
				cfg.Agent.Opencode.Binary = stub
				cfg.Agent.Opencode.Interface = "full"
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openSessionDB(t)
			root := t.TempDir()
			card := seedCardWithTitleAgent(t, db, root, title, tc.agent)

			// The stub prints its cwd and every received argv element on its
			// own line, so capture-pane exposes exactly what tmux's shell
			// parsed out of the escaped command line.
			stubBody := `printf 'CWD:%s\n' "$PWD"
n=0
for a in "$@"; do n=$((n+1)); printf 'ARG%d:%s\n' "$n" "$a"; done
echo READY
sleep 300`
			writeStub(t, tc.stub, stubBody)

			cfg := config.Default()
			tc.configure(cfg, tc.stub)
			tm := tmuxTest(t)
			bootServer(t, tm)
			rec := trace.NewRecorder(db)
			m := newManager(tm, cfg, db, rec, func(string, ...any) {})
			name := SessionName(card.ID)
			t.Cleanup(func() { tm.KillSession(name) })

			if err := m.Ensure(context.Background(), card); err != nil {
				t.Fatalf("Ensure: %v", err)
			}
			waitForSession(t, tm, name, true)

			if !strings.HasPrefix(name, "loom-") {
				t.Fatalf("session name %q, want loom-<id> prefix (ADR-001 §4.4)", name)
			}

			// tmux soft-wraps long lines at the pane width; flatten before
			// substring checks so a wrap point mid-path/mid-prompt doesn't
			// spuriously break the assertion (no characters are inserted at
			// a soft wrap, so this is a lossless join).
			pane := tm.CapturePane(name)
			flat := strings.ReplaceAll(pane, "\n", "")
			if !strings.Contains(flat, "CWD:"+root) {
				t.Fatalf("pane cwd mismatch, want CWD:%s in:\n%s", root, pane)
			}
			for i, flag := range tc.wantArgs {
				want := fmt.Sprintf("ARG%d:%s", i+1, flag)
				if !strings.Contains(flat, want) {
					t.Fatalf("pane missing %q in:\n%s", want, pane)
				}
			}
			// The prompt must arrive as ONE argv element, byte-for-byte —
			// proof the §4.4 escaper round-trips through tmux's re-parse.
			wantPrompt := fmt.Sprintf("ARG%d:%s", len(tc.wantArgs)+1, title)
			if !strings.Contains(flat, wantPrompt) {
				t.Fatalf("pane missing reconstructed prompt %q in:\n%s", wantPrompt, pane)
			}
		})
	}
}

// TestReconcileOnStartupAttributesGitChanges is the T21 "git reconciliation
// attributes changes (a run that outlives loom)" row: a run is opened
// (baseline snapshotted) with no fsnotify watcher ever attached to it — as
// if the loom process that launched it exited before this process started —
// and a fresh Manager's ReconcileOnStartup must still attribute the file
// change purely from the stored git baseline pair (ADR-001 §5).
func TestReconcileOnStartupAttributesGitChanges(t *testing.T) {
	db := openSessionDB(t)
	root := gitRepo(t, map[string]string{"a.txt": "one"})
	card := seedCardAt(t, db, root)

	baseline, err := trace.SnapshotBaseline(root)
	if err != nil {
		t.Fatalf("SnapshotBaseline: %v", err)
	}
	rec := trace.NewRecorder(db)
	if _, err := rec.StartRun(card.ID, root, baseline); err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	// No Watch() call for this run: it "outlives loom" by construction — no
	// live fsnotify watcher ever recorded its changes.
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}

	tm := tmuxTest(t)
	bootServer(t, tm)
	m := newManager(tm, config.Default(), db, rec, func(string, ...any) {})
	// No loom-<id> session exists for this card, so ReconcileOnStartup treats
	// the run as ended and finalizes it via completeRun's git reconcile,
	// exactly as a freshly started loom process would.
	if err := m.ReconcileOnStartup(context.Background()); err != nil {
		t.Fatalf("ReconcileOnStartup: %v", err)
	}
	if n := countEvents(t, db, card.ID, "trace_end"); n != 1 {
		t.Fatalf("trace_end = %d, want 1", n)
	}
	if n := countEvents(t, db, card.ID, "file_change"); n != 1 {
		t.Fatalf("file_change = %d, want 1 (b.txt attributed with no live watcher)", n)
	}
	if fc := endFilesChanged(t, db, card.ID); fc != 1 {
		t.Fatalf("files_changed = %d, want 1", fc)
	}
}

// TestOpencodeFullTUIAutoSubmitCanary is the T21/DESIGN-002 §16 "Regression
// canary" row (review finding F5): it automates the docs/PROBE-full-tui.md
// method as a standing test, for both the mini and full interfaces, so a
// future opencode version that regresses `--prompt` to prefill-only (rather
// than auto-submitting) fails a test instead of silently idling a `--detach`
// run. It needs the real opencode CLI, network, and a configured model — a
// stub cannot exhibit prefill-vs-auto-submit behavior — so it is opt-in:
// skipped unless LOOM_TEST_LIVE_OPENCODE=1 is set (even when opencode is on
// PATH), so `go test ./...` never makes a live LLM call by default. Run it
// explicitly on each opencode major bump (§15/§8).
func TestOpencodeFullTUIAutoSubmitCanary(t *testing.T) {
	if os.Getenv("LOOM_TEST_LIVE_OPENCODE") != "1" {
		t.Skip("set LOOM_TEST_LIVE_OPENCODE=1 to run the live full-TUI auto-submit regression canary (see docs/PROBE-full-tui.md)")
	}
	bin, err := exec.LookPath("opencode")
	if err != nil {
		t.Skip("opencode not installed; skipping full-TUI auto-submit regression canary (see docs/PROBE-full-tui.md)")
	}

	for _, iface := range []string{"mini", "full"} {
		t.Run(iface, func(t *testing.T) {
			tm := tmuxTest(t)
			bootServer(t, tm)
			name := uniqueName(t)
			t.Cleanup(func() { tm.KillSession(name) })

			const prompt = "Reply with exactly: OK"
			argv := []string{bin, "--prompt", prompt}
			if iface == "mini" {
				argv = []string{bin, "--mini", "--prompt", prompt}
			}
			if err := tm.NewSession(name, t.TempDir(), agent.CommandLine(argv)); err != nil {
				t.Fatalf("NewSession: %v", err)
			}
			waitForSession(t, tm, name, true)

			// Poll rather than a single fixed sleep: real model latency
			// varies with the configured provider, unlike a stub.
			var pane string
			var gotResponse bool
			deadline := time.Now().Add(60 * time.Second)
			for time.Now().Before(deadline) {
				pane = tm.CapturePane(name)
				for _, ln := range strings.Split(pane, "\n") {
					if strings.TrimSpace(ln) == "OK" {
						gotResponse = true
					}
				}
				if gotResponse {
					break
				}
				time.Sleep(2 * time.Second)
			}
			if !gotResponse {
				t.Fatalf("regression (F5): opencode %s interface did not auto-submit + respond within 60s — pane:\n%s", iface, pane)
			}
			if ok, err := tm.HasSession(name); err != nil || !ok {
				t.Fatalf("session died after the prompt turn (LaunchMode=Interactive expects it stays alive): ok=%v err=%v", ok, err)
			}
		})
	}
}
