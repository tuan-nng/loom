package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"loom/internal/agent"
	"loom/internal/store"
)

// formKind discriminates the overlays (ADR-001 §3.5): the four T18 forms
// (n/N/m/e), the T20 / search, and the T20 s/w switch pickers.
type formKind int

const (
	formNewCard formKind = iota
	formEditCard
	formNewColumn
	formMoveCard
	formSearch
	formBoardSwitch
	formWorkspaceSwitch
)

// fieldKind discriminates the two interactive field behaviors: a text field
// is a textinput, a cycle field is a closed option set cycled in place with
// left/right (DESIGN-002 §14: the agent picker is empty-default plus the
// known agents, never a nested overlay).
type fieldKind int

const (
	fieldText fieldKind = iota
	fieldCycle
)

// field is one row of a form overlay. options are the display labels; values,
// when non-nil, are the store-facing values parallel to options (the column
// picker shows names but submits IDs). index is the selected option; touched
// marks an explicit user cycle so edit marshalling can distinguish "re-cycled
// to the seeded value" (write it) from "never touched" (leave untouched).
type field struct {
	kind    fieldKind
	label   string
	input   textinput.Model // fieldText only
	options []string        // fieldCycle: display labels
	values  []string        // fieldCycle: parallel store values (nil = labels)
	index   int             // fieldCycle: selected option
	touched bool            // fieldCycle: the user cycled it
}

// selectedLabel is the display text of the selected cycle option. Empty
// option sets (a board stripped of every column via `column delete`) render
// as "" rather than panicking on the index.
func (f field) selectedLabel() string {
	if len(f.options) == 0 {
		return ""
	}
	return f.options[f.index]
}

// selectedValue is the store-facing value of the selected cycle option.
func (f field) selectedValue() string {
	if len(f.options) == 0 {
		return ""
	}
	if f.values != nil {
		return f.values[f.index]
	}
	return f.options[f.index]
}

// cycle wraps the selection by delta and marks the field touched.
func (f *field) cycle(delta int) {
	n := len(f.options)
	if n == 0 {
		return
	}
	f.index = (f.index + delta + n) % n
	f.touched = true
}

// form is one open overlay: a titled box of fields navigated with
// tab/shift+tab, submitted with enter, cancelled with esc. All form logic
// lives here behind the svc seam, so tests drive a form directly without a
// Model or the fetch cycle.
type form struct {
	kind         formKind
	title        string
	svc          Service
	card         store.Card // edit: pre-edit snapshot; move: card to move
	boardID      string     // new-column target board
	defaultAgent string     // agent picker default (DESIGN-002 §14)
	fields       []field
	focus        int
	err          string // form-local validation error line
	closed       bool   // set on cancel/submit; the caller clears m.form
}

// Field order per kind — marshalling references these, not magic indices.
const (
	nfTitle = iota
	nfColumn
	nfPriority
	nfAgent
)

const (
	efTitle = iota
	efDescription
	efObjective
	efAcceptance
	efPriority
	efLabels
	efAgent
)

const (
	cfName = iota
	cfStage
)

const (
	mfColumn = iota
)

// openNewCardForm is the n overlay: title, the board's columns (seeded with
// the focused column), priority (seeded "medium", the store default), and the
// agent picker (seeded at the empty-default entry).
func openNewCardForm(svc Service, cols []store.Column, defaultAgent string, focusCol int) *form {
	colNames := make([]string, len(cols))
	colIDs := make([]string, len(cols))
	for i, c := range cols {
		colNames[i] = c.Name
		colIDs[i] = c.ID
	}
	if focusCol < 0 || focusCol >= len(cols) {
		focusCol = 0
	}
	f := &form{
		kind:         formNewCard,
		title:        "New Card",
		svc:          svc,
		defaultAgent: defaultAgent,
		fields: []field{
			textField("title", ""),
			cycleField("column", colNames, colIDs, focusCol),
			cycleField("priority", store.ValidPriorities, nil, indexOf(store.ValidPriorities, "medium")),
			agentField(defaultAgent, indexOf(agentOptions(defaultAgent), "")),
		},
	}
	f.syncFocus()
	return f
}

// openEditCardForm is the e overlay: every mutable card field (title,
// description, objective, acceptance criteria, priority, labels, agent),
// seeded from the card. Column is deliberately absent — column changes go
// through MoveCard (store.CardUpdate has no column writer, ADR-001 §3.3). The
// agent picker seeds at the card's resolved agent (DESIGN-002 §14).
func openEditCardForm(svc Service, card store.Card, defaultAgent string) *form {
	opts := agentOptions(defaultAgent)
	f := &form{
		kind:         formEditCard,
		title:        "Edit Card",
		svc:          svc,
		card:         card,
		defaultAgent: defaultAgent,
		fields: []field{
			textField("title", card.Title),
			textField("description", deref(card.Description)),
			textField("objective", deref(card.Objective)),
			textField("acceptance criteria", deref(card.AcceptanceCriteria)),
			cycleField("priority", store.ValidPriorities, nil, indexOf(store.ValidPriorities, card.Priority)),
			textField("labels", deref(card.Labels)),
			agentField(defaultAgent, indexOf(opts, card.AgentOrDefault(defaultAgent))),
		},
	}
	f.syncFocus()
	return f
}

// openNewColumnForm is the N overlay: a name text field and the stage cycle
// (ADR-001 §6 stages), seeded at "todo" like the CLI default.
func openNewColumnForm(svc Service, boardID string) *form {
	f := &form{
		kind:    formNewColumn,
		title:   "New Column",
		svc:     svc,
		boardID: boardID,
		fields: []field{
			textField("name", ""),
			cycleField("stage", store.ValidStages, nil, indexOf(store.ValidStages, "todo")),
		},
	}
	f.syncFocus()
	return f
}

// openMoveCardForm is the m overlay: a single column cycle over the card's
// board (all rendered cards live on the current board, so m.columns is
// exactly the card's board's columns — ErrCrossBoardMove cannot trigger).
func openMoveCardForm(svc Service, card store.Card, cols []store.Column) *form {
	colNames := make([]string, len(cols))
	colIDs := make([]string, len(cols))
	for i, c := range cols {
		colNames[i] = c.Name
		colIDs[i] = c.ID
	}
	f := &form{
		kind:  formMoveCard,
		title: "Move: " + card.Title,
		svc:   svc,
		card:  card,
		fields: []field{
			cycleField("column", colNames, colIDs, indexOf(colIDs, card.ColumnID)),
		},
	}
	f.syncFocus()
	return f
}

// textField builds a text input with the form keymap (tab/up/down must not
// fight the overlay's tab/shift+tab cycling or the board's keys).
func textField(label, value string) field {
	in := textinput.New()
	in.Placeholder = label
	in.SetValue(value)
	in.KeyMap = formInputKeyMap()
	return field{kind: fieldText, label: label, input: in}
}

// cycleField builds a closed option set. values nil means labels are the
// store values.
func cycleField(label string, options, values []string, index int) field {
	if index < 0 || index >= len(options) {
		index = 0
	}
	return field{kind: fieldCycle, label: label, options: options, values: values, index: index}
}

// agentField builds the §14 picker: an empty-default entry plus agent.Known()
// in sorted order. defaultAgent labels the empty entry so the user knows what
// actually launches. Edit forms seed at the resolved agent; create forms seed
// at the empty-default entry (a NULL card follows the config default).
func agentField(defaultAgent string, seed int) field {
	opts := agentOptions(defaultAgent)
	if seed < 0 || seed >= len(opts) {
		seed = 0
	}
	return field{kind: fieldCycle, label: "agent", options: opts, values: agentValues(), index: seed}
}

// agentOptions is the picker's display list: "(default: X)" then the known
// agents (agent.Known() is already sorted, DESIGN-002 §14).
func agentOptions(defaultAgent string) []string {
	opts := make([]string, 0, len(agent.Known())+1)
	opts = append(opts, fmt.Sprintf("(default: %s)", defaultAgent))
	return append(opts, agent.Known()...)
}

// agentValues is the store-facing picker list: "" for the empty-default entry
// (NULL = follow the config default at launch), the agent name otherwise.
func agentValues() []string {
	vals := make([]string, 0, len(agent.Known())+1)
	vals = append(vals, "")
	return append(vals, agent.Known()...)
}

// indexOf returns the index of v in s, or 0 when absent (seeds stay valid).
func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return 0
}

// deref renders a nullable string field as "" when unset.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// formKeyMap is the overlay's own keymap; the board's canonical keymap is dead
// while a form is open (the keyPress guard routes everything here).
type formKeyMap struct {
	cancel    key.Binding // esc
	next      key.Binding // tab
	prev      key.Binding // shift+tab
	submit    key.Binding // enter
	cycleNext key.Binding // right — cycle fields
	cyclePrev key.Binding // left — cycle fields
}

func defaultFormKeyMap() formKeyMap {
	return formKeyMap{
		cancel:    key.NewBinding(key.WithKeys("esc")),
		next:      key.NewBinding(key.WithKeys("tab")),
		prev:      key.NewBinding(key.WithKeys("shift+tab")),
		submit:    key.NewBinding(key.WithKeys("enter")),
		cycleNext: key.NewBinding(key.WithKeys("right")),
		cyclePrev: key.NewBinding(key.WithKeys("left")),
	}
}

// formInputKeyMap is each text field's keymap: the textinput defaults bind
// tab (accept suggestion) and up/down (suggestions), all of which collide
// with the overlay's cycling and the board's keys, so they are disabled.
func formInputKeyMap() textinput.KeyMap {
	km := textinput.DefaultKeyMap()
	off := key.NewBinding(key.WithDisabled())
	km.AcceptSuggestion = off
	km.NextSuggestion = off
	km.PrevSuggestion = off
	return km
}

// update handles one key against the overlay keymap, mutating the form in
// place and returning the cmd to run (a submit cmd, or nil). The error line
// clears on every keystroke.
func (f *form) update(msg tea.KeyPressMsg) tea.Cmd {
	f.err = ""
	km := defaultFormKeyMap()
	cur := &f.fields[f.focus]

	switch {
	case key.Matches(msg, km.cancel):
		f.closed = true
		return nil
	case key.Matches(msg, km.submit):
		return f.submit()
	case key.Matches(msg, km.next):
		f.advance(1)
		return nil
	case key.Matches(msg, km.prev):
		f.advance(-1)
		return nil
	case key.Matches(msg, km.cycleNext):
		if cur.kind == fieldCycle {
			cur.cycle(1)
			return nil
		}
		return f.forward(msg)
	case key.Matches(msg, km.cyclePrev):
		if cur.kind == fieldCycle {
			cur.cycle(-1)
			return nil
		}
		return f.forward(msg)
	}
	return f.forward(msg)
}

// advance wraps the field focus by delta and re-syncs which text input owns
// the keyboard.
func (f *form) advance(delta int) {
	n := len(f.fields)
	if n == 0 {
		return
	}
	f.focus = (f.focus + delta + n) % n
	f.syncFocus()
}

// syncFocus blurs every text field except the focused one, so exactly one
// input owns the keyboard and the cursor.
func (f *form) syncFocus() {
	for i := range f.fields {
		if f.fields[i].kind != fieldText {
			continue
		}
		if i == f.focus {
			f.fields[i].input.Focus()
		} else {
			f.fields[i].input.Blur()
		}
	}
}

// forward routes unclaimed keys to the focused text field (typing, cursor
// movement, backspace, etc.). Cycle fields swallow unhandled keys.
func (f *form) forward(msg tea.KeyPressMsg) tea.Cmd {
	if f.focus >= len(f.fields) || f.fields[f.focus].kind != fieldText {
		return nil
	}
	upd, cmd := f.fields[f.focus].input.Update(msg)
	f.fields[f.focus].input = upd
	return cmd
}

// submit validates the form and, when valid, closes it and returns the async
// service cmd. Validation failures stay open with the error rendered in the
// form-local error line.
func (f *form) submit() tea.Cmd {
	switch f.kind {
	case formNewCard:
		in, err := f.cardInput()
		if err != nil {
			f.err = err.Error()
			return nil
		}
		f.closed = true
		return func() tea.Msg {
			card, cerr := f.svc.CreateCard(in)
			return cardCreatedMsg{card: card, err: cerr}
		}
	case formEditCard:
		u, err := f.cardUpdate()
		if err != nil {
			f.err = err.Error()
			return nil
		}
		f.closed = true
		id := f.card.ID
		return func() tea.Msg {
			card, cerr := f.svc.UpdateCard(id, u)
			return cardUpdatedMsg{card: card, err: cerr}
		}
	case formNewColumn:
		name, stage, err := f.columnInput()
		if err != nil {
			f.err = err.Error()
			return nil
		}
		f.closed = true
		return func() tea.Msg {
			col, cerr := f.svc.CreateColumn(f.boardID, name, stage)
			return columnCreatedMsg{col: col, err: cerr}
		}
	case formMoveCard:
		f.closed = true
		id, to := f.card.ID, f.fields[mfColumn].selectedValue()
		return func() tea.Msg {
			card, cerr := f.svc.MoveCard(context.Background(), id, to, nil, nil)
			return cardMovedMsg{card: card, err: cerr}
		}
	case formSearch:
		// Empty query is a valid commit: it clears the active filter.
		f.closed = true
		return func() tea.Msg {
			return searchMsg{query: f.fields[0].input.Value()}
		}
	case formBoardSwitch:
		f.closed = true
		id := f.fields[0].selectedValue()
		return func() tea.Msg {
			b, serr := f.svc.ShowBoard(id)
			return boardSwitchedMsg{board: b, err: serr}
		}
	case formWorkspaceSwitch:
		f.closed = true
		id := f.fields[0].selectedValue()
		return func() tea.Msg {
			ws, serr := f.svc.SwitchWorkspace(id)
			return workspaceSwitchedMsg{ws: ws, err: serr}
		}
	}
	return nil
}

// cardInput marshals the new-card form. Empty optional fields stay nil (the
// store default/NULL); the agent picker's empty-default entry yields nil so a
// NULL card follows the config default at launch (DESIGN-002 §14).
func (f *form) cardInput() (store.CardInput, error) {
	title := strings.TrimSpace(f.fields[nfTitle].input.Value())
	if title == "" {
		return store.CardInput{}, fmt.Errorf("title is required")
	}
	col := f.fields[nfColumn].selectedValue()
	if col == "" {
		return store.CardInput{}, fmt.Errorf("board has no columns")
	}
	in := store.CardInput{
		ColumnID: col,
		Title:    title,
		Priority: f.fields[nfPriority].selectedValue(),
	}
	if a := f.fields[nfAgent].selectedValue(); a != "" {
		in.Agent = &a
	}
	return in, nil
}

// cardUpdate marshals the edit form. Text fields value-diff against the
// original (changed → set, even to "" for the nullable fields which clears to
// NULL; unchanged → untouched). Cycle fields write only when the user
// explicitly cycled them — the seeded value read as "not touched" preserves
// the card's existing agent/priority (nil vs &"" vs value, ADR-001 §3.3).
func (f *form) cardUpdate() (store.CardUpdate, error) {
	var u store.CardUpdate
	title := strings.TrimSpace(f.fields[efTitle].input.Value())
	if title == "" {
		return u, fmt.Errorf("title is required")
	}
	if title != f.card.Title {
		u.Title = &title
	}
	if d := f.fields[efDescription].input.Value(); d != deref(f.card.Description) {
		u.Description = &d
	}
	if o := f.fields[efObjective].input.Value(); o != deref(f.card.Objective) {
		u.Objective = &o
	}
	if ac := f.fields[efAcceptance].input.Value(); ac != deref(f.card.AcceptanceCriteria) {
		u.AcceptanceCriteria = &ac
	}
	if l := f.fields[efLabels].input.Value(); l != deref(f.card.Labels) {
		u.Labels = &l
	}
	if f.fields[efPriority].touched {
		p := f.fields[efPriority].selectedValue()
		if p != f.card.Priority {
			u.Priority = &p
		}
	}
	if f.fields[efAgent].touched {
		a := f.fields[efAgent].selectedValue()
		if a != f.card.AgentOrDefault(f.defaultAgent) {
			u.Agent = &a
		}
	}
	return u, nil
}

// columnInput marshals the new-column form.
func (f *form) columnInput() (name, stage string, err error) {
	name = strings.TrimSpace(f.fields[cfName].input.Value())
	if name == "" {
		return "", "", fmt.Errorf("name is required")
	}
	return name, f.fields[cfStage].selectedValue(), nil
}

// view renders the form as a centered bordered box over the terminal.
func (f *form) view(width, height int) string {
	boxW := width - 4
	if boxW > 48 {
		boxW = 48
	}
	if boxW < 20 {
		boxW = 20
	}
	inW := boxW - 2

	var b strings.Builder
	b.WriteString(formTitleStyle.Render(f.title))
	b.WriteString("\n\n")
	for i := range f.fields {
		fld := &f.fields[i]
		b.WriteString(formLabelStyle.Render(fld.label + ":"))
		b.WriteString("\n")
		switch fld.kind {
		case fieldText:
			in := fld.input
			in.SetWidth(inW)
			b.WriteString(in.View())
		case fieldCycle:
			if len(fld.options) == 0 {
				b.WriteString(formHintStyle.Render("(none available)"))
			} else {
				b.WriteString(formCursorStyle.Render("▸ ") + formValueStyle.Render(fld.selectedLabel()))
				if len(fld.options) > 1 {
					b.WriteString(formHintStyle.Render("  ←/→"))
				}
			}
		}
		b.WriteString("\n\n")
	}
	if f.err != "" {
		b.WriteString(formErrStyle.Render(f.err))
		b.WriteString("\n")
	}
	b.WriteString(formHintStyle.Render("tab next · shift+tab back · enter submit · esc cancel"))

	box := formBoxStyle.Render(b.String())
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// formView is the Model-side overlay render hook (ADR-001 §3.5).
func (m Model) formView() string {
	if m.form == nil {
		return ""
	}
	return m.form.view(m.width, m.height)
}

// cardCreatedMsg / cardUpdatedMsg / columnCreatedMsg / cardMovedMsg are the
// four form submit results, folded by the after* handlers below.
type cardCreatedMsg struct {
	card store.Card
	err  error
}

type cardUpdatedMsg struct {
	card store.Card
	err  error
}

type columnCreatedMsg struct {
	col store.Column
	err error
}

type cardMovedMsg struct {
	card store.Card
	err  error
}

// afterCardCreated folds a card create: success re-fetches the board (the
// new card materializes) and refocuses it; failure becomes a toast.
func (m Model) afterCardCreated(msg cardCreatedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.note = "create card: " + msg.err.Error()
		return m, nil
	}
	m.note = "created " + msg.card.Title
	m.refocus(msg.card.ID)
	return m, m.fetchCmd()
}

// afterCardUpdated folds a card update: success re-fetches and refocuses the
// edited card; failure becomes a toast.
func (m Model) afterCardUpdated(msg cardUpdatedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.note = "update card: " + msg.err.Error()
		return m, nil
	}
	m.note = "updated " + msg.card.Title
	m.refocus(msg.card.ID)
	return m, m.fetchCmd()
}

// afterColumnCreated folds a column create: success re-fetches so the new
// column renders; failure becomes a toast. No refocus — columns are not
// cards.
func (m Model) afterColumnCreated(msg columnCreatedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.note = "add column: " + msg.err.Error()
		return m, nil
	}
	m.note = "added column " + msg.col.Name
	return m, m.fetchCmd()
}

// afterCardMoved folds a move: success re-fetches and refocuses the moved
// card in its new column (the done-stage auto-kill ran inside MoveCard);
// failure becomes a toast.
func (m Model) afterCardMoved(msg cardMovedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.note = "move card: " + msg.err.Error()
		return m, nil
	}
	m.note = "moved " + msg.card.Title
	m.refocus(msg.card.ID)
	return m, m.fetchCmd()
}

// formUpdate is the Model-side input guard while a form is open: every key
// goes to the overlay, never the board. A closed form clears the field.
func (m Model) formUpdate(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.form == nil {
		return m, nil
	}
	cmd := m.form.update(msg)
	if m.form.closed {
		m.form = nil
	}
	return m, cmd
}

var (
	formBoxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	formTitleStyle  = lipgloss.NewStyle().Bold(true).Underline(true)
	formLabelStyle  = lipgloss.NewStyle().Bold(true)
	formValueStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	formCursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	formErrStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	formHintStyle   = lipgloss.NewStyle().Faint(true)
)
