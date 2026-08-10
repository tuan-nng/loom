package cli

import (
	"errors"
	"strings"
	"testing"

	"loom/internal/session"
	"loom/internal/store"
)

// seedStatusDB inits a workspace/board in a's db and returns columnID by stage.
func seedStatusDB(t *testing.T, a *App) map[string]string {
	t.Helper()
	if err := a.run([]string{"init", t.TempDir()}); err != nil {
		t.Fatalf("init: %v", err)
	}
	_, b, err := a.svc.ResolveSelection()
	if err != nil {
		t.Fatalf("ResolveSelection: %v", err)
	}
	cols, err := store.ListColumns(a.db, b.ID)
	if err != nil {
		t.Fatalf("ListColumns: %v", err)
	}
	byStage := make(map[string]string, len(cols))
	for _, c := range cols {
		byStage[c.Stage] = c.ID
	}
	return byStage
}

func addStatusCard(t *testing.T, a *App, columnID, title string) store.Card {
	t.Helper()
	c, err := store.CreateCard(a.db, store.CardInput{ColumnID: columnID, Title: title})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	return c
}

func TestRunStatusNormal(t *testing.T) {
	stub := &stubSess{}
	a, out, _ := newTestApp(t, stub)
	cols := seedStatusDB(t, a)
	backlogA := addStatusCard(t, a, cols["backlog"], "Alpha")
	backlogB := addStatusCard(t, a, cols["backlog"], "Beta")
	addStatusCard(t, a, cols["dev"], "Gamma")
	stub.statusRes = map[string]session.SessionStatus{
		backlogA.ID: {Running: true, Attached: true},
		backlogB.ID: {Running: true, Attached: false},
	}

	if err := a.run([]string{"status"}); err != nil {
		t.Fatalf("status: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"workspace: ",
		"board: Board",
		"column: Backlog (backlog) 2",
		"column: In Progress (dev) 1",
		"session: ◉ Alpha",
		"session: ● Beta",
		"run: none",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("status output missing %q:\n%s", want, got)
		}
	}
	if stub.reconcileCalls != 1 {
		t.Errorf("reconcile calls = %d, want 1", stub.reconcileCalls)
	}
}

func TestRunStatusSessionLinesSorted(t *testing.T) {
	stub := &stubSess{}
	a, out, _ := newTestApp(t, stub)
	cols := seedStatusDB(t, a)
	zeta := addStatusCard(t, a, cols["backlog"], "Zeta")
	alpha := addStatusCard(t, a, cols["backlog"], "Alpha")
	stub.statusRes = map[string]session.SessionStatus{
		zeta.ID:  {Running: true, Attached: false},
		alpha.ID: {Running: true, Attached: false},
	}

	if err := a.run([]string{"status"}); err != nil {
		t.Fatalf("status: %v", err)
	}
	got := out.String()
	aIdx := strings.Index(got, "session: ● Alpha")
	zIdx := strings.Index(got, "session: ● Zeta")
	if aIdx < 0 || zIdx < 0 {
		t.Fatalf("session lines missing:\n%s", got)
	}
	if aIdx > zIdx {
		t.Errorf("session lines not sorted by title:\n%s", got)
	}
}

// TestRunStatusSameTitleDeterministic verifies two live cards sharing a title
// render in a fixed (card-id) order regardless of map iteration.
func TestRunStatusSameTitleDeterministic(t *testing.T) {
	stub := &stubSess{}
	a, out, _ := newTestApp(t, stub)
	cols := seedStatusDB(t, a)
	first := addStatusCard(t, a, cols["backlog"], "Duplicate")
	second := addStatusCard(t, a, cols["backlog"], "Duplicate")
	stub.statusRes = map[string]session.SessionStatus{
		second.ID: {Running: true, Attached: false},
		first.ID:  {Running: true, Attached: false},
	}

	if err := a.run([]string{"status"}); err != nil {
		t.Fatalf("status: %v", err)
	}
	want := "session: ● Duplicate\nsession: ● Duplicate\n"
	if !strings.Contains(out.String(), want) {
		t.Errorf("two same-title sessions must both render:\n%s", out.String())
	}
}

func TestRunStatusNoSessions(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	seedStatusDB(t, a)
	if err := a.run([]string{"status"}); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out.String(), "session: none") {
		t.Errorf("status missing 'session: none':\n%s", out.String())
	}
}

func TestRunStatusDegradesWithoutTmux(t *testing.T) {
	stub := &stubSess{probeErr: errors.New("session: tmux not found in PATH (install it: 'apt install tmux' or 'brew install tmux')")}
	a, out, errw := newTestApp(t, stub)
	seedStatusDB(t, a)

	if err := a.run([]string{"status"}); err != nil {
		t.Fatalf("status: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "workspace: ") || !strings.Contains(got, "column: Backlog") {
		t.Errorf("degraded status missing board summary:\n%s", got)
	}
	if strings.Contains(got, "session:") {
		t.Errorf("degraded status should omit session lines:\n%s", got)
	}
	if strings.Contains(got, "run:") {
		t.Errorf("degraded status should omit run lines:\n%s", got)
	}
	if !strings.Contains(errw.String(), "tmux unavailable") {
		t.Errorf("stderr missing tmux notice: %q", errw.String())
	}
	if stub.reconcileCalls != 0 {
		t.Errorf("degraded status ran reconcile %d times, want 0", stub.reconcileCalls)
	}
}

func TestRunStatusNotInitialized(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	err := a.run([]string{"status"})
	if err == nil {
		t.Fatal("status on empty db succeeded, want error")
	}
	if out.Len() != 0 {
		t.Errorf("status on empty db printed to stdout: %q", out.String())
	}
	if !strings.Contains(err.Error(), "run loom init") {
		t.Errorf("status error = %q, want 'run loom init'", err.Error())
	}
}

func TestRunStatusRecentRuns(t *testing.T) {
	a, out, _ := newTestApp(t, &stubSess{})
	cols := seedStatusDB(t, a)
	card := addStatusCard(t, a, cols["backlog"], "Shipped")
	runID := store.NewID()
	if err := store.StartRun(a.db, card.ID, runID, "", ""); err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := store.EndRun(a.db, runID, 30000, 8); err != nil {
		t.Fatalf("EndRun: %v", err)
	}

	if err := a.run([]string{"status"}); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out.String(), "run: Shipped (30000ms, files=8)") {
		t.Errorf("status missing run line:\n%s", out.String())
	}
}
