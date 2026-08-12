package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"loom/internal/store"
)

// TestHelpOverlayRendersAndCloses: ? opens the keymap overlay listing the
// canonical §3.5 bindings, and esc/q both close it.
func TestHelpOverlayRendersAndCloses(t *testing.T) {
	m := readyModel(t, newBoardService())
	m, _ = press(t, m, '?')
	if m.help == nil {
		t.Fatal("? did not open the help overlay")
	}
	got := plain(m.View().Content)
	for _, want := range []string{
		"Keymap",
		"search/filter cards",
		"switch board",
		"switch workspace",
		"quit (confirm if sessions attached)",
		"open card session",
		"kill card session",
		"esc/q close",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("help overlay missing %q:\n%s", want, got)
		}
	}

	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.help != nil {
		t.Fatal("esc did not close the help overlay")
	}

	m, _ = press(t, m, '?')
	m, _ = press(t, m, 'q')
	if m.help != nil {
		t.Fatal("q did not close the help overlay")
	}
}

// TestHelpSwallowsKeys: board navigation must not leak through while the
// overlay is up.
func TestHelpSwallowsKeys(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{{ID: "k1", ColumnID: "c-blog", Title: "alpha"}}
	m := readyModel(t, svc)
	m, _ = press(t, m, '?')
	focus := m.focus
	m, _ = press(t, m, 'j')
	if m.help == nil {
		t.Fatal("board key closed the help overlay")
	}
	if m.focus != focus {
		t.Errorf("board key active while help open: focus %d -> %d", focus, m.focus)
	}
}