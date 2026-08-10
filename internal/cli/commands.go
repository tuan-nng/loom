package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"loom/internal/store"
)

// validStages is the closed stage set enforced by the columns.stage CHECK
// (ADR-001 §3.3). The CLI validates up front for a friendly message; the schema
// remains the enforcement point.
var validStages = map[string]bool{"backlog": true, "todo": true, "dev": true, "review": true, "done": true}

const defaultColumnStage = "todo"

// boardOf resolves the target board for a column command. An explicit --board
// matches within the resolved workspace first (its boards are the natural
// target), then scans other workspaces in created order as a deterministic
// fallback — so a board name is never silently ambiguous between workspaces.
// A boardless fallback surfaces a clear error rather than "run loom init".
func (a *App) boardOf(fs *flag.FlagSet, boardName string) (store.Workspace, store.Board, error) {
	if boardName == "" {
		ws, b, err := a.svc.ResolveSelection()
		if err != nil {
			return store.Workspace{}, store.Board{}, err
		}
		return ws, b, nil
	}

	ws, err := a.svc.ResolveWorkspace()
	if err == nil {
		if b, err := a.findBoard(ws.ID, boardName); err == nil {
			return ws, b, nil
		}
	}
	all, err := store.ListWorkspaces(a.db)
	if err != nil {
		return store.Workspace{}, store.Board{}, err
	}
	for _, w := range all {
		if b, err := a.findBoard(w.ID, boardName); err == nil {
			return w, b, nil
		}
	}
	return store.Workspace{}, store.Board{}, fmt.Errorf("board %q not found", boardName)
}

func runWorkspaceList(a *App, args []string) error {
	fs := flag.NewFlagSet("workspace list", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 0, 0); err != nil {
		return err
	}
	ws, err := store.ListWorkspaces(a.db)
	if err != nil {
		return err
	}
	for _, w := range ws {
		fmt.Fprintf(a.out, "%s\t%s\n", w.Name, w.RootPath)
	}
	return nil
}

// runWorkspaceCreate creates a workspace named <name> rooted at the current
// directory (root_path is NOT NULL and the spec's command has no path arg, so
// cwd mirrors init's default).
func runWorkspaceCreate(a *App, args []string) error {
	fs := flag.NewFlagSet("workspace create", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 1, 1); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	w, err := store.CreateWorkspace(a.db, fs.Args()[0], cwd)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "created workspace %s (%s)\n", w.Name, w.RootPath)
	return nil
}

// runWorkspaceSwitch persists the workspace as the current selection; the
// board selection is reset and lazily re-resolved (ADR-001 §6, board
// SwitchWorkspace semantics).
func runWorkspaceSwitch(a *App, args []string) error {
	fs := flag.NewFlagSet("workspace switch", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 1, 1); err != nil {
		return err
	}
	w, err := a.findWorkspace(fs.Args()[0])
	if err != nil {
		return err
	}
	if _, err := a.svc.SwitchWorkspace(w.ID); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "switched to workspace %s\n", w.Name)
	return nil
}

func runCodebaseAdd(a *App, args []string) error {
	fs := flag.NewFlagSet("codebase add", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 1, 1); err != nil {
		return err
	}
	ws, err := a.svc.ResolveWorkspace()
	if err != nil {
		return err
	}
	path, err := filepath.Abs(fs.Args()[0])
	if err != nil {
		return err
	}
	cb, err := store.CreateCodebase(a.db, ws.ID, path)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "added codebase %s\n", cb.Path)
	return nil
}

func runCodebaseList(a *App, args []string) error {
	fs := flag.NewFlagSet("codebase list", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 0, 0); err != nil {
		return err
	}
	ws, err := a.svc.ResolveWorkspace()
	if err != nil {
		return err
	}
	cbs, err := store.ListCodebases(a.db, ws.ID)
	if err != nil {
		return err
	}
	for _, cb := range cbs {
		fmt.Fprintln(a.out, cb.Path)
	}
	return nil
}

func runBoardList(a *App, args []string) error {
	fs := flag.NewFlagSet("board list", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 0, 0); err != nil {
		return err
	}
	ws, err := a.svc.ResolveWorkspace()
	if err != nil {
		return err
	}
	bs, err := store.ListBoards(a.db, ws.ID)
	if err != nil {
		return err
	}
	for _, b := range bs {
		fmt.Fprintln(a.out, b.Name)
	}
	return nil
}

// runBoardCreate creates a board (seeding its five default columns) in the
// resolved workspace and persists the selection (ADR-001 §6). It resolves the
// workspace only — a boardless workspace is exactly what this command fixes.
func runBoardCreate(a *App, args []string) error {
	fs := flag.NewFlagSet("board create", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 1, 1); err != nil {
		return err
	}
	ws, err := a.svc.ResolveWorkspace()
	if err != nil {
		return err
	}
	b, err := a.svc.CreateBoard(ws.ID, fs.Args()[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "created board %s\n", b.Name)
	return nil
}

// runBoardShow resolves the board by name within the selected workspace and
// persists the selection {workspace, board} (ADR-001 §6).
func runBoardShow(a *App, args []string) error {
	fs := flag.NewFlagSet("board show", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 1, 1); err != nil {
		return err
	}
	ws, err := a.svc.ResolveWorkspace()
	if err != nil {
		return err
	}
	b, err := a.findBoard(ws.ID, fs.Args()[0])
	if err != nil {
		return err
	}
	if _, err := a.svc.ShowBoard(b.ID); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "showing board %s\n", b.Name)
	return nil
}

func runBoardDelete(a *App, args []string) error {
	fs := flag.NewFlagSet("board delete", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 1, 1); err != nil {
		return err
	}
	ws, err := a.svc.ResolveWorkspace()
	if err != nil {
		return err
	}
	b, err := a.findBoard(ws.ID, fs.Args()[0])
	if err != nil {
		return err
	}
	if err := a.svc.DeleteBoard(b.ID); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "deleted board %s\n", b.Name)
	return nil
}

// runColumnAdd appends a column to the board (--board or selection). --stage
// defaults to "todo" (ADR-001 §6 marks it optional) and is validated against
// the CHECK set up front.
func runColumnAdd(a *App, args []string) error {
	fs := flag.NewFlagSet("column add", flag.ContinueOnError)
	boardName := fs.String("board", "", "board name")
	stage := fs.String("stage", defaultColumnStage, "column stage (backlog|todo|dev|review|done)")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 1, 1); err != nil {
		return err
	}
	if !validStages[*stage] {
		return fmt.Errorf("invalid stage %q (accepted: backlog, todo, dev, review, done)", *stage)
	}
	_, b, err := a.boardOf(fs, *boardName)
	if err != nil {
		return err
	}
	c, err := store.CreateColumn(a.db, b.ID, fs.Args()[0], *stage)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "added column %s (stage %s) to board %s\n", c.Name, c.Stage, b.Name)
	return nil
}

func runColumnList(a *App, args []string) error {
	fs := flag.NewFlagSet("column list", flag.ContinueOnError)
	boardName := fs.String("board", "", "board name")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 0, 0); err != nil {
		return err
	}
	_, b, err := a.boardOf(fs, *boardName)
	if err != nil {
		return err
	}
	cs, err := store.ListColumns(a.db, b.ID)
	if err != nil {
		return err
	}
	for _, c := range cs {
		fmt.Fprintf(a.out, "%s\t%s\n", c.Name, c.Stage)
	}
	return nil
}

func runColumnDelete(a *App, args []string) error {
	fs := flag.NewFlagSet("column delete", flag.ContinueOnError)
	boardName := fs.String("board", "", "board name")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 1, 1); err != nil {
		return err
	}
	_, b, err := a.boardOf(fs, *boardName)
	if err != nil {
		return err
	}
	c, err := a.findColumn(b.ID, fs.Args()[0])
	if err != nil {
		return err
	}
	if err := a.svc.DeleteColumn(c.ID); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "deleted column %s\n", c.Name)
	return nil
}
