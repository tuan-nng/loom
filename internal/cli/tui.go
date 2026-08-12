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
// when the handoff returns (ADR-001 §4.4, T17). Inside an enclosing tmux
// client ($TMUX set) it opens a new window of that outer session instead of
// nesting a second tmux client inside the current pane (mirrors
// session.Tmux.AttachCommand; duplicated here because bin is unexported),
// so the handoff returns almost immediately and the board reappears with
// the session available as a tab. Outside tmux, falls back to a direct
// attach.
func (t tuiService) TmuxAttach(cardID string) (*exec.Cmd, error) {
	name := session.SessionName(cardID)
	if os.Getenv("TMUX") != "" {
		return exec.Command("tmux", "new-window", "-n", name, "--", "tmux", "-L", t.tmuxServer, "attach-session", "-t", name), nil
	}
	return exec.Command("tmux", "-L", t.tmuxServer, "attach-session", "-t", name), nil
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
