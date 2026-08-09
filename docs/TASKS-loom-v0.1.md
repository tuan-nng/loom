# TASKS: Loom v0.1 — Feature-Dev Implementation Backlog

**Source of truth:** `DESIGN-002-loom-multi-agent.md` (implementation blueprint),
`ADR-001` (base architecture), `ADR-002` (multi-agent, to be adopted at the end).
The repo is docs-only today — every task below builds from scratch.

**How to use:** each task is one bounded deliverable sized for a single
`feature-dev` run. The task is the *spec*; the cited DESIGN/ADR sections are the
*authoritative detail*. A task is done when its code compiles, its tests pass,
and its Acceptance items are green. Tasks are dependency-ordered; the lanes in
§"Execution order" run in parallel.

**Conventions every task follows:**
- Module `loom`, Go 1.23+, layout per DESIGN-002 §4.2. No new dependencies
  beyond the pinned set (C1): BubbleTea v2.0.7, Bubbles v0.18.0, LipGloss
  v0.11.0, Glamour v0.7.0, modernc.org/sqlite v1.33.0, goose v3.21.0, fsnotify
  v1.7.0.
- Verify with `go build ./...`, `go vet ./...`, `go test ./...`. Project
  coverage bar: 80% across unit/integration, with the explicit manual-E2E
  exception (ADR-001 §10). No linter config exists yet — `go vet` is the gate.
- No code comments unless they document a non-obvious design invariant (the
  ADRs are the prose; code comments only for e.g. the `AUTOINCREMENT` rationale).
- Stub agents in integration tests are shell scripts on `$PATH` (ADR-001 §10).

---

## Execution order

```
T0  (feasibility residual)     — can run any time, independent
│
├── LANE A — store               ├── LANE B — config + agent
│  T5 migrations ──► T6 kanban   │  T1 config ─► T2 agent contract
│  T6 ──► T7 cards ──► T8 traces │  T2 ──► T3 drivers (needs T2)
│                                  LANE C — tmux wrapper T10 (anytime after T0, needs nothing else)
│
└── T9 git reconcile (needs T8) ──► T11 watcher+recorder (needs T8) ──► T12 SessionManager
        (needs T9, T11, T10, T3, T2, T8)
   └── T13 BoardService (needs T12, T7, T8)
        ├── T14 CLI router ──► T15 CLI cards ──► T16 CLI session
        └── T17 TUI shell ──► T18 TUI board ──► T19 TUI forms ──► T20 TUI detail ──► T21 TUI extras
   └── T22 integration tests (needs T12)
   └── T23 docs adoption (needs everything green)
```

---

## Milestone 0 — Feasibility residual

### T0. Full-TUI `--prompt` auto-submit probe + gate release
**Spec:** DESIGN-002 §3.2.3, §9.2, §15, §16 ("Regression canary"); ADR-002 §1.2.

**Scope.** The only open Phase 0 question: does `opencode --prompt '<ctx>'`
(no `--mini`) auto-submit in the **full TUI**, and does the TUI stay alive
after a turn? Until confirmed, `interface = "full"` is **hard-gated**: it must
fail startup validation (§3.2.3).

- Empirically verify against the installed opencode (same method as P1/P2 in
  §3.1): create a detached `-L loomprobe` tmux session running `opencode
  --prompt 'Reply with exactly: OK'`, wait ~5s, `capture-pane`; assert the pane
  shows the model's response (not the prefilled input) and the session is still
  alive at t+30s.
- If confirmed: lift the gate — remove the `interface = "full"` startup
  validation rejection, enable the `full` argv path in the opencode driver,
  add a `full` case to the argv unit tests. If **not** confirmed: leave the gate
  in place and record the finding in the risk table (§15) with the exact opencode
  version — the `mini` interface still ships.

**Acceptance.** A decision is recorded (probe transcript + verdict in
`docs/`); the gate is either lifted-with-tests or left-failing-with-finding.

---

## Milestone 1 — Foundation

### T1. Config package (`internal/config`)
**Spec:** DESIGN-002 §11; ADR-001 §5 "Config schema" (superseded, see §11).

**Files:** `internal/config/config.go`, `internal/config/config_test.go`.

**Scope.** `Config`, `AgentConfig`, `ClaudeConfig`, `OpencodeConfig`,
`SessionConfig`, `DatabaseConfig` structs exactly per §11 (TOML tags as shown).
`Default()` (binaries `claude`/`opencode`, `interface = "mini"`,
`default = "claude"`, `tmux_server = "loom"`, `prefix = "C-a"`,
db path `~/.config/loom/loom.db`). `Load()` resolves via
`os.UserConfigDir()/loom/config.toml`, `~` expands `database.path`; missing file
→ `Default()`. `Validate()` is **config-local only**: `default`/`interface` are
string-valued but their membership checks live in `agent.Validate` (§4.2
dependency rule, F4) — do **not** import `agent`; a stale `prompt_model` key
under `[claude]` is a validation error that names `model` (loud migration).

**Acceptance.** Table tests: missing file → defaults; present file → parsed;
`prompt_model` → error naming `model`; `~` expansion; `interface` outside
`{"mini","full"}` → error listing accepted values.

### T2. Agent package — contract + helpers (`internal/agent` core)
**Spec:** DESIGN-002 §4.2 (dependency rule: `agent` imports `config` only),
§5, §5.1, §6, §7, §8, §11 (gate), §16 (unit rows).

**Files:** `internal/agent/driver.go`, `card.go`, `prompt.go`, `escape.go`.

**Scope.** The driver abstraction (no drivers yet — that is T3):
- `LaunchMode` (`interactive` | `run`), `SessionSpec{Argv, SendKeys}`,
  `Driver` interface — signatures **as corrected in §5.2**:
  `Resolve(cfg *config.Config) (string, error)` and
  `Launch(exe string, card Card, cfg *config.Config) (SessionSpec, error)`.
- Registry: `drivers` map, `Get`, `Known` (sorted), `IsKnown` (§5.1).
- `agent.Card` projection + `AgentOrDefault(def string) string` (§6).
- `BuildPrompt(c Card) string` (§7 — title always present; description /
  objective / acceptance_criteria omitted when empty).
- `PosixEscape` + `CommandLine` (§8 — quote *every* argv element; `SendKeys`
  never quoted).
- `agent.Validate(cfg *config.Config) error`: `Default` ∈ `Known()`; and the
  §3.2.3 **gate** — `interface = "full"` fails validation until T0 lifts it.
- `agent.Card` map + tests per §16.

**Acceptance.** Table tests: `BuildPrompt` omits empty sections and preserves
verbatim content; `PosixEscape` idempotent on `'` and newlines; `CommandLine`
escapes every element (never just the prompt); `AgentOrDefault` (NULL → default,
explicit → card value); `Validate` rejects unknown `default` and (while gated)
`interface = "full"` with accepted values in the message.

### T3. Agent drivers — claude + opencode (`internal/agent`)
**Spec:** DESIGN-002 §5.1, §9, §16 (unit — driver row).

**Files:** `internal/agent/claude.go`, `opencode.go`, `agent_test.go`.

**Scope.** Register `claudeDriver` and `opencodeDriver` in the `drivers` map:
- `claudeDriver`: `Name()="claude"`, `LaunchMode=Interactive`,
  `Resolve` via `exec.LookPath(cfg.Agent.Claude.Binary)`,
  `Launch` = `[exe, BuildPrompt(card)]` + `--model` when
  `cfg.Agent.Claude.Model != ""`; `SendKeys: ""` (§9.1).
- `opencodeDriver`: `Name()="opencode"`, `LaunchMode=Interactive`,
  `Resolve` via `exec.LookPath(cfg.Agent.Opencode.Binary)`;
  `Launch` per §9.2 — `interface = "full"` → `--prompt` only, default `"mini"` →
  `--mini --prompt`; pass-throughs appended *after* the prompt, only when set:
  `--model`, `--agent`, `--auto`; `SendKeys: ""`. (`"full"` path is dormant
  while T0 gates it, but the branch must exist and be table-tested.)
- Do **not** pass opencode session flags (`--dir`/`--title`/`-c`/`-s`/`--fork`)
  — §10.3.

**Acceptance.** Table-driven argv tests per driver/card/config matrix (§16):
claude positional vs opencode `--mini --prompt`; pass-throughs appear only when
set; `full` vs `mini`; `Resolve` honors `[agent.*] binary`. `agent.Known()`
returns `["claude","opencode"]`.

### T4. Store — migrations + pragmas (`internal/store`)
**Spec:** ADR-001 §3.3 (full DDL), §10 (schema verification); DESIGN-002 §12,
§12.1; ADR-002 §6.

**Files:** `internal/store/migrate/00001_initial.sql`,
`00002_card_agent.sql`, `embed.go`, `store.go`.

**Scope.** goose v3 with `embed.FS`. `00001_initial.sql` = the **full** ADR-001
§3.3 DDL verbatim (workspaces → boards → columns → codebases → cards → traces,
in that REFERENCES order; `traces` with `seq INTEGER PRIMARY KEY AUTOINCREMENT`,
`id NOT NULL UNIQUE`, partial unique `idx_traces_run_lifecycle`, partial
`idx_traces_open_runs`; the 7-table schema incl. `ui_state` with its single-row
CHECK). `00002_card_agent.sql` = `ALTER TABLE cards ADD COLUMN agent TEXT CHECK
(agent IN ('claude','opencode'))` + `DROP COLUMN` down (§12, §12.1).
`store.go`: `Open(path)` asserting the four pragmas in order on every connection
(WAL, `foreign_keys=ON`, `busy_timeout=5000`, `synchronous=NORMAL`), WAL set via
`SetMaxOpenConns(1)`-compatible single writer, migration runner.

**Acceptance.** Tests: pragmas asserted on a fresh connection (`PRAGMA
foreign_keys` is ON — §10 makes the cascade test fail without it); deleting a
board cascades columns+cards; migration up creates the §3.3 schema; `00002`
rejects `agent='bogus'` (`CHECK constraint failed`), accepts NULL; `goose down`
restores the 00001 schema; existing cards migrate `agent=NULL`.

### T5. Store — kanban CRUD (`internal/store`)
**Spec:** ADR-001 §3.3, §5 (ui_state), §6 (default columns, `loom init` data).

**Files:** `internal/store/workspaces.go`, `boards.go`, `columns.go`,
`codebases.go`.

**Scope.** Workspace / Board / Column / Codebase CRUD with parameterized
queries; ID generation = 16 random bytes `crypto/rand`, hex (32 chars) — same
generator everywhere; `strftime('%Y-%m-%dT%H:%M:%f','now')` timestamps
(not `datetime('now')`); `columns.stage` CHECK enforced; ordered by `position`
(boards/columns) / `created_at` (workspaces/codebases); `ui_state` single-row
get/set (`last_workspace_id`/`last_board_id`, `ON DELETE SET NULL`), plus the
helpers the CLI needs for `loom init`: create default board "Board" + five
columns (`Backlog`/`To Do`/`In Progress`/`Review`/`Done`, positions
0,1000,2000,3000,4000, one per stage) — idempotent for an existing workspace.

**Acceptance.** CRUD round-trips per entity; cross-entity FK cascade behavior;
`ui_state` is single-row (second insert fails); `loom init` helper idempotent.

### T6. Store — cards CRUD + reorder (`internal/store`)
**Spec:** ADR-001 §3.3 (cards DDL incl. the `agent` column from 00002), §3.4,
§6 (move semantics, cross-board rejection); DESIGN-002 §6.

**Files:** `internal/store/cards.go`.

**Scope.** Card CRUD with the nullable `Agent *string` field (§6) +
`AgentOrDefault(def string)`. Position/reorder **with the pre-write rebalance**
(§3.4): new card `position = max+1000`; move sets `(prev+next)/2`; when the gap
is exhausted (`next - prev <= 1`), renumber the whole column to
`0,1000,2000,…` in display order and apply the pending move — all in **one
transaction**. The move function is the only writer of `column_id` and MUST keep
`board_id`/`workspace_id` in sync; moving to a column on a different board is
rejected at the store layer. `priority`/`labels`/`codebase_id` fields.

**Acceptance.** Table tests: rebalance triggers **at `next-prev <= 1`** (assert
the pre-write renumber, not a post-hoc duplicate repair — §10); insert/move/
update/delete round-trips; `Agent` NULL ↔ value round-trips; `AgentOrDefault`
NULL→default, explicit→card value; cross-board move rejected; cascade from
column/board delete.

### T7. Store — traces run lifecycle (`internal/store`)
**Spec:** ADR-001 §3.3 (traces DDL, data_json shapes), §5; DESIGN-002 §10.2
(`AbortRun` semantics), §16.

**Files:** `internal/store/traces.go`.

**Scope.** Run lifecycle over `traces`: `StartRun(cardID, runID)` →
`trace_start` (data_json carries the git baseline pair — shape per §3.3);
`RecordChange(runID, path, operation)` → `file_change` (`created|modified|
deleted`); `EndRun(runID, durationMs, filesChanged)` → `trace_end`;
`OpenRuns()` (trace_start with no trace_end — feeds reconcile-on-startup);
`AbortRun(runID)` → **DELETE the whole run** (all rows for `run_id`) —
it never leaves an orphaned `trace_end` (§10.2 invariant 4). Ordering by `seq`,
never timestamps. `run_id` from the shared 16-byte hex generator.

**Acceptance.** Start→change→end round-trip; second `trace_start` for one
`(card_id, run_id)` rejected by `idx_traces_run_lifecycle`; a burst of events
written in one millisecond is totally ordered by `seq` (a timestamp-only test
proves nothing — §10); `seq` survives `VACUUM`; `AbortRun` removes every row for
the run_id; `OpenRuns` finds only un-finalized runs.

---

## Milestone 2 — Core lifecycle

### T8. Trace — git reconciliation (`internal/trace`)
**Spec:** ADR-001 §5 (baseline pair + path-keyed diff), §3.3 data_json; DESIGN-002 §16 (git reconcile test row).

**Files:** `internal/trace/git.go`.

**Scope.** The pure reconciliation logic (no fsnotify — that is T9):
`SnapshotBaseline(root)` → `{base_head, porcelain}` (`git rev-parse HEAD` +
`git status --porcelain`, only when inside a git repo); `Reconcile(baseline,
current) → []Change{path, operation}` implementing the **path-keyed** algorithm
(§5): parse both porcelain snapshots into `path → status-letter` maps and diff
on the path — (a) in C absent from B = new/untracked; (b) in both with a
different letter = changed; (c) present in both, identical letter = ambiguous →
**include as `modified`** (over-attribution bias); (d) in B absent from C =
`modified`; (e) if HEAD moved, `git diff --name-status base HEAD` adds its
paths (`A→created, M→modified, D→deleted, R→delete old + create new`);
`Dedup(live []Change) []Change` keeps only paths fsnotify did not already
record; `FilesChanged` = unique paths. Run via short-lived `exec` git clients.

**Acceptance.** Table-driven tests per §16/ADR-001 §10: status-letter-only change
→ **not** a change; already-dirty-at-baseline file → included as `modified`;
rename → delete+create; untracked new file captured (porcelain, not just
`git diff`); HEAD-moved committed set; dedup vs live events.

### T9. Trace — watcher + recorder (`internal/trace`)
**Spec:** ADR-001 §4.6 (watch scope, ignore rules, recursive walk), §3.3
(file_change shape); DESIGN-002 §10.2 (`Watch`, `AbortRun` interplay).

**Files:** `internal/trace/recorder.go`, `watcher.go`.

**Scope.** `Recorder` wrapping `internal/store`: `StartRun(cardID, runID, root,
baseline)` (writes `trace_start` with the T8 baseline), `RecordChange`,
`EndRun`, `AbortRun`. `Watcher`: recursive fsnotify registration over the watch
scope at start; **built-in defaults always skipped** (`.git`, `node_modules`,
`target`, `dist`, `build`, `vendor`, `.venv`, `__pycache__`); `.loomignore`
gitignore-style patterns merged on top (§4.6); new directories discovered via
`fsnotify.Create` get a watch added on the fly; `file_change.path` is
watch-scope-relative. Watcher is scoped to a `run_id` and stopped on EndRun/
AbortRun.

**Acceptance.** A temp tree with ignored + watched files records only watched
events with relative paths; creating a nested dir mid-session starts recording
its children; `.loomignore` honored; stopping the watcher stops recording;
Recorder write round-trips through the store.

### T10. Tmux client wrapper (`internal/session`)
**Spec:** DESIGN-002 §10.1; ADR-001 §4.4 (server/socket/naming), §11 (tmux refs).

**Files:** `internal/session/tmux.go`.

**Scope.** Thin wrapper bound to the configured `-L <server>`:
`NewSession(name, cwd, command)` (new-session -d -s name -c cwd
"command"), `HasSession(name)`, `CapturePane(name)`, `SendKeys(name, keys)`,
`KillSession(name)`, `ListSessions()` (via `-F '#{session_name}'`),
`SessionName(id)` = `"loom-" + id`. `bin` resolved once at startup; a
startup tmux ≥ 3.x check with an install hint (ADR-001 §8). Use exec-free
error handling so a missing server socket surfaces as a typed error.

**Acceptance.** Against real tmux: session create/query/capture/send-kill/
list round-trip on an isolated `-L loomselftest` server; `SessionName` rejects
`:` (ADR-001 §4.4).

### T11. SessionManager (`internal/session`)
**Spec:** DESIGN-002 §5.2, §6, §10.2 (the entire refactor + invariants), §4.3
sequence; ADR-001 §4.1.

**Files:** `internal/session/manager.go`, `session_test.go` (stub-driver unit
tests).

**Scope.** `Manager` with `ensure` exactly per §10.2 — the **only** lifecycle
code that touches the driver: resolve the card's agent (§6 projection),
`agent.Get`, `driver.Resolve` (fail open, **no** `trace_start` on error),
`driver.Launch`, snapshot baseline **before** launch, `tmux.NewSession`,
then the ~500ms startup probe that **captures pane during the window**, and
only then `StartRun` + `Watch` + optional `SendKeys` — all wrapped in the
deferred `KillSession` + `AbortRun` that closes every post-create error path.
Guarantee the four §10.2 invariants. `attach` (tea.ExecProcess → tmux
attach-session), `kill`, `status` (2s poll → per-card running/attached),
`reconcile-on-startup` (finalize open runs whose session is absent) — these are
**byte-for-byte ADR-001 behavior**, no driver involvement. `LaunchMode` is
logged, informational.

**Acceptance.** Stub-driver unit tests (§16 "Integration — per driver" +
"Failure paths"): reuse-no-new-run on second ensure; nonexistent binary →
`not found in PATH` error, **no session, no trace_start**; resolved-but-runtime-
failing stub → probe fails, session killed, **no** trace_start (C7);
post-create error (forced `StartRun` failure) → no session + no trace rows
left (invariants 1–2); reconcile-on-startup finalizes exactly one `trace_end`
for a missed completion.

### T12. BoardService (`internal/board`)
**Spec:** DESIGN-002 §4.1–4.2; ADR-001 §3.1, §4.1 step 4, §6.

**Files:** `internal/board/service.go`.

**Scope.** The orchestration seam both CLI and TUI consume: kanban operations
(create/list/move/update/delete card, board/column/workspace switching with
`ui_state` persistence) wired to the store, plus session actions delegated to
`SessionManager`: open (ensure+attach), open --detach, close (kill+finalize),
and the **done-stage move rule** — moving a card into a `done`-stage column
auto-kills its session and finalizes the trace (§4.1 step 4); watch-root
selection (`codebase path ?? workspace root`) applied on open.

**Acceptance.** Move-to-done triggers kill+finalize and survives target
validation; open/close round-trip through SessionManager; current
workspace/board resolution incl. the fallback chain (ui_state → most-recent
workspace → error "run loom init").

---

## Milestone 3 — CLI

### T13. CLI router + workspace/board/column/config/init/status
**Spec:** DESIGN-002 §4.2, §13; ADR-001 §6 (full surface + State semantics).

**Files:** `internal/cli/root.go`, `status.go`, `config.go`.

**Scope.** Small stdlib-`flag` subcommand router (no cobra; §13). Commands:
`loom` (dispatch to TUI), `loom init [<dir>]` (idempotent), `loom config`
(prints the §11 schema), `loom status`, `loom version`, `loom help`, and the
`workspace` (`list/create/switch/codebase add/codebase list`), `board`
(`list/create/show/delete`), `column` (`add --stage/list/delete`) trees —
behaviors per ADR-001 §6 incl. default-columns seeding and ui_state persistence
on `workspace switch`/`board show`. `main.go` (cmd/loom) wires config load +
`agent.Validate` + store open + dispatch.

**Acceptance.** Scripted flows: init → workspace/board/column CRUD → status;
state fallback chain; each mutation is scriptable (CLI/TUI parity).

### T14. CLI card commands (with `--agent`)
**Spec:** DESIGN-002 §13 (flag semantics incl. `--agent=` reset); ADR-001 §6.

**Files:** `internal/cli/card.go`.

**Scope.** `loom card add <title>` with `--description/--objective/
--acceptance-criteria/--priority/--labels/--codebase/--board/--column/--agent`;
`update` (same flags; `--agent <name>` sets, `--agent=` **resets to NULL** —
distinguish absent vs present/empty via `flag.Visit`, §13); `list
[--board|--column|--search]` showing the agent badge; `show`; `move <id>
<column>` (done-stage kill per T12); `delete`. Non-empty `--agent` values
validated against `agent.Known()` (C8).

**Acceptance.** add/update/list/show/move/delete round-trips; `--agent=` on
update clears to NULL and a later config change re-defaults the card; invalid
`--agent` rejected with accepted values; `list` badge correct for NULL and
explicit cards.

### T15. CLI session commands (`open/close/attach/sessions`)
**Spec:** DESIGN-002 §13 (unchanged commands); ADR-001 §6, §4.1.

**Files:** `internal/cli/session.go`.

**Scope.** `loom card open <id>` (ensure + attach), `open --detach` (ensure,
return), `loom card close <id>` (kill + finalize, non-interactive),
`loom attach <id>`, `loom sessions` (live `loom-*` sessions → card mapping
with `●`/`◉`), each delegating to T12/T11. No `--run` flag (§13).

**Acceptance.** Against a stub agent + real tmux: open → session exists →
`loom sessions` shows it → close finalizes exactly one run; `--detach` returns
with the session running.

---

## Milestone 4 — TUI

### T16. TUI shell + board layout
**Spec:** ADR-001 §3.1, §3.5 (canonical keymap); DESIGN-002 §4.1, §14.

**Files:** `internal/tui/app.go`, `keymap.go`, `board.go`.

**Scope.** BubbleTea app root (init/update/view loop, tea.Msg wiring), the
canonical keymap table (§3.5 — every key present, mapped or stubbed), and the
5-column board layout via `lipgloss.JoinHorizontal` (kancli pattern); each
column is a `bubbles/list` with a custom delegate; `j/k`/`h/l` navigation; quit
handling: `q` confirms when sessions attached, `Q` force-quits (sessions keep
running detached); status bar.

**Acceptance.** BubbleTea test framework: navigation, quit confirm/cancel,
layout renders 5 columns.

### T17. TUI board content + session markers
**Spec:** DESIGN-002 §14 (badge); ADR-001 §3.5, §4.1 step 3, §4.2.

**Files:** `internal/tui/board.go` (render delegate), app wiring.

**Scope.** Card cell rendering: title (bold), priority (colored), labels, and
the **agent badge** (`cl` / `oc` from `card.AgentOrDefault(cfg.Agent.Default)`,
§14); live session markers `●` running / `◉` attached driven by the 2s poll
(T11 status) and rediscovered sessions on startup (§4.4); `Enter` → open the
card's session via its agent (T12) + `tea.ExecProcess("tmux",["-L","loom",
"attach-session",...])`; `K` → kill + finalize; status-bar toast when a detached
session completes.

**Acceptance.** Badge correct per resolved agent; markers transition
running→gone on session end; Enter attaches (integration: handoff command
correct), K kills.

### T18. TUI forms (new/edit card + agent picker, new column, move)
**Spec:** DESIGN-002 §14 (picker); ADR-001 §3.5.

**Files:** `internal/tui/forms.go`.

**Scope.** `n` new-card form (title, board, column) with the **agent picker**
(empty = default, plus `claude`/`opencode`; default form value = the card's
resolved agent, §14); `e` edit-card form (same fields); `N` new column
(name + stage); `m` move-card column picker (done-stage rule via T12).

**Acceptance.** Forms create/update correctly; agent picker writes NULL for
empty and the name otherwise; move picker routes to T12.

### T19. TUI card detail view
**Spec:** DESIGN-002 §14 (agent field); ADR-001 §3.5, §4.6.

**Files:** `internal/tui/card_detail.go`.

**Scope.** `d` detail view: description / objective / acceptance criteria
(Glamour markdown), priority, labels, **"Agent: claude | opencode (default)"**
field, "Codebase: <path>", trace history — per-run "Files Changed (last
session)" from trace events (§3.3), session state `●`/`◉`, run duration.

**Acceptance.** Fields render; agent shows `(default)` suffix for NULL cards;
files-changed aggregates unique paths per run_id.

### T20. TUI extras — search, board/workspace switch, help overlay
**Spec:** ADR-001 §3.5 (`/`, `s`, `w`, `?`), §6.

**Files:** `internal/tui/app.go` / `board.go` additions.

**Scope.** `/` search/filter over cards; `s` board switch and `w` workspace
switch (persist ui_state via T12); `?` help overlay listing the canonical
keymap.

**Acceptance.** Filter narrows the board; switching persists selection;
overlay renders.

---

## Milestone 5 — Verification & docs

### T21. Integration tests — stub drivers × real tmux
**Spec:** DESIGN-002 §16 (integration + failure paths + regression canary);
ADR-001 §10.

**Files:** `internal/session/session_test.go` (extend) + stub scripts.

**Scope.** Parametrized integration suite against stub `claude` and stub
`opencode` shell scripts and the real `tmux` (isolated `-L loomselftest`):
per-driver card open → session name `loom-<id>`, cwd, quoted argv visible in
`capture-pane`; session end → exactly one `trace_end`; `loom card close` kills +
finalizes; git reconciliation attributes changes (a run that outlives loom).
Failure paths per §16: nonexistent binary → open fails at `Resolve` with
`not found in PATH`, no session, no pane (assert message, not pane text);
resolved-but-failing stub → probe fails, session killed, no `trace_start` (C7);
missed-completion (<2s stub) → reconcile-on-startup finalizes exactly one
`trace_end`; forced `StartRun` failure → deferred KillSession leaves no session
and no trace rows. **Regression canary:** post-probe `capture-pane` asserts the
first pane line is the stub's "response", not a prefilled input line (F5).

**Acceptance.** All cases green against real tmux; canary fails if the pane
still shows the prompt line (verify by temporarily breaking the stub if needed).

### T22. Docs adoption
**Spec:** DESIGN-002 §18, ADR-002 amendment log; ADR-001 §8/§11.

**Files:** `docs/ADR-002-loom-multi-agent.md`, `docs/README*` (if created),
any probe transcripts from T0.

**Scope.** Adopt ADR-002: apply the amendment-log corrections **in the body**
(§1.2 `run --interactive` validity, §3.1 driver signatures per §5.2, §4.2
`SendKeys` default `""`, §6 migration verified) and flip status to Adopted;
fold the T0 full-TUI verdict into §15; update the §8 risk rows with any
findings. If the repo has no README, create one (build, config, both drivers,
verified opencode version range per §15).

**Acceptance.** ADR-002 body no longer contradicts DESIGN-002; `git log`
shows the adoption commit; risk table reflects the T0 verdict.
