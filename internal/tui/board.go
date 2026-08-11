package tui

import (
	"fmt"
	"io"
	"strings"

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

// cardDelegate renders one card row: cursor, agent badge, bold title, and the
// live session marker (ADR-001 §3.5, DESIGN-002 §14).
type cardDelegate struct{}

func (cardDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	ci, ok := item.(cardItem)
	if !ok {
		return
	}
	marker := "  "
	if index == m.Index() {
		marker = "▸ "
	}
	title := cardTitleStyle.Render(ci.card.Title)
	line := marker + cardBadgeStyle.Render("["+ci.badge+"]") + " " + title
	if ci.attached {
		line += sessionAttachedStyle.Render(" ◉")
	} else if ci.running {
		line += sessionRunningStyle.Render(" ●")
	}
	fmt.Fprintln(w, line)
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
)

// buildLists creates the column lists from the current snapshot, replacing
// the stale set. Each list renders no title (the header row is the board's)
// and no pagination/help; navigation is the board's canonical keymap. Badges
// resolve against the launch default agent; markers are frozen from the
// current status snapshot (the poll refreshes them in place).
func (m *Model) buildLists() {
	cardsByCol := make(map[string][]list.Item, len(m.columns))
	for _, c := range m.cards {
		cardsByCol[c.ColumnID] = append(cardsByCol[c.ColumnID], cardItem{
			card:     c,
			badge:    agentBadge(c.AgentOrDefault(m.defaultAgent)),
			running:  m.status[c.ID].Running,
			attached: m.status[c.ID].Attached,
		})
	}
	lists := make([]list.Model, len(m.columns))
	for i, col := range m.columns {
		l := list.New(cardsByCol[col.ID], cardDelegate{}, 0, 0)
		l.KeyMap = columnKeyMap()
		l.SetShowTitle(false)
		l.SetShowStatusBar(false)
		l.SetShowPagination(false)
		l.SetShowHelp(false)
		l.SetFilteringEnabled(false)
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

// relayout sizes each column list for the current terminal: columns share
// the width evenly (with column gaps), the list body is everything below the
// header and status rows. A freshly-resized terminal with a zero width is
// skipped until the first real WindowSizeMsg lands.
func (m *Model) relayout() {
	n := len(m.lists)
	if n == 0 || m.width <= 0 || m.height <= 0 {
		return
	}
	colWidth := (m.width - (n-1)*colGap) / n
	if colWidth < 1 {
		colWidth = 1
	}
	bodyHeight := m.height - headerHeight - statusHeight
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	for i := range m.lists {
		m.lists[i].SetSize(colWidth, bodyHeight)
	}
}

// layout composes the header row, the five column bodies, and the status bar
// into one string. A T18 form overlay replaces the whole board with its
// centered box.
func (m Model) layout() string {
	colWidth := 0
	if n := len(m.lists); n > 0 {
		colWidth = (m.width - (n-1)*colGap) / n
		if colWidth < 1 {
			colWidth = 1
		}
	}
	cols := make([]string, len(m.lists))
	for i := range m.lists {
		cols[i] = m.columnView(i, colWidth)
	}
	board := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	out := lipgloss.JoinVertical(lipgloss.Top, board, m.statusBar())
	if m.form != nil {
		return m.formView()
	}
	return out
}

// columnView renders one column: a header (name + card count, focused column
// emphasized) and the list body.
func (m Model) columnView(i, width int) string {
	col := m.columns[i]
	title := fmt.Sprintf("%s %d", col.Name, m.listCount(i))
	style := columnInactiveStyle
	if i == m.focus {
		style = columnFocusStyle
	}
	header := style.Width(width).MaxWidth(width).Render(title)
	body := m.lists[i].View()
	return lipgloss.JoinVertical(lipgloss.Top, header, body)
}

func (m Model) listCount(i int) int {
	if i >= len(m.columns) {
		return 0
	}
	n := 0
	for _, c := range m.cards {
		if c.ColumnID == m.columns[i].ID {
			n++
		}
	}
	return n
}

// runningCount is the number of live card sessions (●), and attachedCount
// how many are attached (◉). The status map only contains running cards.
func (m Model) runningCount() int   { return len(m.status) }
func (m Model) attachedCount() int {
	n := 0
	for _, s := range m.status {
		if s.Attached {
			n++
		}
	}
	return n
}

// statusBar is the single bottom line: `workspace › board` pinned left, the
// session summary / note pinned right. With the quit overlay open the right
// half is the confirm prompt.
func (m Model) statusBar() string {
	left := fmt.Sprintf("%s › %s", m.ws.Name, m.board.Name)

	var right string
	switch {
	case m.confirmQuit:
		right = "quit? (y/enter yes · n/esc no · Q force)"
	case m.note != "":
		right = m.note
	default:
		running, attached := m.runningCount(), m.attachedCount()
		if running == 0 {
			right = "no sessions"
		} else if attached > 0 {
			right = fmt.Sprintf("● %d running · ◉ %d attached", running, attached)
		} else {
			right = fmt.Sprintf("● %d running", running)
		}
	}

	sep := "  "
	avail := m.width - lipgloss.Width(left) - lipgloss.Width(sep)
	if rw := lipgloss.Width(right); rw > avail {
		// Slim the right side to the available cells rather than wrapping
		// the bar onto a second line.
		r := []rune(right)
		if avail < 1 {
			right = ""
		} else {
			right = string(r[:avail]) + "…"
		}
	}
	line := strings.TrimRight(left+sep+right, " ")
	return lipgloss.NewStyle().Width(m.width).Render(line)
}

var (
	columnFocusStyle    = lipgloss.NewStyle().Bold(true).Underline(true)
	columnInactiveStyle = lipgloss.NewStyle().Faint(true)

	// cardTitleStyle bolds the card title (T17 cell spec: title bold).
	cardTitleStyle = lipgloss.NewStyle().Bold(true)

	// cardBadgeStyle faints the [cl]/[oc] tag so titles dominate the cell.
	cardBadgeStyle = lipgloss.NewStyle().Faint(true)

	// sessionRunningStyle / sessionAttachedStyle are the ●/◉ live markers
	// (ADR-001 §3.5): attached outranks running, and both are tinted.
	sessionRunningStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	sessionAttachedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
)