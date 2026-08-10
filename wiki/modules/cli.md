---
description: The subcommand router and stdlib-flag command surface — the scriptable half of loom (CLI/TUI parity), landed as real Go source (T13 router, T14 card subtree).
tags:
  - wiki
  - module
  - cli
title: cli (internal/cli)
type: module
---
## Summary

`internal/cli` is the **non-TUI command surface** — a small stdlib-`flag` subcommand router mirroring ADR-001 §6 (no cobra; the surface is fixed and fully enumerated). It landed as real Go source in T13: `Main(args []string) int` boots config + agent-validation + store, then dispatches the workspace/board/column/init/config/status/version/help trees. `loom` alone prints help for now (the TUI it will eventually launch is T16); every state mutation is scriptable. Spec: ADR-001 §6, DESIGN-002 §13.

## Responsibilities

- Route subcommands against the ADR-001 §6 command table: `init`, `config`, `status`, `version`, `help`, `workspace` (list/create/switch/codebase add/list), `board` (list/create/show/delete), `column` (add `--stage`/list/delete), `card` (add/update/list/show/move/delete — T14; `open`/`close` stay stubbed for T15, as do `attach`/`sessions`) — stubbed leaves answer with an honest "not implemented in this build (planned)" error.
- Wire the composition root: `config.Load()` → `agent.Validate` → `MkdirAll(db dir)` → `store.Open` → a **lazy session proxy** → `board.NewService`, so store-only commands never require `tmux` on PATH.
- `loom status` renders a deterministic `key: value` line stream (workspace/root/board, columns with card counts, session markers `●`/`◉`, recent runs), running reconcile-on-startup first and **degrading to a board summary** when tmux is unavailable.
- `loom config` prints the **effective** loaded config as TOML (BurntSushi `toml.NewEncoder`, no `omitempty` — zero values are meaningful and the output round-trips).
- Persist current workspace/board to `ui_state` on `workspace switch` / `board show`/`create` via [BoardService](../modules/board.md) — selection state never lives in `config.toml` (ADR-001 §5).
- `card` subtree (T14) drives the full card lifecycle through [BoardService](../modules/board.md): `add` (validate `--priority` against `low|medium|high` up front, `--agent` against `agent.Known()` with an accepted-values error, `--column`/`--codebase` resolved by name/path, defaults to the board's lowest-position column and `medium` priority), `update` (merge-patch where **absent flags are never touched** — `--agent=`/`--codebase=` clear to NULL via `flag.Visit` presence tracking, the first use in this codebase; `--title=`/`--priority=` empties rejected; deliberately no `--board`/`--column` — `store.CardUpdate` excludes `column_id`, so repositioning goes through `card move`), `list` (optional `--board`/`--column` scope + client-side `--search` substring over title/description; renders a `cl`/`oc` **agent badge** borrowed from DESIGN-002 §14's TUI convention), `show` (key:value dump, appending `(default)` when the agent is NULL after `AgentOrDefault`), `move <id> <column>` (resolves the column within the card's own board, delegates to `BoardService.MoveCard`, which applies the done-stage auto-kill rule), and `delete`.

## Public API / entry points

```go
func Main(args []string) int // returns the process exit code (0 success, 1 error)
var Version = "dev"          // overridable: go build -ldflags "-X loom/internal/cli.Version=v0.1.0"

type App struct { /* unexported deps bundle: cfg, db, svc, sess, out, errw */ }
func newApp(cfg *config.Config, db *sql.DB, sess sessionManager, out, errw io.Writer) *App

// sessionManager — cli's copy of board's seam plus probe() (tmux-availability
// check for status degradation). *lazySession is the production impl.
type sessionManager interface {
    Ensure/Attach/Kill(ctx, store.Card) error
    Status(ctx) (map[string]session.SessionStatus, error)
    ReconcileOnStartup(ctx) error
    probe() error
}
```

Handlers are unexported `run*(a *App, args []string) error`: `runInit`, `runConfig`, `runStatus`, `runVersion`, `runHelp`, `runWorkspaceList/Create/Switch`, `runCodebaseAdd/List`, `runBoardList/Create/Show/Delete`, `runColumnAdd/List/Delete`, `runCardAdd/Update/List/Show/Move/Delete`, `stubNotBuilt`.

## Card command invariants

- `card update` is a strict merge-patch: `flag.Visit` (not `*flag.Value`) decides whether a flag was passed, so an absent `--agent` leaves the stored agent untouched while a bare `--agent=` clears it to NULL (the effective default `cfg.Agent.Default` applies at render/list time, not at write time). Same for `--codebase=`.
- Empty strings that are meaningless as a value — `--title=`, `--priority=` — are rejected rather than written; the closed priority set is validated up front.
- `--agent` (add) is validated against `agent.Known()` and rejects with the accepted list rendered by `acceptedAgents()`; `card list`/`show` derive the badge/label from `Card.AgentOrDefault(a.cfg.Agent.Default)` so NULL-agent cards never crash rendering.
- `card move` targets the column inside the card's own board (ADR-001; `ErrCrossBoardMove` unreachable through this CLI path), reusing `findBoard`/`findColumn` name resolution and the `board: %w` error convention.

## Key files

- `internal/cli/root.go` — the `command` table (groups have `sub`, leaves have `run`, mirroring §6), `App`/`newApp`, `Main` bootstrap + `finish` (exit-code mapping), `parseFlags`/`expectArgs`, and the `reorderFlags` helper that moves value-taking flags ahead of positionals (`column add <name> --stage X` is §6 order, which stdlib flag stops at otherwise). `-h`/`--help` route to stdout, exit 0.
- `internal/cli/commands.go` — workspace/board/column/codebase handlers + `boardOf`/`findWorkspace`/`findBoard`/`findColumn` name resolution (names are the human surface; IDs are opaque, ADR-001 §3.3). `--stage` validated against the CHECK set up front, defaulting to `todo`.
- `internal/cli/card.go` — the T14 card subtree: `runCardAdd/Update/List/Show/Move/Delete` plus the `columnOf`, `findCodebase`, `filterCards`, `agentBadge`, `acceptedAgents`, `derefStr` helpers. `findCodebase` resolves a `--codebase <path>` against the workspace's registered codebases by absolute path.
- `internal/cli/lazy.go` — `lazySession`: defers `session.New`+`NewManager` to the first session-touching call and **caches success or failure** under a mutex. Store-only commands never touch tmux; `status` calls `probe()` and degrades on error (the cached error embeds tmux's install hint); T15 command failures surface the same error for free.
- `internal/cli/status.go` — `runStatus` + renderers; `sessionRow` sorted by (title, card) so same-title sessions stay deterministic.
- `internal/cli/config.go` — `runConfig` → `toml.NewEncoder(a.out).Encode(a.cfg)`.
- `cmd/loom/main.go` — the 5-line entry point: `os.Exit(cli.Main(os.Args[1:]))`.
- Tests: `root_test.go`, `commands` flows in `root_test.go`, `status_test.go`, `config_test.go`, `lazy_test.go`, `card_test.go` (T14 acceptance: add/update/list/show/move/delete round-trips, `--agent=` reset + re-default on config change, invalid-agent rejection, NULL-vs-explicit badge, done-stage kill on move), `main_test.go` (env-isolated `Main` flow). 80.0% cli coverage.

## Dependencies

- `config` (struct), `agent` (`agent.Validate` at startup, C8), `store`, `session` (types only — via the lazy proxy), `board` (`NewService`). `github.com/BurntSushi/toml` for the config encoder. No cobra.

## Participates in

- Entry point dispatch from `cmd/loom/main.go` (TUI vs CLI; TUI is T16).
- Calls [BoardService](../modules/board.md) for selection persistence, workspace/board/column CRUD, and (T14) the full card lifecycle — `card move` reuses the SAME `MoveCard` done-stage kill path the [TUI flow](../flows/card-open-complete.md) documents; the lazy [session proxy](../modules/session.md) is what [SessionManager](../modules/session.md) gets constructed through.
- [store.RecentRuns](../modules/store.md) feeds `status`'s recent-runs section.

## Related

- Architecture: [Architecture Overview](../architecture/overview.md) · [Session Model](../architecture/session-model.md)
- Modules: [board](../modules/board.md) · [session](../modules/session.md) · [store](../modules/store.md)
- Flows: [Card open → completion](../flows/card-open-complete.md) · [Attach/detach](../flows/attach-detach.md)
