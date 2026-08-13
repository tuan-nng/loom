# loom

A terminal-native, multi-agent Kanban board. Each card is a unit of work; opening
a card launches a coding agent — [`claude`](https://docs.anthropic.com/claude-code)
or [`opencode`](https://opencode.ai) — in a detached [tmux](https://github.com/tmux/tmux)
session scoped to the card's codebase. You attach to talk to the agent, detach
to let it keep running, and loom traces every file it touches (via `fsnotify`
plus a git-baseline reconciliation) so you always know what changed and when a
run finished.

See [`docs/ADR-001-loom-architecture.md`](docs/ADR-001-loom-architecture.md)
(base architecture) and
[`docs/ADR-002-loom-multi-agent.md`](docs/ADR-002-loom-multi-agent.md)
(multi-agent design, **Adopted**) for the full rationale; this README covers
build, configuration, and day-to-day use.

## Requirements

- **Go 1.23+** to build.
- **tmux >= 3.x** on `$PATH` — loom refuses to start on an older version.
- At least one coding agent on `$PATH` (or pointed to via config,
  see [Configuration](#configuration)):
  - [`claude`](https://docs.anthropic.com/claude-code) (Claude Code), or
  - [`opencode`](https://opencode.ai) — **verified against v1.18.15**
    (`docs/PROBE-full-tui.md`, `docs/DESIGN-002-loom-multi-agent.md` §3). Both
    its `--mini` (default) and full-TUI interfaces are supported; re-run the
    Phase 0 probes on each opencode major version bump.

Neither agent is a Go dependency — both are user-owned external binaries loom
shells out to via a detached tmux session.

## Build

```sh
go build -o loom ./cmd/loom
```

Embed a version string at build time (`loom version` otherwise prints `dev`):

```sh
go build -ldflags "-X loom/internal/cli.Version=v0.1.0" -o loom ./cmd/loom
```

## Quick start

```sh
loom init                 # create ~/.config/loom, the db, a default board
                           # ("Board" with Backlog/To Do/In Progress/Review/Done)
loom card add "Fix the flaky retry test" --agent claude
loom                       # launch the TUI — j/k or h/l to navigate, Enter to
                           # open a card's session, ? for the full keymap
```

Everything the TUI does is also scriptable from the CLI (`loom help` for the
full command tree):

```sh
loom card add "Add opencode support" --objective "..." --agent opencode
loom card open <id> --detach   # ensure the session, don't attach
loom card list --board Board
loom attach <id>               # attach to a running session
loom card close <id>           # kill the session, finalize the trace
loom status                    # board, live sessions, recent runs
```

## Using the loom UI

Bare `loom` launches the board TUI when stdout is an interactive terminal;
piped/non-interactive invocations print help instead, so scripts get a
deterministic surface.

### Layout

The screen is a row of columns — one per board stage (Backlog / To Do / In
Progress / Review / Done by default) — each headed by its name and card
count, with a one-line status bar pinned to the bottom (`workspace › board`
on the left, a session summary or the last action's note on the right). Each
card row shows an agent badge (`[cl]` claude, `[oc]` opencode) and a live
session marker: `●` running, `◉` attached, nothing for an idle card.

### Keymap

One table governs the board, the card-detail pane, and the help overlay —
every key mirrors a CLI command, so nothing here is TUI-exclusive:

| Key | Action | CLI equivalent |
|-----|--------|----------------|
| `j`/`k`, `↓`/`↑` | Focus previous/next card | — |
| `h`/`l`, `←`/`→` | Focus previous/next column | — |
| `Enter` | Open card: create-if-needed + attach to its tmux session | `loom card open <id>` |
| `K` | Kill the card's session and finalize its trace | `loom card close <id>` |
| `n` | New card (title/board/column/agent form) | `loom card add <title>` |
| `N` | New column | `loom column add <name>` |
| `m` | Move card (column picker); moving into a `done`-stage column auto-kills the session | `loom card move <id> <column>` |
| `d` | Card detail: metadata, resolved agent, codebase path, run history | `loom card show <id>` |
| `e` | Edit card fields | `loom card update <id>` |
| `/` | Search/filter cards by title or description | `loom card list --search <q>` |
| `s` | Switch board (within the current workspace) | `loom board show <name>` |
| `w` | Switch workspace | `loom workspace switch <name>` |
| `?` | Help overlay (renders this same table) | `loom help` |
| `q`, `Ctrl+c` | Quit — asks to confirm if any session is attached | — |
| `Q` | Force quit — sessions keep running detached | — |

Overlays (forms, card detail, help, the quit confirm) own every keypress
while open; `esc` (also `q` for detail/help) closes them without side
effects.

### Attaching and detaching

`Enter` ensures the card's tmux session exists and hands the terminal to it —
you're now talking directly to the agent. loom runs its own private tmux
server (`-L loom`, `prefix C-a`) so it never collides with your own tmux
config; detach with `Ctrl-a d` (the standard tmux detach binding) to return
to the board and leave the agent running. A detached session survives loom
exiting entirely — the next `loom` invocation rediscovers it and shows its
`●`/`◉` marker again.

### What updates live vs. on refresh

The board polls session status every 2s, so `●`/`◉` markers and the status
bar's running/attached counts update on their own. Card/column data itself
is not polled — mutations made by another `loom`/CLI process while the TUI is
open only appear after switching boards (`s`) or restarting `loom`.

## Configuration

`loom init` seeds `~/.config/loom/config.toml` (`$XDG_CONFIG_HOME/loom` on
Linux, the OS default config dir elsewhere) if it does not already exist;
`loom` runs with built-in defaults when the file is absent. `loom config`
prints the effective, fully-resolved config.

```toml
[agent]
default = "claude"          # "claude" | "opencode" — used when a card has no
                             # explicit agent (cards.agent NULL)

[agent.claude]
binary = "claude"           # resolved via $PATH, or an absolute path
model = ""                  # optional; maps to claude's --model

[agent.opencode]
binary = "opencode"
model = ""                  # optional; maps to opencode's -m/--model
opencode_agent = ""         # optional; maps to opencode's --agent
interface = "full"          # "full" (default, the standard opencode TUI) |
                             # "mini" (split-footer REPL)
auto_approve = false        # true -> passes --auto (approve permissions
                             # not explicitly denied)

[session]
tmux_server = "loom"        # tmux -L server name loom's sessions live on
prefix = "C-a"

[database]
path = "~/.config/loom/loom.db"
```

Per-card agent overrides live on the card itself (`--agent` on `card add` /
`card update`; `--agent=` with no value resets to NULL, i.e. "follow
`[agent] default`"). The TUI's `n`/`e` forms expose the same picker.

## Agents

| | claude | opencode |
|---|---|---|
| Launch | `claude '<prompt>'` [`--model <m>`] | `opencode --mini --prompt '<prompt>'` (or `--prompt` only for `interface = "full"`) [`--model`] [`--agent`] [`--auto`] |
| Completion | session ends when you quit the REPL | same — the mini/full-TUI stays alive until you quit |
| Permissions | claude's own | config/agent-driven; `--auto` or in-pane "ask" prompts while attached |

Both drivers share the same session lifecycle, completion detection (the tmux
session disappearing), and file-change tracing — see ADR-002 §3–§4 for the
driver contract and §16 / `docs/DESIGN-002-loom-multi-agent.md` §16 for the
verification matrix.

## Testing

```sh
go build ./...
go vet ./...
go test ./...
```

Integration tests in `internal/session` run against a real, isolated tmux
server (`-L loomselftest`) with stub `claude`/`opencode` scripts on `$PATH` —
no network or real agent binary required. One test is the exception:
`TestOpencodeFullTUIAutoSubmitCanary` makes a **live** opencode/LLM call to
guard against a future auto-submit regression (review finding F5); it is
skipped unless you opt in:

```sh
LOOM_TEST_LIVE_OPENCODE=1 go test ./internal/session/... -run TestOpencodeFullTUIAutoSubmitCanary -v
```

The manual E2E step (board → card → open with each agent → detach → reattach
→ trace on completion) is not automated — see `docs/ADR-001-loom-architecture.md`
§10 for the coverage-bar exception.

## Project layout

```
cmd/loom/           entry point
internal/config/    config.toml load/validate
internal/agent/     driver contract, prompt/escape, claude + opencode drivers
internal/store/     sqlite schema (goose migrations) + CRUD
internal/trace/     git-baseline reconciliation, fsnotify watcher, recorder
internal/session/   tmux wrapper + SessionManager (ensure/attach/kill/status)
internal/board/     orchestration seam: kanban ops + session actions
internal/cli/        stdlib-flag command router
internal/tui/        BubbleTea board, forms, detail view, search, help
docs/                ADRs, design doc, verification transcripts
```

## Docs

- [`docs/ADR-001-loom-architecture.md`](docs/ADR-001-loom-architecture.md) — base architecture (tmux session model, store schema, CLI/TUI surface).
- [`docs/ADR-002-loom-multi-agent.md`](docs/ADR-002-loom-multi-agent.md) — multi-agent support (opencode as a second driver). **Adopted.**
- [`docs/DESIGN-002-loom-multi-agent.md`](docs/DESIGN-002-loom-multi-agent.md) — the implementation blueprint ADR-002 hands off to.
- [`docs/PROBE-full-tui.md`](docs/PROBE-full-tui.md) — the T0 full-TUI auto-submit probe transcript.
- [`docs/TASKS-loom-v0.1.md`](docs/TASKS-loom-v0.1.md) — the v0.1 implementation backlog.
