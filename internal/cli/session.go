package cli

import (
	"context"
	"flag"
	"fmt"
)

// runCardOpen ensures the card's session and, unless --detach, hands the
// terminal to it (ADR-001 §4.1 steps 1-2, DESIGN-002 §13). No probe here:
// OpenCard's lazy materialization is the tmux gate, so a missing tmux fails
// the open loudly instead of degrading. The title comes from an upfront
// GetCard, matching runCardDelete's echo convention.
func runCardOpen(a *App, args []string) error {
	fs := flag.NewFlagSet("card open", flag.ContinueOnError)
	detach := fs.Bool("detach", false, "ensure the session but do not attach")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 1, 1); err != nil {
		return err
	}
	id := fs.Args()[0]
	c, err := a.svc.GetCard(id)
	if err != nil {
		return err
	}
	if err := a.svc.OpenCard(context.Background(), id, *detach); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "opened card %s (%s)\n", c.Title, c.ID)
	return nil
}

// runCardClose kills the card's session and finalizes its run's trace
// (ADR-001 §4.1 step 4, non-interactive). Kill on an already-absent session
// is a no-op (manager.go:243-265), so closing a never-opened card succeeds.
func runCardClose(a *App, args []string) error {
	fs := flag.NewFlagSet("card close", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 1, 1); err != nil {
		return err
	}
	id := fs.Args()[0]
	c, err := a.svc.GetCard(id)
	if err != nil {
		return err
	}
	if err := a.svc.CloseCard(context.Background(), id); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "closed card %s (%s)\n", c.Title, c.ID)
	return nil
}

// runAttach is a pure attach: the card must already have a live session
// (session.Manager.Attach errors "session: card <id> has no live session"
// otherwise). The tmux attach-session client owns the terminal, so there is
// nothing to print on success.
func runAttach(a *App, args []string) error {
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 1, 1); err != nil {
		return err
	}
	return a.svc.Attach(context.Background(), fs.Args()[0])
}

// runSessions prints one `session: <marker> <title>` line per live session,
// reusing status's renderer verbatim so the two outputs can never drift.
// Read-only: a missing tmux degrades with a notice (exit 0) exactly like
// `loom status` (status.go:50-53), not a hard failure.
func runSessions(a *App, args []string) error {
	fs := flag.NewFlagSet("sessions", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 0, 0); err != nil {
		return err
	}
	if err := a.sess.probe(); err != nil {
		fmt.Fprintf(a.errw, "notice: tmux unavailable: %v; no sessions to show\n", err)
		return nil
	}
	if err := a.svc.ReconcileOnStartup(context.Background()); err != nil {
		return err
	}
	return renderSessions(a)
}