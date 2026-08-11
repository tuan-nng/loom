package tui

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"loom/internal/board"
	"loom/internal/session"
	"loom/internal/store"
)

// ansiRE strips the SGR/styling escape sequences lipgloss interleaves between
// styled runes, so assertions can match plain text.
var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

// fakeService stubs the whole Service seam with hand-set data, recording the
// session writes so tests assert the open/kill handoffs.
type fakeService struct {
	ws        store.Workspace
	board     store.Board
	cols      []store.Column
	cards     []store.Card
	status    map[string]session.SessionStatus
	err       error
	statusErr error
	defaultA  string
	openErr   error
	closeErr  error
	openCalls []struct {
		cardID string
		detach bool
	}
	closeCalls []string

	createInput store.CardInput
	createErr   error
	createOut   *store.Card
	updateID    string
	updateInput store.CardUpdate
	updateErr   error
	updateOut   *store.Card
	createCol   []struct {
		boardID, name, stage string
	}
	createColErr error
	moveCalls    []struct {
		cardID, toColumnID string
		beforeID, afterID  *string
	}
	moveErr error
	moveOut *store.Card

	codebases  []store.Codebase
	cbErr      error
	runs       map[string][]store.CardRun
	runsErr    error
	runsCalled []string

	workspaces   []store.Workspace
	boards       []store.Board
	showBoardID  string
	showBoardErr error
	switchWsID   string
	switchWsErr  error
}

func (f fakeService) ResolveSelection() (store.Workspace, store.Board, error) {
	return f.ws, f.board, f.err
}

func (f fakeService) ListColumns(string) ([]store.Column, error) {
	return f.cols, f.err
}

func (f fakeService) ListCardsByBoard(string) ([]store.Card, error) {
	return f.cards, f.err
}

func (f fakeService) SessionStatus(context.Context) (map[string]session.SessionStatus, error) {
	return f.status, f.statusErr
}

func (f *fakeService) OpenCard(_ context.Context, cardID string, detach bool) error {
	f.openCalls = append(f.openCalls, struct {
		cardID string
		detach bool
	}{cardID, detach})
	return f.openErr
}

func (f *fakeService) CloseCard(_ context.Context, cardID string) error {
	f.closeCalls = append(f.closeCalls, cardID)
	return f.closeErr
}

// TmuxAttach builds the canonical attach handoff argv (same shape the CLI
// adapter produces) so the board's ExecProcess can be asserted without tmux.
func (f fakeService) TmuxAttach(cardID string) (*exec.Cmd, error) {
	return exec.Command("tmux", "-L", "loom", "attach-session", "-t", session.SessionName(cardID)), nil
}

func (f fakeService) DefaultAgent() string { return f.defaultA }

func (f *fakeService) CreateCard(in store.CardInput) (store.Card, error) {
	f.createInput = in
	if f.createErr != nil {
		return store.Card{}, f.createErr
	}
	if f.createOut != nil {
		return *f.createOut, nil
	}
	return store.Card{ID: "new1", ColumnID: in.ColumnID, BoardID: f.board.ID, Title: in.Title, Priority: in.Priority, Agent: in.Agent}, nil
}

func (f *fakeService) UpdateCard(id string, u store.CardUpdate) (store.Card, error) {
	f.updateID = id
	f.updateInput = u
	if f.updateErr != nil {
		return store.Card{}, f.updateErr
	}
	if f.updateOut != nil {
		return *f.updateOut, nil
	}
	var card store.Card
	for _, c := range f.cards {
		if c.ID == id {
			card = c
			break
		}
	}
	if u.Title != nil {
		card.Title = *u.Title
	}
	return card, nil
}

func (f fakeService) GetCard(id string) (store.Card, error) {
	for _, c := range f.cards {
		if c.ID == id {
			return c, nil
		}
	}
	return store.Card{}, errors.New("card not found")
}

func (f fakeService) GetCodebase(id string) (store.Codebase, error) {
	for _, cb := range f.codebases {
		if cb.ID == id {
			return cb, nil
		}
	}
	return store.Codebase{}, errors.New("codebase not found")
}

func (f *fakeService) RunsForCard(cardID string) ([]store.CardRun, error) {
	f.runsCalled = append(f.runsCalled, cardID)
	if f.runsErr != nil {
		return nil, f.runsErr
	}
	return f.runs[cardID], nil
}

func (f *fakeService) CreateColumn(boardID, name, stage string) (store.Column, error) {
	f.createCol = append(f.createCol, struct {
		boardID, name, stage string
	}{boardID, name, stage})
	if f.createColErr != nil {
		return store.Column{}, f.createColErr
	}
	return store.Column{ID: "col-new", BoardID: boardID, Name: name, Stage: stage}, nil
}

func (f *fakeService) MoveCard(_ context.Context, cardID, toColumnID string, beforeID, afterID *string) (store.Card, error) {
	f.moveCalls = append(f.moveCalls, struct {
		cardID, toColumnID string
		beforeID, afterID  *string
	}{cardID, toColumnID, beforeID, afterID})
	if f.moveErr != nil {
		return store.Card{}, f.moveErr
	}
	if f.moveOut != nil {
		return *f.moveOut, nil
	}
	for i := range f.cards {
		if f.cards[i].ID == cardID {
			f.cards[i].ColumnID = toColumnID
			return f.cards[i], nil
		}
	}
	return store.Card{}, errors.New("card not found")
}

func (f fakeService) ListBoards(string) ([]store.Board, error) {
	return f.boards, f.err
}

func (f fakeService) ListWorkspaces() ([]store.Workspace, error) {
	return f.workspaces, f.err
}

func (f *fakeService) ShowBoard(boardID string) (store.Board, error) {
	f.showBoardID = boardID
	if f.showBoardErr != nil {
		return store.Board{}, f.showBoardErr
	}
	for _, b := range f.boards {
		if b.ID == boardID {
			return b, nil
		}
	}
	return f.board, nil
}

func (f *fakeService) SwitchWorkspace(workspaceID string) (store.Workspace, error) {
	f.switchWsID = workspaceID
	if f.switchWsErr != nil {
		return store.Workspace{}, f.switchWsErr
	}
	for _, w := range f.workspaces {
		if w.ID == workspaceID {
			return w, nil
		}
	}
	return f.ws, nil
}

// execMsg runs a cmd and returns the msg it produces. Only safe for the plain
// cmds in these tests (never the attach ExecProcess, which would exec tmux).
func execMsg(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	return cmd()
}

func defaultColumns() []store.Column {
	var needs = []struct{ name, id string }{
		{"Backlog", "c-blog"}, {"To Do", "c-todo"}, {"In Progress", "c-dev"},
		{"Review", "c-review"}, {"Done", "c-done"},
	}
	cols := make([]store.Column, len(needs))
	for i, n := range needs {
		cols[i] = store.Column{ID: n.id, BoardID: "b1", Name: n.name, Position: i * 1000}
	}
	return cols
}

// readyModel returns a fully-typed Model in phaseReady driven through its
// real Init → fetch cycle with the given service, laid out at 120x30. Callers
// then drive it with keys.
func readyModel(t *testing.T, svc Service) Model {
	t.Helper()
	m, _ := New(svc).Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	msg := m.Init()()
	m, _ = m.Update(msg)
	return m.(Model)
}

// newBoardService wraps fakeService so tests read like the production seam.
// defaultA mirrors config.Default's agent default ("claude").
func newBoardService() *fakeService {
	return &fakeService{
		ws:       store.Workspace{ID: "w1", Name: "loom"},
		board:    store.Board{ID: "b1", WorkspaceID: "w1", Name: "board"},
		cols:     defaultColumns(),
		defaultA: "claude",
	}
}

func press(t *testing.T, m Model, code rune) (Model, tea.Cmd) {
	t.Helper()
	nm, cmd := m.Update(tea.KeyPressMsg{Code: code})
	return nm.(Model), cmd
}

// typeText drives printable characters into the focused field, one
// KeyPressMsg per rune. press() only sets Code, but textinput inserts from
// msg.Text — so typing needs both fields set (Code to match bindings, Text to
// insert).
func typeText(t *testing.T, m Model, s string) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, r := range s {
		var nm tea.Model
		nm, cmd = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = nm.(Model)
	}
	return m, cmd
}

// pressKey drives any key including special keys and modifiers (shift+tab
// etc.) via its tea.KeyPressMsg directly.
func pressKey(t *testing.T, m Model, k tea.KeyPressMsg) (Model, tea.Cmd) {
	t.Helper()
	nm, cmd := m.Update(k)
	return nm.(Model), cmd
}

// update drives msg through the model and asserts the Update contract (same
// concrete type out as in).
func update(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	nm, cmd := m.Update(msg)
	return nm.(Model), cmd
}

// isQuitCmd reports whether cmd produces a tea.QuitMsg when executed. Only
// called on cmds the model returns from key handling.
func isQuitCmd(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// TestLayoutRendersFiveColumns checks the ready board's view carries every
// column header plus the workspace › board status line.
func TestLayoutRendersFiveColumns(t *testing.T) {
	m := readyModel(t, newBoardService())
	content := plain(m.View().Content)
	for _, name := range []string{"Backlog", "To Do", "In Progress", "Review", "Done"} {
		if !strings.Contains(content, name) {
			t.Errorf("board layout missing column %q:\n%s", name, content)
		}
	}
	if !strings.Contains(content, "loom › board") {
		t.Errorf("status bar missing selection:\n%s", content)
	}
}

// TestNavigation tests cross-column focus (l/h) and in-column cursor (j).
func TestNavigation(t *testing.T) {
	m := readyModel(t, newBoardService())

	for i := 1; i < 4; i++ {
		m, _ = press(t, m, 'l')
		if m.focus != i {
			t.Errorf("after %d×l, focus = %d, want %d", i, m.focus, i)
		}
	}
	for i := 2; i >= 0; i-- {
		m, _ = press(t, m, 'h')
		if m.focus != i {
			t.Errorf("after back h, focus = %d, want %d", m.focus, i)
		}
	}

	svc := newBoardService()
	svc.cards = []store.Card{
		{ID: "k1", ColumnID: "c-blog", Title: "alpha"},
		{ID: "k2", ColumnID: "c-blog", Title: "beta"},
		{ID: "k3", ColumnID: "c-blog", Title: "gamma"},
	}
	m = readyModel(t, svc)

	m, _ = press(t, m, 'j')
	if got := m.lists[0].Index(); got != 1 {
		t.Errorf("after j, focused index = %d, want 1", got)
	}
	m, _ = press(t, m, 'k')
	if got := m.lists[0].Index(); got != 0 {
		t.Errorf("after k, focused index = %d, want 0", got)
	}
}

// TestQuitWithoutSessions quits immediately when nothing runs.
func TestQuitWithoutSessions(t *testing.T) {
	m := readyModel(t, newBoardService())
	m, cmd := press(t, m, 'q')
	if !isQuitCmd(cmd) {
		t.Errorf("q with no sessions did not quit; confirmQuit=%v", m.confirmQuit)
	}
}

// TestQuitConfirmForce covers the q-with-sessions → overlay flow: q opens,
// n cancels and shows the board again, q→y quits; Q always force-quits.
func TestQuitConfirmForce(t *testing.T) {
	svc := newBoardService()
	svc.status = map[string]session.SessionStatus{"k1": {Running: true}}
	m := readyModel(t, svc)

	m, cmd := press(t, m, 'q')
	if isQuitCmd(cmd) || !m.confirmQuit {
		t.Fatalf("q with sessions should open overlay, confirmQuit=%v", m.confirmQuit)
	}

	m, cmd = press(t, m, 'n')
	if isQuitCmd(cmd) || m.confirmQuit {
		t.Fatalf("n should cancel the overlay, confirmQuit=%v", m.confirmQuit)
	}

	m, cmd = press(t, m, 'q')
	if !m.confirmQuit {
		t.Fatalf("q after cancel should reopen overlay")
	}
	m, cmd = press(t, m, 'y')
	if !isQuitCmd(cmd) {
		t.Errorf("y on overlay should quit, got cmd=%v", cmd)
	}

	m, cmd = press(t, m, 'Q')
	if !isQuitCmd(cmd) {
		t.Errorf("Q should force-quit, got cmd=%v", cmd)
	}
}

// TestInitErrRoutesToErrorView checks ErrNotInitialized renders the "run loom
// init" hint instead of a board.
func TestInitErrRoutesToErrorView(t *testing.T) {
	svc := &fakeService{err: board.ErrNotInitialized}
	m, _ := New(svc).Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m, _ = m.Update(fetchMsg{err: board.ErrNotInitialized})
	if got := plain(m.View().Content); !strings.Contains(got, "run loom init") {
		t.Errorf("error view = %q, want 'run loom init' hint", got)
	}
}

// TestSearchFiltersBoard walks `/`: the search overlay opens, typing an
// enter-committed query narrows the board to title/description matches (same
// semantics as `card list --search`), the status bar shows the active filter,
// and esc cancels without changing the filter.
func TestSearchFiltersBoard(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{
		{ID: "k1", ColumnID: "c-blog", Title: "buy milk"},
		{ID: "k2", ColumnID: "c-todo", Title: "milk run"},
		{ID: "k3", ColumnID: "c-todo", Title: "wash car"},
		{ID: "k4", ColumnID: "c-blog", Title: "alpha", Description: strp("grocery errand")},
	}
	m := readyModel(t, svc)

	m, _ = press(t, m, '/')
	if !m.searching {
		t.Fatal("/ did not open search")
	}
	content := plain(m.View().Content)
	if !strings.Contains(content, "enter filter") {
		t.Errorf("search overlay missing hint:\n%s", content)
	}

	// Type and commit "milk": only the two milk cards survive, the count
	// headers drop the others.
	m, _ = typeText(t, m, "milk")
	m, _ = press(t, m, '\r')
	if m.searching || m.searchQuery != "milk" {
		t.Fatalf("searching=%v query=%q, want closed with milk", m.searching, m.searchQuery)
	}
	content = plain(m.View().Content)
	if !strings.Contains(content, "buy milk") || !strings.Contains(content, "milk run") {
		t.Errorf("filtered board missing matching card:\n%s", content)
	}
	if strings.Contains(content, "wash car") || strings.Contains(content, "alpha") {
		t.Errorf("filtered board still shows non-matching card:\n%s", content)
	}
	if !strings.Contains(content, "/milk") {
		t.Errorf("status bar missing filter indicator:\n%s", content)
	}

	// Esc cancels: reopening and pressing esc leaves the filter untouched.
	m, _ = press(t, m, '/')
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.searching || m.searchQuery != "milk" {
		t.Errorf("esc cancel: searching=%v query=%q, want closed with milk kept", m.searching, m.searchQuery)
	}
}

// TestSearchMatchesDescription confirms the filter also matches the card
// description field, mirroring `card list --search`.
func TestSearchMatchesDescription(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{
		{ID: "k1", ColumnID: "c-blog", Title: "alpha", Description: strp("grocery errand")},
		{ID: "k2", ColumnID: "c-blog", Title: "beta"},
	}
	m := readyModel(t, svc)

	m, _ = press(t, m, '/')
	m, _ = typeText(t, m, "grocery")
	m, _ = press(t, m, '\r')
	content := plain(m.View().Content)
	if !strings.Contains(content, "alpha") {
		t.Errorf("title-only board lost description match:\n%s", content)
	}
	if strings.Contains(content, "beta") {
		t.Errorf("description filter leaked a non-matching card:\n%s", content)
	}
}

// TestSearchEmptyCommitClearsFilter: committing an empty query (the search
// overlay reopens pre-filled, so clear it with backspaces) restores the full
// board.
func TestSearchEmptyCommitClearsFilter(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{{ID: "k1", ColumnID: "c-blog", Title: "alpha"}}
	m := readyModel(t, svc)

	m, _ = press(t, m, '/')
	m, _ = typeText(t, m, "alpha")
	m, _ = press(t, m, '\r')
	if m.searchQuery != "alpha" {
		t.Fatalf("query = %q, want alpha", m.searchQuery)
	}

	m, _ = press(t, m, '/')
	for range "alpha" {
		m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	m, _ = press(t, m, '\r')
	if m.searchQuery != "" {
		t.Errorf("empty commit kept query %q, want cleared", m.searchQuery)
	}
	if got := plain(m.View().Content); !strings.Contains(got, "alpha") {
		t.Errorf("cleared filter still hides alpha:\n%s", got)
	}
}

// TestHelpOverlayRendersAndCloses: `?` renders the canonical §3.5 keymap fram
// the board; any key closes it.
func TestHelpOverlayRendersAndCloses(t *testing.T) {
	m := readyModel(t, newBoardService())
	m, _ = press(t, m, '?')
	if !m.help {
		t.Fatal("? did not open the help overlay")
	}
	content := plain(m.View().Content)
	for _, want := range []string{"switch board", "switch workspace", "search/filter cards", "kill session"} {
		if !strings.Contains(content, want) {
			t.Errorf("help overlay missing %q:\n%s", want, content)
		}
	}

	m, _ = press(t, m, 'x')
	if m.help {
		t.Error("help overlay did not close on a key")
	}
	content = plain(m.View().Content)
	if strings.Contains(content, "Help — loom keymap") {
		t.Errorf("help overlay still rendered after closing:\n%s", content)
	}
	if !strings.Contains(content, "loom › board") {
		t.Errorf("clicking away should restore the board:\n%s", content)
	}
}

// TestBoardSwitchPickerAndSubmit walks `s`: the picker opens with the current
// workspace's boards, cycling right selects the second board, and submit
// persists the selection through ShowBoard then re-fetches.
func TestBoardSwitchPickerAndSubmit(t *testing.T) {
	svc := newBoardService()
	svc.boards = []store.Board{
		{ID: "b1", WorkspaceID: "w1", Name: "board"},
		{ID: "b2", WorkspaceID: "w1", Name: "board2"},
	}
	m := readyModel(t, svc)

	m, cmd := press(t, m, 's')
	msg := execMsg(t, cmd).(boardsMsg)
	if msg.err != nil {
		t.Fatalf("boardsMsg err = %v", msg.err)
	}
	m, _ = update(t, m, msg)
	if m.form == nil || m.form.kind != formSwitchBoard {
		t.Fatalf("s did not open the board-switch form, form=%v", m.form)
	}
	if got := plain(m.View().Content); !strings.Contains(got, "Switch Board") {
		t.Errorf("switch-board view missing title:\n%s", got)
	}

	// Cycle right to board2 and submit.
	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyRight})
	m, cmd = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	msg2 := execMsg(t, cmd).(boardSwitchedMsg)
	if msg2.err != nil {
		t.Fatalf("boardSwitchedMsg err = %v", msg2.err)
	}
	if svc.showBoardID != "b2" {
		t.Errorf("ShowBoard called with %q, want b2", svc.showBoardID)
	}
	m, cmd = update(t, m, msg2)
	if !strings.Contains(m.note, "switched board") {
		t.Errorf("note = %q, want switch toast", m.note)
	}
	if cmd == nil {
		t.Error("afterBoardSwitched returned nil cmd, want re-fetch")
	}
}

// TestWorkspaceSwitchPickerAndSubmit walks `w`: the picker opens with all
// workspaces and submit persists through SwitchWorkspace then re-fetches.
func TestWorkspaceSwitchPickerAndSubmit(t *testing.T) {
	svc := newBoardService()
	svc.workspaces = []store.Workspace{
		{ID: "w1", Name: "loom"},
		{ID: "w2", Name: "other"},
	}
	m := readyModel(t, svc)

	m, cmd := press(t, m, 'w')
	msg := execMsg(t, cmd).(workspacesMsg)
	m, _ = update(t, m, msg)
	if m.form == nil || m.form.kind != formSwitchWorkspace {
		t.Fatalf("w did not open the workspace-switch form, form=%v", m.form)
	}
	if got := plain(m.View().Content); !strings.Contains(got, "Switch Workspace") {
		t.Errorf("switch-workspace view missing title:\n%s", got)
	}

	m, _ = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyRight})
	m, cmd = pressKey(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	msg2 := execMsg(t, cmd).(workspaceSwitchedMsg)
	if msg2.err != nil {
		t.Fatalf("workspaceSwitchedMsg err = %v", msg2.err)
	}
	if svc.switchWsID != "w2" {
		t.Errorf("SwitchWorkspace called with %q, want w2", svc.switchWsID)
	}
	m, cmd = update(t, m, msg2)
	if !strings.Contains(m.note, "switched workspace") {
		t.Errorf("note = %q, want switch toast", m.note)
	}
	if cmd == nil {
		t.Error("afterWorkspaceSwitched returned nil cmd, want re-fetch")
	}
}

func strp(s string) *string { return &s }

// TestBadgeAndMarkersRender checks the §14 badge and ●/◉ markers render from
// the card's resolved agent and live status.
func TestBadgeAndMarkersRender(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{
		{ID: "k1", ColumnID: "c-blog", Title: "alpha", Agent: strp("opencode")},
		{ID: "k2", ColumnID: "c-blog", Title: "beta"}, // NULL agent → config default
	}
	svc.status = map[string]session.SessionStatus{
		"k1": {Running: true, Attached: true},
		"k2": {Running: true},
	}
	m := readyModel(t, svc)
	got := plain(m.View().Content)
	if !strings.Contains(got, "[oc]") {
		t.Errorf("opencode card missing [oc] badge:\n%s", got)
	}
	if !strings.Contains(got, "[cl]") {
		t.Errorf("default-agent card missing [cl] badge:\n%s", got)
	}
	if !strings.Contains(got, "◉") {
		t.Errorf("attached card missing ◉ marker:\n%s", got)
	}
	if !strings.Contains(got, "●") {
		t.Errorf("running card missing ● marker:\n%s", got)
	}
}

// TestSessionCompletionToast covers the running→gone transition: a poll that
// finds a previously-live card absent raises the detached-completion toast
// and drops its marker.
func TestSessionCompletionToast(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{{ID: "k1", ColumnID: "c-blog", Title: "alpha"}}
	svc.status = map[string]session.SessionStatus{"k1": {Running: true}}
	m := readyModel(t, svc)
	if got := plain(m.View().Content); !strings.Contains(got, "●") {
		t.Fatalf("expected ● before completion:\n%s", got)
	}

	m, _ = update(t, m, statusMsg{status: map[string]session.SessionStatus{}})
	if !strings.Contains(m.note, "session ended") || !strings.Contains(m.note, "alpha") {
		t.Errorf("note = %q, want session-ended toast naming alpha", m.note)
	}
	if got := plain(m.View().Content); strings.Contains(got, "●") {
		t.Errorf("marker still shown after session end:\n%s", got)
	}
}

// TestKillToastSuppressesCompletionToast verifies the killedByUser bookkeeping:
// a card killed with K does not get re-toasted as "session ended" by the next
// poll, but a fresh session ensured after the kill is a new run whose natural
// end must toast again (the flag clears on reopen).
func TestKillToastSuppressesCompletionToast(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{{ID: "k1", ColumnID: "c-blog", Title: "alpha"}}
	svc.status = map[string]session.SessionStatus{"k1": {Running: true}}
	m := readyModel(t, svc)

	m, cmd := press(t, m, 'K')
	km := execMsg(t, cmd).(killMsg)
	m, _ = update(t, m, km)
	if !strings.Contains(m.note, "killed") {
		t.Fatalf("after K note = %q, want killed toast", m.note)
	}
	m, _ = update(t, m, statusMsg{status: map[string]session.SessionStatus{}})
	if strings.Contains(m.note, "session ended") {
		t.Errorf("killed card re-toasted as session ended: %q", m.note)
	}

	// reopen a fresh session (ignore the attach handoff — it would exec tmux)
	m, cmd = press(t, m, '\r')
	om := execMsg(t, cmd).(openMsg)
	m, _ = update(t, m, om)
	m, _ = update(t, m, statusMsg{status: map[string]session.SessionStatus{"k1": {Running: true}}})
	m, _ = update(t, m, statusMsg{status: map[string]session.SessionStatus{}})
	if !strings.Contains(m.note, "session ended") {
		t.Errorf("re-opened card's natural end was suppressed: %q", m.note)
	}
}

// TestOpenCardEnsuresThenAttaches walks Enter: the ensure runs detached (the
// board owns the terminal), the openMsg then yields the attach handoff, and a
// failed ensure degrades to a toast without attaching.
func TestOpenCardEnsuresThenAttaches(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{{ID: "k1", ColumnID: "c-blog", Title: "alpha"}}
	m := readyModel(t, svc)

	m, cmd := press(t, m, '\r')
	om := execMsg(t, cmd).(openMsg)
	if len(svc.openCalls) != 1 {
		t.Fatalf("OpenCard calls = %d, want 1", len(svc.openCalls))
	}
	if !svc.openCalls[0].detach {
		t.Error("OpenCard detach = false, want true (ensure then attach via ExecProcess)")
	}
	if om.err != nil {
		t.Fatalf("openMsg err = %v", om.err)
	}
	m, cmd = update(t, m, om)
	if cmd == nil {
		t.Fatal("afterOpen returned nil cmd, want the attach handoff")
	}

	m, cmd = update(t, m, attachDoneMsg{cardID: "k1"})
	if m.note != "" {
		t.Errorf("after attach done note = %q, want empty", m.note)
	}
	if cmd == nil {
		t.Error("afterAttach returned nil cmd, want a status refresh")
	}
}

// TestOpenFailureToasts checks a failed ensure never attaches and surfaces the
// service error in the status bar.
func TestOpenFailureToasts(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{{ID: "k1", ColumnID: "c-blog", Title: "alpha"}}
	svc.openErr = errors.New("boom")
	m := readyModel(t, svc)

	m, cmd := press(t, m, '\r')
	m, _ = update(t, m, execMsg(t, cmd))
	if !strings.Contains(m.note, "boom") {
		t.Errorf("note = %q, want open failure toast", m.note)
	}
}

// TestKillCard verifies K routes the focused card to CloseCard and the
// success clears its marker via a status refresh.
func TestKillCard(t *testing.T) {
	svc := newBoardService()
	svc.cards = []store.Card{{ID: "k1", ColumnID: "c-blog", Title: "alpha"}}
	svc.status = map[string]session.SessionStatus{"k1": {Running: true}}
	m := readyModel(t, svc)

	m, cmd := press(t, m, 'K')
	km := execMsg(t, cmd).(killMsg)
	if len(svc.closeCalls) != 1 || svc.closeCalls[0] != "k1" {
		t.Fatalf("CloseCard calls = %v, want [k1]", svc.closeCalls)
	}
	m, cmd = update(t, m, km)
	if cmd == nil {
		t.Error("afterKill returned nil cmd, want a status refresh")
	}
	if !strings.Contains(m.note, "killed") {
		t.Errorf("note = %q, want killed toast", m.note)
	}
}

// TestHandoffCommand pins the tmux attach-session argv the board runs via
// tea.ExecProcess (integration handoff, T17 acceptance).
func TestHandoffCommand(t *testing.T) {
	c, err := newBoardService().TmuxAttach("k1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tmux", "-L", "loom", "attach-session", "-t", "loom-k1"}
	if !reflect.DeepEqual(c.Args, want) {
		t.Errorf("attach argv = %v, want %v", c.Args, want)
	}
}

// TestPollCadenceArmsOnReady confirms the first fetchMsg arms the 2s ticker
// and that each pollMsg re-arms it (tea.Every is one-shot, so the handler
// must return a fresh poll command or the board freezes after one tick).
func TestPollCadenceArmsOnReady(t *testing.T) {
	svc := newBoardService()
	m := New(svc)
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	msg := m.Init()()
	m, cmd := update(t, m, msg)
	if cmd == nil {
		t.Fatal("fetch landing did not arm the poll cmd")
	}
	m, cmd = update(t, m, pollMsg{})
	if cmd == nil {
		t.Error("pollMsg did not re-arm the poll")
	}
	_ = m
}
