# Loom: A CLI-Native Kanban Task Tracker with Claude Code Launch

**Status:** Research & Design Document — **superseded for implementation
details by [ADR-001](./ADR-001-loom-architecture.md)** as of the 2026-07-02
design review. This doc's schema, CLI surface, Claude-launch mechanics, and
package list have already drifted from ADR-001 once (see ADR-001's
Amendment Log) and are corrected there, not here. Read this doc for the
**rationale** (why Go over Rust, what Weave gets right/wrong, comparison
tables) — for anything you're about to implement against (exact SQL, exact
launch flag, exact CLI commands, exact dependency versions), use ADR-001.
**Date:** 2026-07-02
**Goal:** Design a standalone CLI tool that combines a Kanban board with Claude Code launch capability, inspired by Weave's architecture but rebuilt as a terminal-native, user-driven experience.

---

## 1. Problem Statement

### What Weave gets right
Weave (web-based multi-agent coordination platform) achieves something unique:

1. **The Kanban IS the task model** — the board organizes work visually with columns representing workflow stages.
2. **Multi-runtime abstraction** — `CodingAgent` trait abstracts execution backends.
3. **Rich prompt construction** — 10-section structured prompt with column-stage awareness, gate requirements, lane history, and handoff context.
4. **Single binary deployment** — one executable, SQLite backend, no reverse proxy or Node runtime.

### What Weave misses / why a CLI tool makes sense
1. **Web dependency** — Weave requires a browser. For terminal-first developers who live in tmux/zellij, a web UI is friction.
2. **Complexity overhead** — Rust/Axum backend + React/Tailwind frontend + 150 features. Heavy to adopt, modify, or extend.
3. **Browser-only interaction** — No TUI or CLI-driven workflow. You can't `weave create "fix the auth bug"` from a terminal.
4. **Automation complexity** — Automated subprocess mode with structured prompts, stream-json parsing, session management, and lifecycle watchdog adds significant surface area that many users don't need.
5. **Deployment surface** — Still a server process you must start/stop/monitor, not a disposable `just do the thing` CLI.

### What a CLI-native Kanban tool should do differently
| Concern | Weave approach | Loom approach |
|---------|---------------|---------------|
| **Interaction model** | Web UI (React/SSE) | Terminal (BubbleTea TUI + CLI commands) |
| **Agent backend** | Automated subprocess + structured prompts | User-driven tmux-session launch (attach/detach) |
| **Persistence** | SQLite via Axum REST API | SQLite with direct rusqlite access |
| **Kanban UI** | React + @dnd-kit horizontal scroll | Terminal columnar layout with keyboard navigation |
| **Startup** | `./weave-server --port 3000` then browser | `loom start` → TUI or `loom card add "task"` |
| **Extensibility** | Trait-based agent registry | Subprocess invocation |

---

## 2. Architecture Overview

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Terminal (BubbleTea TUI)                       │
│  Views: Kanban Board · Card Detail · Settings · Help             │
│  Components: Column · Card · StatusBar                           │
│  Keybindings: jk/hl · Enter (launch Claude) · m (move) · n (new)│
│              · d (detail) · / (search) · q (quit)               │
└─────────────────────────────┬───────────────────────────────────┘
                               │ Go channels / messages
┌─────────────────────────────┴───────────────────────────────────┐
│                    Application Core                               │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐   │
│  │ Board Service│  │SessionManager│  │   Trace Recorder     │   │
│  │ (kanban CRUD,│  │(tmux sessions,│  │ (fsnotify watcher,   │   │
│  │  card move)  │  │ attach,detach │  │  file_change events, │   │
│  │              │  │  status)      │  │  trace persistence)  │   │
│  └──────────────┘  └──────────────┘  └──────────────────────┘   │
│  ┌──────────────┐  ┌────────────────────────────────────────┐   │
│  │ Config Mgr   │  │ Codebase Manager (git repos, paths)    │   │
│  └──────────────┘  └────────────────────────────────────────┘   │
└─────────────────────────────┬───────────────────────────────────┘
                               │
┌─────────────────────────────┴───────────────────────────────────┐
│                    SQLite Persistence                             │
│  Tables: workspaces, boards, columns, cards, codebases,          │
│          traces, artifacts, notes                                │
└──────────────────────────────────────────────────────────────────┘
```

### Technology Choices

| Layer | Technology | Rationale |
|-------|-----------|-----------|
| **Language** | Go | Terminal UI ecosystem (BubbleTea, Lip Gloss, Glamour) is Go-native; fast compilation; excellent subprocess management; minimal runtime |
| **TUI Framework** | [BubbleTea](https://github.com/charmbracelet/bubbletea) + [Bubbles](https://github.com/charmbracelet/bubbles) | Elm Architecture for terminals; viewport, textinput, paginator, spinner, viewport components available |
| **Terminal** | [bubbletea]'s `tea.Program` with `tea.ExecProcess` | Attach/detach handoff to per-card tmux sessions (full-terminal, lazygit pattern) |
| **Database** | SQLite via `modernc.org/sqlite` (pure Go, no CGO) | Single binary, zero config |
| **Subprocess** | `os/exec` with `context.Context` for cancellation | Standard, well-understood |
| **Config** | TOML file `~/.config/loom/config.toml` | Human-editable, widely used in CLI tools |
| **Styling** | [Lip Gloss](https://github.com/charmbracelet/lipgloss) | CSS-like styling for terminal UIs |
| **Markdown** | [Glamour](https://github.com/charmbracelet/glamour) | Render markdown content in card detail |
| **File watching** | `fsnotify` | Trace file changes during Claude Code sessions |
| **Session / PTY** | tmux (external runtime dep) | Detached per-card agent sessions; attach/detach keeps the human in the loop; tmux owns the PTY and child supervision |

### Why Go over Rust for this tool?
1. **BubbleTea is the best terminal UI framework in any language** — Elm Architecture, mature ecosystem, Charmbracelet tooling. Rust's `ratatui` is similar but the Charm ecosystem is deeper.
2. **Subprocess management** — Go's `os/exec` + `context.Context` is simpler for cancel/timeout than tokio's process model.
3. **Compilation speed** — Go compiles in seconds vs Rust's minutes. Fast iteration on a TUI is critical.
4. **Binary size** — Go binaries are ~8-15MB vs Rust's ~5-15MB. Neither is a meaningful difference.
5. **SQLite** — `modernc.org/sqlite` is a pure-Go SQLite implementation (no CGO needed), producing truly static binaries.
6. **Weave is already Rust** — building loom in Go creates a complementary tool, not a competitor that shares the same code/ecosystem.

---

## 3. Data Model

### Core Tables (8 tables)

```sql
-- Workspace (project context)
CREATE TABLE workspaces (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    root_path TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    archived_at TEXT
);

-- Boards (kanban boards within a workspace)
CREATE TABLE boards (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Columns (kanban lanes — simplified: no automation, no specialist binding)
CREATE TABLE columns (
    id TEXT PRIMARY KEY,
    board_id TEXT NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    stage TEXT NOT NULL DEFAULT 'dev',    -- backlog | todo | dev | review | done
    position INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Cards (kanban tasks — simplified: no session binding, no provider, no evidence)
CREATE TABLE cards (
    id TEXT PRIMARY KEY,
    column_id TEXT NOT NULL REFERENCES columns(id) ON DELETE CASCADE,
    board_id TEXT NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT,
    objective TEXT,
    acceptance_criteria TEXT,
    verification_commands TEXT,
    test_cases TEXT,
    priority TEXT DEFAULT 'medium',
    labels TEXT,
    position INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Codebases (registered git repositories)
CREATE TABLE codebases (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    label TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(workspace_id, path)
);

-- Traces (file change events during Claude sessions)
CREATE TABLE traces (
    id TEXT PRIMARY KEY,
    card_id TEXT REFERENCES cards(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,   -- trace_start | file_change | trace_end
    data_json TEXT NOT NULL,    -- {"path": "...", "operation": "created|modified|deleted"}
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Artifacts (user-managed evidence)
CREATE TABLE artifacts (
    id TEXT PRIMARY KEY,
    card_id TEXT REFERENCES cards(id) ON DELETE CASCADE,
    artifact_type TEXT NOT NULL,
    name TEXT NOT NULL,
    content TEXT NOT NULL,
    mime_type TEXT DEFAULT 'text/plain',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Notes (persistent user notes)
CREATE TABLE notes (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    note_type TEXT DEFAULT 'general',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
```

---

## 4. CLI Command Design

### Command hierarchy

```
loom                                    # Launch TUI (default)
loom init                               # Initialize loom in current directory
loom config                             # Show/edit configuration
loom workspace                          # Workspace management
  loom workspace list
  loom workspace create <name>
  loom workspace switch <name>
  loom workspace codebase add <path>
  loom workspace codebase list
loom board                              # Board management
  loom board list
  loom board create <name>
  loom board create <name> --template standard|empty
  loom board show <name>
  loom board delete <name>
loom card                               # Card management
  loom card add <title> [--description <text>] [--board <name>] [--column <name>]
  loom card list [--board <name>] [--column <name>] [--filter <status>]
  loom card show <id>
  loom card move <id> <column>
  loom card open <id>                   # Launch Claude Code for card (CLI, no TUI)
  loom card update <id> [--title <text>] [--description <text>] [--priority <p>]
  loom card delete <id>
loom column                             # Column management
  loom column add <name> [--board <name>] [--stage <stage>]
  loom column list [--board <name>]
loom status                             # Show overall status (board summary, recent traces)
loom version                            # Show version
loom help                               # Show help
```

### Key CLI design principles
1. **Non-TUI commands work without a terminal** — `loom card add`, `loom card move`, `loom card open` are pure CLI tools usable in scripts.
2. **TUI is launched without subcommands** — `loom` alone launches the interactive TUI.
3. **CLI commands mirror the TUI actions** — everything you can do in the TUI you can do with CLI commands, enabling CI/CD integration.

---

## 5. TUI Design

### Screen layout

```
┌─ Loom · my-project ─────────────────────── [?] [Q] ──────────┐
│ ┌─ Board: Feature Pipeline ── [ + Card ] [ + Col ] ────────┐ │
│ │ ┌─Backlog──┐ ┌─To Do─────┐ ┌─In Prog──┐ ┌─Review───┐ ┌─Done─┐│ │
│ │ │▸ Fix auth│ │▸ Add tests│ │▸ API v2  │ │▸ Refactor│ │▸ CI  ││ │
│ │ │  high    │ │  medium   │ │  high    │ │  low     │ │  ✓   ││ │
│ │ │          │ │           │ │          │ │          │ │      ││ │
│ │ │▸ Cache   │ │▸ Docs     │ │          │ │          │ │      ││ │
│ │ │  medium  │ │  low      │ │          │ │          │ │      ││ │
│ │ │          │ │           │ │          │ │          │ │      ││ │
│ │ │[+ Card ] │ │[+ Card ]  │ │[+ Card ] │ │[+ Card ] │ │[+C ] ││ │
│ │ └──────────┘ └───────────┘ └──────────┘ └──────────┘ └──────┘│ │
│ └──────────────────────────────────────────────────────────────┘ │
│ ┌─ Status: 5 cards across 2 boards │ Workspace: my-project ────┐ │
│ └──────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────┘

Keybindings:
  j/k or ↓/↑    Navigate cards
  h/l or ←/→    Navigate columns
  Enter          Launch Claude Code for selected card
  Enter          Open card: create (if needed) + attach to its tmux session
  K              Kill selected card's session + finalize its trace
  n              New card
  N              New column
  m              Move card (then select target column)
  /              Search/filter cards
  s              Switch board
  w              Switch workspace
  ?              Help
  q              Quit
  Q              Force quit
Ctrl+c           Quit
```

Session indicators: `●` = session running (agent live), `◉` = someone
attached, blank = idle.

### Card detail view

```
┌─ Card: Fix auth bug (#a3f2) ───────────────────────────────────┐
│                                                                 │
│  Status: In Progress     Priority: high                         │
│  Board: Feature Pipeline                                         │
│  Codebase: /home/user/project                                   │
│                                                                 │
│  ─── Description ──────────────────────────────────────────    │
│  Users are reporting auth failures when tokens expire           │
│  during long sessions. The refresh flow in auth.ts              │
│  doesn't handle the 401 + retry correctly.                      │
│                                                                 │
│  ─── Acceptance Criteria ──────────────────────────────────    │
│  ☐ Token refresh retries up to 3 times                         │
│  ☐ Fallback to login on persistent failure                     │
│  ☐ Unit tests for refresh flow                                 │
│                                                                 │
│  ─── Files Changed (last session) ─────────────────────────    │
│  M  src/auth.ts                                                 │
│  M  src/auth.test.ts                                            │
│                                                                 │
│  ─── Actions ──────────────────────────────────────────────    │
│  [Enter] Launch Claude   [m] Move   [e] Edit   [d] Delete      │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### Claude Code launch flow

When the user presses Enter on a card from the board view:

1. Loom reads the card's title, description, and acceptance criteria
2. SessionManager.ensure(card) — create (or reuse) the detached `-L loom` tmux session running `claude` with the card context as a POSIX single-quoted positional prompt; record `trace_start` and start an `fsnotify` watcher
3. Loom calls `tea.ExecProcess("tmux", ["-L","loom","attach-session","-t","loom-<id>"])` — the terminal is handed to the session, showing Claude Code's native TUI (thinking, diffs, permission dialogs, tool calls)
4. The user watches/steers/answers prompts, then detaches (`prefix-d`) — back at the board, agent still running; or the agent finishes and the session ends
5. When the session disappears, loom's 2s poll stops the watcher, reconciles `git diff` vs the baseline, records `file_change` events, and records `trace_end`
6. The user is back at the kanban board (or reattaches later via Enter)

---

## 6. Claude Code Launch Design

```
1. User presses Enter on a card in the board view (or `loom card open <id>`)
2. loom reads card from DB, gets workspace root_path
3. SessionManager.ensure(card):
   - if session `loom-<id>` exists → reuse it
   - else `tmux -L loom new-session -d -s loom-<id> -c <root>
        "claude 'Card: {title}\n\nDescription: … Acceptance Criteria: …'"`
     (POSIX single-quoted positional prompt — never -p/--print)
4. loom records trace_start (with git baseline) and starts the fsnotify watcher
5. loom calls tea.ExecProcess("tmux", ["-L","loom","attach-session","-t","loom-<id>"])
   → the terminal shows Claude Code's native TUI
6. The user watches/steers/answers prompts, then detaches (`prefix-d`)
   → back at the board; the agent keeps running in its session
7. Claude exits (or the user kills it) → the session disappears
8. loom's 2s poll detects the session is gone → stops fsnotify, reconciles
   `git diff` vs the baseline, records file_change events, records trace_end
```
10. loom records trace_end event
11. User is returned to the kanban board
```
- **tmux sessions, not a direct child process** — Each card is its own
  detached `-L loom` tmux session whose command is `claude`. tmux owns the PTY
  and supervision, so loom never parents `claude` and has no cleanup to do; a
  session ends exactly when claude exits, which is how completion is detected.
  No ANSI stripping, no SIGWINCH forwarding, no PID tracking.
- **Human in the loop by attach/detach** — `tea.ExecProcess("tmux attach")` is
  still the lazygit-style full-terminal handoff, but the human is now
  *optionally* in the loop: attach to watch/steer/interrupt, detach and let the
  agent continue, reattach later — even after loom itself has exited.
- **Concurrent cards** — N cards, N sessions, no cap. Enter on a running card
  just reattaches.
- **Card context as initial prompt** — Passed as a POSIX single-quoted
  positional argument so the session command is `claude '<card context>'`.
  Claude Code manages its own `--resume`; loom keeps the tmux session (not the
  conversation) alive across attach/detach.
- **File tracing: fsnotify + git reconciliation** — fsnotify gives live
  granularity while a loom process runs; a git baseline (`HEAD` +
  `git status --porcelain`) captured at trace_start lets a session that
  outlived loom still attribute its file changes on completion.
- **No output parsing** — Claude Code's output is shown as-is in the
  terminal. Loom only records filesystem side effects.
- **No session persistence** — Claude Code manages its own `--resume` internally. Loom doesn't track sessions, messages, or conversation history.
- **File watching via fsnotify** — Detects file changes during the Claude session. Records them as trace events for the card. The card detail view shows "Files Changed" from this history.
- **No output parsing** — Claude Code's output is displayed as-is in the terminal. Loom only captures file system side effects.

---

## 7. File System Layout

```
~/.config/loom/
├── config.toml              # Global configuration
└── loom.db                  # SQLite database

Project directory (optional):
.loom/
├── config.toml              # Project-specific overrides
└── .loomignore              # Patterns to exclude from file watching
```

### Sample config.toml

```toml
[claude]
binary = "claude"           # Path to Claude Code binary
model = ""                  # Default model (empty = Claude's default)

[database]
path = "~/.config/loom/loom.db"

[ui]
default_board = ""          # Board to show on startup (empty = last used)
```

---

## 8. Implementation Plan

### Phase 0: tmux Feasibility Test (0.5 day)
Minimal BubbleTea app with a hardcoded card list. Pressing Enter creates a detached `-L loom` session running a stub `claude`, then `tea.ExecProcess("tmux", ["-L","loom","attach-session","-t",...])`. Validates handoff/handback, session persistence after loom quits, and completion detection.

### Phase 1: Scaffolding + Data Model (2-3 days)
- Go module (`go mod init loom`), BubbleTea v2, Bubbles, LipGloss, Glamour
- SQLite schema (8 tables) + goose migration system with `embed.FS`
- Store layer: Workspace, Board, Column, Card CRUD with parameterized queries
- CLI command scaffolding: `loom init`, `loom workspace create`, `loom board create`, `loom card add`

### Phase 2: Kanban Board TUI + Claude Launch (5-7 days)
- 5-column board layout via `lipgloss.JoinHorizontal` (follow kancli pattern)
- Each column is a `bubbles/list` with custom delegate for card rendering
- Card shows: title (bold), priority (colored), labels
- Keyboard navigation: `j/k` focus cards, `h/l` focus columns
- **Enter** on card → create-or-attach the card's tmux session (`tea.ExecProcess("tmux", ["-L","loom","attach-session","-t",name])`), card context as positional prompt
- **K** → kill the card's session and finalize its trace
- **d** → card detail view (description, acceptance criteria, trace history)
- Card creation, deletion, column movement (`m` key + column picker)
- File watching via `fsnotify` → trace events in DB
- Trace view in card detail: "Files Changed" list
- Search/filter with `/` key

### Phase 3: Polish + CLI (2-3 days)
- `loom card open <id>` CLI command (non-TUI launch)
- Config file (`~/.config/loom/config.toml`): claude binary path
- Status bar, workspace switching
- Notes system (user-created planning notes)
- Card detail editing (title, description, acceptance criteria)

**Total: ~10-14 days** (down from ~25-35 days in original plan)

---

## 9. Key Design Decisions (with Weave Context)

### What to adopt from Weave

| Decision | Why |
|----------|-----|
| **Kanban as task tracking model** | The core insight. Cards represent work items organized in columns. |
| **Workspace-scoped resources** | All data belongs to a workspace. |
| **Single binary deployment** | Zero external dependencies. |

### What to do differently from Weave

| Decision | Why |
|----------|-----|
| **Go instead of Rust** | BubbleTea ecosystem is the best terminal UI framework. Faster iteration. |
| **No web server** | TUI eliminates the need for HTTP, SSE, React, Vite, CORS, build.rs. |
| **No automated agents** | The user drives the workflow. No prompt injection, no auto-trigger, no lifecycle supervision. |
| **tmux sessions for Claude launch** | Detached per-card sessions; attach/detach keeps the human in the loop; full-terminal handoff preserves Claude Code's native TUI. No output parsing needed. |
| **No agent-facing tools** | No move_card, update_card, or other kanban tools. The user moves cards manually. |
| **No A2A protocol** | Out of scope. CLI-first, single-user. |
| **No browser UI** | Terminal is the only interface. |
| **Simpler data model** | 8 tables instead of 13. No sessions, messages, providers, specialists, watchdog. |

### What Weave got wrong / forced by web architecture

| Weave pain point | Root cause | Loom fix |
|-----------------|------------|----------|
| CLI stream-json parsing fragility | Output must be normalized into `StreamEvent` for web SSE | No parsing — the agent's native TUI is shown as-is |
| N+1 session lookups in kanban prompt | SQL queries per peer in lane-history loop | No sessions, no lane history |
| Frontend state-sync bugs | React/TanStack complexity | BubbleTea Elm Architecture (single state tree) |
| Silent-deadlock at kanban auto-spawn | Complex mode/runtime compatibility matrix | No auto-spawn — user launches manually |
| Web dependency | Requires browser | Terminal-native |

---

## 10. Risks and Mitigations

| Risk | Severity | Mitigation |
|------|----------|------------|
| **Process group cleanup** | Medium | Use `cmd.SysProcAttr.Setpgid=true` then `syscall.Kill(-pid, SIGTERM)` + 5s timeout + SIGKILL |
| **SQLite concurrent access** | Low | WAL mode + single writer; `_busy_timeout=5000` in DSN |
| **Binary size (~15MB with pure Go SQLite)** | Low | Acceptable for CLI tool; pure Go is worth the size for portability |
| **BubbleTea learning curve** | Low | Charm ecosystem has excellent tutorials; kancli is a direct reference |
| **Lost context between Claude sessions** | Medium | Claude Code manages its own `--resume`. Loom passes card details as initial prompt but doesn't track session state. |

---

## 11. Verification Strategy

| Layer | What to test | How |
|-------|-------------|-----|
| **Unit** | Store CRUD, CLI commands | Go table-driven tests (`go test`) |
| **Integration** | Card open → session create/attach → completion → trace | Scripted test against a stub `claude` shell script + real `tmux` (asserts `-L loom` session name `loom-<id>`, cwd, quoted prompt in the pane, session-end → `trace_end`, kill path) plus manual verification with real Claude Code |
| **TUI** | Keyboard navigation, column layout, card movement | BubbleTea test framework (`tea.NewProgram` with test model) |
| **E2E** | Full flow: create board → add card → Enter → Claude → file changes → trace → return | Manual with real Claude Code |

---

## 12. Resolved Design Questions

1. **File watching scope**: Watch the workspace root_path. All file changes during a Claude session are recorded as trace events for the card; a git baseline captured at trace_start reconciles changes made while no loom process was alive.
2. **Card context to Claude**: Pass card title, description, and acceptance criteria as a POSIX single-quoted **positional** prompt argument in the tmux session command (never `-p`/`--print` — see ADR-001 §4).
3. **Multiple cards simultaneously**: Supported from v0.1 — each card is its own detached `-L loom` tmux session; no concurrency cap. Enter on a running card reattaches.
4. **Trace value**: Cards accumulate `file_change` events for every file modified during Claude sessions. Card detail shows "Files Changed" list. Useful for review.
5. **Keybinding**: Enter on card = open the card's tmux session (create + attach). `K` = kill session + finalize trace. 'd' = card detail.

---

## 13. Key Go Packages

```go
// go.mod
require (
    github.com/charmbracelet/bubbletea v2.0.7          // TUI framework — pinned to v2 per ADR-001 §2.2 (this doc previously listed v0.26.0; v2 is authoritative)
    github.com/charmbracelet/bubbles v0.18.0        // Widgets (viewport, list, spinner, textinput)
    github.com/charmbracelet/lipgloss v0.11.0       // Styling
    github.com/charmbracelet/glamour v0.7.0         // Markdown rendering
    modernc.org/sqlite v1.33.0                      // Pure-Go SQLite
    github.com/pressly/goose/v3 v3.21.0             // DB migrations
    github.com/fsnotify/fsnotify v1.7.0             // File watching
)

No new Go dependencies for the session layer: tmux is invoked as an external
binary, and prompt quoting is a ~5-line POSIX single-quote escaper (no
`shlex` dependency).
```

### Removed from original

| Package | Removed because |
|---------|-----------------|
| `creack/pty` | No inline PTY needed |
| `google/uuid` | Card IDs can be simpler (no session IDs) — see ADR-001 §3.3 for the resolved ID-generation strategy (`crypto/rand`, no dependency) |
| `acarl005/stripansi` | No ANSI stripping needed |

---

## 14. References

- **Weave**: `/mnt/data/works/weave` — multi-agent coordination platform (Rust, web-based)
- **BubbleTea**: https://github.com/charmbracelet/bubbletea (43.5k stars, v2.0.7)
- **kancli**: https://github.com/charm-and-friends/kancli (223 stars, canonical BubbleTea Kanban reference)
- **k9s**: https://github.com/derailed/k9s (34k stars, proven multi-pane TUI in tview)
- **lazygit**: https://github.com/jesseduffield/lazygit (80k stars, ExecProcess pattern)
- **modernc.org/sqlite**: Pure-Go SQLite (no CGO, ~90% C speed)
- **pressly/goose**: SQLite migration library
- **tmux**: https://github.com/tmux/tmux (v3.6) — detached per-card sessions, attach/detach, `-L` socket
- **Charm ecosystem**: https://charm.sh/ (BubbleTea, LipGloss, Bubbles, Glamour, Huh)
