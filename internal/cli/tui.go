package cli

import (
	"io"
	"os"
	"os/exec"

	"loom/internal/board"
	"loom/internal/session"
	"loom/internal/tui"
)

// tuiService wraps *board.Service with the config the board service itself
// deliberately does not carry: the launch default agent (badge resolution,
// DESIGN-002 §14) and the tmux attach handoff argv (server name). Everything
// else — selection, cards, columns, sessions, open/kill — is board.Service.
type tuiService struct {
	*board.Service
	defaultAgent string
	tmuxServer   string
}

func (t tuiService) DefaultAgent() string { return t.defaultAgent }

// TmuxAttach builds the attach handoff the TUI runs via tea.ExecProcess:
// tmux owns the terminal while attached, and BubbleTea restores the board
// when the handoff returns (ADR-001 §4.4, T17). On an enclosing tmux client
// ($TMUX set) the handoff links the session's window into the enclosing
// session as a normal tab — a plain pane, not a nested client — so it returns
// almost immediately and the board reappears with the session available as a
// tab; standalone it is a direct attach. The decision lives in
// session.AttachCommand, reached here via AttachCommandFor so the TUI and the
// session manager cannot drift apart.
func (t tuiService) TmuxAttach(cardID string) (*exec.Cmd, error) {
	return session.AttachCommandFor(t.tmuxServer, session.SessionName(cardID))
}

// runTUI boots the board TUI for the resolved selection and blocks until it
// quits (ADR-001 §6: bare `loom` launches the TUI). It only takes the
// terminal when stdout is interactive; piped invocation falls back to help so
// scripts keep a deterministic, non-interactive surface (TestRunBarePrintsHelp
// runs non-TTY for this reason).
func runTUI(a *App) error {
	if !isTerminal(a.out) {
		return a.printHelp()
	}
	return tui.Run(tuiService{
		Service:      a.svc,
		defaultAgent: a.cfg.Agent.Default,
		tmuxServer:   a.cfg.Session.TmuxServer,
	})
}

// isTerminal reports whether w is an interactive terminal (a character
// device). It is stdlib-only — no golang.org/x/term dep outside the pinned
// set.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
