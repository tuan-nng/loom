package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"loom/internal/session"
	"loom/internal/store"
)

// cardDetail is the T19 `d` overlay: a read-only detail pane for the focused
// card — metadata, resolved agent, codebase path, and the run history with
// per-run duration and unique files changed (ADR-001 §3.5, DESIGN-002 §14).
// It is snapshot-built on open; esc closes it and every key is swallowed
// while it is up, exactly like the T18 forms.
type cardDetail struct {
	card         store.Card
	codebase     string // resolved codebase path, "" when unset/unresolvable
	agentLabel   string // "claude (default)" / "opencode"
	defaultAgent string // config default, for the (default) suffix
	runs         []store.CardRun
	status       session.SessionStatus
	err          string // one-line load error (codebase/runs), shown inline
}

// openCardDetail builds the detail pane for the card under the cursor. A
// focused card is required (the list cursor can sit on empty space).
func (m Model) openCardDetail() (Model, tea.Cmd) {
	id, ok := m.focusedCardID()
	if !ok {
		m.note = "no card to inspect"
		return m, nil
	}
	card, ok := m.cardByID(id)
	if !ok {
		m.note = "no card to inspect"
		return m, nil
	}
	d := cardDetail{
		card:         card,
		agentLabel:   card.AgentOrDefault(m.defaultAgent),
		defaultAgent: m.defaultAgent,
		status:       m.status[id],
	}
	if card.CodebaseID != nil && *card.CodebaseID != "" {
		cb, err := m.svc.GetCodebase(*card.CodebaseID)
		if err != nil {
			d.err = "codebase: " + err.Error()
		} else {
			d.codebase = cb.Path
		}
	}
	runs, err := m.svc.RunsForCard(id)
	if err != nil {
		d.err = "runs: " + err.Error()
	} else {
		d.runs = runs
	}
	m.detail = &d
	return m, nil
}

// detailUpdate owns every key while the pane is open; only esc (and q, the
// canonical close) close it.
func (m Model) detailUpdate(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.detail == nil {
		return m, nil
	}
	if key.Matches(msg, key.NewBinding(key.WithKeys("esc", "q"))) {
		m.detail = nil
	}
	return m, nil
}

// detailView renders the pane centered over the board, boxed like the forms.
func (m Model) detailView() string {
	if m.detail == nil {
		return ""
	}
	return m.detail.view(m.width, m.height)
}

// view composes the detail pane content and centers it.
func (d cardDetail) view(width, height int) string {
	boxW := width - 4
	if boxW > 56 {
		boxW = 56
	}
	if boxW < 20 {
		boxW = 20
	}

	var b strings.Builder
	b.WriteString(detailTitleStyle.Render(d.card.Title))
	b.WriteString("\n\n")

	meta := []string{
		fmt.Sprintf("Priority: %s", detailValueStyle.Render(d.card.Priority)),
	}
	if d.card.Labels != nil && *d.card.Labels != "" {
		meta = append(meta, fmt.Sprintf("Labels: %s", detailValueStyle.Render(*d.card.Labels)))
	}
	b.WriteString(strings.Join(meta, "   "))
	b.WriteString("\n")

	if d.codebase != "" {
		b.WriteString(fmt.Sprintf("Codebase: %s\n", detailValueStyle.Render(d.codebase)))
	}
	agent := d.agentLabel
	if d.card.Agent == nil || *d.card.Agent == "" {
		agent = fmt.Sprintf("%s (default)", d.agentLabel)
	}
	b.WriteString(fmt.Sprintf("Agent: %s\n", detailValueStyle.Render(agent)))

	if s := d.card.Description; s != nil && *s != "" {
		b.WriteString("\n" + detailSectionStyle.Render("Description") + "\n")
		b.WriteString(renderMarkdown(*s))
	}
	if s := d.card.Objective; s != nil && *s != "" {
		b.WriteString("\n" + detailSectionStyle.Render("Objective") + "\n")
		b.WriteString(renderMarkdown(*s))
	}
	if s := d.card.AcceptanceCriteria; s != nil && *s != "" {
		b.WriteString("\n" + detailSectionStyle.Render("Acceptance Criteria") + "\n")
		b.WriteString(renderMarkdown(*s))
	}

	b.WriteString("\n" + detailSectionStyle.Render("Runs") + "\n")
	if len(d.runs) == 0 {
		b.WriteString(detailHintStyle.Render("(no runs yet)"))
	} else {
		for _, r := range d.runs {
			b.WriteString("  " + detailRunLine(r, d.status) + "\n")
		}
	}

	if d.err != "" {
		b.WriteString("\n" + detailErrStyle.Render(d.err) + "\n")
	}

	b.WriteString("\n" + detailHintStyle.Render("esc/q close"))

	box := formBoxStyle.Render(b.String())
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// detailRunLine renders one run: session marker, started time, duration, and
// the unique files changed.
func detailRunLine(r store.CardRun, status session.SessionStatus) string {
	marker := "  "
	if status.Running {
		if status.Attached {
			marker = sessionAttachedStyle.Render("◉")
		} else {
			marker = sessionRunningStyle.Render("●")
		}
	}
	started := r.StartedAt
	// The store writes created_at as store.TraceTimeLayout (%Y-%m-%dT%H:%M:%f,
	// no timezone); parse with it so the friendly clock renders instead of
	// falling back to the raw string.
	if t, err := time.Parse(store.TraceTimeLayout, r.StartedAt); err == nil {
		started = t.Format("Jan _2 15:04")
	}
	dur := "open"
	if r.EndedAt != nil {
		dur = fmt.Sprintf("%s", time.Duration(r.DurationMs)*time.Millisecond)
	}
	line := fmt.Sprintf("%s %s  %s", marker, detailRunTimeStyle.Render(started), detailHintStyle.Render(dur))
	if len(r.Files) > 0 {
		line += fmt.Sprintf("  %s", detailHintStyle.Render(strings.Join(r.Files, ", ")))
	}
	return line
}

// renderMarkdown renders one description/objective/acceptance field. It is a
// tolerant text renderer (heading/emphasis/list stripped to plain lines);
// the full Glamour path is a later enhancement (T19 spec).
func renderMarkdown(s string) string {
	var b strings.Builder
	for _, ln := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(trimmed, "###"):
			b.WriteString(detailSectionStyle.Render(strings.TrimSpace(strings.TrimPrefix(trimmed, "###"))))
		case strings.HasPrefix(trimmed, "##"):
			b.WriteString(detailSectionStyle.Render(strings.TrimSpace(strings.TrimPrefix(trimmed, "##"))))
		case strings.HasPrefix(trimmed, "- "):
			b.WriteString("  • " + detailHintStyle.Render(strings.TrimPrefix(trimmed, "- ")))
		default:
			b.WriteString(trimmed)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

var (
	detailTitleStyle   = lipgloss.NewStyle().Bold(true).Underline(true)
	detailSectionStyle = lipgloss.NewStyle().Bold(true)
	detailValueStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	detailRunTimeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	detailHintStyle    = lipgloss.NewStyle().Faint(true)
	detailErrStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)
