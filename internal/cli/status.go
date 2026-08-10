package cli

import (
	"context"
	"flag"
	"fmt"

	"loom/internal/store"
)

// recentRunsLimit is how many completed runs `loom status` shows.
const recentRunsLimit = 5

// runStatus renders the ADR-001 §6 "overall status" as a deterministic stream
// of `key: value` lines: the resolved selection, columns with card counts,
// live sessions (● running / ◉ attached), and recent completed runs. When tmux
// is unavailable, the board summary is printed with a notice and the session/
// reconcile sections are skipped.
func runStatus(a *App, args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 0, 0); err != nil {
		return err
	}

	ws, b, err := a.svc.ResolveSelection()
	if err != nil {
		return err // ErrNotInitialized propagates as "loom: run loom init"
	}
	fmt.Fprintf(a.out, "workspace: %s\n", ws.Name)
	fmt.Fprintf(a.out, "root: %s\n", ws.RootPath)
	fmt.Fprintf(a.out, "board: %s\n", b.Name)

	cols, err := store.ListColumns(a.db, b.ID)
	if err != nil {
		return err
	}
	counts, err := columnCounts(a, b.ID)
	if err != nil {
		return err
	}
	for _, c := range cols {
		fmt.Fprintf(a.out, "column: %s (%s) %d\n", c.Name, c.Stage, counts[c.ID])
	}

	// Reconcile + session markers need tmux; degrade with a notice when it is
	// absent (the probe error embeds the install hint verbatim).
	if err := a.sess.probe(); err != nil {
		fmt.Fprintf(a.errw, "notice: tmux unavailable: %v; showing board summary only\n", err)
		return nil
	}
	if err := a.svc.ReconcileOnStartup(context.Background()); err != nil {
		return err
	}

	if err := renderSessions(a); err != nil {
		return err
	}
	return renderRuns(a)
}

// columnCounts maps column ID → card count in one board pass.
func columnCounts(a *App, boardID string) (map[string]int, error) {
	cs, err := a.svc.ListCardsByBoard(boardID)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(cs))
	for _, c := range cs {
		counts[c.ColumnID]++
	}
	return counts, nil
}

// sessionRow is one rendered session line before output.
type sessionRow struct {
	title string
	card  string // tie-break: same-title cards still render deterministically
	mark  string
}

// renderSessions prints one `session: <marker> <title>` line per live session
// (◉ attached / ● running), sorted by title for determinism. `session: none`
// when no session is live.
func renderSessions(a *App) error {
	st, err := a.svc.SessionStatus(context.Background())
	if err != nil {
		return err
	}
	if len(st) == 0 {
		fmt.Fprintln(a.out, "session: none")
		return nil
	}
	rows := make([]sessionRow, 0, len(st))
	for cardID, status := range st {
		c, err := a.svc.GetCard(cardID)
		if err != nil {
			if store.IsNotFound(err) {
				continue // run cascades away with the card on next reconcile
			}
			return err
		}
		mark := "●"
		if status.Attached {
			mark = "◉"
		}
		rows = append(rows, sessionRow{title: c.Title, card: cardID, mark: mark})
	}
	sortRows(rows)
	for _, r := range rows {
		fmt.Fprintf(a.out, "session: %s %s\n", r.mark, r.title)
	}
	return nil
}

// renderRuns prints the most-recent completed runs, newest first. `run: none`
// when there are none.
func renderRuns(a *App) error {
	runs, err := store.RecentRuns(a.db, recentRunsLimit)
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		fmt.Fprintln(a.out, "run: none")
		return nil
	}
	for _, r := range runs {
		fmt.Fprintf(a.out, "run: %s (%dms, files=%d)\n", r.Title, r.DurationMs, r.FilesChanged)
	}
	return nil
}

// sortRows orders session rows by (title, card) so output is deterministic
// even when two live cards share a title.
func sortRows(rows []sessionRow) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && (rows[j].title < rows[j-1].title || (rows[j].title == rows[j-1].title && rows[j].card < rows[j-1].card)); j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}
