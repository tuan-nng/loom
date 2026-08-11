package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"loom/internal/session"
	"loom/internal/store"
)

// detailCard seeds the fake board with one card carrying the full T19 field
// set and returns the model focused on it.
func detailCard(t *testing.T, svc *fakeService) Model {
	t.Helper()
	svc.cards = []store.Card{
		{
			ID:          "k1",
			ColumnID:    "c-blog",
			Title:       "detail me",
			Priority:    "high",
			Labels:      strp("golang,cli"),
			Description: strp("## The what\n- alpha\n- beta"),
			Objective:   strp("ship the thing"),
			CodebaseID:  strp("cb1"),
		},
	}
	m := readyModel(t, svc)
	return m
}

func TestDetailOpensOnD(t *testing.T) {
	svc := newBoardService()
	m := detailCard(t, svc)

	m, _ = press(t, m, 'd')
	if m.detail == nil {
		t.Fatal("d did not open the detail pane")
	}
	if m.detail.card.ID != "k1" {
		t.Fatalf("detail card = %q, want k1", m.detail.card.ID)
	}
}

func TestDetailNoCard(t *testing.T) {
	svc := newBoardService()
	m := readyModel(t, svc) // empty board, cursor on nothing
	m, _ = press(t, m, 'd')
	if m.detail != nil {
		t.Fatal("d opened detail with no focused card")
	}
	if m.note == "" {
		t.Fatal("no-card d produced no notice")
	}
}

func TestDetailEscCloses(t *testing.T) {
	svc := newBoardService()
	m := detailCard(t, svc)
	m, _ = press(t, m, 'd')
	if m.detail == nil {
		t.Fatal("d did not open the detail pane")
	}
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.detail != nil {
		t.Fatal("esc did not close the detail pane")
	}
}

func TestDetailRendersFields(t *testing.T) {
	svc := newBoardService()
	svc.codebases = []store.Codebase{{ID: "cb1", Path: "/repo/src"}}
	ended := "2026-08-11T12:35:02.001"
	svc.runs = map[string][]store.CardRun{
		"k1": {
			{RunID: "r1", StartedAt: "2026-08-11T12:34:56.789", EndedAt: &ended, DurationMs: 5212, Files: []string{"a.go", "b.go"}, FilesChanged: 2},
		},
	}
	svc.status = map[string]session.SessionStatus{"k1": {Running: true, Attached: true}}
	m := detailCard(t, svc)

	m, _ = press(t, m, 'd')
	got := plain(m.View().Content)

	for _, want := range []string{
		"detail me",
		"Priority: high",
		"Labels: golang,cli",
		"Codebase: /repo/src",
		"Agent: claude (default)",
		"The what",
		"alpha",
		"ship the thing",
		"Aug 11 12:34", // friendly clock from the store's created_at layout
		"a.go, b.go",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("detail view missing %q:\n%s", want, got)
		}
	}
}

func TestDetailAgentExplicit(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{
		{ID: "k1", ColumnID: "c-blog", Title: "x", Agent: strp("opencode")},
	}
	m := readyModel(t, svc)
	m, _ = press(t, m, 'd')
	got := plain(m.View().Content)
	if !strings.Contains(got, "Agent: opencode") {
		t.Errorf("detail missing explicit agent line:\n%s", got)
	}
	if strings.Contains(got, "(default)") {
		t.Errorf("explicit agent rendered with (default) suffix:\n%s", got)
	}
}

func TestDetailRunsNoRunsAndOpenRun(t *testing.T) {
	svc := newBoardService()
	svc.runs = map[string][]store.CardRun{"k1": nil}
	m := detailCard(t, svc)
	m, _ = press(t, m, 'd')
	if got := plain(m.View().Content); !strings.Contains(got, "(no runs yet)") {
		t.Errorf("empty run list not shown:\n%s", got)
	}
}

func TestDetailRunsErrorInline(t *testing.T) {
	svc := newBoardService()
	svc.runsErr = errDetail
	m := detailCard(t, svc)
	m, _ = press(t, m, 'd')
	if m.detail == nil {
		t.Fatal("runs error closed the detail pane")
	}
	if m.detail.err == "" {
		t.Fatal("runs error not surfaced inline")
	}
}

func TestDetailRunsFetched(t *testing.T) {
	svc := newBoardService()
	m := detailCard(t, svc)
	m, _ = press(t, m, 'd')
	if len(svc.runsCalled) != 1 || svc.runsCalled[0] != "k1" {
		t.Fatalf("RunsForCard calls = %v, want [k1]", svc.runsCalled)
	}
}

var errDetail = errors.New("detail boom")
