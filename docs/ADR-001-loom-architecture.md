# ADR-001: Loom — Architecture for a CLI-Native Kanban Task Tracker with Claude Code Launch

**Status:** Adopted
**Date:** 2026-07-02
**Author:** MiMo Agent
**Area:** Architecture, Terminal UI

---

## Amendment Log

| Date | Change |
|------|--------|
| 2026-07-02 | Pre-implementation design review. Corrected the Claude Code launch flag (`-p` would have broken interactive handoff — verified against the installed `claude` CLI), pinned a single BubbleTea version (resolving a mismatch with the research doc), specified card ID generation and position/reorder strategy, added a `CHECK` constraint to `columns.stage`, documented `traces.data_json` shapes for all event types, clarified `cards.board_id`/`workspace_id` denormalization and enforced move semantics, wired `.loomignore` and default ignore patterns into the fsnotify design, scoped the process-cleanup risk to when it actually applies, added `loom note` CLI commands for parity, and scoped the verification strategy's manual-testing exception explicitly. This ADR (not `RESEARCH-loom-detailed-design.md`) is the canonical source for implementation details going forward — see the note at the top of the research doc. |
| 2026-08-07 | Superseded the "ExecProcess exclusively, tmux rejected" decision (§2.3) with a **tmux session model**: each card's agent runs as the command of a detached tmux session on a dedicated `-L loom` server; the human is in the loop by attaching (`tea.ExecProcess` → `tmux attach-session`) and detaching at will; N concurrent sessions (one per card); completion is detected by session disappearance, so loom never parents `claude`. The card prompt is passed as a POSIX single-quoted positional argument (mechanics verified against tmux 3.6 — notably `:` is forbidden in session names, and the server auto-exits when its last session ends). File traces gain a git-snapshot reconciliation so sessions that outlive loom are still attributed. CLI adds `loom card open <id> [--detach]`, `loom card close <id>`, `loom attach`, `loom sessions`. tmux moved into v0.1 (was a v0.2 future item); §8 adds tmux / nested-tmux / orphan-session risks. |
| 2026-08-07 | Full-doc consistency review. Fixed internal contradictions: `trace_start` `data_json` now documents the git baseline it actually stores (§3.3) instead of `{}`; removed duplicated §8 risk rows; added a canonical keybinding table (§3.5) so the TUI, CLI, and diagrams agree. Resolved schema gaps: `traces` gains a `run_id` (per open→complete cycle) so "Files Changed (last session)" is computable and `trace_end.files_changed` counts unique paths; `traces.card_id` is now `NOT NULL`; `cards` gains a nullable `codebase_id` (session cwd + watch scope, §4.6) so the detail view's "Codebase" field is real; `cards.status` was **dropped** (column stage is the single workflow source of truth; archival is out of scope) and `priority` gained a `low|medium|high` CHECK; `labels` documented as comma-separated. Fixed the git-reconciliation algorithm (§5): porcelain-snapshot diff (captures untracked files) + `git diff` between heads if HEAD moved, deduped against live fsnotify events. Defined previously-missing behavior: `loom init` semantics + default board/columns, current workspace/board persistence (`[ui] last_workspace/last_board`), a full config schema (§5), artifact CLI parity (`loom artifact ...`), expanded card-add flags (objective/AC/verification/tests/labels/codebase), and prompt construction now includes objective + optional verification/test sections (§4.5). Challenged and amended the "single binary" principle (§1.3) to name tmux/claude as the only external runtime deps with a tracked tmux-less fallback (§9), and documented the real per-session cost of "no concurrency cap" (§4.2). |

---

## 1. Context

### 1.1 The Problem

Weave is a web-based multi-agent coordination platform with a React+Svelte frontend and a Rust (Axum + SQLite) backend. It has a deep model: Kanban lanes auto-trigger Claude Code sessions, agents receive structured prompts, and bidirectional kanban tools let agents talk back to the board.

But Weave has a fundamental UX gap for terminal-native developers: **you need a browser.** The `weave-server` process runs on localhost:3000 and requires a web browser for full interaction. There is no TUI, no `weave do "fix the auth bug"` CLI command.

A CLI-native tool would serve developers who live in tmux/zellij and want a Kanban board in their terminal that can launch Claude Code to work on a task, track what files changed, and manage task flow — all without leaving the terminal.

### 1.2 Research Sources

- **Weave source** (`/mnt/data/works/weave`): Full Kanban-agent coordination model with 150+ features
- **BubbleTea** (43.5k stars): The leading Go TUI framework, Elm Architecture, cell-based renderer
- **kancli** (charm-and-friends/kancli, 223 stars): Reference BubbleTea Kanban implementation in ~400 lines
- **k9s** (34k stars): Proven multi-pane TUI in tview with shell-in-pod terminal support
- **lazygit** (80k stars): Proven `ExecProcess` pattern for shelling out to editor/CLI from a TUI
- **gh-dash** (12k stars): Columnar PR/issue views in BubbleTea
- **Claude share** (ADE architecture review): Theia vs Tauri+Monaco vs Code-OSS analysis

---

### 1.3 Guiding Principles

| Principle | Description |
|-----------|-------------|
| **Terminal-native** | The primary interface is a TUI. CLI commands must work without a terminal for scripting. |
| **Single binary** | One executable, SQLite backend, no CORS, no reverse proxy, no Node runtime. External runtime deps are limited to `tmux` (session/PTY layer) and `claude` (the agent) — both are user-owned tools, not loom code; a tmux-less direct-PTY fallback is tracked in §9. |
| **Kanban as task tracking** | The board organizes tasks visually. Opening a card = launching Claude Code in your terminal. |
| **Progressive complexity** | v0.1 does one thing well: a Kanban board that launches Claude Code when you press Enter. |
| **Go for TUI** | Go + BubbleTea for the terminal tier. |
| **Simplicity over automation** | The user drives the workflow. No automated agents, no prompt injection, no lifecycle supervision. |

---

## 2. Decision

### 2.1 Primary Decision

**Build loom in Go using the BubbleTea framework**, with the Charm ecosystem (LipGloss, Bubbles, Glamour) for the Kanban TUI, a **dedicated tmux server** (`-L loom`) providing one detached session per card for the agent to run in, and `tea.ExecProcess` for the attach/detach handoff to those sessions.

**Rejected Alternatives:**

| Alternative | Reason for Rejection |
|---|---|
| **Rust + Ratatui** | Ratatui (21.3k stars) is a strong alternative but lacks the Charm ecosystem depth (Bubbles widgets, Glamour markdown, kancli reference). Compile-debug cycle is 10x slower. |
| **Python + Textual** | Textual has the best layout engine (CSS Grid) and a mature widget library. Requires Python runtime — cannot produce a single binary. |
| **Extending Weave (Rust)** | Would require adding a TUI to an existing web app. Weave is 900+ tests, 150+ features, Rust/Axum/React — heavy to modify. Building Go-native is lighter and independent. |
| **tmux + shell scripts** | Too limited for complex state management (SQLite, multi-card tracking, file trace recording). |

### 2.2 Technology Stack

| Layer | Technology | Version | Rationale |
|---|---|---|---|
| **Language** | Go | 1.23+ | Fast compilation, excellent subprocess mgmt, BubbleTea ecosystem |
| **TUI Framework** | BubbleTea v2 | v2.0.7 | Elm Architecture, 43.5k stars, 21k dependents, cell-based partial renderer |
| **TUI Widgets** | Bubbles | v0.18.0 | Viewport, list, textinput, spinner, paginator, table |
| **Styling** | Lip Gloss | v0.11.0 | CSS-like terminal styling, `JoinHorizontal`, `Width` |
| **Markdown Rendering** | Glamour | v0.7.0 | Render markdown content in card detail view |
| **Database** | modernc.org/sqlite | v1.33.0 | Pure-Go SQLite, no CGO, WAL mode, ~90% C speed |
| **Migrations** | pressly/goose v3 | v3.21.0 | Embed SQL via `embed.FS`, simple migration runner |
| **Keyboard Input** | BubbleTea built-in | — | `key.Matcher` patterns for modal keybinding |
| **File Watching** | fsnotify | v1.7.0 | Trace file changes during Claude Code sessions |
| **Session / PTY** | tmux (external runtime dep) | 3.x | Dedicated `-L loom` server: one detached session per card; owns the PTY, signals, and child cleanup so loom never parents `claude`; attach/detach handoff |
| **Process Management** | Go stdlib | — | `os/exec` for the short-lived `tmux` client and session commands; `context.Context` for cancellation; no direct `claude` child |

### 2.3 Terminal Launch Decision

**Each card maps to a persistent tmux session; the human is in the loop by attaching and detaching.** When the user presses Enter on a card (or runs `loom card open <id>`), loom creates or reuses a **detached tmux session** running Claude Code with the card's context as its initial prompt, then hands the terminal to that session via `tea.ExecProcess("tmux", ["-L","loom","attach-session","-t","loom-<id>"])`. The user can watch, steer, answer permission prompts, or interrupt the agent, then detach (`prefix d`) — Claude Code keeps running in the background session while the board stays usable. Multiple cards run concurrently, one session each.

| | `tea.ExecProcess` pop-over | tmux session model (chosen) |
|---|---|---|
| Human in the loop | Forced — the terminal is held for the session's whole duration | Optional — attach to steer, detach and let it run |
| Session persistence | Dies with loom | Survives loom exiting; the tmux server owns the process |
| Concurrency | One foreground process at a time | N sessions, one per card, near-zero loom-side cost |
| Supervision | Loom parents `claude` and must handle cleanup | tmux owns the PTY, signal routing, and child cleanup |

`tea.ExecProcess` is **retained**, but its child is now the tmux attach client rather than `claude` itself — the full-terminal handoff/handback still behaves exactly like the proven lazygit pattern. **Direct inline PTY remains rejected** (ANSI stripping, SIGWINCH forwarding, marginal benefit).

Rejected within this decision:
- **`tmux switch-client` when loom runs inside tmux** — it only switches between sessions of a single server, and loom's sessions live on a separate `-L loom` server, so it cannot reach them. loom attaches instead; nested tmux renders fine, and the loom server remaps its prefix to `C-a` so nested keybindings don't collide (§4.4).
- **Board-as-tmux-layout** (a pane/window per column or per agent inside the user's own tmux session) — intrusive and zellij-incompatible; deferred to §9.

---

## 3. Architecture

### 3.1 Layer Diagram

```
┌──────────────────────────────────────────────────────────────────┐
│                     Terminal (BubbleTea TUI)                       │
│  Models: Board · CardDetail · Settings · Help                     │
│  Components: Column · Card · StatusBar                            │
│  Keybindings (canonical, §3.5):                                   │
│    j/k ↓/↑ cards · h/l ←/→ columns · Enter open/attach ·          │
│    m move · n new card · N new column · K kill session ·          │
│    d detail · e edit · / search · s board · w workspace ·         │
│    ? help · q quit · Q force quit · Ctrl+c quit                   │
└──────────────────────────────┬───────────────────────────────────┘
                               │ tea.Msg (Go channels)
┌──────────────────────────────┴───────────────────────────────────┐
│                      Application Core                              │
│  ┌──────────────────┐  ┌──────────────────┐  ┌────────────────┐  │
│  │   BoardService   │  │  SessionManager │  │  TraceRecorder │  │
│  │  (kanban CRUD,   │  │  (tmux ensure /  │  │  (fsnotify →    │  │
│  │   card movement) │  │   attach / kill │  │   file_change   │  │
│  │                  │  │   status)       │  │   events)      │  │
│  └──────────────────┘  └──────────────────┘  └────────────────┘  │
└──────────────────────────────┬───────────────────────────────────┘
                               │
┌──────────────────────────────┴───────────────────────────────────┐
│          SQLite Store (modernc.org/sqlite)                        │
│  Tables: workspaces, boards, columns, cards, codebases,          │
│          traces, artifacts, notes  (8 total)                      │
│  Migration: pressly/goose with embed.FS                           │
└──────────────────────────────────────────────────────────────────┘
```

### 3.2 Data Flow — User Opens Card → Claude Code (tmux session model)

```
User presses Enter on a card (or `loom card open <id>`)
         │
         ▼
  ┌──────┴──────────────────────────────────┐
  │  SessionManager.ensure(card)             │
  │   session `loom-<id>` exists? → reuse    │
  │   else create detached (cwd = card's     │
  │   codebase path or workspace root):      │
  │     tmux -L loom new-session -d          │
  │       -s loom-<id> -c <root>             │
  │       "claude '<card context>'"          │
  │   new run_id; trace_start (git baseline) │
  │   start fsnotify watcher (goroutine)     │
  └──────┬──────────────────────────────────┘
         │
         ▼
  ┌──────┴──────────────────────────────────┐
  │  Attach:                                 │
  │  tea.ExecProcess("tmux",                │
  │    ["-L","loom","attach-session",       │
  │     "-t","loom-<id>"])                  │
  │  Terminal → tmux client → claude TUI.   │
  │  User watches/steers, answers prompts.  │
  └──────┬──────────────────────────────────┘
         │ (prefix-d) detach → back at board; session keeps running
         │ (claude exits) → session ends → attach returns
         ▼
  ┌──────┴──────────────────────────────────┐
  │  2s poll loop:                           │
  │   tmux -L loom list-sessions -F         │
  │     '#{session_name} #{session_attached}'│
  │   → per-card ● running / ◉ attached     │
  │   → when `loom-<id>` disappears:        │
  │     stop watcher, git-reconcile vs      │
  │     baseline, file_change (dedup),      │
  │     trace_end for the run_id            │
  └─────────────────────────────────────────┘
```

### 3.3 Data Schema (8 tables)

**ID generation:** All `TEXT PRIMARY KEY` IDs are generated in-process as 16
random bytes from `crypto/rand`, hex-encoded (32 hex chars). `traces.run_id`
uses the same generator (also 32 hex chars). This avoids pulling in
`google/uuid` (or any dependency) while keeping IDs collision-safe and
opaque; no time-ordering property is needed since every table already has its
own `created_at`/`position` for ordering.

```sql
-- Workspaces
CREATE TABLE workspaces (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    root_path TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    archived_at TEXT
);

-- Boards
CREATE TABLE boards (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Columns
CREATE TABLE columns (
    id TEXT PRIMARY KEY,
    board_id TEXT NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    stage TEXT NOT NULL DEFAULT 'dev'
        CHECK (stage IN ('backlog', 'todo', 'dev', 'review', 'done')),
    position INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Cards
-- board_id and workspace_id are denormalized off column_id to avoid a join
-- on every "list cards in board/workspace" query. The store layer's card-move
-- function is the only writer of column_id and MUST keep board_id in sync;
-- moving a card to a column in a different board is rejected at the store
-- layer (v0.1 has no cross-board move — see §6 CLI surface).
-- codebase_id optionally binds a card to a registered codebase; when set it
-- selects the session's cwd and the fsnotify watch scope (§4.6). There is no
-- `status` column: workflow state is expressed by the card's column (stage);
-- removal is an explicit delete, archival is out of scope for v0.1.
CREATE TABLE cards (
    id TEXT PRIMARY KEY,
    column_id TEXT NOT NULL REFERENCES columns(id) ON DELETE CASCADE,
    board_id TEXT NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    codebase_id TEXT REFERENCES codebases(id) ON DELETE SET NULL,
    title TEXT NOT NULL,
    description TEXT,
    objective TEXT,
    acceptance_criteria TEXT,
    verification_commands TEXT,
    test_cases TEXT,
    priority TEXT NOT NULL DEFAULT 'medium'
        CHECK (priority IN ('low', 'medium', 'high')),
    labels TEXT,                -- comma-separated, e.g. "frontend, auth, urgent"
    position INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Codebases (registered project directories)
CREATE TABLE codebases (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    label TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(workspace_id, path)
);

-- Traces (file change events during Claude sessions)
-- A "run" is one open→complete cycle of a card's session: trace_start opens a
-- run, file_change/trace_end events for that run share its run_id. run_id
-- (16 hex bytes, same generator as table IDs) lets the card-detail view
-- compute "Files Changed (last session)" and per-run duration even when a card
-- is opened many times over its life.
-- data_json shapes by event_type:
--   trace_start:  {"git": {"base_head": "<40-hex sha>", "porcelain": "<git status --porcelain output>"}}
--                 (git fields present only when the watch scope is inside a git repo)
--   file_change: {"path": "<watch-scope-relative path>", "operation": "created|modified|deleted"}
--   trace_end:   {"duration_ms": <int>, "files_changed": <int>}   -- files_changed = unique paths in this run
CREATE TABLE traces (
    id TEXT PRIMARY KEY,
    card_id TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    run_id TEXT NOT NULL,
    event_type TEXT NOT NULL
        CHECK (event_type IN ('trace_start', 'file_change', 'trace_end')),
    data_json TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_traces_card_run ON traces(card_id, run_id);

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

### 3.4 Card Position / Reorder Strategy

New cards get `position = (max(position) in target column) + 1000`.
Moving a card between two existing cards sets `position = (prev + next) / 2`
(integer division). If two cards end up with the same position (accumulated
drift after ~10 inserts at the same gap), the store layer renumbers the
whole column in steps of 1000 as a single transaction before the write that
triggered the collision. This is the standard gap-based Kanban approach —
no full-column renumber on every ordinary move.

### 3.5 Keybindings (canonical)

One table governs the board, the card-detail view, and the pop-over/help. The
same keys are mirrored by their CLI commands (a TUI action is always scriptable).

| Key | Action | CLI equivalent |
|-----|--------|----------------|
| `j`/`k`, `↓`/`↑` | Focus previous/next card | — |
| `h`/`l`, `←`/`→` | Focus previous/next column | — |
| `Enter` | Open card: create-if-needed + attach to its tmux session | `loom card open <id>` |
| `K` | Kill the card's session and finalize its trace | `loom card close <id>` |
| `n` | New card (prompt for title, board, column) | `loom card add <title>` |
| `N` | New column | `loom column add <name>` |
| `m` | Move card (column picker) | `loom card move <id> <column>` |
| `d` | Card detail view | `loom card show <id>` |
| `e` | Edit card fields | `loom card update <id>` |
| `/` | Search/filter cards | `loom card list --search <q>` |
| `s` | Switch board | `loom board show <name>` |
| `w` | Switch workspace | `loom workspace switch <name>` |
| `?` | Help overlay | `loom help` |
| `q` | Quit (confirm if sessions are attached) | — |
| `Q` | Force quit (sessions keep running detached) | — |
| `Ctrl+c` | Quit (same as `q`) | — |

There is no separate "launch Claude" key: opening a card *is* the launch.
Session state is shown per card as `●` (running) / `◉` (attached); idle cards
show no marker.

---

**Loom never parents the `claude` process.** Every card's agent runs as the
command of a detached **tmux session** on a dedicated `-L loom` server. tmux
owns the PTY, the terminal, signal routing, and child cleanup; loom only ever
runs the short-lived `tmux` client and polls session state. Because a session
command ends when `claude` exits, **session existence == agent running** — no
PID tracking and no output parsing.

### 4.1 Session Lifecycle

A *run* = one open→complete cycle of a card's session. Each run gets a fresh
`run_id` (16 hex bytes) at creation, and every trace event for that cycle
(`trace_start`, `file_change`, `trace_end`) carries it.

1. **ensure** — if session `loom-<id>` already exists, reuse it (reattach).
   Otherwise create it detached in the watch scope's root (the card's
   codebase path if set, else the workspace root, §4.6):
   `tmux -L loom new-session -d -s loom-<id> -c <root> "claude <prompt>"`
   Then generate a `run_id`, record the `trace_start` event (including the
   git baseline snapshot, §4.6), and start the fsnotify watcher.
2. **attach** — hand the terminal to the session's client:
   `tea.ExecProcess("tmux", ["-L","loom","attach-session","-t","loom-<id>"])`.
   This is still the lazygit-pattern full-terminal handoff — the child is the
   tmux client, which renders claude's native TUI. On `prefix d` the client
   detaches and loom's board returns; on claude exit the session ends and
   attach returns.
3. **complete** — a 2-second poll loop (`tmux -L loom list-sessions -F
   '#{session_name} #{session_attached}'`) drives per-card `●` running / `◉`
   attached indicators. When `loom-<id>` disappears, loom stops the watcher,
   reconciles against the git baseline (§4.6), emits any missing `file_change`
   events for that `run_id`, and records `trace_end` for it.
4. **kill** — `K` in the board, or `loom card close <id>`, runs
   `tmux -L loom kill-session -t loom-<id>` and then finalizes the run's trace
   (same reconcile + `trace_end` path as complete).

No automated prompts. No output parsing. No resume management — Claude Code
still handles its own `--resume` internally.

### 4.2 Concurrency

N cards run concurrently, one session each, fully independent — tmux makes
the second and later sessions effectively free. Enter on a running card simply
reattaches. v0.1 sets no concurrency cap. The real cost is not tmux but the
agent: each live session holds one interactive Claude Code turn loop, which
consumes the user's API budget; `loom sessions` and the board's `●` markers
make idle/forgotten sessions visible so they can be killed (`K`).

### 4.3 Attach/Detach — the Human in the Loop

The agent picks up the task in a background session; the human is **in the
loop by choice**: attach to watch, steer, answer permission prompts, or Ctrl-C
the agent; detach to let it continue unattended while the board stays usable.
The session also survives loom exiting entirely — the agent keeps working, and
a later loom invocation rediscovers running sessions (§4.4) and reattaches.
This is strictly more flexible than the earlier v0.1 `ExecProcess` pop-over,
which *forced* holding the terminal for the session's duration.

### 4.4 Session Naming and the Dedicated Server

- **Server:** `tmux -L loom` — a private socket (`$TMUX_TMPDIR/loom`, falling
  back to `/tmp/tmux-*/loom`) so loom sessions never collide with or pollute
  the user's own tmux, and `tmux -L loom ls` is fast and scoped. The server
  loads a loom-owned `tmux.conf` that sets `prefix C-a` (avoids nested-prefix
  conflicts when loom itself runs inside tmux), `detach-on-destroy off`
  (a destroyed session returns the client to the outer terminal instead of
  dropping it), and leaves `exit-empty on` so the server exits automatically
  when its last session ends — no dangling process.
- **Session name:** `loom-<cardid>` — colons are forbidden (tmux parses `:`
  as `session:window`, silently corrupting the name). The full 32-hex card id
  is unique and self-describing.
- **Rediscovery:** on startup, loom (TUI or `loom status`) lists `loom-*`
  sessions on the loom server, maps each to its card by the id in the name,
  and marks those cards `●` running. This is what lets an agent session
  outlive loom.

### 4.5 Prompt Construction and Quoting

The card's title, description, objective, and acceptance criteria become the
prompt, as a plain **positional argument** to `claude` (never `-p`/`--print`,
which runs one non-interactive turn and exits — that would break the attach
model). When set, `verification_commands` is appended as a "Verify with:"
section and `test_cases` as a "Acceptance tests:" section so the structured
task fields are never dropped. The assembled prompt is:

```
{title}

## Description
{description}

## Objective
{objective}

## Acceptance Criteria
{acceptance_criteria}

## Verify with
{verification_commands}

## Acceptance tests
{test_cases}
```

Empty fields are omitted. Because tmux executes the session command via
`$SHELL -c`, the prompt is POSIX single-quoted: wrap it in `'` and escape any
inner `'` as `'\''`. Newlines are fine inside single quotes. (An equivalent
alternative: create the session empty and type the command with
`tmux send-keys -l` literal keys, which avoids all quoting — but then the pane
keeps an interactive shell, so an explicit `; exit` must be appended for
completion detection to work.)

### 4.6 File Watching Scope

The **watch scope** is the card's codebase path when the card has a
`codebase_id`, otherwise the workspace `root_path`. This makes "Codebase:
<path>" in the card-detail view real, and keeps watching bounded to what the
card is actually about.

`fsnotify` does not watch recursively on its own. On `trace_start`, loom
walks the watch scope and registers one watch per directory, skipping (and not
descending into) any directory matched by the ignore rules below; new
directories created during the session are detected via `fsnotify.Create`
events and get a watch added on the fly.

Ignore rules, applied in order:
1. Built-in default ignores: `.git`, `node_modules`, `target`, `dist`,
   `build`, `vendor`, `.venv`, `__pycache__` — always skipped, not
   configurable off.
2. `.loomignore` at the top of the watch scope — gitignore-style patterns,
   merged on top of the defaults.

`file_change` `data_json.path` is stored relative to the watch scope. Without
the ignore rules, watching a real repo root would record every compiler/VCS
artifact as a `file_change` trace event.

---

## 5. Implementation Phases

**Git reconciliation.** fsnotify only records changes while a loom process is
alive, but a tmux session can outlive loom (§4.4). So on `trace_start` loom
snapshots a **baseline pair**: `git rev-parse HEAD` (only when the watch scope
is inside a git repo) plus `git status --porcelain`, stored in the
`trace_start` `data_json` (§3.3). On completion loom takes a second pair and
computes the authoritative file set for the run as:

1. `git status --porcelain` at completion **minus** the baseline porcelain —
   this catches untracked files created during the run (which `git diff` can
   never see) as well as modifications/deletions.
2. If HEAD moved (the agent committed), `git diff --name-status <base_head> HEAD`
   covers the committed changes; files it reports are merged in.
3. The union of the two is reconciled against the live fsnotify
   `file_change` events: loom emits a `file_change` row for the run only for
   paths fsnotify did not already record (no double counting), using the
   porcelain letter as the operation (`A`→created, `M`/`R`→modified, `D`→deleted).

File attribution is therefore robust across loom restarts. If the watch scope
is not a git repository, trace fidelity is fsnotify-only (a documented
limitation).

### Phase 0: tmux Feasibility Test (0.5 day)
Minimal BubbleTea app with a hardcoded card list. Pressing Enter creates a detached `-L loom` session running a stub `claude`, then `tea.ExecProcess("tmux", ["-L","loom","attach-session","-t",...])`. Validates: session creation + quoted-prompt command, attach/detach handoff/handback, session persists after loom quits, and completion is detected when the session disappears.

### Phase 1: Scaffolding + Data Model (2-3 days)
- Go module (`go mod init loom`), BubbleTea v2, Bubbles, LipGloss
- SQLite schema (8 tables) + goose migration system with `embed.FS`
- Store layer: Workspace, Board, Column, Card CRUD with parameterized queries
- CLI command scaffolding: `loom init`, `loom workspace create`, `loom board create`, `loom card add`

### Phase 2: Kanban Board TUI + Claude Launch (5-7 days)
- 5-column board layout via `lipgloss.JoinHorizontal` (follow kancli pattern)
- Each column is a `bubbles/list` with custom delegate for card rendering
- Card shows: title (bold), priority (colored), labels, live session marker
- Keyboard navigation: `j/k` focus cards, `h/l` focus columns
- **SessionManager** (tmux wrapper): `ensure` (create-or-reuse), `attach`, `kill`, `status` — one detached `-L loom` session per card
- **Enter** on card → create-or-attach the card's session (`tea.ExecProcess("tmux", ["-L","loom","attach-session","-t",name])`), card context as the positional prompt
- **K** → kill the card's session and finalize its trace
- 2s poll → `●` running / `◉` attached indicators per card; status-bar toast when a detached session completes
- **d** → card detail view (description, acceptance criteria, trace history, session state)
- Card creation, deletion, column movement (`m` key + column picker)
- File watching via `fsnotify` → trace events recorded to DB; git-baseline reconciliation on completion
- Trace view in card detail: "Files Changed" list from trace history
- Search/filter with `/` key

### Phase 3: Polish + CLI (2-3 days)
- `loom card open <id>` / `--detach`, `loom card close <id>`, `loom attach <id>`, `loom sessions`, `loom artifact ...`
- Config file (`~/.config/loom/config.toml`) — see "Config schema" below
- Status bar, workspace switching
- Notes system (user-created planning notes)
- Card detail editing (title, description, objective, acceptance criteria, verification, tests, priority, labels)
- Git-reconciliation test (session that outlives loom still attributes file changes, §5)

**Config schema** (`~/.config/loom/config.toml`, single global config in v0.1;
no project-level config — per-workspace divergence is a v0.2 question):

```toml
[claude]
binary = "claude"             # Path to the Claude Code binary
prompt_model = ""             # Default model (empty = Claude's default)

[session]
tmux_server = "loom"          # Socket name for the dedicated server (-L <name>)
prefix = "C-a"                # Loom server prefix (nested-tmux safety, §4.4)

[database]
path = "~/.config/loom/loom.db"

[ui]
last_workspace = ""           # Current workspace (updated on switch, §6 "State")
last_board = ""               # Current board
```

**Total: ~10-14 days**

---

## 6. CLI Command Surface

```
loom                           # Launch TUI (default)
loom init [<dir>]              # Initialize loom: create a workspace for <dir> (default cwd,
                               #   name = dir basename), a default board "Board", and the 5
                               #   default columns (see below). Idempotent for existing workspaces.
loom config                    # Show/edit TOML config
loom workspace                 # Workspace management
  loom workspace list
  loom workspace create <name>
  loom workspace switch <name>   # persists as the current workspace (§6 "State")
  loom workspace codebase add <path>
  loom workspace codebase list
loom board                     # Board management
  loom board list
  loom board create <name>
  loom board show <name>
  loom board delete <name>
loom card                      # Card management
  loom card add <title> [--description <text>] [--objective <text>]
      [--acceptance-criteria <text>] [--verification <text>] [--tests <text>]
      [--priority <low|medium|high>] [--labels a,b] [--codebase <path>]
      [--board <name>] [--column <name>]
  loom card list [--board <name>] [--column <name>] [--search <q>]   # shows ● running marker
  loom card show <id>
  loom card move <id> <column>
  loom card open <id>          # create (if needed) + attach to the card's tmux session
  loom card open <id> --detach # create the session and return; no attach (scripting)
  loom card close <id>         # kill the session + finalize the run's trace (non-interactive)
  loom card update <id> [--title <text>] [--description <text>] [--priority <p>]
      [--codebase <path>] [--board <name>] [--column <name>]
  loom card delete <id>
  loom attach <id>             # attach to a running card session
  loom sessions                # list active loom tmux sessions → card mapping
loom column                    # Column management
  loom column add <name> [--board <name>] [--stage <stage>]
  loom column list [--board <name>]
  loom column delete <name> [--board <name>]
loom artifact                  # Artifacts (user-managed evidence, e.g. screenshots/reports)
  loom artifact add <card-id> <name> [--type <type>] [--content <text>] [--mime <mime>]
  loom artifact list <card-id>
  loom artifact show <id>
  loom artifact delete <id>
loom note                      # Notes management
  loom note add <title> [--content <text>] [--type <type>]
  loom note list
  loom note show <id>
  loom note delete <id>
loom status                    # Show overall status
loom version
loom help
```

**Default columns** created by `loom init` and `loom board create`: five
columns in order, one per stage — `Backlog`(backlog), `To Do`(todo),
`In Progress`(dev), `Review`(review), `Done`(done), positions 0,1000,2000,3000,4000.
There is no empty-board template in v0.1; `loom board create` always seeds
these five, and a board may later add or remove columns (extra columns may
reuse a stage).

**State (current workspace/board).** `loom workspace switch`, `loom board
show`/`create`, and launching the TUI persist the last-used workspace and board
in `~/.config/loom/config.toml` under `[ui] last_workspace` / `last_board`
(§5 "Config schema"). Flag-less commands (`loom card add <title>`,
`loom status`, the TUI) resolve against that state.

Non-TUI commands (`loom card add`, `loom card move`, `loom card close`, `loom sessions`, `loom note ...`, `loom artifact ...`) work as pure CLI for scripting. `loom` alone launches the interactive TUI.

`loom card open <id>` is **interactive**: it creates the card's tmux session if
needed and attaches, the same way Enter does inside the TUI. `--detach` creates
the session and returns immediately, which *is* scriptable — but it still
launches an interactive Claude Code in that session, so it means "start the
agent and leave it running", not "run the card headless to completion". There
is deliberately no non-interactive card-execution command in v0.1 (Claude Code
itself would need a non-interactive auth/permission mode for that). "CLI/TUI
parity" means every *state mutation* (add/move/update/delete/close) is
scriptable, not that Claude execution itself can complete unattended.

---

## 7. Key Decisions vs. Weave

| Area | Weave | Loom |
|------|-------|------|
| **Language** | Rust (Axum) | Go (BubbleTea) |
| **UI** | React + SSE | TUI (BubbleTea) |
| **Persistence** | SQLite via Axum | SQLite direct |
| **Transport** | REST + SSE | Go channels |
| **Agent model** | Automated subprocess + prompt injection | User-driven tmux-session launch (attach/detach) |
| **Kanban tools** | Agent-facing (7 tools) | None (user moves cards manually) |
| **Lifecycle** | tokio watchdog | None needed |
| **Scope** | 150+ features, 900+ tests | 3 phases, ~30 key tasks |

**What Weave does that loom adopts:**
- Kanban as task tracking model
- Workspace-scoped resources
- Single binary deployment

**What loom does differently (terminal-first advantages):**
- TUI eliminates HTTP, SSE, React, Vite, CORS, build.rs — one binary
- Claude Code's native TUI is preserved — attach hands off the full terminal; sessions survive loom and run concurrently
- BubbleTea Elm Architecture gives a single state tree — no React state bugs
- No automated agents — the user is always in control
- Drastically simpler: 8 tables instead of 13, 3 phases instead of 6

---

## 8. Risks

| **tmux missing / too old** | Low | Detect at startup; require tmux ≥ 3.x (format flags, `session_attached`); fail fast with an install hint (`apt install tmux` / `brew install tmux`). A tmux-less direct-PTY fallback is tracked as a v0.2 future item (§9). |
| **Nested tmux (loom inside tmux)** | Low | Attach works nested; the loom server remaps its prefix to `C-a` via a loom-owned `tmux.conf`, so nested keybindings don't collide (§4.4). |
| **Orphaned agent sessions** | Low | Always discoverable: `loom sessions`, the board's `●` marker, or plain `tmux -L loom ls`. Kill with `K` / `loom card close <id>`; a session never outlives tmux itself, and the `-L loom` server exits when its last session ends — no dangling process. |
| **Prompt quoting through tmux's shell** | Low | POSIX single-quote escaper (§4.5); asserted by the stub-`claude` integration test. |
| **Session-name collision** | Low | Names are `loom-<32-hex-card-id>` on a dedicated `-L loom` server; colons forbidden (§4.4). |
| **Process group cleanup** | Low (removed by this model) | `claude` is a child of the tmux server, not loom — loom only runs the short-lived tmux client. The old Setpgid/SIGTERM/SIGKILL concern now applies only to a future daemon that spawns children directly (§9). |
| **SQLite concurrent access** | Low | WAL mode + `_busy_timeout=5000` + single connection (`SetMaxOpenConns(1)`) |
| **BubbleTea API stability** | Low | Pin to v2.0.7. Wait 1 month after v2 major releases before upgrading. |
| **Lost context between Claude sessions** | Medium | Claude Code has its own `--resume`. Loom passes card title+desc+objective+AC (+ optional verification/test sections, §4.5) as the initial prompt and keeps the tmux session alive across attach/detach; it does not manage resume state. |

---

## 9. Future Considerations (Post-v0.1)

| Feature | When | Why Not Now |
|---------|------|-------------|
| **Daemon mode** (`loom start`) | v0.2 | tmux already provides session persistence; a daemon's only remaining job is unattended trace recording + session status when no loom process is alive |
| **tmux-less fallback** (direct PTY via `creack/pty`) | v0.2 | Reintroduces the child-process cleanup, SIGWINCH, and ANSI concerns §2.3 removed; only worth it if tmux-less users emerge. Guards the "single binary" principle for environments where tmux is unavailable. |
| **Board-as-tmux-layout** (pane/window per agent in the user's own tmux session) | v0.3 | Intrusive and zellij-incompatible; attach/detach already covers the human-in-the-loop need |
| **Git branch per card** | v0.3 | Isolate work per card |
| **zellij backend** | Not planned | Different session/plugin model; tmux is the v0.1 target |
| **Web companion UI** | Not planned | Web would reintroduce Weave's complexity |
| **Multi-user / auth** | Not planned | Single-user, localhost-first |

---

## 10. Verification Strategy

| Layer | What | How |
|-------|------|-----|
| **Unit** | Store CRUD, CLI commands, position/reorder logic, prompt quoting | Go table-driven tests |
| **Integration** | Card open → session create/attach → completion → trace | Scripted test against a stub `claude` shell script and the real `tmux` binary: asserts the `-L loom` session is created with name `loom-<id>` and cwd, that the captured pane shows the correctly quoted prompt, that the session ends and `trace_end` is recorded, that `loom card close` kills + finalizes, and that a session which outlives loom still attributes its file changes via git reconciliation (§5). **Plus** manual verification with the real Claude Code binary. |
| **TUI** | Keyboard navigation, column layout, card movement, session indicators | BubbleTea test framework |
| **E2E** | Full flow: board → card → create/attach → Claude works → detach → board live → reattach → trace on completion | Manual with the real Claude Code binary |

**Explicit exception to the project's 80%-across-unit/integration/E2E
coverage bar:** the tmux attach handoff to `claude` and the resulting TUI
render are only fully verifiable by a human watching a real terminal. The
stub-`claude` integration test covers the mechanics (session name, quoted argv
in the pane, cwd, completion detection, trace recording, git reconciliation,
kill path) automatically; actual interactive behavior stays a manual E2E step
for every release.

---

## 11. References

- **Weave**: `/mnt/data/works/weave` — source project (Rust, web-based Kanban × agent coordination)
- **BubbleTea**: https://github.com/charmbracelet/bubbletea (v2.0.7, 43.5k stars)
- **kancli**: https://github.com/charm-and-friends/kancli (reference BubbleTea Kanban, 223 stars)
- **k9s**: https://github.com/derailed/k9s (v0.51.0, 34k stars, multi-pane tview TUI)
- **lazygit**: https://github.com/jesseduffield/lazygit (80k stars, ExecProcess pattern)
- **modernc.org/sqlite**: Pure-Go SQLite (no CGO, ~90% C performance, WAL mode)
- **pressly/goose**: https://github.com/pressly/goose (v3, SQLite migration library)
- **Charm ecosystem**: https://charm.sh/ (BubbleTea, LipGloss, Bubbles, Glamour)
- **Claude share**: ADE architecture review — Theia vs Tauri+Monaco vs Code-OSS comparison
- **tmux**: https://github.com/tmux/tmux (v3.6 verified) — `new-session -d` / `attach-session` / `send-keys -l` / `kill-session` / `list-sessions -F '#{session_name} #{session_attached}'`, dedicated `-L <socket>` server

---

*This ADR captures the architecture for loom v0.1. It will be updated as the project evolves through its 3 implementation phases.*
