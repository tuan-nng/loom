package tui

import (
	tea "charm.land/bubbletea/v2"

	"loom/internal/store"
)

// boardSwitchedMsg is the result of an s picker submit: the new board after
// ShowBoard persisted the selection (ADR-001 §6), ready for a re-fetch.
type boardSwitchedMsg struct {
	board store.Board
	err   error
}

// workspaceSwitchedMsg is the result of a w picker submit: the new workspace
// after SwitchWorkspace reset the board selection (ADR-001 §6).
type workspaceSwitchedMsg struct {
	ws  store.Workspace
	err error
}

// switchForm builds a single-field cycle picker seeded at the current
// selection: left/right cycles, enter submits, esc cancels — the same
// interaction the T18 cycle fields use. options are display labels, values
// the store-facing ids.
func switchForm(svc Service, kind formKind, title, label string, options, values []string, seed int) *form {
	f := &form{
		kind:   kind,
		title:  title,
		svc:    svc,
		fields: []field{cycleField(label, options, values, seed)},
	}
	f.syncFocus()
	return f
}

// openBoardPicker lists the current workspace's boards (s, ADR-001 §3.5).
// The list read is synchronous — the same DB-only call the CLI makes — and a
// failure or an empty board set degrades to a status-bar notice without
// opening an overlay.
func (m Model) openBoardPicker() (Model, tea.Cmd) {
	boards, err := m.svc.ListBoards(m.ws.ID)
	if err != nil {
		m.note = "switch board: " + err.Error()
		return m, nil
	}
	if len(boards) == 0 {
		m.note = "switch board: no boards"
		return m, nil
	}
	names := make([]string, len(boards))
	ids := make([]string, len(boards))
	for i, b := range boards {
		names[i], ids[i] = b.Name, b.ID
	}
	m.form = switchForm(m.svc, formBoardSwitch, "Switch Board", "board", names, ids, indexOf(ids, m.board.ID))
	return m, nil
}

// openWorkspacePicker lists every workspace (w, ADR-001 §3.5), seeded at the
// current one. Same open semantics as openBoardPicker.
func (m Model) openWorkspacePicker() (Model, tea.Cmd) {
	workspaces, err := m.svc.ListWorkspaces()
	if err != nil {
		m.note = "switch workspace: " + err.Error()
		return m, nil
	}
	if len(workspaces) == 0 {
		m.note = "switch workspace: no workspaces"
		return m, nil
	}
	names := make([]string, len(workspaces))
	ids := make([]string, len(workspaces))
	for i, ws := range workspaces {
		names[i], ids[i] = ws.Name, ws.ID
	}
	m.form = switchForm(m.svc, formWorkspaceSwitch, "Switch Workspace", "workspace", names, ids, indexOf(ids, m.ws.ID))
	return m, nil
}

// afterBoardSwitched folds an s submit: success re-fetches the board (the
// selection and its columns/cards land via applyFetch's board-transition
// reset); failure becomes a toast.
func (m Model) afterBoardSwitched(msg boardSwitchedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.note = "switch board: " + msg.err.Error()
		return m, nil
	}
	m.note = "switched to " + msg.board.Name
	return m, m.fetchCmd()
}

// afterWorkspaceSwitched folds a w submit: success re-fetches, which resolves
// the new workspace's first board (SwitchWorkspace cleared the board
// selection, ADR-001 §6); failure becomes a toast.
func (m Model) afterWorkspaceSwitched(msg workspaceSwitchedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.note = "switch workspace: " + msg.err.Error()
		return m, nil
	}
	m.note = "switched to " + msg.ws.Name
	return m, m.fetchCmd()
}