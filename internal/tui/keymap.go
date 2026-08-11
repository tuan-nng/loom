package tui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
)

// KeyMap is the canonical board keymap (ADR-001 §3.5). Every key in the table
// is present; those whose feature lands in a later task bind to a status-bar
// stub that names it, so the board never swallows a key silently.
type KeyMap struct {
	CursorUp   key.Binding // j/k, ↑/↓ — focused column's cards
	CursorDown key.Binding
	Left       key.Binding // h/l, ←/→ — column focus
	Right      key.Binding
	PageUp     key.Binding // scroll the focused column
	PageDown   key.Binding
	GoToStart  key.Binding
	GoToEnd    key.Binding

	Open       key.Binding // Enter — open card session (T17)
	Kill       key.Binding // K — kill + finalize (T17)
	NewCard    key.Binding // n — new card form (T18)
	NewColumn  key.Binding // N — new column form (T18)
	Move       key.Binding // m — column picker (T18)
	Detail     key.Binding // d — detail view (T19)
	Edit       key.Binding // e — edit card form (T18)
	Search     key.Binding // / — filter (T20)
	Board      key.Binding // s — switch board (T20)
	Workspace  key.Binding // w — switch workspace (T20)
	Help       key.Binding // ? — help overlay (T20)

	Quit       key.Binding // q, Ctrl+c — confirm if sessions attached
	ForceQuit  key.Binding // Q — quit, sessions keep running detached
	ConfirmYes key.Binding // y, Enter — confirm the quit overlay
	ConfirmNo  key.Binding // n, Esc — cancel the quit overlay
}

// defaultKeyMap returns the §3.5 binding table. Quit confirm keys share the
// board's n/Enter; they are only consulted while the overlay is open.
func defaultKeyMap() KeyMap {
	return KeyMap{
		CursorUp:   key.NewBinding(key.WithKeys("up", "k")),
		CursorDown: key.NewBinding(key.WithKeys("down", "j")),
		Left:       key.NewBinding(key.WithKeys("left", "h")),
		Right:      key.NewBinding(key.WithKeys("right", "l")),
		PageUp:     key.NewBinding(key.WithKeys("pgup")),
		PageDown:   key.NewBinding(key.WithKeys("pgdown")),
		GoToStart:  key.NewBinding(key.WithKeys("home", "g")),
		GoToEnd:    key.NewBinding(key.WithKeys("end", "G")),

		Open:      key.NewBinding(key.WithKeys("enter")),
		Kill:      key.NewBinding(key.WithKeys("K")),
		NewCard:   key.NewBinding(key.WithKeys("n")),
		NewColumn: key.NewBinding(key.WithKeys("N")),
		Move:      key.NewBinding(key.WithKeys("m")),
		Detail:    key.NewBinding(key.WithKeys("d")),
		Edit:      key.NewBinding(key.WithKeys("e")),
		Search:    key.NewBinding(key.WithKeys("/")),
		Board:     key.NewBinding(key.WithKeys("s")),
		Workspace: key.NewBinding(key.WithKeys("w")),
		Help:      key.NewBinding(key.WithKeys("?")),

		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c")),
		ForceQuit:  key.NewBinding(key.WithKeys("Q")),
		ConfirmYes: key.NewBinding(key.WithKeys("y", "enter")),
		ConfirmNo:  key.NewBinding(key.WithKeys("n", "esc")),
	}
}

// columnKeyMap is the keymap for each column's bubbles list. The list's
// defaults hijack h/l (page), / (filter), q (quit) and ? (help) — all owned
// by the canonical keymap — so each column keeps only the cursor and paging
// keys the board forwards to it; everything else is disabled. NextPage/
// PrevPage keep pgup/pgdown (no h/l) so paging never collides with column
// focus.
func columnKeyMap() list.KeyMap {
	km := list.DefaultKeyMap()
	km.CursorUp = key.NewBinding(key.WithKeys("up", "k"))
	km.CursorDown = key.NewBinding(key.WithKeys("down", "j"))
	km.PrevPage = key.NewBinding(key.WithKeys("pgup"))
	km.NextPage = key.NewBinding(key.WithKeys("pgdown"))
	km.GoToStart = key.NewBinding(key.WithKeys("home", "g"))
	km.GoToEnd = key.NewBinding(key.WithKeys("end", "G"))
	off := key.NewBinding(key.WithDisabled())
	km.Filter = off
	km.ClearFilter = off
	km.CancelWhileFiltering = off
	km.AcceptWhileFiltering = off
	km.ShowFullHelp = off
	km.CloseFullHelp = off
	km.Quit = off
	km.ForceQuit = off
	return km
}