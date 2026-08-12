package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// helpOverlay is the ? overlay: a stateless boxed listing of the canonical
// §3.5 keymap (ADR-001 §3.5, "One table governs the board, the card-detail
// view, and the pop-over/help"). esc/q closes it and every key is swallowed
// while it is up, exactly like the card-detail pane.
type helpOverlay struct{}

// openHelp opens the ? overlay.
func (m Model) openHelp() (Model, tea.Cmd) {
	m.help = &helpOverlay{}
	return m, nil
}

// helpUpdate owns every key while the pane is open; only esc (and q, the
// canonical close) close it.
func (m Model) helpUpdate(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.help == nil {
		return m, nil
	}
	if key.Matches(msg, key.NewBinding(key.WithKeys("esc", "q"))) {
		m.help = nil
	}
	return m, nil
}

// helpView is the Model-side overlay render hook.
func (m Model) helpView() string {
	if m.help == nil {
		return ""
	}
	return m.help.view(m.width, m.height)
}

func (helpOverlay) view(width, height int) string {
	boxW := width - 4
	if boxW > 56 {
		boxW = 56
	}
	if boxW < 20 {
		boxW = 20
	}

	var b strings.Builder
	b.WriteString(helpTitleStyle.Render("Keymap"))
	b.WriteString("\n\n")
	for _, row := range helpRows() {
		b.WriteString(fmt.Sprintf("  %s  %s\n", helpKeyStyle.Render(row.keys), helpActionStyle.Render(row.action)))
	}
	b.WriteString("\n" + helpHintStyle.Render("esc/q close"))

	box := formBoxStyle.Render(b.String())
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// helpRow is one §3.5 keymap row: the bound keys and the action.
type helpRow struct {
	keys, action string
}

// helpRows renders the canonical keymap from the keymap table itself, so the
// overlay can never drift from the bindings (ADR-001 §3.5).
func helpRows() []helpRow {
	km := defaultKeyMap()
	return []helpRow{
		{keyNames(km.CursorUp, km.CursorDown), "focus previous/next card"},
		{keyNames(km.Left, km.Right), "focus previous/next column"},
		{keyNames(km.Open), "open card session"},
		{keyNames(km.Kill), "kill card session"},
		{keyNames(km.NewCard), "new card"},
		{keyNames(km.NewColumn), "new column"},
		{keyNames(km.Move), "move card"},
		{keyNames(km.Detail), "card detail"},
		{keyNames(km.Edit), "edit card"},
		{keyNames(km.Search), "search/filter cards"},
		{keyNames(km.Board), "switch board"},
		{keyNames(km.Workspace), "switch workspace"},
		{keyNames(km.Help), "help overlay"},
		{keyNames(km.Quit), "quit (confirm if sessions attached)"},
		{keyNames(km.ForceQuit), "force quit"},
	}
}

// keyNames flattens one or more bindings to their slash-joined key strings.
func keyNames(bs ...key.Binding) string {
	names := make([]string, 0, len(bs))
	for _, b := range bs {
		names = append(names, strings.Join(b.Keys(), "/"))
	}
	return strings.Join(names, ", ")
}

var (
	helpTitleStyle  = lipgloss.NewStyle().Bold(true).Underline(true)
	helpKeyStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	helpActionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	helpHintStyle   = lipgloss.NewStyle().Faint(true)
)