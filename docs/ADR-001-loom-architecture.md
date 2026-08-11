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
| 2026-08-08 | Architectural correctness review. Seven fixes, all closing gaps where the design would have produced **plausible-looking wrong data rather than an error**. (1) §8's risk table had no header row and did not render. (2) `codebases` is declared before `cards` (which references it), and §3.3 now specifies the per-connection pragma set — `foreign_keys=ON` was never stated, and without it every `ON DELETE CASCADE` in the schema is inert. (3) `config.claude.binary` is resolved to an absolute path via `exec.LookPath` before the session command is built, because the tmux server inherits the environment of whichever client first started it, not loom's; a ~500ms startup probe now deletes (not finalizes) the `trace_start` of a session that never launched. (4) The 2s poll is demoted to a liveness *indicator* and a reconcile-on-startup pass (§4.1 step 5) becomes the completion guarantee — a run ending between polls, or while loom is not running, previously stayed open forever. (5) §5 git reconciliation now diffs baseline vs completion porcelain keyed on **path**, not on the raw status line: a status-letter change alone was counted as an edit, and a file already dirty at baseline produced a byte-identical line and vanished entirely (under-attribution, the worse failure); ambiguous already-dirty paths are now included. (6) Event ordering was decoupled from timestamps entirely. The first pass moved all timestamps to `strftime('%Y-%m-%dT%H:%M:%f')` (`datetime('now')` ties at whole seconds), but executing the schema showed that insufficient: consecutive inserts complete inside a single millisecond and still share a value, so millisecond precision only narrows the tie window. `traces` therefore gains a monotonic `seq` (`INTEGER PRIMARY KEY AUTOINCREMENT`) as the sole ordering key — `AUTOINCREMENT` specifically, because `VACUUM` renumbers a bare rowid and would silently reorder trace history — with `traces.id` demoted to `NOT NULL UNIQUE` and timestamps kept for display/duration only. (7) Current workspace/board move out of `config.toml` into a single-row `ui_state` table: writing selection state into a hand-edited config rewrites the user's file on every board switch and lets concurrent `loom` invocations clobber each other. Each fix was executed rather than reasoned about: the DDL was run against SQLite (cascades verified live, and verified *inert* without the pragma), and the reconciliation algorithm was replayed on a purpose-built git repo where the original provably dropped a file the agent had edited. Also: §3.4's rebalance trigger corrected to fire at `next - prev <= 1` (pre-write) rather than on an observed duplicate (post-write, already corrupt); `traces` gains a partial UNIQUE index closing the double-Enter double-run race. §10 gains failure-path and schema-constraint test rows for each. Structural: §4.1–4.6 were orphaned under §3 — no `## 4` heading existed at all — so a `## 4. Session Model` heading was added over the block that already served as its intro; top-level numbering is now contiguous 1–11. The three scope questions left open above were then resolved: `notes` and `artifacts` are **cut from v0.1** — their tables, the `loom artifact ...` / `loom note ...` CLI groups, and the Phase 3 notes bullet are removed, the schema is now **6 domain tables + `ui_state`**, and both return as v0.2 items in §9 (v0.1 keeps its scope on the board → card → claude → trace loop). `verification_commands`/`test_cases` were **folded into `description`**: the columns are dropped from `cards` (§3.3), the prompt is built from title/description/objective/acceptance-criteria only (§4.5), and the `--verification`/`--tests` add-flags are gone. `columns.stage` gained its behavior: **moving a card to a `done`-stage column auto-kills the session and finalizes the trace** (§4.1, keybinding `m`, CLI `loom card move`), so stage can no longer drift inert. |
| 2026-08-11 | T20 implementation: search (`/`), board switch (`s`), workspace switch (`w`), help overlay (`?`) — all now live in the TUI. §3.5 keybinding table is fully implemented; the `mapped or stubbed` note is retired. |

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
| **TUI Framework** | BubbleTea v2 | `charm.land/bubbletea/v2` v2.0.7 | Elm Architecture, 43.5k stars, 21k dependents, cell-based partial renderer |
| **TUI Widgets** | Bubbles v2 | `charm.land/bubbles/v2` v2.1.1 | Viewport, list, textinput, spinner, paginator, table (the v2 companion to BubbleTea v2; v0.18.0 was the v1 module and cannot compile against v2 `tea` types) |
| **Styling** | Lip Gloss v2 | `charm.land/lipgloss/v2` v2.0.5 | CSS-like terminal styling, `JoinHorizontal`, `Width` (v2 module; v0.11.0 was v1) |
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
│  Tables: workspaces, boards, columns, codebases, cards,          │
│          traces  (6 domain) + ui_state                          │
│  Pragmas: WAL · foreign_keys=ON · busy_timeout=5000              │
│  Migration: pressly/goose with embed.FS                           │
└──────────────────────────────────────────────────────────────────┘
```

### 3.2 Data Flow — User Opens Card → Claude Code (tmux session model)

```
User presses Enter on a card (or `loom card open <id>`)
         │
         ▼
  ┌──────┴──────────────────────────────────┐
  │  SessionManager.resolve()                │
  │   exec.LookPath(config.claude.binary)   │
  │   → absolute path, or fail the open     │
  │   (tmux server's PATH ≠ loom's, §4.1.0) │
  └──────┬──────────────────────────────────┘
         │
         ▼
  ┌──────┴──────────────────────────────────┐
  │  SessionManager.ensure(card)             │
  │   session `loom-<id>` exists? → reuse    │
  │   else create detached (cwd = card's     │
  │   codebase path or workspace root):      │
  │     tmux -L loom new-session -d          │
  │       -s loom-<id> -c <root>             │
  │       "<abs-claude> '<card context>'"    │
  │   new run_id; trace_start (git baseline) │
  │   start fsnotify watcher (goroutine)     │
  └──────┬──────────────────────────────────┘
         │
         ▼
  ┌──────┴──────────────────────────────────┐
  │  Startup probe (~500ms):                 │
  │   session still exists?                  │
  │   no → capture-pane, DELETE trace_start, │
  │        error toast  (never a "run")      │
  └──────┬──────────────────────────────────┘
         │ yes
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
  │  2s poll loop (liveness indicator):      │
  │   tmux -L loom list-sessions -F         │
  │     '#{session_name} #{session_attached}'│
  │   → per-card ● running / ◉ attached     │
  │   → when `loom-<id>` disappears:        │
  │     stop watcher, git-reconcile vs      │
  │     baseline (path-keyed, §5),          │
  │     file_change (dedup), trace_end      │
  └──────┬──────────────────────────────────┘
         │ ...but a run can end unseen: between
         │ two polls, or while loom is not running
         ▼
  ┌──────┴──────────────────────────────────┐
  │  Reconcile-on-startup (correctness       │
  │  backstop, §4.1 step 5):                 │
  │   runs with trace_start and no trace_end │
  │   whose session is absent → finalize     │
  │   (same reconcile + trace_end path)      │
  └─────────────────────────────────────────┘
```

### 3.3 Data Schema (6 domain tables + ui_state)

**ID generation:** All opaque row IDs are generated in-process as 16 random
bytes from `crypto/rand`, hex-encoded (32 hex chars). `traces.run_id` uses the
same generator. This avoids pulling in `google/uuid` (or any dependency) while
keeping IDs collision-safe and opaque. They carry **no** ordering property, and
nothing is allowed to infer order from them.

`traces` is the one exception to "opaque id is the primary key": there `id` is
`NOT NULL UNIQUE` and the primary key is the monotonic `seq` below. Every other
table keeps `id TEXT PRIMARY KEY`, ordering by `position` (cards, boards,
columns) or `created_at` (workspaces, codebases), where ordering is a
display concern and a tie is harmless.

**Timestamps.** Every `created_at`/`updated_at` uses
`strftime('%Y-%m-%dT%H:%M:%f','now')`, not `datetime('now')`. `datetime('now')`
has whole-second granularity, and trace events within one run routinely land in
the same second (`trace_start` immediately followed by its first
`file_change`), which makes them unorderable and breaks per-run duration.

**Ordering is not the timestamp's job.** Millisecond precision narrows the tie
window but does not close it: consecutive inserts measurably complete inside a
single millisecond and still share a `created_at` value. Timestamps are
therefore treated as *display and duration* data only, and **`traces` carries
an explicit monotonic `seq`** (`INTEGER PRIMARY KEY AUTOINCREMENT`) that is the
sole ordering key for events within a run. `AUTOINCREMENT` is deliberate rather
than a bare rowid — a plain rowid is renumbered by `VACUUM`, which would
silently reorder history. Event order must be exact (a `file_change` recorded
before its run's `trace_start` is a corrupt run), so it gets a guaranteed total
order rather than a probabilistic one.

**Connection pragmas.** Every connection opened by the store layer sets, in
this order, before any query:

```
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
PRAGMA synchronous = NORMAL;
```

`foreign_keys` is OFF by default in SQLite, and without it every
`ON DELETE CASCADE` below is decorative — deleting a board would silently
orphan its columns and cards. `journal_mode` is persistent (stored in the file
header) but is re-asserted for clarity; the other three are per-connection and
must be re-issued on every open.

**Table order.** The DDL below is ordered so that every `REFERENCES` target
exists before its referent (`workspaces → boards → columns → codebases →
cards → traces`). With `foreign_keys = ON`, goose applies
migrations in this order and a forward reference would fail.

```sql
-- Workspaces
CREATE TABLE workspaces (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    root_path TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now')),
    archived_at TEXT
);

-- Boards
CREATE TABLE boards (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now'))
);

-- Columns
-- stage carries a real behavior, not just a label: moving a card into a
-- column whose stage is 'done' auto-kills the card's session and finalizes
-- its trace (§4.1). Extra columns may reuse a stage, so the trigger is
-- 'target column's stage == done', never a column id.
CREATE TABLE columns (
    id TEXT PRIMARY KEY,
    board_id TEXT NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    stage TEXT NOT NULL DEFAULT 'dev'
        CHECK (stage IN ('backlog', 'todo', 'dev', 'review', 'done')),
    position INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now'))
);

-- Codebases (registered project directories)
-- Declared before `cards` because cards.codebase_id references it.
CREATE TABLE codebases (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    label TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now')),
    UNIQUE(workspace_id, path)
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
    -- verification/test content is NOT a separate column: it is folded into
    -- `description` (a card's prompt is built from title/description/objective/
    -- acceptance_criteria only, §4.5). A dedicated column whose only reader is
    -- the prompt concatenator is `description` with extra steps.
    acceptance_criteria TEXT,
    priority TEXT NOT NULL DEFAULT 'medium'
        CHECK (priority IN ('low', 'medium', 'high')),
    labels TEXT,                -- comma-separated, e.g. "frontend, auth, urgent"
    position INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now'))
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
    -- seq is the ordering key for events within a run (see "Ordering is not
    -- the timestamp's job" above). AUTOINCREMENT, not a bare rowid: VACUUM
    -- renumbers plain rowids and would silently reorder trace history.
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    card_id TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    run_id TEXT NOT NULL,
    event_type TEXT NOT NULL
        CHECK (event_type IN ('trace_start', 'file_change', 'trace_end')),
    data_json TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now'))
);
CREATE INDEX idx_traces_card_run ON traces(card_id, run_id, seq);

-- A run has exactly one trace_start and at most one trace_end. Without this,
-- a double-Enter race (two `ensure` calls before the first session appears in
-- the poll) opens two concurrent runs for one card and "last session" becomes
-- ambiguous. file_change is deliberately excluded — a run has many.
CREATE UNIQUE INDEX idx_traces_run_lifecycle
    ON traces(card_id, run_id, event_type)
    WHERE event_type IN ('trace_start', 'trace_end');

-- Open-run lookup for the reconcile-on-startup pass (§4.1 step 5).
CREATE INDEX idx_traces_open_runs
    ON traces(event_type, card_id) WHERE event_type = 'trace_start';

-- artifacts and notes are v0.2 (see §9): they had zero integration with the
-- core loop (board → card → claude → trace) and were cut from v0.1. No rows
-- exist here until v0.2 re-adds their tables.
```

### 3.4 Card Position / Reorder Strategy

New cards get `position = (max(position) in target column) + 1000`.
Moving a card between two existing cards sets `position = (prev + next) / 2`
(integer division).

**Rebalance trigger.** The store layer renumbers *before* the write, not after
a collision is observed. The gap is exhausted when the computed midpoint equals
either neighbour — i.e. when `next - prev <= 1` — which is the last moment a
distinct position is still available. Checking instead for "two cards share a
position" is too late: by the time the duplicate is visible it has already been
written, and column order is non-deterministic until it is repaired. Repeated
insertion at the same gap exhausts a 1000-step gap after ~10 moves, so the
trigger fires in normal use and must be correct.

On trigger, the whole column is renumbered to `0, 1000, 2000, …` in the current
display order, and the pending move is then applied against the fresh
positions — both inside a single transaction, so no reader observes the
intermediate state. This is the standard gap-based Kanban approach: no
full-column renumber on an ordinary move, and an O(n) repair only when a gap
actually runs out.

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
| `m` | Move card (column picker); target column's stage `done` auto-kills the session (§4.1) | `loom card move <id> <column>` |
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

## 4. Session Model

**Loom never parents the `claude` process.** Every card's agent runs as the
command of a detached **tmux session** on a dedicated `-L loom` server. tmux
owns the PTY, the terminal, signal routing, and child cleanup; loom only ever
runs the short-lived `tmux` client and polls session state. Because a session
command ends when `claude` exits, **session existence == agent running** — no
PID tracking and no output parsing.

That equivalence holds only once the session command has actually started, and
only while a loom process is watching. Two corollaries follow, both handled in
§4.1 rather than assumed away: a session whose command never launched (bad
binary, bad cwd) is *absent* for the same reason a completed one is, so absence
alone cannot mean "done" — hence the startup probe; and absence observed by
nobody is not observed at all, so the 2s poll is a liveness indicator and
reconcile-on-startup is the actual completion guarantee.

### 4.1 Session Lifecycle

A *run* = one open→complete cycle of a card's session. Each run gets a fresh
`run_id` (16 hex bytes) at creation, and every trace event for that cycle
(`trace_start`, `file_change`, `trace_end`) carries it.

0. **resolve** — before building any session command, resolve
   `config.claude.binary` to an **absolute path** via `exec.LookPath` in
   loom's own environment, and fail the open with a user-visible error if it
   does not resolve. This is not defensive padding: the tmux server inherits
   the environment of whichever client first started it, which is frequently
   not the shell the user launched loom from (a login shell, a systemd user
   unit, an older `-L loom` server started hours earlier). A bare `claude` in
   the session command therefore resolves against an environment loom does not
   control, and a `PATH` miss makes the session exit instantly — which the
   completion path below would otherwise record as a successful zero-file run.
1. **ensure** — if session `loom-<id>` already exists, reuse it (reattach).
   Otherwise create it detached in the watch scope's root (the card's
   codebase path if set, else the workspace root, §4.6):
   `tmux -L loom new-session -d -s loom-<id> -c <root> "<abs-claude> <prompt>"`
   Then generate a `run_id`, record the `trace_start` event (including the
   git baseline snapshot, §5), and start the fsnotify watcher.

   **Startup probe.** Immediately after `new-session` returns, loom waits
   ~500ms and re-checks that `loom-<id>` still exists. A session that is
   already gone means the command never got off the ground (bad binary,
   non-executable, bad cwd, shell syntax error). In that case loom captures
   the pane's scrollback (`tmux -L loom capture-pane -p -t loom-<id>`, read
   before the session is reaped where possible), deletes the just-written
   `trace_start` row rather than finalizing it, and surfaces the captured text
   as an error toast. A failed launch must never appear in trace history as a
   completed run.
2. **attach** — hand the terminal to the session's client:
   `tea.ExecProcess("tmux", ["-L","loom","attach-session","-t","loom-<id>"])`.
   This is still the lazygit-pattern full-terminal handoff — the child is the
   tmux client, which renders claude's native TUI. On `prefix d` the client
   detaches and loom's board returns; on claude exit the session ends and
   attach returns.
3. **complete** — a 2-second poll loop (`tmux -L loom list-sessions -F
   '#{session_name} #{session_attached}'`) drives per-card `●` running / `◉`
   attached indicators. When `loom-<id>` disappears, loom stops the watcher,
   reconciles against the git baseline (§5), emits any missing `file_change`
   events for that `run_id`, and records `trace_end` for it.
4. **kill** — `K` in the board, `loom card close <id>`, or a **move into a
   `done`-stage column** (keyboard `m` or `loom card move <id> <column>`) runs
   `tmux -L loom kill-session -t loom-<id>` and then finalizes the run's trace
   (same reconcile + `trace_end` path as complete). The move triggers this
   automatically so that "mark done" and "stop the agent" cannot diverge — an
   agent left running in Done is the one case a board user will not notice. The
   move is rejected (and no session is touched) if the target column is on a
   different board, as §3.3 requires.
5. **reconcile-on-startup** — the 2s poll in step 3 only observes sessions
   while a loom process is alive, so it is *not* sufficient as the sole
   completion detector. A session that starts and ends between two polls, or
   ends while no loom process is running at all, would leave its `trace_start`
   row open forever. On every startup (TUI, `loom status`, `loom sessions`)
   loom therefore queries for runs having a `trace_start` and no matching
   `trace_end`, cross-references the live `loom-*` session list, and finalizes
   every such run whose session is absent: git-reconcile against the stored
   baseline (§5), emit the missing `file_change` rows, write `trace_end`. The
   poll loop is a liveness *indicator*; this step is the correctness backstop.
   It is the same code path that makes a session outliving loom attributable
   (§4.4), so it costs nothing extra.

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
model). The assembled prompt is built from exactly four fields — title,
description, objective, acceptance criteria:

```
{title}

## Description
{description}

## Objective
{objective}

## Acceptance Criteria
{acceptance_criteria}
```

Any verification commands or test cases belong in `description` (there are no
separate columns — §3.3); they flow through verbatim and are never parsed by
loom. Keep the sections as they arrive; loom does not infer structure from
markdown headings.

Empty fields are omitted. Because tmux executes the session command via
`$SHELL -c`, the prompt is POSIX single-quoted: wrap it in `'` and escape any
inner `'` as `'\''`. Newlines are fine inside single quotes. The binary the
prompt is passed to is the **absolute** path resolved in §4.1 step 0, not a
bare `claude` — the quoting and the resolution are independent requirements and
both must hold for the session command to run. (An equivalent alternative:
create the session empty and type the command with `tmux send-keys -l` literal
keys, which avoids all quoting — but then the pane keeps an interactive shell,
so an explicit `; exit` must be appended for completion detection to work, and
the §4.1 startup probe no longer distinguishes a failed launch from a live
shell. The quoted-argv form is preferred for exactly that reason.)

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

**Both porcelain snapshots are parsed into a `path → status-letter` map, and
every set operation below is keyed on the path, never on the raw porcelain
line.** Line-wise set difference is wrong in both directions: the same file can
change its two-letter status between snapshots without the agent touching it
(staging moves ` M` → `M `, producing a "new" line for an unchanged file), and
a file already dirty at baseline that the agent then modified further yields a
byte-identical line and disappears from the difference entirely. Under-
attribution — silently dropping a file the agent really changed — is the worse
failure, so the algorithm is deliberately biased toward over-attribution.

1. **Working-tree set.** Let `B` and `C` be the baseline and completion
   porcelain maps. A path is in the working-tree change set if it is in `C` and
   either (a) absent from `B` — newly dirtied or newly untracked during the
   run — or (b) present in `B` with a *different* status letter. Paths present
   in both with an identical letter are **ambiguous**: the file was already
   dirty and may or may not have been touched again. loom includes them, and
   marks the emitted row's operation `modified`. Additionally, any path in `B`
   but not in `C` is included as `modified` (the agent staged, committed, or
   reverted it).
2. **Committed set.** If HEAD moved, `git diff --name-status <base_head> HEAD`
   contributes its paths, with `A`→created, `M`→modified, `D`→deleted, and
   `R`→two entries (old path `deleted`, new path `created`).
3. **Union and dedup.** The union of (1) and (2), keyed on path, is reconciled
   against the live fsnotify `file_change` events already recorded for this
   `run_id`: loom emits a `file_change` row only for paths fsnotify did not
   already record, so a path is never counted twice. `trace_end.files_changed`
   is the count of unique paths across both sources.

File attribution is therefore robust across loom restarts. Two documented
limitations: if the watch scope is not a git repository, trace fidelity is
fsnotify-only; and a file that was already dirty before the run is attributed
to the run whether or not the agent touched it (the ambiguous case in step 1).

### Phase 0: tmux Feasibility Test (0.5 day)
Minimal BubbleTea app with a hardcoded card list. Pressing Enter creates a detached `-L loom` session running a stub `claude`, then `tea.ExecProcess("tmux", ["-L","loom","attach-session","-t",...])`. Validates: session creation + quoted-prompt command, attach/detach handoff/handback, session persists after loom quits, and completion is detected when the session disappears. **Also validates the two failure paths up front**, since both are cheap here and expensive to retrofit: a session whose command exits immediately (bad binary) must be distinguishable from one that completed, and the tmux server's inherited `PATH` must be observed directly (`tmux -L loom show-environment`) rather than assumed to match loom's.

### Phase 1: Scaffolding + Data Model (2-3 days)
- Go module (`go mod init loom`), BubbleTea v2, Bubbles, LipGloss
- SQLite schema (6 domain tables + `ui_state`) + goose migration system with `embed.FS`
- Connection pragmas asserted on open: WAL, `foreign_keys=ON`, `busy_timeout=5000` (§3.3) — with a test, since a missing `foreign_keys` makes every cascade inert and fails silently
- Store layer: Workspace, Board, Column, Card CRUD with parameterized queries
- Position/reorder with the pre-write rebalance at `next - prev <= 1` (§3.4)
- CLI command scaffolding: `loom init`, `loom workspace create`, `loom board create`, `loom card add`

### Phase 2: Kanban Board TUI + Claude Launch (5-7 days)
- 5-column board layout via `lipgloss.JoinHorizontal` (follow kancli pattern)
- Each column is a `bubbles/list` with custom delegate for card rendering
- Card shows: title (bold), priority (colored), labels, live session marker
- Keyboard navigation: `j/k` focus cards, `h/l` focus columns
- **SessionManager** (tmux wrapper): `resolve` (absolute `claude` path), `ensure` (create-or-reuse + startup probe), `attach`, `kill`, `status` — one detached `-L loom` session per card
- **Enter** on card → create-or-attach the card's session (`tea.ExecProcess("tmux", ["-L","loom","attach-session","-t",name])`), card context as the positional prompt
- **K** → kill the card's session and finalize its trace
- 2s poll → `●` running / `◉` attached indicators per card; status-bar toast when a detached session completes
- Reconcile-on-startup for runs left open by a missed completion or a loom restart (§4.1 step 5) — required before trace history can be trusted
- **d** → card detail view (description, acceptance criteria, trace history, session state)
- Card creation, deletion, column movement (`m` key + column picker)
- File watching via `fsnotify` → trace events recorded to DB; path-keyed git-baseline reconciliation on completion (§5)
- Trace view in card detail: "Files Changed" list from trace history
- Search/filter with `/` key

### Phase 3: Polish + CLI (2-3 days)
- `loom card open <id>` / `--detach`, `loom card close <id>`, `loom attach <id>`, `loom sessions`
- Config file (`~/.config/loom/config.toml`) — see "Config schema" below — read-only from loom's side; current workspace/board persist to `ui_state`
- Status bar, workspace switching
- Card detail editing (title, description, objective, acceptance criteria, priority, labels)
- `loom card move` auto-kills the session when the target column's stage is `done` (§4.1)
- Failure-path tests: bad-binary open leaves no `trace_start`; a run orphaned by killing loom is finalized exactly once on next startup (§10)
- Git-reconciliation test (session that outlives loom still attributes file changes, §5)

**Config schema** (`~/.config/loom/config.toml`, single global config in v0.1;
no project-level config — per-workspace divergence is a v0.2 question). The
config file holds **user intent only** — values a human writes and loom never
rewrites:

```toml
[claude]
binary = "claude"             # Resolved to an absolute path at open (§4.1 step 0)
prompt_model = ""             # Default model (empty = Claude's default)

[session]
tmux_server = "loom"          # Socket name for the dedicated server (-L <name>)
prefix = "C-a"                # Loom server prefix (nested-tmux safety, §4.4)

[database]
path = "~/.config/loom/loom.db"
```

**Runtime state lives in the database, not the config file.** The current
workspace and board are *selection state*, not configuration, and storing them
in `config.toml` would be a category error with two concrete failure modes:
loom would rewrite a hand-edited TOML file on every board switch (destroying
comments, ordering, and formatting, since Go TOML encoders serialize from the
struct rather than patching the source), and two concurrent `loom` invocations
would clobber each other's selection on their last write. Both are silent.

State therefore lives in a single-row table, written transactionally with the
rest of the store:

```sql
CREATE TABLE ui_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),   -- single row, enforced
    last_workspace_id TEXT REFERENCES workspaces(id) ON DELETE SET NULL,
    last_board_id TEXT REFERENCES boards(id) ON DELETE SET NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now'))
);
INSERT INTO ui_state (id) VALUES (1);
```

Storing IDs rather than names also makes the reference survive a rename, and
`ON DELETE SET NULL` means deleting the current workspace degrades to "no
selection" instead of dangling. `ui_state` is infrastructure, not a domain
table — the v0.1 domain count is 6 (see §3.3 and the §3.1 diagram).

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
      [--acceptance-criteria <text>] [--priority <low|medium|high>]
      [--labels a,b] [--codebase <path>] [--board <name>] [--column <name>]
  loom card list [--board <name>] [--column <name>] [--search <q>]   # shows ● running marker
  loom card show <id>
  loom card move <id> <column> # to a done-stage column also kills the session (§4.1)
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
in the database's single-row `ui_state` table (§5 "Config schema" → "Runtime
state"), **not** in `config.toml` — the config file is user-authored intent and
loom never rewrites it. Flag-less commands (`loom card add <title>`,
`loom status`, the TUI) resolve against that state; when it is unset or its
target has been deleted (`ON DELETE SET NULL`), they fall back to the
most-recently-created workspace and its first board, and error only if none
exists (`run loom init`).

Non-TUI commands (`loom card add`, `loom card move`, `loom card close`, `loom sessions`) work as pure CLI for scripting. `loom` alone launches the interactive TUI.

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
- Drastically simpler: 6 domain tables instead of 13, 3 phases instead of 6

---

## 8. Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| **tmux missing / too old** | Low | Detect at startup; require tmux ≥ 3.x (format flags, `session_attached`); fail fast with an install hint (`apt install tmux` / `brew install tmux`). A tmux-less direct-PTY fallback is tracked as a v0.2 future item (§9). |
| **Nested tmux (loom inside tmux)** | Low | Attach works nested; the loom server remaps its prefix to `C-a` via a loom-owned `tmux.conf`, so nested keybindings don't collide (§4.4). |
| **Orphaned agent sessions** | Low | Always discoverable: `loom sessions`, the board's `●` marker, or plain `tmux -L loom ls`. Kill with `K` / `loom card close <id>`; a session never outlives tmux itself, and the `-L loom` server exits when its last session ends — no dangling process. |
| **Prompt quoting through tmux's shell** | Low | POSIX single-quote escaper (§4.5); asserted by the stub-`claude` integration test. |
| **Session-name collision** | Low | Names are `loom-<32-hex-card-id>` on a dedicated `-L loom` server; colons forbidden (§4.4). |
| **Process group cleanup** | Low (removed by this model) | `claude` is a child of the tmux server, not loom — loom only runs the short-lived tmux client. The old Setpgid/SIGTERM/SIGKILL concern now applies only to a future daemon that spawns children directly (§9). |
| **Session command never starts (`claude` not on the tmux server's PATH)** | Medium | The tmux server inherits the env of whichever client first started it, which is often not the shell loom was launched from. `config.claude.binary` is resolved to an absolute path via `exec.LookPath` before the session command is built, and a ~500ms startup probe catches a session that dies immediately; the `trace_start` row is deleted (not finalized) and the captured pane is surfaced as an error. Without both, a failed launch would be recorded as a successful zero-file run (§4.1 steps 0–1). |
| **Missed completion (session starts and ends between two 2s polls, or while loom is not running)** | Medium | The poll loop is a liveness indicator, not the completion detector. Reconcile-on-startup finalizes every run with a `trace_start` and no `trace_end` whose session is absent (§4.1 step 5) — the same path that attributes sessions outliving loom, so it costs nothing extra. |
| **Under-attributed file changes (porcelain line-diff)** | Medium | Reconciliation diffs baseline vs completion porcelain keyed on **path**, not on the raw status line: a status-letter change alone is not a real edit, and a file already dirty at baseline yields a byte-identical line and would vanish from a line-wise difference. Ambiguous already-dirty paths are included (biased toward over-attribution, §5). |
| **Concurrent runs for one card (double-Enter race)** | Low | `idx_traces_run_lifecycle` is a partial UNIQUE index over `(card_id, run_id, event_type)` for `trace_start`/`trace_end`, so a second open cannot silently create a second live run (§3.3). |
| **Unorderable trace events** | Medium | Timestamps alone are **not** a total order: `datetime('now')` ties at one second, and even `strftime(..%f..)` ties within a millisecond — consecutive inserts measurably complete inside one. `traces.seq` (`INTEGER PRIMARY KEY AUTOINCREMENT`) is therefore the sole ordering key, with `AUTOINCREMENT` chosen because `VACUUM` renumbers a bare rowid and would silently reorder history. Timestamps remain for display and duration only (§3.3). |
| **Config file rewritten under the user** | Low | `config.toml` is user intent and is never written by loom; current workspace/board live in the single-row `ui_state` table, which is also safe against two concurrent `loom` invocations clobbering each other (§5). |
| **SQLite concurrent access** | Low | WAL mode + `_busy_timeout=5000` + single connection (`SetMaxOpenConns(1)`). `foreign_keys=ON` is set per-connection — without it every `ON DELETE CASCADE` in §3.3 is inert. |
| **BubbleTea API stability** | Low | Pin to v2.0.7. Wait 1 month after v2 major releases before upgrading. |
| **Lost context between Claude sessions** | Medium | Claude Code has its own `--resume`. Loom passes card title + description + objective + acceptance criteria as the initial prompt (§4.5) and keeps the tmux session alive across attach/detach; it does not manage resume state. |

---

## 9. Future Considerations (Post-v0.1)

| Feature | When | Why Not Now |
|---------|------|-------------|
| **Daemon mode** (`loom start`) | v0.2 | tmux already provides session persistence; a daemon's only remaining job is unattended trace recording + session status when no loom process is alive |
| **tmux-less fallback** (direct PTY via `creack/pty`) | v0.2 | Reintroduces the child-process cleanup, SIGWINCH, and ANSI concerns §2.3 removed; only worth it if tmux-less users emerge. Guards the "single binary" principle for environments where tmux is unavailable. |
| **Artifacts** (user-managed evidence: screenshots, reports, links per card) | v0.2 | Cut from v0.1 — zero integration with the core loop; the card-detail view would gain a "Files Changed" trace list before it gains artifacts. Tables + `loom artifact ...` return here. |
| **Notes** (persistent user planning notes) | v0.2 | Cut from v0.1 for the same reason as artifacts; a workspace-scoped `notes` table + `loom note ...` return here. |
| **Board-as-tmux-layout** (pane/window per agent in the user's own tmux session) | v0.3 | Intrusive and zellij-incompatible; attach/detach already covers the human-in-the-loop need |
| **Git branch per card** | v0.3 | Isolate work per card |
| **zellij backend** | Not planned | Different session/plugin model; tmux is the v0.1 target |
| **Web companion UI** | Not planned | Web would reintroduce Weave's complexity |
| **Multi-user / auth** | Not planned | Single-user, localhost-first |

---

## 10. Verification Strategy

| Layer | What | How |
|-------|------|-----|
| **Unit** | Store CRUD, CLI commands, prompt quoting; **position rebalance triggered at `next - prev <= 1`** (assert the pre-write renumber, not a post-hoc duplicate repair); **porcelain path-keyed diff** (table-driven: status-letter-only change → not a change; already-dirty file → included; rename → delete+create) | Go table-driven tests |
| **Integration** | Card open → session create/attach → completion → trace | Scripted test against a stub `claude` shell script and the real `tmux` binary: asserts the `-L loom` session is created with name `loom-<id>` and cwd, that the captured pane shows the correctly quoted prompt, that the session ends and `trace_end` is recorded, that `loom card close` kills + finalizes, and that a session which outlives loom still attributes its file changes via git reconciliation (§5). **Plus** manual verification with the real Claude Code binary. |
| **Failure paths** | The two ways a run is silently mis-recorded | Stub-`claude` variants: (a) **bad binary** — point `config.claude.binary` at a nonexistent path and assert the open fails with the captured pane text and leaves **no** `trace_start` row (§4.1 step 0–1); (b) **missed completion** — a stub that exits in <2s, plus a run whose `trace_start` is written with loom then killed, and assert reconcile-on-startup finalizes exactly one `trace_end` (§4.1 step 5). Both are regression guards for bugs that produce plausible-looking data rather than errors. |
| **Schema** | Constraints actually enforced | Assert `PRAGMA foreign_keys` is ON for every pooled connection and that deleting a board cascades its columns/cards; assert `idx_traces_run_lifecycle` rejects a second `trace_start` for one `(card_id, run_id)`; assert a burst of trace events written inside a single millisecond is still totally ordered by `seq` (a timestamp-only assertion passes vacuously and proves nothing); assert `seq` values survive a `VACUUM` unchanged. |
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
