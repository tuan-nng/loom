package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// The board is a panel UI: every column is a filled surface with a titled
// border, every overlay is a raised card on that surface, and the header and
// status bars frame both. Because a terminal cell keeps its background only
// while a style is active, every style that lands on a surface carries that
// surface's background explicitly — a nested style that omits it would punch
// a hole through the panel where its text sits.

// palette is the resolved colour set for one terminal background. Two
// instances exist (dark/light); applyTheme swaps between them once the
// terminal answers the background-colour query.
type palette struct {
	text     color.Color // primary foreground
	dim      color.Color // secondary foreground (labels, hints)
	muted    color.Color // tertiary foreground (borders, placeholders)
	accent   color.Color // focus/brand colour
	accentFg color.Color // text drawn on top of accent

	surface    color.Color // column / overlay fill
	surfaceAlt color.Color // header + status bars, idle cursor row
	selected   color.Color // cursor row in the focused column

	running  color.Color // ● live session
	attached color.Color // ◉ attached session
	danger   color.Color // errors, high priority
	warn     color.Color // medium priority, confirmations
}

func darkPalette() palette {
	return palette{
		text:       lipgloss.Color("252"),
		dim:        lipgloss.Color("245"),
		muted:      lipgloss.Color("240"),
		accent:     lipgloss.Color("111"),
		accentFg:   lipgloss.Color("233"),
		surface:    lipgloss.Color("235"),
		surfaceAlt: lipgloss.Color("237"),
		selected:   lipgloss.Color("24"),
		running:    lipgloss.Color("42"),
		attached:   lipgloss.Color("170"),
		danger:     lipgloss.Color("203"),
		warn:       lipgloss.Color("179"),
	}
}

func lightPalette() palette {
	return palette{
		text:       lipgloss.Color("236"),
		dim:        lipgloss.Color("242"),
		muted:      lipgloss.Color("249"),
		accent:     lipgloss.Color("25"),
		accentFg:   lipgloss.Color("231"),
		surface:    lipgloss.Color("254"),
		surfaceAlt: lipgloss.Color("252"),
		selected:   lipgloss.Color("152"),
		running:    lipgloss.Color("28"),
		attached:   lipgloss.Color("90"),
		danger:     lipgloss.Color("160"),
		warn:       lipgloss.Color("130"),
	}
}

// rowStyles is one card row's segment set, pre-bound to the background the
// row sits on so the highlight never breaks between segments.
type rowStyles struct {
	bg     color.Color
	badge  lipgloss.Style
	title  lipgloss.Style
	desc   lipgloss.Style
	fill   lipgloss.Style
	marker lipgloss.Style
}

func newRowStyles(p palette, bg color.Color, cursor bool) rowStyles {
	title := lipgloss.NewStyle().Background(bg).Foreground(p.text)
	if cursor {
		title = title.Bold(true)
	}
	return rowStyles{
		bg:     bg,
		badge:  lipgloss.NewStyle().Background(bg).Foreground(p.muted),
		title:  title,
		desc:   lipgloss.NewStyle().Background(bg).Foreground(p.dim),
		fill:   lipgloss.NewStyle().Background(bg),
		marker: lipgloss.NewStyle().Background(bg),
	}
}

// bar renders n cells of the row background, used to pad a row to the full
// column width so the highlight reaches both edges.
func (r rowStyles) bar(n int) string {
	if n <= 0 {
		return ""
	}
	return r.fill.Render(strings.Repeat(" ", n))
}

// priorityBar renders the leading priority rule in the row's background.
func (r rowStyles) priorityBar(priority string) string {
	return lipgloss.NewStyle().Background(r.bg).Foreground(priorityColor(priority)).Render("▎")
}

var (
	pal = darkPalette()

	// Column chrome: a filled panel with a titled top border.
	columnBorderStyle      lipgloss.Style
	columnBorderFocusStyle lipgloss.Style
	columnTitleStyle       lipgloss.Style
	columnTitleFocusStyle  lipgloss.Style
	columnCountStyle       lipgloss.Style
	columnCountFocusStyle  lipgloss.Style
	columnBodyStyle        lipgloss.Style
	columnBodyFocusStyle   lipgloss.Style
	columnEmptyStyle       lipgloss.Style
	columnGapStyle         lipgloss.Style

	// Card rows, one set per background they can land on.
	rowIdle       rowStyles
	rowCursor     rowStyles
	rowCursorIdle rowStyles

	// sessionRunningStyle / sessionAttachedStyle are the ●/◉ live markers
	// (ADR-001 §3.5): attached outranks running, and both are tinted.
	sessionRunningStyle  lipgloss.Style
	sessionAttachedStyle lipgloss.Style

	// Header and status bars.
	headerFillStyle lipgloss.Style
	brandStyle      lipgloss.Style
	crumbStyle      lipgloss.Style
	crumbSepStyle   lipgloss.Style
	chipStyle       lipgloss.Style
	statusFillStyle lipgloss.Style
	statusTextStyle lipgloss.Style
	statusKeyStyle  lipgloss.Style
	statusWarnStyle lipgloss.Style

	// Overlays (forms, help, card detail) share one raised-card chrome.
	formBoxStyle    lipgloss.Style
	formTitleStyle  lipgloss.Style
	formLabelStyle  lipgloss.Style
	formValueStyle  lipgloss.Style
	formCursorStyle lipgloss.Style
	formErrStyle    lipgloss.Style
	formHintStyle   lipgloss.Style

	helpTitleStyle  lipgloss.Style
	helpKeyStyle    lipgloss.Style
	helpActionStyle lipgloss.Style
	helpHintStyle   lipgloss.Style

	detailTitleStyle   lipgloss.Style
	detailSectionStyle lipgloss.Style
	detailValueStyle   lipgloss.Style
	detailRunTimeStyle lipgloss.Style
	detailHintStyle    lipgloss.Style
	detailErrStyle     lipgloss.Style

	// Loading / error screens.
	splashStyle lipgloss.Style
	errorStyle  lipgloss.Style
)

func init() { applyTheme(true) }

// applyTheme rebinds every style to the palette matching the terminal
// background. It runs once at init (dark, the TUI default) and again when the
// terminal answers tea.RequestBackgroundColor.
func applyTheme(isDark bool) {
	if isDark {
		pal = darkPalette()
	} else {
		pal = lightPalette()
	}
	p := pal

	on := func(bg color.Color) lipgloss.Style { return lipgloss.NewStyle().Background(bg) }

	columnBorderStyle = on(p.surface).Foreground(p.muted)
	columnBorderFocusStyle = on(p.surface).Foreground(p.accent)
	columnTitleStyle = on(p.surface).Foreground(p.dim).Bold(true)
	columnTitleFocusStyle = on(p.surface).Foreground(p.accent).Bold(true)
	columnCountStyle = on(p.surface).Foreground(p.muted)
	columnCountFocusStyle = on(p.accent).Foreground(p.accentFg).Bold(true)
	columnBodyStyle = on(p.surface).
		Border(lipgloss.RoundedBorder(), false, true, true, true).
		BorderForeground(p.muted).
		BorderBackground(p.surface)
	columnBodyFocusStyle = columnBodyStyle.BorderForeground(p.accent)
	columnEmptyStyle = on(p.surface).Foreground(p.muted).PaddingLeft(2)
	columnGapStyle = lipgloss.NewStyle()

	rowIdle = newRowStyles(p, p.surface, false)
	rowCursor = newRowStyles(p, p.selected, true)
	rowCursorIdle = newRowStyles(p, p.surfaceAlt, true)

	sessionRunningStyle = lipgloss.NewStyle().Foreground(p.running)
	sessionAttachedStyle = lipgloss.NewStyle().Foreground(p.attached)

	headerFillStyle = on(p.surfaceAlt).Foreground(p.dim)
	brandStyle = on(p.accent).Foreground(p.accentFg).Bold(true)
	crumbStyle = on(p.surfaceAlt).Foreground(p.text).Bold(true)
	crumbSepStyle = on(p.surfaceAlt).Foreground(p.muted)
	chipStyle = on(p.surface).Foreground(p.dim)
	statusFillStyle = on(p.surfaceAlt).Foreground(p.dim)
	statusTextStyle = on(p.surfaceAlt).Foreground(p.text)
	statusKeyStyle = on(p.surfaceAlt).Foreground(p.accent).Bold(true)
	statusWarnStyle = on(p.surfaceAlt).Foreground(p.warn).Bold(true)

	formBoxStyle = on(p.surface).
		Foreground(p.text).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.accent).
		BorderBackground(p.surface).
		Padding(0, 1)
	formTitleStyle = on(p.surface).Foreground(p.accent).Bold(true)
	formLabelStyle = on(p.surface).Foreground(p.dim).Bold(true)
	formValueStyle = on(p.surface).Foreground(p.text)
	formCursorStyle = on(p.surface).Foreground(p.accent)
	formErrStyle = on(p.surface).Foreground(p.danger)
	formHintStyle = on(p.surface).Foreground(p.muted)

	helpTitleStyle = formTitleStyle
	helpKeyStyle = on(p.surface).Foreground(p.accent)
	helpActionStyle = on(p.surface).Foreground(p.text)
	helpHintStyle = formHintStyle

	detailTitleStyle = formTitleStyle
	detailSectionStyle = on(p.surface).Foreground(p.dim).Bold(true)
	detailValueStyle = formValueStyle
	detailRunTimeStyle = on(p.surface).Foreground(p.text)
	detailHintStyle = formHintStyle
	detailErrStyle = formErrStyle

	splashStyle = lipgloss.NewStyle().Foreground(p.accent).Bold(true)
	errorStyle = lipgloss.NewStyle().Foreground(p.danger)
}

// priorityColor tints the card's leading rule by store priority (ADR-001
// §3.3 closed set: low/medium/high).
func priorityColor(priority string) color.Color {
	switch priority {
	case "high":
		return pal.danger
	case "medium":
		return pal.warn
	default:
		return pal.muted
	}
}

// truncateText clips s to at most maxWidth display cells, marking the cut
// with an ellipsis. Card titles and column names are user text of any
// length, and a column is only ever a handful of cells wide.
func truncateText(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	if maxWidth == 1 {
		return "…"
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w+rw > maxWidth-1 {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}

// barLine lays a left and a right segment on one full-width bar, filling the
// space between them with fill. Both segments arrive pre-styled, so a width
// too small to hold them drops the right one rather than wrapping the bar.
func barLine(width int, fill lipgloss.Style, left, right string) string {
	if width <= 0 {
		return ""
	}
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)
	if gap := width - lw - rw; gap >= 1 {
		return left + fill.Render(strings.Repeat(" ", gap)) + right
	}
	if lw >= width {
		return left
	}
	return left + fill.Render(strings.Repeat(" ", width-lw))
}
