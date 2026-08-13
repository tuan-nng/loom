package tui

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"

	"loom/internal/store"
)

// cardItem adapts a store.Card to a bubbles list item, carrying the render
// decorations resolved at build time: the §14 agent badge and the live
// session marker (● running / ◉ attached).
type cardItem struct {
	card     store.Card
	badge    string
	running  bool
	attached bool
}

// FilterValue is the title, matching `loom card list --search` semantics.
func (i cardItem) FilterValue() string { return i.card.Title }

// cardDelegate renders one card row as a full-width cell on the column
// surface: a priority rule, the agent badge, the title, and the live session
// marker pinned right (ADR-001 §3.5, DESIGN-002 §14). focused says whether
// the row's column holds the board focus, which decides how loud the cursor
// row is: a full accent highlight in the focused column, a muted one
// elsewhere so every column still shows where its cursor rests.
type cardDelegate struct{ focused bool }

// rowWidths: the fixed cells a row spends outside the title — the priority
// rule, the gap after it, and the gap plus glyph reserved for the marker.
const (
	cardRulePad   = 2 // "▎" + gap
	cardMarkerPad = 2 // gap + ●/◉
	cardTrailPad  = 1 // right breathing room
)

func (d cardDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	ci, ok := item.(cardItem)
	if !ok {
		return
	}
	width := m.Width()
	if width < cardRulePad+1 {
		width = cardRulePad + 1
	}

	rs := rowIdle
	if index == m.Index() {
		rs = rowCursorIdle
		if d.focused {
			rs = rowCursor
		}
	}

	marker, markerStyle := "", rs.marker
	switch {
	case ci.attached:
		marker, markerStyle = "◉", rs.marker.Foreground(pal.attached)
	case ci.running:
		marker, markerStyle = "●", rs.marker.Foreground(pal.running)
	}

	badge := "[" + ci.badge + "]"
	reserved := cardRulePad + lipgloss.Width(badge) + 1 + cardTrailPad
	if marker != "" {
		reserved += cardMarkerPad
	}
	title := truncateText(ci.card.Title, width-reserved)

	line := rs.priorityBar(ci.card.Priority) + rs.bar(1) +
		rs.badge.Render(badge) + rs.bar(1) + rs.title.Render(title)

	used := cardRulePad + lipgloss.Width(badge) + 1 + lipgloss.Width(title)
	if marker != "" {
		line += rs.bar(width-used-1-cardTrailPad) + markerStyle.Render(marker) + rs.bar(cardTrailPad)
	} else {
		line += rs.bar(width - used)
	}
	// No trailing newline: the list joins rows itself, and an extra one grows
	// the column past the height it was sized for.
	fmt.Fprint(w, line)
}

func (cardDelegate) Height() int  { return 1 }
func (cardDelegate) Spacing() int { return 0 }
func (cardDelegate) Update(tea.Msg, *list.Model) tea.Cmd {
	return nil
}

const (
	headerHeight = 1
	statusHeight = 1
	colGap       = 1

	// columnChrome is the vertical cost of a column's frame: the titled top
	// border and the bottom border.
	columnChrome = 2
	// columnSides is the horizontal cost of a column's left/right border.
	columnSides = 2
)

// buildLists creates the column lists from the current snapshot, replacing
// the stale set. Each list renders no title (the header row is the board's)
// and no pagination/help; navigation is the board's canonical keymap. Badges
// resolve against the launch default agent; markers are frozen from the
// current status snapshot (the poll refreshes them in place).
func (m *Model) buildLists() {
	cardsByCol := make(map[string][]list.Item, len(m.columns))
	for _, c := range m.visibleCards() {
		cardsByCol[c.ColumnID] = append(cardsByCol[c.ColumnID], cardItem{
			card:     c,
			badge:    agentBadge(c.AgentOrDefault(m.defaultAgent)),
			running:  m.status[c.ID].Running,
			attached: m.status[c.ID].Attached,
		})
	}
	lists := make([]list.Model, len(m.columns))
	for i, col := range m.columns {
		l := list.New(cardsByCol[col.ID], cardDelegate{focused: i == m.focus}, 0, 0)
		l.KeyMap = columnKeyMap()
		l.SetShowTitle(false)
		l.SetShowStatusBar(false)
		l.SetShowPagination(false)
		l.SetShowHelp(false)
		l.SetFilteringEnabled(false)
		l.SetStatusBarItemName("card", "cards")
		l.Styles.NoItems = columnEmptyStyle
		lists[i] = l
	}
	m.lists = lists
	if m.focus >= len(m.lists) {
		m.focus = 0
	}
}

// refreshMarkers rewrites each column's items with the current session
// markers, preserving the cursor (SetItems leaves the cursor alone). Item
// order and count never change — only the ●/◉ decorations do.
func (m *Model) refreshMarkers() {
	for i := range m.lists {
		items := m.lists[i].Items()
		updated := make([]list.Item, len(items))
		for j, it := range items {
			ci := it.(cardItem)
			ci.running = m.status[ci.card.ID].Running
			ci.attached = m.status[ci.card.ID].Attached
			updated[j] = ci
		}
		m.lists[i].SetItems(updated)
	}
}

// refocus records the card the cursor should land on once the post-mutation
// snapshot lands. Called from the form after* handlers; applyFetch's contract
// is unchanged (it still rebuilds and resets focus), applyPendingFocus runs
// after the fresh lists exist.
func (m *Model) refocus(cardID string) { m.pendingFocus = cardID }

// applyPendingFocus lands the column focus and list cursor on the card
// recorded by refocus, if the fresh snapshot holds it. Inert otherwise.
func (m *Model) applyPendingFocus() {
	if m.pendingFocus == "" {
		return
	}
	id := m.pendingFocus
	m.pendingFocus = ""
	for i := range m.lists {
		for j, it := range m.lists[i].Items() {
			ci, ok := it.(cardItem)
			if ok && ci.card.ID == id {
				m.focus = i
				m.lists[i].Select(j)
				return
			}
		}
	}
}

// agentBadge is the short tag DESIGN-002 §14 defines: "cl" for claude, "oc"
// for opencode, the resolved name for any future driver.
func agentBadge(name string) string {
	switch name {
	case "claude":
		return "cl"
	case "opencode":
		return "oc"
	default:
		return name
	}
}

// columnWidth is the outer width of one column (its frame included), sharing
// the terminal evenly across the column count with a gap between neighbours.
func (m Model) columnWidth() int {
	n := len(m.lists)
	if n == 0 || m.width <= 0 {
		return 0
	}
	w := (m.width - (n-1)*colGap) / n
	if w < columnSides+1 {
		w = columnSides + 1
	}
	return w
}

// relayout sizes each column list for the current terminal: columns share
// the width evenly (with column gaps), and the list body is everything the
// header, status bar, and the column's own frame leave behind. A freshly
// resized terminal with a zero width is skipped until the first real
// WindowSizeMsg lands.
func (m *Model) relayout() {
	n := len(m.lists)
	if n == 0 || m.width <= 0 || m.height <= 0 {
		return
	}
	bodyWidth := m.columnWidth() - columnSides
	if bodyWidth < 1 {
		bodyWidth = 1
	}
	bodyHeight := m.height - headerHeight - statusHeight - columnChrome
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	for i := range m.lists {
		m.lists[i].SetSize(bodyWidth, bodyHeight)
	}
}

// layout composes the header bar, the column row, and the status bar into one
// string. A T18 form overlay replaces the whole board with its centered box.
func (m Model) layout() string {
	if m.form != nil {
		return m.formView()
	}
	if m.detail != nil {
		return m.detailView()
	}
	if m.help != nil {
		return m.helpView()
	}
	return lipgloss.JoinVertical(lipgloss.Top, m.headerBar(), m.boardView(), m.statusBar())
}

// boardView is the column row: every column framed and filled, separated by a
// one-cell gap so the panels read as distinct surfaces.
func (m Model) boardView() string {
	if len(m.lists) == 0 {
		return ""
	}
	width := m.columnWidth()
	parts := make([]string, 0, 2*len(m.lists)-1)
	gap := columnGapStyle.Render(strings.Repeat(" ", colGap))
	for i := range m.lists {
		if i > 0 {
			parts = append(parts, gap)
		}
		parts = append(parts, m.columnView(i, width))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// columnView renders one column as a filled panel: the name and card count
// live in the top border, the cards fill the body, and the focused column
// trades its muted frame for the accent one.
func (m Model) columnView(i, width int) string {
	focused := i == m.focus
	inner := width - columnSides
	if inner < 1 {
		inner = 1
	}

	frame := columnBodyStyle
	if focused {
		frame = columnBodyFocusStyle
	}
	l := m.lists[i]
	l.SetDelegate(cardDelegate{focused: focused})
	// lipgloss counts the border inside Width, so the frame is asked for the
	// column's outer width and the list fills the inner cells.
	body := frame.Width(width).Render(l.View())
	return lipgloss.JoinVertical(lipgloss.Top, m.columnTitleBar(i, width, focused), body)
}

// columnTitleBar draws the column's top border with its name and card count
// set into the rule: ╭─ Backlog ──── 3 ─╮. A column too narrow to hold both
// degrades to a plain rule rather than overflowing into its neighbour.
func (m Model) columnTitleBar(i, width int, focused bool) string {
	border, title, count := columnBorderStyle, columnTitleStyle, columnCountStyle
	if focused {
		border, title, count = columnBorderFocusStyle, columnTitleFocusStyle, columnCountFocusStyle
	}

	label := strconv.Itoa(m.listCount(i))
	// "╭─ " + name + " " + rule + " " + count + " ─╮"
	const fixed = 8
	room := width - fixed - lipgloss.Width(label)
	if room < 1 {
		return border.Render(strings.Repeat("─", width))
	}
	name := truncateText(m.columns[i].Name, room)
	rule := room - lipgloss.Width(name)

	return border.Render("╭─ ") + title.Render(name) +
		border.Render(" "+strings.Repeat("─", rule)+" ") +
		count.Render(label) + border.Render(" ─╮")
}

// headerBar is the top chrome: the loom brand, the `workspace › board`
// breadcrumb, and — pinned right — the active search filter and card count.
func (m Model) headerBar() string {
	if m.width <= 0 {
		return ""
	}
	left := brandStyle.Render(" loom ") +
		crumbStyle.Render(" "+m.ws.Name) +
		crumbSepStyle.Render(" › ") +
		crumbStyle.Render(m.board.Name+" ")

	var right string
	if m.search != "" {
		right = chipStyle.Render(" /"+truncateText(m.search, 16)+" ") + headerFillStyle.Render(" ")
	}
	right += headerFillStyle.Render(fmt.Sprintf("%d cards ", len(m.visibleCards())))

	return barLine(m.width, headerFillStyle, left, right)
}

func (m Model) listCount(i int) int {
	if i >= len(m.columns) {
		return 0
	}
	n := 0
	for _, c := range m.visibleCards() {
		if c.ColumnID == m.columns[i].ID {
			n++
		}
	}
	return n
}

// runningCount is the number of live card sessions (●), and attachedCount
// how many are attached (◉). The status map only contains running cards.
func (m Model) runningCount() int { return len(m.status) }
func (m Model) attachedCount() int {
	n := 0
	for _, s := range m.status {
		if s.Attached {
			n++
		}
	}
	return n
}

// statusBar is the single bottom line: the live session summary (or the
// current toast, or the quit confirmation) pinned left, the key hints pinned
// right. The hints are the first thing a new user needs and the last thing an
// old one reads, so they yield to nothing but the terminal's width.
func (m Model) statusBar() string {
	if m.width <= 0 {
		return ""
	}

	var left string
	switch {
	case m.confirmQuit:
		left = statusWarnStyle.Render(" quit? ") +
			statusTextStyle.Render("y/enter yes · n/esc no · Q force ")
	case m.note != "":
		left = statusTextStyle.Render(" " + m.note + " ")
	default:
		left = " " + m.sessionSummary() + " "
	}

	return barLine(m.width, statusFillStyle, left, m.hints()+statusFillStyle.Render(" "))
}

// sessionSummary is the ●/◉ live-session tally, or the idle placeholder when
// no card session is running.
func (m Model) sessionSummary() string {
	running, attached := m.runningCount(), m.attachedCount()
	if running == 0 {
		return statusFillStyle.Render("no sessions")
	}
	out := statusFillStyle.Foreground(pal.running).Render("●") +
		statusTextStyle.Render(fmt.Sprintf(" %d running", running))
	if attached > 0 {
		out += statusFillStyle.Render(" · ") +
			statusFillStyle.Foreground(pal.attached).Render("◉") +
			statusTextStyle.Render(fmt.Sprintf(" %d attached", attached))
	}
	return out
}

// hints is the always-on key legend, rendered from the canonical §3.5 keymap
// so it can never drift from the bindings.
func (m Model) hints() string {
	km := defaultKeyMap()
	pairs := [][2]string{
		{firstKey(km.Open), "open"},
		{firstKey(km.NewCard), "new"},
		{firstKey(km.Detail), "detail"},
		{firstKey(km.Search), "search"},
		{firstKey(km.Help), "help"},
		{firstKey(km.Quit), "quit"},
	}
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, statusKeyStyle.Render(p[0])+statusFillStyle.Render(" "+p[1]))
	}
	return strings.Join(parts, statusFillStyle.Render(" · "))
}

// firstKey is a binding's primary key, the one the legend advertises.
func firstKey(b key.Binding) string {
	if ks := b.Keys(); len(ks) > 0 {
		return ks[0]
	}
	return ""
}
