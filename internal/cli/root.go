package cli

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"loom/internal/agent"
	"loom/internal/board"
	"loom/internal/config"
	"loom/internal/store"
)

// Version is overridable at build time:
//
//	go build -ldflags "-X loom/internal/cli.Version=v0.1.0"
var Version = "dev"

// helpText mirrors the ADR-001 §6 surface plus DESIGN-002 §13's --agent
// additions. T15 wired the session commands (card open/close, attach, sessions).
const helpText = `usage: loom <command> [args]

commands:
  loom                        launch the TUI (default)
  loom init [<dir>]           initialize loom for a directory
  loom config                 print the effective TOML config
  loom status                 show the current board, sessions, and recent runs
  loom version                print the version
  loom help                   show this help

  loom workspace list
  loom workspace create <name>
  loom workspace switch <name>
  loom workspace codebase add <path>
  loom workspace codebase list

  loom board list
  loom board create <name>
  loom board show <name>
  loom board delete <name>

  loom column add <name> [--board <name>] [--stage <stage>]
  loom column list [--board <name>]
  loom column delete <name> [--board <name>]

  loom card add <title> [--description <text>] [--objective <text>]
      [--acceptance-criteria <text>] [--priority <low|medium|high>]
      [--labels a,b] [--codebase <path>] [--board <name>] [--column <name>]
      [--agent <name>]
  loom card list [--board <name>] [--column <name>] [--search <q>]
  loom card show <id>
  loom card move <id> <column>
  loom card open <id> [--detach]
  loom card close <id>
  loom card update <id> [--title <text>] [--description <text>]
      [--objective <text>] [--acceptance-criteria <text>]
      [--priority <p>] [--labels a,b] [--codebase <path>] [--agent <name>]
  loom card delete <id>

  loom attach <id>
  loom sessions
`

// command is one node in the fixed ADR-001 §6 surface. Groups have sub; leaves
// have run.
type command struct {
	usage string
	run   func(a *App, args []string) error
	sub   map[string]*command
}

func rootCommands() map[string]*command {
	return map[string]*command{
		"init":    {usage: "loom init [<dir>]", run: runInit},
		"config":  {usage: "loom config", run: runConfig},
		"status":  {usage: "loom status", run: runStatus},
		"version": {usage: "loom version", run: runVersion},
		"help":    {usage: "loom help", run: runHelp},
		"workspace": {usage: "loom workspace <list|create|switch|codebase>", sub: map[string]*command{
			"list":   {usage: "loom workspace list", run: runWorkspaceList},
			"create": {usage: "loom workspace create <name>", run: runWorkspaceCreate},
			"switch": {usage: "loom workspace switch <name>", run: runWorkspaceSwitch},
			"codebase": {usage: "loom workspace codebase <add|list>", sub: map[string]*command{
				"add":  {usage: "loom workspace codebase add <path>", run: runCodebaseAdd},
				"list": {usage: "loom workspace codebase list", run: runCodebaseList},
			}},
		}},
		"board": {usage: "loom board <list|create|show|delete>", sub: map[string]*command{
			"list":   {usage: "loom board list", run: runBoardList},
			"create": {usage: "loom board create <name>", run: runBoardCreate},
			"show":   {usage: "loom board show <name>", run: runBoardShow},
			"delete": {usage: "loom board delete <name>", run: runBoardDelete},
		}},
		"column": {usage: "loom column <add|list|delete>", sub: map[string]*command{
			"add":    {usage: "loom column add <name> [--board <name>] [--stage <stage>]", run: runColumnAdd},
			"list":   {usage: "loom column list [--board <name>]", run: runColumnList},
			"delete": {usage: "loom column delete <name> [--board <name>]", run: runColumnDelete},
		}},
		"card": {usage: "loom card <add|list|show|move|open|close|update|delete>", sub: map[string]*command{
			"add":    {usage: "loom card add <title> [--description <text>] [--objective <text>] [--acceptance-criteria <text>] [--priority <low|medium|high>] [--labels a,b] [--codebase <path>] [--board <name>] [--column <name>] [--agent <name>]", run: runCardAdd},
			"update": {usage: "loom card update <id> [--title <text>] [--description <text>] [--objective <text>] [--acceptance-criteria <text>] [--priority <p>] [--labels a,b] [--codebase <path>] [--agent <name>]", run: runCardUpdate},
			"list":   {usage: "loom card list [--board <name>] [--column <name>] [--search <q>]", run: runCardList},
			"show":   {usage: "loom card show <id>", run: runCardShow},
			"move":   {usage: "loom card move <id> <column>", run: runCardMove},
			"delete": {usage: "loom card delete <id>", run: runCardDelete},
			"open":   {usage: "loom card open <id> [--detach]", run: runCardOpen},
			"close":  {usage: "loom card close <id>", run: runCardClose},
		}},
		"attach":   {usage: "loom attach <id>", run: runAttach},
		"sessions": {usage: "loom sessions", run: runSessions},
	}
}

// App bundles the CLI's dependencies so handlers are testable without main.go:
// a fake session manager and buffer writers are injectable via newApp.
type App struct {
	cfg  *config.Config
	db   *sql.DB
	svc  *board.Service
	sess sessionManager
	out  io.Writer
	errw io.Writer
}

func newApp(cfg *config.Config, db *sql.DB, sess sessionManager, out, errw io.Writer) *App {
	return &App{cfg: cfg, db: db, svc: board.NewService(db, sess), sess: sess, out: out, errw: errw}
}

// Main is the CLI entry point: it loads config, validates the agent surface,
// opens the store, wires the lazy session proxy and board service, then
// dispatches args. It returns the process exit code (0 success, 1 error).
func Main(args []string) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "loom: %v\n", err)
		return 1
	}
	if err := agent.Validate(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "loom: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "loom: %v\n", err)
		return 1
	}
	db, err := store.Open(cfg.Database.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "loom: %v\n", err)
		return 1
	}
	defer db.Close()
	a := newApp(cfg, db, newLazySession(cfg, db), os.Stdout, os.Stderr)
	return a.finish(a.run(args))
}

// finish converts a run error into an exit code, printing it to a.errw:
// usage errors carry a "run 'loom help'" hint, everything else the error
// itself (both prefixed `loom:`). errHelpPrinted (a leaf's -h/--help, already
// rendered to stdout) exits 0.
func (a *App) finish(err error) int {
	if err == nil || errors.Is(err, errHelpPrinted) {
		return 0
	}
	var ue *usageError
	if errors.As(err, &ue) {
		fmt.Fprintf(a.errw, "loom: %s\n", ue.msg)
		fmt.Fprintln(a.errw, "run 'loom help'")
		return 1
	}
	fmt.Fprintf(a.errw, "loom: %v\n", err)
	return 1
}

// run dispatches args against the command tree. Bare `loom` prints help (the
// TUI it will eventually launch is not built until T16).
func (a *App) run(args []string) error {
	if len(args) == 0 {
		return a.printHelp()
	}
	switch args[0] {
	case "--help", "-h", "help":
		return a.printHelp()
	}
	cmd, ok := rootCommands()[args[0]]
	if !ok {
		return &usageError{msg: fmt.Sprintf("unknown command %q", args[0])}
	}
	return a.dispatch(cmd, args[1:])
}

func (a *App) dispatch(cmd *command, args []string) error {
	if cmd.run != nil {
		return cmd.run(a, args)
	}
	if len(args) == 0 {
		return &usageError{msg: cmd.usage}
	}
	next, ok := cmd.sub[args[0]]
	if !ok {
		return &usageError{msg: fmt.Sprintf("unknown command %q", args[0])}
	}
	return a.dispatch(next, args[1:])
}

func (a *App) printHelp() error {
	_, err := fmt.Fprint(a.out, helpText)
	return err
}

// usageError signals a malformed invocation: the message goes to stderr with a
// "run 'loom help'" hint, exit 1.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

// errHelpPrinted is the sentinel parseFlags returns after printing a leaf's
// usage line for -h/--help. It propagates through every handler (which return
// parseFlags' error immediately) to finish, which maps it to exit 0.
var errHelpPrinted = errors.New("help printed")

// parseFlags parses fs against args, routing -h/--help output to a.out (exit 0)
// and parse errors to a usageError (stderr, exit 1). args are reordered so
// flags precede positionals first: ADR-001 §6 puts positionals before flags
// (`column add <name> --stage X`), which stdlib flag stops at otherwise.
func parseFlags(a *App, fs *flag.FlagSet, args []string) error {
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(a.out, "usage: %s\n", fs.Name())
			return errHelpPrinted
		}
		return &usageError{msg: err.Error()}
	}
	return nil
}

// reorderFlags moves value-taking flag tokens ahead of positional tokens while
// preserving relative order within each group. A `--name value` pair stays
// together (detected via the registered flag's IsBoolFlag); `--name=value`
// needs no lookahead. Unknown flags are left for Parse to reject.
func reorderFlags(fs *flag.FlagSet, args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-" || !strings.HasPrefix(a, "-") {
			pos = append(pos, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimPrefix(strings.TrimPrefix(a, "-"), "-")
		if name == "" || strings.Contains(name, "=") {
			continue // "--" or inline value: nothing to look ahead for
		}
		f := fs.Lookup(name)
		if f == nil {
			continue // unknown flag; let Parse reject it
		}
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			continue
		}
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, pos...)
}

// expectArgs enforces the positional-argument count after flag parsing (stdlib
// flag can only count positionals post-parse). max < 0 means unbounded.
func expectArgs(fs *flag.FlagSet, min, max int) error {
	n := len(fs.Args())
	if n < min || (max >= 0 && n > max) {
		if min == max {
			return &usageError{msg: fmt.Sprintf("%s: expected %d argument(s), got %d", fs.Name(), min, n)}
		}
		return &usageError{msg: fmt.Sprintf("%s: expected %d-%d arguments, got %d", fs.Name(), min, max, n)}
	}
	return nil
}

func runVersion(a *App, args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 0, 0); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "loom %s\n", Version)
	return nil
}

func runHelp(a *App, args []string) error {
	fs := flag.NewFlagSet("help", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	return a.printHelp()
}

// runInit initializes loom for a directory (default cwd): a workspace named
// after the directory plus the default board and five columns, idempotent for
// an already-registered root (ADR-001 §6). The directory is recorded as-is; it
// is not created.
func runInit(a *App, args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if err := expectArgs(fs, 0, 1); err != nil {
		return err
	}
	dir := "."
	if len(fs.Args()) == 1 {
		dir = fs.Args()[0]
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	w, err := store.InitWorkspace(a.db, abs)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "initialized %s (%s)\n", w.Name, w.RootPath)
	return nil
}

// findWorkspace resolves a workspace by name (names are unique enough for CLI
// use; IDs are opaque and not for humans, ADR-001 §3.3).
func (a *App) findWorkspace(name string) (store.Workspace, error) {
	ws, err := store.ListWorkspaces(a.db)
	if err != nil {
		return store.Workspace{}, err
	}
	for _, w := range ws {
		if w.Name == name {
			return w, nil
		}
	}
	return store.Workspace{}, fmt.Errorf("workspace %q not found", name)
}

func (a *App) findBoard(workspaceID, name string) (store.Board, error) {
	bs, err := store.ListBoards(a.db, workspaceID)
	if err != nil {
		return store.Board{}, err
	}
	for _, b := range bs {
		if b.Name == name {
			return b, nil
		}
	}
	return store.Board{}, fmt.Errorf("board %q not found", name)
}

func (a *App) findColumn(boardID, name string) (store.Column, error) {
	cs, err := store.ListColumns(a.db, boardID)
	if err != nil {
		return store.Column{}, err
	}
	for _, c := range cs {
		if c.Name == name {
			return c, nil
		}
	}
	return store.Column{}, fmt.Errorf("column %q not found", name)
}
