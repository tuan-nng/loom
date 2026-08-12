package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"loom/internal/store"
)

// TestBoardSwitchOpensCyclesSubmits walks s: the picker opens seeded at the
// current board, left/right cycles, enter submits via ShowBoard (persists the
// selection), and the re-fetch lands the new board.
func TestBoardSwitchOpensCyclesSubmits(t *testing.T) {
	svc := newBoardService()
	svc.boards = []store.Board{
		{ID: "b1", WorkspaceID: "w1", Name: "board"},
		{ID: "b2", WorkspaceID: "w1", Name: "other"},
	}
	m := readyModel(t, svc)

	m, _ = press(t, m, 's')
	if m.form == nil || m.form.kind != formBoardSwitch {
		t.Fatalf("s did not open the board picker, form=%v", m.form)
	}
	if got := m.form.fields[0].selectedValue(); got != "b1" {
		t.Fatalf("picker seeded at %q, want b1 (current board)", got)
	}
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyRight})
	if got := m.form.fields[0].selectedValue(); got != "b2" {
		t.Fatalf("after right, selected = %q, want b2", got)
	}
	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m, _ = update(t, m, execMsg(t, cmd))
	if svc.showBoardID != "b2" {
		t.Fatalf("ShowBoard id = %q, want b2", svc.showBoardID)
	}
	if !strings.Contains(m.note, "other") {
		t.Errorf("note = %q, want the switched toast", m.note)
	}

	svc.board = store.Board{ID: "b2", WorkspaceID: "w1", Name: "other"}
	m, _ = update(t, m, fetchMsg{ws: svc.ws, board: svc.board, cols: svc.cols, cards: svc.cards, status: svc.status})
	if m.board.ID != "b2" {
		t.Errorf("board after refetch = %q, want b2", m.board.ID)
	}
	if m.focus != 0 {
		t.Errorf("column focus after switch = %d, want 0", m.focus)
	}
}

// TestWorkspaceSwitchSubmitsPersists walks w: seeded at the current
// workspace, cycles, enter submits via SwitchWorkspace (persists the
// selection, clears the board selection per ADR-001 §6).
func TestWorkspaceSwitchSubmitsPersists(t *testing.T) {
	svc := newBoardService()
	svc.workspaces = []store.Workspace{
		{ID: "w1", Name: "loom"},
		{ID: "w2", Name: "other"},
	}
	m := readyModel(t, svc)

	m, _ = press(t, m, 'w')
	if m.form == nil || m.form.kind != formWorkspaceSwitch {
		t.Fatalf("w did not open the workspace picker, form=%v", m.form)
	}
	if got := m.form.fields[0].selectedValue(); got != "w1" {
		t.Fatalf("picker seeded at %q, want w1 (current workspace)", got)
	}
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyRight})
	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m, _ = update(t, m, execMsg(t, cmd))
	if svc.switchWsID != "w2" {
		t.Fatalf("SwitchWorkspace id = %q, want w2", svc.switchWsID)
	}
	if !strings.Contains(m.note, "other") {
		t.Errorf("note = %q, want the switched toast", m.note)
	}
}

// TestPickerEscCancelsWithoutCall: esc on either picker closes without any
// service write — the selection is untouched.
func TestPickerEscCancelsWithoutCall(t *testing.T) {
	svc := newBoardService()
	svc.boards = []store.Board{{ID: "b1", WorkspaceID: "w1", Name: "board"}}
	svc.workspaces = []store.Workspace{{ID: "w1", Name: "loom"}}
	m := readyModel(t, svc)

	m, _ = press(t, m, 's')
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.form != nil {
		t.Fatal("esc did not close the board picker")
	}
	if svc.showBoardID != "" {
		t.Errorf("esc triggered ShowBoard(%q)", svc.showBoardID)
	}

	m, _ = press(t, m, 'w')
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.form != nil {
		t.Fatal("esc did not close the workspace picker")
	}
	if svc.switchWsID != "" {
		t.Errorf("esc triggered SwitchWorkspace(%q)", svc.switchWsID)
	}
}

// TestPickerOpenErrNoOverlay: a failed list read degrades to a status-bar
// notice instead of opening an overlay.
func TestPickerOpenErrNoOverlay(t *testing.T) {
	svc := newBoardService()
	svc.boardsErr = errors.New("db down")
	m := readyModel(t, svc)
	m, _ = press(t, m, 's')
	if m.form != nil {
		t.Fatal("board picker opened despite the list error")
	}
	if !strings.Contains(m.note, "db down") {
		t.Errorf("note = %q, want the list error", m.note)
	}

	svc2 := newBoardService()
	svc2.workspacesErr = errors.New("db down")
	m2 := readyModel(t, svc2)
	m2, _ = press(t, m2, 'w')
	if m2.form != nil {
		t.Fatal("workspace picker opened despite the list error")
	}
}

// TestSwitchClearsBoardScopedKillSuppression: kill-suppression is board-scoped
// — a mutation re-fetch on the same board preserves it, but a board transition
// (s/w) clears it so a visited card's natural end toasts again when the user
// returns.
func TestSwitchClearsBoardScopedKillSuppression(t *testing.T) {
	svc := newBoardService()
	m := readyModel(t, svc)
	m.killedByUser = map[string]bool{"k1": true}

	m, _ = update(t, m, fetchMsg{ws: svc.ws, board: svc.board, cols: svc.cols, cards: svc.cards, status: svc.status})
	if len(m.killedByUser) != 1 {
		t.Fatalf("same-board refetch reset kill suppression: %v", m.killedByUser)
	}

	m.killedByUser = map[string]bool{"k1": true}
	other := store.Board{ID: "b2", WorkspaceID: "w1", Name: "other"}
	m, _ = update(t, m, fetchMsg{ws: svc.ws, board: other, cols: svc.cols, cards: svc.cards, status: svc.status})
	if len(m.killedByUser) != 0 {
		t.Errorf("board transition kept kill suppression: %v", m.killedByUser)
	}
}

// TestSwitchSubmitErrToasts: a ShowBoard/SwitchWorkspace failure on submit
// (not on list) closes the picker and degrades to a toast without re-fetching.
func TestSwitchSubmitErrToasts(t *testing.T) {
	svc := newBoardService()
	svc.boards = []store.Board{{ID: "b1", WorkspaceID: "w1", Name: "board"}}
	svc.showBoardErr = errors.New("boom")
	m := readyModel(t, svc)

	m, _ = press(t, m, 's')
	m, cmd := pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m, _ = update(t, m, execMsg(t, cmd))
	if m.form != nil {
		t.Fatal("board picker still open after failed submit")
	}
	if !strings.Contains(m.note, "boom") {
		t.Errorf("note = %q, want the ShowBoard error", m.note)
	}

	svc2 := newBoardService()
	svc2.workspaces = []store.Workspace{{ID: "w1", Name: "loom"}}
	svc2.switchWsErr = errors.New("boom")
	m2 := readyModel(t, svc2)
	m2, _ = press(t, m2, 'w')
	m2, cmd = pressKey(t, m2, tea.KeyPressMsg{Code: tea.KeyEnter})
	m2, _ = update(t, m2, execMsg(t, cmd))
	if m2.form != nil {
		t.Fatal("workspace picker still open after failed submit")
	}
	if !strings.Contains(m2.note, "boom") {
		t.Errorf("note = %q, want the SwitchWorkspace error", m2.note)
	}
}

// TestPickerEmptyListNoOverlay: an empty board/workspace set degrades to a
// notice rather than an empty picker.
func TestPickerEmptyListNoOverlay(t *testing.T) {
	svc := newBoardService()
	m := readyModel(t, svc)
	m, _ = press(t, m, 's')
	if m.form != nil {
		t.Fatal("empty board set opened an overlay")
	}
	if !strings.Contains(m.note, "no boards") {
		t.Errorf("note = %q, want the no-boards notice", m.note)
	}

	svc2 := newBoardService()
	svc2.workspaces = nil
	m2 := readyModel(t, svc2)
	m2, _ = press(t, m2, 'w')
	if m2.form != nil {
		t.Fatal("empty workspace set opened an overlay")
	}
	if !strings.Contains(m2.note, "no workspaces") {
		t.Errorf("note = %q, want the no-workspaces notice", m2.note)
	}
}