// Package tui is the BubbleTea terminal UI (ADR-001 §5, DESIGN-002 §4.1). It
// renders the current board's five columns as a row of bubbles lists and owns
// the canonical §3.5 keymap: every key is bound here or routed to a stub that
// names the task shipping it, so the board is fully navigable and keyboard
// halts are deliberate. The service seam lets tests fake the whole board
// surface without a store or tmux.
package tui

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"loom/internal/board"
	"loom/internal/session"
	"loom/internal/store"
)

// Service is the surface the board renders and acts on, satisfied by a
// *board.Service wrapped with the CLI's config (the TUI consumes the same
// seam as the CLI, DESIGN-002 §4.2). SessionStatus is folded in so tests
// inject one fake instead of two seams; status also doubles as the startup
// reconcile (manager.Status finalizes disappeared sessions, ADR-001 §4.1
// step 3). The T17 session keys are the write half: OpenCard ensures a
// session (detach leaves it running headless), CloseCard kills + finalizes,
// TmuxAttach returns the attach handoff the TUI runs via tea.ExecProcess so
// BubbleTea owns the terminal, and DefaultAgent resolves card agent badges
// (DESIGN-002 §14). The T18 forms are the other write half: CreateCard,
// UpdateCard, GetCard, CreateColumn, and MoveCard (which applies the
// done-stage auto-kill rule, ADR-001 §4.1 step 4). The T20 extras complete
// the §3.5 surface: ListWorkspaces/ListBoards feed the s/w switch pickers,
// and ShowBoard/SwitchWorkspace persist the selection to ui_state (T12,
// ADR-001 §6).
type Service interface {
	ResolveSelection() (store.Workspace, store.Board, error)
	ListColumns(boardID string) ([]store.Column, error)
	ListCardsByBoard(boardID string) ([]store.Card, error)
	ListWorkspaces() ([]store.Workspace, error)
	ListBoards(workspaceID string) ([]store.Board, error)
	ShowBoard(boardID string) (store.Board, error)
	SwitchWorkspace(workspaceID string) (store.Workspace, error)
	SessionStatus(ctx context.Context) (map[string]session.SessionStatus, error)
	OpenCard(ctx context.Context, cardID string, detach bool) error
	CloseCard(ctx context.Context, cardID string) error
	TmuxAttach(cardID string) (*exec.Cmd, error)
	DefaultAgent() string
	CreateCard(in store.CardInput) (store.Card, error)
	UpdateCard(id string, u store.CardUpdate) (store.Card, error)
	GetCard(id string) (store.Card, error)
	GetCodebase(id string) (store.Codebase, error)
	CreateColumn(boardID, name, stage string) (store.Column, error)
	MoveCard(ctx context.Context, cardID, toColumnID string, beforeID, afterID *string) (store.Card, error)
	RunsForCard(cardID string) ([]store.CardRun, error)
}

// phase is the model's lifecycle state.
type phase int

const (
	phaseLoading phase = iota
	phaseReady
	phaseError
)

// fetchMsg carries one board snapshot: the resolved selection, its columns,
// the session states, and the error that failed any step. A nil/zero step is
// "not reached"; err short-circuits into the error view.
type fetchMsg struct {
	ws        store.Workspace
	board     store.Board
	cols      []store.Column
	cards     []store.Card
	status    map[string]session.SessionStatus
	statusErr error
	err       error
}

// pollMsg is the 2s session poll timer firing; the model answers with a
// statusMsg read off the critical path.
type pollMsg struct{}

// statusMsg carries one session-status tick (●/◉ markers, ADR-001 §3.5). A
// non-nil err degrades to a status-bar notice and keeps the last known state.
type statusMsg struct {
	status map[string]session.SessionStatus
	err    error
}

// openMsg is the result of an Enter ensure: the session exists (possibly
// freshly created) and the terminal may attach.
type openMsg struct {
	cardID string
	err    error
}

// killMsg is the result of a K kill+finalize.
type killMsg struct {
	cardID string
	err    error
}

// attachDoneMsg arrives when the tmux attach-session handoff exits (the user
// detached or it failed): BubbleTea restored the terminal and the board can
// resume.
type attachDoneMsg struct {
	cardID string
	err    error
}

// boardsMsg carries the current workspace's boards for the `s` switch picker.
// The listing is fetched off the critical path so the board key handler never
// blocks Update on a DB read.
type boardsMsg struct {
	boards []store.Board
	err    error
}

// workspacesMsg carries every workspace for the `w` switch picker.
type workspacesMsg struct {
	workspaces []store.Workspace
	err        error
}

// boardSwitchedMsg is the result of a board-switch submit: the persisted
// selection (ShowBoard writes {workspace, board} to ui_state) or the error.
type boardSwitchedMsg struct {
	board store.Board
	err   error
}

// workspaceSwitchedMsg is the result of a workspace-switch submit: the
// persisted selection (SwitchWorkspace writes {workspace, board: nil}) or the
// error.
type workspaceSwitchedMsg struct {
	ws  store.Workspace
	err error
}

// Model is the board application state.
type Model struct {
	svc          Service
	defaultAgent string // badge resolution (DESIGN-002 §14), from svc

	phase   phase
	errText string

	ws      store.Workspace
	board   store.Board
	columns []store.Column
	cards   []store.Card
	lists   []list.Model // one per column, same order as columns
	focus   int          // index of the focused column

	status map[string]session.SessionStatus

	// killedByUser are cards this board killed with K; the poll suppresses
	// their "session ended" toast so it never overwrites the kill notice.
	killedByUser map[string]bool

	width, height int

	note string // status-bar toast (stub key hints, session notices)

	confirmQuit bool // q pressed with sessions attached: overlay open

	form   *form       // n/N/m/e overlay open (T18); nil = board navigation active
	detail *cardDetail // `d` detail pane open (T19); nil = closed

	// search overlays the board on `/` (T20): searching gates every key to the
	// input, searchQuery is the applied filter (" = unfiltered). The active
	// query narrows the rendered board to title/description matches, the same
	// client-side semantics as `loom card list --search` (ADR-001 §3.5).
	search      textinput.Model
	searching   bool
	searchQuery string

	help bool // `?` help overlay open (T20); any key closes it

	// pendingFocus is the card the cursor should land on once the post-mutation
	// snapshot lands. The lists at submit time are stale (the fetch is in
	// flight), so refocus only records intent; applyPendingFocus consumes it in
	// applyFetch after the fresh lists build.
	pendingFocus string
}

// New returns the board model ready for tea.NewProgram. The default agent is
// read from the service so the badge never drifts from launch config.
func New(svc Service) Model {
	search := textinput.New()
	search.KeyMap = formInputKeyMap()
	return Model{
		svc:          svc,
		defaultAgent: svc.DefaultAgent(),
		phase:        phaseLoading,
		killedByUser: make(map[string]bool),
		search:       search,
	}
}

// Init starts the single fetch that materializes the board snapshot.
func (m Model) Init() tea.Cmd {
	return m.fetchCmd()
}

// fetchCmd reads the whole board snapshot off the critical path (the DB and
// session calls run in the returned command, not in Update).
func (m Model) fetchCmd() tea.Cmd {
	return func() tea.Msg {
		ws, b, err := m.svc.ResolveSelection()
		if err != nil {
			return fetchMsg{err: err}
		}
		cols, err := m.svc.ListColumns(b.ID)
		if err != nil {
			return fetchMsg{err: err}
		}
		cards, err := m.svc.ListCardsByBoard(b.ID)
		if err != nil {
			return fetchMsg{err: err}
		}
		status, statusErr := m.svc.SessionStatus(context.Background())
		return fetchMsg{ws: ws, board: b, cols: cols, cards: cards, status: status, statusErr: statusErr}
	}
}

// Update routes messages. WindowSizeMsg arrives once at startup and on every
// resize; fetchMsg lands the first snapshot. Key handling is delegated per
// phase: navigation only exists in the ready board.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.phase == phaseReady {
			m.relayout()
		}
		return m, nil
	case fetchMsg:
		return m.applyFetch(msg)
	case pollMsg:
		return m, tea.Batch(m.statusTickCmd(), m.startPoll())
	case statusMsg:
		return m.applyStatus(msg)
	case openMsg:
		return m.afterOpen(msg)
	case killMsg:
		return m.afterKill(msg)
	case attachDoneMsg:
		return m.afterAttach(msg)
	case boardsMsg:
		return m.applyBoards(msg)
	case workspacesMsg:
		return m.applyWorkspaces(msg)
	case boardSwitchedMsg:
		return m.afterBoardSwitched(msg)
	case workspaceSwitchedMsg:
		return m.afterWorkspaceSwitched(msg)
	case cardCreatedMsg:
		return m.afterCardCreated(msg)
	case cardUpdatedMsg:
		return m.afterCardUpdated(msg)
	case columnCreatedMsg:
		return m.afterColumnCreated(msg)
	case cardMovedMsg:
		return m.afterCardMoved(msg)
	case tea.KeyPressMsg:
		if m.phase != phaseReady {
			return m, nil
		}
		return m.keyPress(msg)
	}
	return m, nil
}

// applyFetch folds a snapshot into the model. ErrNotInitialized gets its
// actionable hint; any other unconditional error and the session-tick error
// route to the error view.
func (m Model) applyFetch(msg fetchMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.phase = phaseError
		m.errText = msg.err.Error()
		if errors.Is(msg.err, board.ErrNotInitialized) {
			m.errText = "no workspace yet: run loom init"
		}
		return m, nil
	}
	if msg.statusErr != nil {
		m.phase = phaseError
		m.errText = msg.statusErr.Error()
		return m, nil
	}
	m.phase = phaseReady
	m.ws, m.board = msg.ws, msg.board
	m.columns = msg.cols
	m.cards = msg.cards
	m.status = msg.status
	m.focus = 0
	m.buildLists()
	m.relayout()
	m.applyPendingFocus()
	return m, m.startPoll()
}

// startPoll arms one 2s session tick (ADR-001 §4.2 step 3): the status
// markers and completion toasts stay live without a keystroke. tea.Every is
// one-shot (fires a single pollMsg then stops), so the pollMsg handler
// re-arms it with startPoll again — the board never stops ticking.
func (m Model) startPoll() tea.Cmd {
	return tea.Every(2*time.Second, func(time.Time) tea.Msg { return pollMsg{} })
}

// statusTickCmd reads one session-status snapshot off the critical path.
func (m Model) statusTickCmd() tea.Cmd {
	return func() tea.Msg {
		status, err := m.svc.SessionStatus(context.Background())
		return statusMsg{status: status, err: err}
	}
}

// applyStatus folds one poll tick in: markers update (cursor-preserving via
// refreshMarkers), and a session that was live last tick and is gone now —
// unless we killed it — raises the "session ended" toast (detached
// completion, ADR-001 §3.5). A tick error degrades to a notice, keeping the
// last known state rather than dropping markers.
func (m Model) applyStatus(msg statusMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.note = "sessions: " + msg.err.Error()
		return m, nil
	}
	ended := m.endedSessions(msg.status)
	m.status = msg.status
	m.refreshMarkers()
	if ended != "" {
		m.note = "session ended: " + ended
	}
	return m, nil
}

// endedSessions returns the title of a card that was live last tick and is
// gone now (and not killed by this board), or "". Live-to-gone is the
// session-completion signal; running/attached both count as live.
func (m Model) endedSessions(next map[string]session.SessionStatus) string {
	for id, st := range m.status {
		if !st.Running && !st.Attached {
			continue
		}
		if _, still := next[id]; still {
			continue
		}
		if m.killedByUser[id] {
			continue
		}
		if title := m.cardTitle(id); title != "" {
			return title
		}
	}
	return ""
}

func (m Model) cardTitle(id string) string {
	for _, c := range m.cards {
		if c.ID == id {
			return c.Title
		}
	}
	return ""
}

// afterOpen continues an Enter press once the session is ensured: the
// terminal hands over to tmux attach-session (tea.ExecProcess owns the TTY
// and suspends the board), or the failure becomes a toast. The killedByUser
// flag clears here: a freshly ensured session is a new run, so its natural
// end must toast like any other.
func (m Model) afterOpen(msg openMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.note = "open " + msg.cardID + ": " + msg.err.Error()
		return m, nil
	}
	delete(m.killedByUser, msg.cardID)
	m.note = "attaching to " + m.cardTitle(msg.cardID) + "…"
	return m, m.attachCmd(msg.cardID)
}

// attachCmd returns the tmux attach-session handoff as a tea.ExecProcess
// command. The Service builds the argv (server, session name) so the board
// stays tmux-free; the callback resumes the model with attachDoneMsg.
func (m Model) attachCmd(cardID string) tea.Cmd {
	c, err := m.svc.TmuxAttach(cardID)
	if err != nil {
		return func() tea.Msg { return attachDoneMsg{cardID: cardID, err: err} }
	}
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return attachDoneMsg{cardID: cardID, err: err}
	})
}

// afterAttach resumes after the user detached (or the handoff failed). The
// board refreshes status immediately so markers reflect what happened while
// it was away.
func (m Model) afterAttach(msg attachDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.note = "attach: " + msg.err.Error()
		return m, nil
	}
	m.note = ""
	return m, m.statusTickCmd()
}

// afterKill folds a K result: success clears the card's live markers (and
// remembers it so the poll doesn't re-toast), failure becomes a toast.
func (m Model) afterKill(msg killMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.note = "kill " + msg.cardID + ": " + msg.err.Error()
		return m, nil
	}
	m.killedByUser[msg.cardID] = true
	m.note = "killed " + m.cardTitle(msg.cardID)
	return m, m.statusTickCmd()
}

// View renders the board (columns + status bar) or the loading/error screens.
func (m Model) View() tea.View {
	switch m.phase {
	case phaseLoading:
		return tea.NewView("loading loom…")
	case phaseError:
		return tea.NewView(m.errText)
	default:
		return tea.NewView(m.layout())
	}
}

// Run starts the board TUI and blocks until it quits, returning the exit
// error. Callers invoke it only when the terminal is interactive (the CLI
// checks the TTY before handing over, rooting bare `loom` in the board).
func Run(svc Service) error {
	p := tea.NewProgram(New(svc))
	_, err := p.Run()
	return err
}

// keyPress handles one key against the canonical §3.5 keymap. Navigation is
// live (j/k within the focused column, h/l across columns, paging); feature
// keys are live since their task shipped (open/kill T17, forms T18, detail
// T19, search/switch/help T20). Quit confirms when sessions live; force quit
// always exits (sessions keep running detached).
func (m Model) keyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	km := defaultKeyMap()

	if m.confirmQuit {
		return m.quitOverlay(msg, km)
	}

	// A form overlay owns every key until it closes (T18): typing, cycling,
	// tab navigation, enter submit, esc cancel. The board's bindings are dead
	// while a form is open, so 'q' types into a focused field rather than
	// quitting.
	if m.form != nil {
		return m.formUpdate(msg)
	}
	if m.detail != nil {
		return m.detailUpdate(msg)
	}
	if m.searching {
		return m.searchUpdate(msg)
	}
	if m.help {
		// The help overlay is dismiss-only: any key closes it (T20).
		m.help = false
		return m, nil
	}

	switch {
	case key.Matches(msg, km.Quit):
		if m.runningCount() == 0 {
			return m, tea.Quit
		}
		m.confirmQuit = true
		return m, nil
	case key.Matches(msg, km.ForceQuit):
		return m, tea.Quit
	case key.Matches(msg, km.Right):
		if m.focus < len(m.lists)-1 {
			m.focus++
		}
		return m, nil
	case key.Matches(msg, km.Left):
		if m.focus > 0 {
			m.focus--
		}
		return m, nil
	case key.Matches(msg, km.CursorUp, km.CursorDown, km.PageUp, km.PageDown, km.GoToStart, km.GoToEnd):
		if m.focus >= len(m.lists) {
			return m, nil
		}
		updated, cmd := m.lists[m.focus].Update(msg)
		m.lists[m.focus] = updated
		return m, cmd
	case key.Matches(msg, km.Open):
		id, ok := m.focusedCardID()
		if !ok {
			m.note = "no card to open"
			return m, nil
		}
		m.note = "opening " + m.cardTitle(id) + "…"
		return m, m.openSessionCmd(id)
	case key.Matches(msg, km.Kill):
		id, ok := m.focusedCardID()
		if !ok {
			m.note = "no card to kill"
			return m, nil
		}
		m.note = "killing " + m.cardTitle(id) + "…"
		return m, m.killCmd(id)
	case key.Matches(msg, km.NewCard):
		m.form = m.newCardForm()
		return m, nil
	case key.Matches(msg, km.NewColumn):
		m.form = m.newColumnForm()
		return m, nil
	case key.Matches(msg, km.Move):
		id, ok := m.focusedCardID()
		if !ok {
			m.note = "no card to move"
			return m, nil
		}
		m.form = m.moveCardForm(id)
		return m, nil
	case key.Matches(msg, km.Edit):
		id, ok := m.focusedCardID()
		if !ok {
			m.note = "no card to edit"
			return m, nil
		}
		m.form = m.editCardForm(id)
		return m, nil
	case key.Matches(msg, km.Detail):
		return m.openCardDetail()
	case key.Matches(msg, km.Search):
		return m.startSearch()
	case key.Matches(msg, km.Board):
		return m, m.boardsCmd()
	case key.Matches(msg, km.Workspace):
		return m, m.workspacesCmd()
	case key.Matches(msg, km.Help):
		m.help = true
		return m, nil
	}
	return m, nil
}

// startSearch opens the `/` search overlay, pre-filled with the active filter
// so it can be extended or cleared. Enter applies the query as the board
// filter; esc cancels without touching the active filter (T20).
func (m Model) startSearch() (tea.Model, tea.Cmd) {
	m.searching = true
	m.search.Focus()
	m.search.SetValue(m.searchQuery)
	m.search.CursorEnd()
	return m, nil
}

// searchUpdate owns every key while the search overlay is open: enter applies
// the query (an empty query clears the filter) and closes, esc cancels, and
// every other key types into the input.
func (m Model) searchUpdate(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	km := defaultFormKeyMap()
	switch {
	case key.Matches(msg, km.cancel):
		m.search.Blur()
		m.searching = false
		return m, nil
	case key.Matches(msg, km.submit):
		m.searchQuery = strings.TrimSpace(m.search.Value())
		m.search.Blur()
		m.searching = false
		m.buildLists()
		m.relayout()
		return m, nil
	}
	updated, _ := m.search.Update(msg)
	m.search = updated
	return m, nil
}

// boardsCmd fetches the current workspace's boards off the critical path so
// the `s` picker opens with fresh data (ADR-001 §6).
func (m Model) boardsCmd() tea.Cmd {
	return func() tea.Msg {
		boards, err := m.svc.ListBoards(m.board.WorkspaceID)
		return boardsMsg{boards: boards, err: err}
	}
}

// workspacesCmd fetches every workspace off the critical path for the `w`
// picker.
func (m Model) workspacesCmd() tea.Cmd {
	return func() tea.Msg {
		workspaces, err := m.svc.ListWorkspaces()
		return workspacesMsg{workspaces: workspaces, err: err}
	}
}

// focusedCardID returns the card under the column cursor. The list's Index is
// page-relative, which is exactly the row the cursor sits on within the items
// slice.
func (m Model) focusedCardID() (string, bool) {
	if m.focus >= len(m.lists) {
		return "", false
	}
	items := m.lists[m.focus].Items()
	idx := m.lists[m.focus].Index()
	if idx < 0 || idx >= len(items) {
		return "", false
	}
	ci, ok := items[idx].(cardItem)
	if !ok {
		return "", false
	}
	return ci.card.ID, true
}

// cardByID returns the in-memory card, used to seed the edit/move forms from
// the snapshot rather than a second service read.
func (m Model) cardByID(id string) (store.Card, bool) {
	for _, c := range m.cards {
		if c.ID == id {
			return c, true
		}
	}
	return store.Card{}, false
}

// newCardForm opens the n overlay against the current snapshot: columns,
// default agent, and the focused column as the seeded target.
func (m Model) newCardForm() *form {
	return openNewCardForm(m.svc, m.columns, m.defaultAgent, m.focus)
}

// newColumnForm opens the N overlay against the current board.
func (m Model) newColumnForm() *form {
	return openNewColumnForm(m.svc, m.board.ID)
}

// editCardForm opens the e overlay seeded from the focused card.
func (m Model) editCardForm(id string) *form {
	c, ok := m.cardByID(id)
	if !ok {
		return nil
	}
	return openEditCardForm(m.svc, c, m.defaultAgent)
}

// moveCardForm opens the m overlay for the focused card, offering its board's
// columns (the current board — all rendered cards live here).
func (m Model) moveCardForm(id string) *form {
	c, ok := m.cardByID(id)
	if !ok {
		return nil
	}
	return openMoveCardForm(m.svc, c, m.columns)
}

// openSessionCmd ensures the card's session without attaching (detach=true —
// the board owns the terminal until the separate attach handoff runs), so the
// ensure's probe window never blocks Update.
func (m Model) openSessionCmd(cardID string) tea.Cmd {
	return func() tea.Msg {
		err := m.svc.OpenCard(context.Background(), cardID, true)
		return openMsg{cardID: cardID, err: err}
	}
}

// killCmd kills + finalizes the card's session off the critical path.
func (m Model) killCmd(cardID string) tea.Cmd {
	return func() tea.Msg {
		err := m.svc.CloseCard(context.Background(), cardID)
		return killMsg{cardID: cardID, err: err}
	}
}

// quitOverlay handles input while the quit confirmation is open: y/enter
// confirms, n/esc cancels, Q force-quits (q and ctrl+c also confirm — the
// user asked to quit twice). Anything else is ignored until the overlay
// closes.
func (m Model) quitOverlay(msg tea.KeyPressMsg, km KeyMap) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, km.ConfirmYes, km.Quit):
		return m, tea.Quit
	case key.Matches(msg, km.ConfirmNo):
		m.confirmQuit = false
		return m, nil
	case key.Matches(msg, km.ForceQuit):
		return m, tea.Quit
	}
	return m, nil
}
