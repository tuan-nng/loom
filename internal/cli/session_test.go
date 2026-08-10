package cli

import (
	"errors"
	"strings"
	"testing"

	"loom/internal/session"
	"loom/internal/store"
)

func TestRunCardOpenAttaches(t *testing.T) {
	stub := &stubSess{}
	a, out, _ := newTestApp(t, stub)
	cols := seedCardBoard(t, a)
	addStatusCard(t, a, cols["todo"], "Alpha")
	card := onlyCard(t, a)

	if err := a.run([]string{"card", "open", card.ID}); err != nil {
		t.Fatalf("card open: %v", err)
	}
	if stub.ensureCalls != 1 {
		t.Errorf("ensure calls = %d, want 1", stub.ensureCalls)
	}
	if stub.attachCalls != 1 {
		t.Errorf("attach calls = %d, want 1", stub.attachCalls)
	}
	if !strings.Contains(out.String(), "opened card Alpha (") {
		t.Errorf("card open output missing echo:\n%s", out.String())
	}
}

func TestRunCardOpenDetach(t *testing.T) {
	stub := &stubSess{}
	a, out, _ := newTestApp(t, stub)
	cols := seedCardBoard(t, a)
	addStatusCard(t, a, cols["todo"], "Alpha")
	addStatusCard(t, a, cols["todo"], "Beta")
	cards := listCards(t, a)
	if len(cards) != 2 {
		t.Fatalf("cards = %d, want 2", len(cards))
	}

	for _, c := range cards {
		if err := a.run([]string{"card", "open", c.ID, "--detach"}); err != nil {
			t.Fatalf("card open --detach %s: %v", c.ID, err)
		}
	}
	if stub.ensureCalls != 2 {
		t.Errorf("ensure calls = %d, want 2", stub.ensureCalls)
	}
	if stub.attachCalls != 0 {
		t.Errorf("attach calls = %d, want 0 (detach)", stub.attachCalls)
	}
	if !strings.Contains(out.String(), "opened card Alpha (") || !strings.Contains(out.String(), "opened card Beta (") {
		t.Errorf("card open --detach output missing echoes:\n%s", out.String())
	}
}

func TestRunCardCloseKills(t *testing.T) {
	stub := &stubSess{}
	a, out, _ := newTestApp(t, stub)
	cols := seedCardBoard(t, a)
	addStatusCard(t, a, cols["todo"], "Alpha")
	card := onlyCard(t, a)

	if err := a.run([]string{"card", "close", card.ID}); err != nil {
		t.Fatalf("card close: %v", err)
	}
	if stub.killCalls != 1 {
		t.Errorf("kill calls = %d, want 1", stub.killCalls)
	}
	if !strings.Contains(out.String(), "closed card Alpha (") {
		t.Errorf("card close output missing echo:\n%s", out.String())
	}
}

func TestRunCloseIdempotent(t *testing.T) {
	stub := &stubSess{}
	a, _, _ := newTestApp(t, stub)
	cols := seedCardBoard(t, a)
	addStatusCard(t, a, cols["todo"], "Alpha")
	card := onlyCard(t, a)

	// Closing a never-opened card succeeds: Manager.Kill treats an absent
	// session as a no-op (the stub always succeeds).
	if err := a.run([]string{"card", "close", card.ID}); err != nil {
		t.Fatalf("card close (never opened): %v", err)
	}
}

func TestRunAttachSilent(t *testing.T) {
	stub := &stubSess{}
	a, out, _ := newTestApp(t, stub)
	cols := seedCardBoard(t, a)
	addStatusCard(t, a, cols["todo"], "Alpha")
	card := onlyCard(t, a)
	before := out.Len()

	if err := a.run([]string{"attach", card.ID}); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if stub.attachCalls != 1 {
		t.Errorf("attach calls = %d, want 1", stub.attachCalls)
	}
	if stub.ensureCalls != 0 {
		t.Errorf("ensure calls = %d, want 0 (pure attach)", stub.ensureCalls)
	}
	if out.Len() != before {
		t.Errorf("attach printed to stdout:\n%s", out.String()[before:])
	}
}

func TestRunAttachNoLiveSession(t *testing.T) {
	stub := &stubSess{attachErr: errors.New("session: card x has no live session")}
	a, _, _ := newTestApp(t, stub)
	cols := seedCardBoard(t, a)
	addStatusCard(t, a, cols["todo"], "Alpha")
	card := onlyCard(t, a)

	err := a.run([]string{"attach", card.ID})
	if err == nil || !strings.Contains(err.Error(), "has no live session") {
		t.Fatalf("attach err = %v, want 'has no live session'", err)
	}
}

func TestRunSessionsShowsLive(t *testing.T) {
	stub := &stubSess{}
	a, out, _ := newTestApp(t, stub)
	cols := seedCardBoard(t, a)
	alpha := addStatusCard(t, a, cols["todo"], "Alpha")
	zeta := addStatusCard(t, a, cols["todo"], "Zeta")
	stub.statusRes = map[string]session.SessionStatus{
		alpha.ID: {Running: true, Attached: true},
		zeta.ID:  {Running: true, Attached: false},
	}

	if err := a.run([]string{"sessions"}); err != nil {
		t.Fatalf("sessions: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "session: ◉ Alpha") || !strings.Contains(got, "session: ● Zeta") {
		t.Fatalf("sessions output missing lines:\n%s", got)
	}
	if strings.Index(got, "session: ◉ Alpha") > strings.Index(got, "session: ● Zeta") {
		t.Errorf("sessions lines not sorted by title:\n%s", got)
	}
	if stub.reconcileCalls != 1 {
		t.Errorf("reconcile calls = %d, want 1", stub.reconcileCalls)
	}
}

func TestRunSessionsEmpty(t *testing.T) {
	stub := &stubSess{}
	a, out, _ := newTestApp(t, stub)
	seedCardBoard(t, a)

	if err := a.run([]string{"sessions"}); err != nil {
		t.Fatalf("sessions: %v", err)
	}
	if !strings.Contains(out.String(), "session: none") {
		t.Errorf("sessions missing 'session: none':\n%s", out.String())
	}
}

func TestRunSessionsDegradesWithoutTmux(t *testing.T) {
	stub := &stubSess{probeErr: errors.New("session: tmux not found in PATH (install it: 'apt install tmux' or 'brew install tmux')")}
	a, out, errw := newTestApp(t, stub)
	seedCardBoard(t, a)
	before := out.Len()

	if err := a.run([]string{"sessions"}); err != nil {
		t.Fatalf("sessions: %v", err)
	}
	if out.Len() != before {
		t.Errorf("degraded sessions printed to stdout:\n%s", out.String()[before:])
	}
	if !strings.Contains(errw.String(), "tmux unavailable") || !strings.Contains(errw.String(), "no sessions to show") {
		t.Errorf("stderr missing tmux degrade notice: %q", errw.String())
	}
	if stub.reconcileCalls != 0 {
		t.Errorf("degraded sessions ran reconcile %d times, want 0", stub.reconcileCalls)
	}
}

func TestRunCardOpenMissingCard(t *testing.T) {
	stub := &stubSess{}
	a, _, _ := newTestApp(t, stub)
	seedCardBoard(t, a)

	err := a.run([]string{"card", "open", "nope"})
	if err == nil || !strings.Contains(err.Error(), "no rows") {
		t.Fatalf("card open missing card err = %v, want not-found", err)
	}
	if stub.ensureCalls != 0 {
		t.Errorf("ensure calls = %d, want 0 for missing card", stub.ensureCalls)
	}
}

func TestRunCardOpenNoTmuxFailsLoudly(t *testing.T) {
	stub := &stubSess{ensureErr: errors.New("session: tmux not found in PATH (install it: 'apt install tmux' or 'brew install tmux')")}
	a, _, _ := newTestApp(t, stub)
	cols := seedCardBoard(t, a)
	addStatusCard(t, a, cols["todo"], "Alpha")
	card := onlyCard(t, a)

	err := a.run([]string{"card", "open", card.ID, "--detach"})
	if err == nil || !strings.Contains(err.Error(), "tmux") {
		t.Fatalf("card open w/o tmux err = %v, want tmux error", err)
	}
	if stub.ensureCalls != 1 {
		t.Errorf("ensure calls = %d, want 1 (open reaches the session gate)", stub.ensureCalls)
	}
}

func TestRunSessionCommandMissingCard(t *testing.T) {
	for _, args := range [][]string{{"card", "close", "nope"}, {"attach", "nope"}} {
		stub := &stubSess{}
		a, _, _ := newTestApp(t, stub)
		seedCardBoard(t, a)
		err := a.run(args)
		if err == nil || !strings.Contains(err.Error(), "no rows") {
			t.Fatalf("%v missing card err = %v, want not-found", args, err)
		}
	}
}

func TestRunSessionUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"card", "open"},
		{"card", "open", "a", "b"},
		{"card", "open", "a", "--bogus"},
		{"card", "close"},
		{"card", "close", "a", "b"},
		{"attach"},
		{"attach", "a", "b"},
		{"sessions", "a"},
	} {
		code, _, errw := runWith(t, args...)
		if code != 1 {
			t.Fatalf("%v bad args exit = %d, want 1", args, code)
		}
		if !strings.Contains(errw, "run 'loom help'") {
			t.Errorf("%v stderr missing help hint: %q", args, errw)
		}
	}
}

func listCards(t *testing.T, a *App) []store.Card {
	t.Helper()
	_, b, err := a.svc.ResolveSelection()
	if err != nil {
		t.Fatalf("ResolveSelection: %v", err)
	}
	cards, err := a.svc.ListCardsByBoard(b.ID)
	if err != nil {
		t.Fatalf("ListCardsByBoard: %v", err)
	}
	return cards
}