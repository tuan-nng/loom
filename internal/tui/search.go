package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"loom/internal/store"
)

// searchMsg is the result of the / form commit; the query becomes the board's
// active filter. An empty query clears the filter.
type searchMsg struct {
	query string
}

// filterCards keeps cards whose title or description contains q
// (case-insensitive), mirroring the CLI's `card list --search` filter so a
// TUI action stays scriptable (ADR-001 §3.5 parity). The store has no search
// primitive, so filtering is client-side over the loaded snapshot.
func filterCards(cards []store.Card, q string) []store.Card {
	q = strings.ToLower(q)
	out := make([]store.Card, 0, len(cards))
	for _, c := range cards {
		if strings.Contains(strings.ToLower(c.Title), q) {
			out = append(out, c)
			continue
		}
		if c.Description != nil && strings.Contains(strings.ToLower(*c.Description), q) {
			out = append(out, c)
		}
	}
	return out
}

// visibleCards is the board's render source: the snapshot filtered by the
// active search, or the snapshot unchanged when no filter is set. buildLists
// and listCount both consume it, so header counts narrow with the filter and
// every re-fetch preserves it.
func (m Model) visibleCards() []store.Card {
	if m.search == "" {
		return m.cards
	}
	return filterCards(m.cards, m.search)
}

// openSearch opens the / overlay seeded with the current filter, so esc
// restores the pre-existing filter and an empty submit clears it.
func (m Model) openSearch() (Model, tea.Cmd) {
	f := &form{
		kind:   formSearch,
		title:  "Search Cards",
		fields: []field{textField("query", m.search)},
	}
	f.syncFocus()
	m.form = f
	return m, nil
}

// afterSearch applies the committed query as the board filter and rebuilds
// the columns in place — no re-fetch, the filter is client-side over the
// snapshot (ADR-001 §3.5).
func (m Model) afterSearch(msg searchMsg) (tea.Model, tea.Cmd) {
	m.search = strings.TrimSpace(msg.query)
	if m.search != "" {
		m.note = fmt.Sprintf("filtered to %d card(s)", len(m.visibleCards()))
	} else {
		m.note = "search cleared"
	}
	m.buildLists()
	m.relayout()
	return m, nil
}