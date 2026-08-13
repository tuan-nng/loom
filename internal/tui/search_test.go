package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"loom/internal/store"
)

// TestSearchFilterNarrowsBoard walks / end to end: the matching cards stay,
// the non-matching one disappears, and the header count narrows with the
// filter (T20 acceptance).
func TestSearchFilterNarrowsBoard(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{
		{ID: "k1", ColumnID: "c-blog", Title: "alpha"},
		{ID: "k2", ColumnID: "c-blog", Title: "beta"},
		{ID: "k3", ColumnID: "c-todo", Title: "alpine"},
	}
	m := readyModel(t, svc)

	m, _ = press(t, m, '/')
	if m.form == nil || m.form.kind != formSearch {
		t.Fatalf("/ did not open the search form, form=%v", m.form)
	}
	m, _ = typeText(t, m, "al")
	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m, _ = update(t, m, execMsg(t, cmd))

	if m.form != nil {
		t.Fatal("search form still open after submit")
	}
	if m.search != "al" {
		t.Errorf("m.search = %q, want \"al\"", m.search)
	}
	got := plain(m.View().Content)
	if !strings.Contains(got, "alpha") {
		t.Errorf("matching card alpha missing after filter:\n%s", got)
	}
	if !strings.Contains(got, "alpine") {
		t.Errorf("matching card alpine missing after filter:\n%s", got)
	}
	if strings.Contains(got, "beta") {
		t.Errorf("non-matching card beta still visible:\n%s", got)
	}
	// The column title bar sets the name and the count into the top border:
	// ╭─ Backlog ──── 1 ─╮
	if !regexp.MustCompile(`Backlog ─+ 1`).MatchString(got) {
		t.Errorf("backlog title-bar count did not narrow to 1:\n%s", got)
	}
	if n := m.lists[0].Items(); len(n) != 1 {
		t.Errorf("backlog items = %d, want 1", len(n))
	}
}

// TestSearchMatchesDescriptionAndCase mirrors the CLI `card list --search`
// filter semantics: case-insensitive substring over title and description.
func TestSearchMatchesDescriptionAndCase(t *testing.T) {
	desc := "login flow"
	svc := newBoardService()
	svc.cards = []store.Card{
		{ID: "k1", ColumnID: "c-blog", Title: "Auth", Description: &desc},
		{ID: "k2", ColumnID: "c-blog", Title: "LOGOUT"},
		{ID: "k3", ColumnID: "c-blog", Title: "Settings"},
	}
	m := readyModel(t, svc)

	m, _ = press(t, m, '/')
	m, _ = typeText(t, m, "LOGIN")
	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m, _ = update(t, m, execMsg(t, cmd))

	if got := m.lists[0].Items(); len(got) != 1 {
		t.Fatalf("visible cards = %d, want 1 (title/description case-insensitive match)", len(got))
	}
	if id := m.lists[0].Items()[0].(cardItem).card.ID; id != "k1" {
		t.Errorf("matching card = %q, want k1", id)
	}
}

// TestSearchEscRestoresFilter: esc on the / overlay cancels the pending query
// and leaves the previous filter in place.
func TestSearchEscRestoresFilter(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{
		{ID: "k1", ColumnID: "c-blog", Title: "alpha"},
		{ID: "k2", ColumnID: "c-blog", Title: "beta"},
	}
	m := readyModel(t, svc)

	m, _ = press(t, m, '/')
	m, _ = typeText(t, m, "al")
	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m, _ = update(t, m, execMsg(t, cmd))
	if n := m.lists[0].Items(); len(n) != 1 {
		t.Fatalf("filtered items = %d, want 1", len(n))
	}

	m, _ = press(t, m, '/')
	if f := m.form; f == nil || f.fields[0].input.Value() != "al" {
		t.Fatalf("search form not seeded with current filter: form=%v", f)
	}
	m, _ = typeText(t, m, "be")
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.form != nil {
		t.Fatal("esc did not close the search form")
	}
	if m.search != "al" {
		t.Errorf("esc changed the filter: search=%q, want \"al\"", m.search)
	}
	if n := m.lists[0].Items(); len(n) != 1 {
		t.Errorf("after esc items = %d, want 1 (filter preserved)", len(n))
	}
}

// TestSearchEmptyClearsFilter: submitting an empty query clears the filter and
// restores every card.
func TestSearchEmptyClearsFilter(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{
		{ID: "k1", ColumnID: "c-blog", Title: "alpha"},
		{ID: "k2", ColumnID: "c-blog", Title: "beta"},
	}
	m := readyModel(t, svc)

	m, _ = press(t, m, '/')
	m, _ = typeText(t, m, "al")
	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m, _ = update(t, m, execMsg(t, cmd))
	if n := m.lists[0].Items(); len(n) != 1 {
		t.Fatalf("filtered items = %d, want 1", len(n))
	}

	m, _ = press(t, m, '/')
	m, cmd = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	m, cmd = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	m, cmd = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m, _ = update(t, m, execMsg(t, cmd))

	if m.search != "" {
		t.Errorf("m.search = %q, want empty", m.search)
	}
	if n := m.lists[0].Items(); len(n) != 2 {
		t.Errorf("after clearing items = %d, want 2", len(n))
	}
}

// TestSearchSurvivesRefetch: a mutation re-fetch rebuilds the board from the
// same filter — the active search is never dropped by applyFetch or the poll.
func TestSearchSurvivesRefetch(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{
		{ID: "k1", ColumnID: "c-blog", Title: "alpha"},
		{ID: "k2", ColumnID: "c-blog", Title: "beta"},
	}
	m := readyModel(t, svc)

	m, _ = press(t, m, '/')
	m, _ = typeText(t, m, "al")
	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m, _ = update(t, m, execMsg(t, cmd))

	m, _ = update(t, m, fetchMsg{ws: svc.ws, board: svc.board, cols: svc.cols, cards: svc.cards, status: svc.status})
	if m.search != "al" {
		t.Errorf("refetch dropped the filter: %q", m.search)
	}
	if n := m.lists[0].Items(); len(n) != 1 {
		t.Errorf("after refetch items = %d, want 1", len(n))
	}
}
