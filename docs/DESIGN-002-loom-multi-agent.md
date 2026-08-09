# DESIGN-002: Loom v0.1 Multi-Agent Implementation Blueprint

**Status:** Ready for implementation (ADR-002 approved)
**Date:** 2026-08-09
**Author:** MiMo Agent (architect)
**Base:** ADR-001 (adopted) + ADR-002 (proposed, this blueprint implements it)

---

## 1. Context & Goal

Loom is a CLI-native Kanban task tracker. Opening a card launches the card's
**coding agent** inside a detached tmux session on a dedicated `-L loom`
server; the human stays in the loop by attach/detach. ADR-002 generalizes the
agent layer so a card (or the whole board) runs its agent as either `claude`
or `opencode`, with the identical tmux lifecycle, completion detection,
file-change tracing, and attach/detach — **no change to the data-flow core**.

The repo today contains only `docs/`. This blueprint specifies the full
system so an implementer (or `feature-dev`) can write code without re-deriving
a decision: package layout, Go contracts, exact argv for both drivers,
config/schema changes, CLI/TUI deltas, the SessionManager refactor, and the
verification matrix. The ADR-001 base (store, trace, TUI, CLI) is specified at
the seam level only — the depth lives in the ADR-002 agent layer.

**Success criteria:** with `[agent] default = "opencode"` and the same card,
`Enter`/`loom card open` produces a working interactive opencode session in a
`-L loom` pane; completion/tracing/close behave byte-for-byte like claude; no
regression when `default = "claude"` (ADR-001 behavior).

---

## 2. Constraints & Non-Functional Requirements

| # | Constraint | Source |
|---|-----------|--------|
| C1 | Single Go binary, BubbleTea TUI, SQLite, goose, fsnotify; **no new dependencies** beyond ADR-001 §2.2 | ADR-001 §1.3 |
| C2 | External runtime deps: `tmux` (3.x) + the configured coding agent (`claude` and/or `opencode`) — both user-owned | ADR-001 §1.3, ADR-002 §2.2 |
| C3 | tmux lifecycle is **agent-agnostic and unchanged**: `-L loom` server, `loom-<id>` naming, ~500ms startup probe, 2s poll, reconcile-on-startup, fsnotify watcher, git-baseline reconcile | ADR-002 §3.1 |
| C4 | `traces` schema untouched; completion detection is "session disappeared" for both agents | ADR-002 §3.1, §4.3 |
| C5 | Only interactive launch ships. `opencode run` autonomous mode is designed (LaunchMode) but **not wired** | ADR-002 §2.1, §9 |
| C6 | `config.toml` is user intent; loom never rewrites it. Missing file = defaults | ADR-001 §5 |
| C7 | A failed launch must never appear as a completed run (no `trace_start` row left behind) | ADR-001 §4.1 |
| C8 | Agent names are validated at three layers: CLI (`agent.Known()`), startup (`agent.Validate(cfg)`), and a `CHECK` constraint on `cards.agent`. (`config` stays a leaf — the cross-package name check lives in `agent`, which already imports `config`, avoiding an import cycle.) | ADR-002 §6 |

---

## 3. Verified Facts (empirical, 2026-08-09 — opencode v1.18.15)

These replace guesses in ADR-002 with observed behavior. The Phase 0 probes
below were run on this machine; the transcripts are the design evidence.

### 3.1 Probe results

| Probe | Command | Result |
|-------|---------|--------|
| P1 | `opencode --mini --prompt 'Reply with exactly: OK'` in a detached `-L loomprobe` tmux session | **Auto-submits.** Pane showed the prefilled `› Reply with exactly: OK`, then the model's `OK`, then `▣ Build · Model default · 5.0s`. No Enter was sent. |
| P2 | same session at t+24s | Session **still alive** after the turn; `▣ Build` status bar visible. `--mini` does **not** exit on idle — session-existence still means "user is in the REPL". Completion semantics (ADR-002 §4.3) hold. |
| P3 | `opencode --mini --prompt x </dev/null` (non-TTY) | `Error: --mini requires a TTY stdout` — confirms a tmux pane PTY satisfies the requirement. |
| P4 | `opencode run --mini x` | `Error: --mini must be used without the run subcommand` — the ADR's quoted error, verbatim. |
| P5 | `opencode run --interactive x </dev/null` | **Accepted** (exit 0, entered the split-footer REPL, `> build · deepseek-v4-flash`). **Contradicts ADR-002 §1.2**, which claims `run --interactive` is rejected. |
| P6 | `opencode --help` / `run --help` | Top-level TUI flags: `--mini --prompt --agent --auto -m -s -c --fork`. `run` flags: positional `message`, `--interactive --dir --title --format --command`. |

### 3.2 Consequences for the design

1. **`SendKeys` default is `""`, not `"Enter"`.** The auto-submit caveat in
   ADR-002 §4.2 is resolved: mini mode sends `--prompt` itself. The `SendKeys`
   field stays on the contract (full-TUI mode and future drivers may need it),
   but the opencode driver ships with `""`. The §4.2 "if it only pre-fills"
   branch becomes dead code unless a future opencode version regresses.
2. **ADR-002 §1.2 factual correction:** `run --interactive` is a *valid*
   interactive split-footer mode in v1.18.15. The rejected combination is
   `run --mini` (P4). This does not change the shipped design (`--mini` stays
   the interactive interface per ADR-002 §4.2); it only means the §1.2 bullet
   must be amended at adoption so implementers are not misled. A future option
   exists to offer `run --interactive` as an alternative interface.
3. **Phase 0 full-TUI gate:** full-TUI `--prompt` auto-submit
   (`interface = "full"`) is assumed to share mini's behavior (same
   `route.prompt` machinery), but it is a **hard gate**, not a residual: the
   value `"full"` must not ship until the Phase 0 probe confirms auto-submit
   and the REPL stays alive (a prefill-only full TUI would idle detached runs
   silently — review finding F5). Until then, `interface = "full"` fails
   startup validation.

---

## 4. Architecture

### 4.1 Component diagram

```
┌──────────────────────────────────────────────────────────────────────┐
│                         cmd/loom (main)                               │
└────────┬──────────────────────────────┬──────────────────────────────┘
         │ internal/tui (BubbleTea)     │ internal/cli (stdlib flag)
         │  Board · CardDetail · Forms  │  card/board/workspace/session
         └────────┬─────────────────────┴──────────────────────────────┘
                  ▼
┌────────────────────────── Application Core ───────────────────────────┐
│  ┌──────────────┐  ┌───────────────────────────────┐  ┌────────────┐  │
│  │ BoardService │  │ SessionManager                │  │TraceRecorder│ │
│  │ kanban CRUD  │  │ ensure / attach / kill /      │  │ fsnotify +  │ │
│  │              │  │ status / probe / reconcile    │  │ git baseline│ │
│  └──────────────┘  └───────────────┬───────────────┘  └────────────┘  │
│                                    │ agent.Get(card.AgentOrDefault)    │
│   ┌────────────────── internal/agent (ADR-002, store-free) ─────────┐  │
│   │  Driver iface · registry · PosixEscape · CommandLine · BuildPrompt│ │
│   │  ┌───────────────┐   ┌────────────────┐                          │  │
│   │  │ claudeDriver  │   │ opencodeDriver │                          │  │
│   │  └───────────────┘   └────────────────┘                          │  │
│   └──────────────────────────────┬───────────────────────────────────┘  │
└─────────────────────────────────┬───────────────────────────────────────┘
                                  │ argv, each element single-quoted (§8)
                                  ▼
                  tmux -L loom new-session -d -s loom-<id>
                     -c <root> "<joined argv>"
                                  ▼
                    agent's native TUI in the pane (PTY)
```

### 4.2 Package layout

```
loom/
├── go.mod                                  module loom, go 1.23+
├── cmd/loom/main.go                        entry point: TUI vs CLI dispatch
└── internal/
    ├── config/                             leaf
    │   ├── config.go                       Config, Default, Load, Validate
    │   └── config_test.go
    ├── agent/                              leaf (imports config only) — ADR-002
    │   ├── driver.go                       Driver, LaunchMode, SessionSpec, registry
    │   ├── card.go                         agent.Card projection + AgentOrDefault
    │   ├── prompt.go                       BuildPrompt (ADR-001 §4.5 template)
    │   ├── escape.go                       PosixEscape, CommandLine
    │   ├── claude.go                       claudeDriver
    │   ├── opencode.go                     opencodeDriver
    │   └── agent_test.go
    ├── store/                              leaf (modernc sqlite + goose)
    │   ├── store.go                        open, pragmas, migration runner
    │   ├── workspaces.go boards.go columns.go codebases.go
    │   ├── cards.go                        Card CRUD + Agent column
    │   ├── traces.go                       trace events, run lifecycle
    │   ├── migrate/embed.go
    │   ├── migrate/00001_initial.sql       ADR-001 §3.3 DDL
    │   └── migrate/00002_card_agent.sql    ADR-002 §6
    ├── trace/
    │   ├── recorder.go                     trace_start/end, files_changed
    │   ├── watcher.go                      fsnotify + .loomignore
    │   └── git.go                          porcelain/path-keyed reconcile
    ├── session/
    │   ├── tmux.go                         thin tmux client wrapper
    │   ├── manager.go                      SessionManager (driver-aware ensure)
    │   └── session_test.go                 stub-driver integration tests
    ├── board/service.go                    BoardService orchestration
    ├── cli/                                subcommand router + stdlib flag
    │   ├── root.go  card.go  config.go  session.go  status.go
    └── tui/
        ├── app.go  board.go  card_detail.go  forms.go  keymap.go
```

Dependency rules (no cycles): `config` and `store` are leaves; `agent`
imports `config` only; `trace` imports `store`; `session` imports `agent`,
`store`, `trace`, `config`; `cli`/`tui` import everything below. `agent` is
deliberately **store-free** (takes an `agent.Card` projection, §6) so driver
unit tests never touch a database. Validation that must see both `config` and
the driver registry (`Agent.Default` ∈ `agent.Known()`) therefore lives in
`agent.Validate(cfg *config.Config)`, called from `main` at startup — it can
never live in `config` without creating the `config → agent` cycle that this
rule forbids (review finding F4).

### 4.3 Sequence diagram — card open → completion (per driver)

The driver touches exactly three points (steps 1, 2/3, 7); everything else is
the unchanged ADR-001 lifecycle.

```
User:Enter | loom card open <id>
  │
  │ 1. agent.Get(card.AgentOrDefault(cfg.Agent.Default))
  ├──────────────────────────────────────────────►  agent  (registry lookup)
  │ 2. exe,err := driver.Resolve(cfg)     [fail open, no trace_start on error]
  │ 3. spec,err := driver.Launch(exe, card, cfg)   → argv = [exe, flags, prompt]
  │ 4. baseline := SnapshotBaseline(root)           [git pair held, §10.2]
  │ 5. tmux -L loom new-session -d -s loom-<id> -c <root> "<CommandLine(argv)>"
  │ 6. ~500ms startup probe (capture-pane DURING the window):
  │      dead → KillSession · error toast · NO trace row ever written
  │ 7. trace.StartRun → trace_start (baseline) · fsnotify watcher  [record AFTER probe, §10.2]
  │ 8. if spec.SendKeys != "" → tmux send-keys -t loom-<id> <key>   ["", shipped]
  │ 9. attach via tea.ExecProcess("tmux",["-L","loom","attach-session","-t",...])
  │        — or --detach returns here; session runs detached
  │        — agent's native TUI renders in the pane (claude REPL | opencode --mini)
  │
  ├── loop 2s poll: list-sessions → ● running / ◉ attached per card ──┐
  └─ user quits agent (or prefix-d, later K/close/done-move)
      session loom-<id> disappears (tmux owns the PTY & child cleanup)
      → stop watcher · git-reconcile vs baseline (path-keyed) ·
        missing file_change rows · trace_end
  └─ reconcile-on-startup: any trace_start with no trace_end whose session
     is absent → same finalize path (correctness backstop, ADR-001 §4.1.5)
```

The driver's `LaunchMode()` is informational in v0.1 (both shipped drivers
return `Interactive`); it is logged here and will drive the card-detail view
when `run` mode ships (§12).

### 4.4 Trade-off analysis & decision rationale

**Agent abstraction (from ADR-002 §2.3, restated for self-containment):**

| Option | How it works | Cost | Buys | First to break |
|--------|--------------|------|------|----------------|
| **A. Driver interface (chosen)** | `Driver` iface (`Name`/`Resolve`/`LaunchMode`/`Launch`), one impl per agent; SessionManager calls the card's driver (§5, §10.2) | One interface + two impls + a ~1-line `ensure` refactor | Clean N-agent extension; per-driver test isolation; loom owns the launch contract (absolute-path resolution, quoting, probe) | If agents diverge beyond the 4 methods, the interface grows — guarded by §15 risk row and YAGNI (no dynamic registration) |
| **B. Config-driven branch** | `switch agent.Name` inside the session-command builder; no new types | Lowest; no interface | No ceremony | The branch grows per agent; launch semantics and tests duplicate — the second agent is exactly where this stops paying |
| **C. External launcher scripts** | Loom shells out to a per-agent script the user maintains | No loom code per agent | Users can add agents without recompiling | Pushes tmux/quoting/probe/absolute-PATH guarantees into user scripts; a bad script silently mis-records runs; violates C1 (agents become loom-adjacent code) |

**Decision:** A. The launch contract is a loom guarantee that must not be
delegated to scripts (C), and a second agent is the threshold where a branch
(B) becomes the thing it would replace. A is small — four methods, two of them
one-liners — and pays for itself at agent #2.

**Session substrate (ADR-001 §2.3 + confirmed 2026-08-09):** the tmux session
model was re-affirmed over the two rejected alternatives, because it is the
mechanism that makes "interactive" and "resume" *cheap*: the session IS the
live conversation (resume is free, no `--resume`/`-s` bookkeeping), N cards
run concurrently with near-zero loom code, and completion detection ("session
disappeared") is exactly what makes claude and opencode interchangeable.
Costs accepted: `tmux` as an external runtime dep (C2), the `$SHELL -c`
quoting ceremony (§8 — exists only because of tmux), and nested-tmux handling.
Rejected: direct `tea.ExecProcess(agent, argv)` pop-over (conversation dies
with the attach; one foreground card; loom parents the child) and a loom-owned
PTY supervisor + daemon (SIGWINCH, child cleanup, process groups — the exact
concerns ADR-001 §2.3 removed).

---

## 5. The Driver Contract (ADR-002 §3.1, corrected)

```go
package agent

// LaunchMode is the agent's completion semantics — what "the session
// disappeared" means. v0.1 ships only Interactive.
type LaunchMode string

const (
    LaunchModeInteractive LaunchMode = "interactive" // user quits the agent REPL/TUI
    LaunchModeRun         LaunchMode = "run"         // task completes (opencode run; future §9)
)

// SessionSpec is the driver's contribution to the session command.
type SessionSpec struct {
    Argv     []string // argv[0] is the absolute binary; every element is
                      // single-quoted at join time (§8)
    SendKeys string   // literal tmux key name (e.g. "Enter") sent AFTER the
                      // startup probe passes; "" = nothing to send. Never
                      // shell-quoted (not part of the $SHELL -c commandline).
}

// Driver is the contract loom needs from a coding agent. Drivers do NOT own
// the session lifecycle; they only produce the command that runs inside the
// session and describe what its end means.
type Driver interface {
    // Name is the agent identifier used in cards.agent and config.toml.
    Name() string // "claude" | "opencode"

    // Resolve returns the absolute path to the agent binary via exec.LookPath
    // in loom's own environment (ADR-001 §4.1 step 0). The tmux server
    // inherits its client's environment, which is frequently not loom's.
    Resolve(cfg *config.Config) (string, error)

    // LaunchMode is the completion semantics.
    LaunchMode() LaunchMode

    // Launch builds the argv for the card's tmux session command. exe is the
    // path Resolve returned (argv[0]); the card context is built by
    // BuildPrompt (§7). Cwd is set by tmux -c, never here.
    Launch(exe string, card Card, cfg *config.Config) (SessionSpec, error)
}
```

### 5.1 Registry

```go
var drivers = map[string]Driver{
    "claude":   claudeDriver{},
    "opencode": opencodeDriver{},
}

// Get returns the named driver. Unknown names error here (programmer error,
// guarded by validation below) — no dynamic registration in v0.1.
func Get(name string) (Driver, error)

// Known returns the sorted known names, for CLI/config validation (C8).
func Known() []string // ["claude", "opencode"]
func IsKnown(name string) bool
```

### 5.2 Two signature corrections over ADR-002 §3.1

| ADR-002 as drawn | Correction | Why |
|---|---|---|
| `Resolve() (string, error)` | `Resolve(cfg *config.Config) (string, error)` | The binary is per-agent **config** (`[agent.claude] binary`); a stateless driver can't resolve without it. |
| `Launch(card Card, cfg *Config) (SessionSpec, error)` | `Launch(exe string, card Card, cfg *config.Config) (SessionSpec, error)` | Prevents a double `LookPath` (ensure resolves at step 0, then Launch would re-resolve). `argv[0] = exe`. |

Both are the ADR's own contract made precise; the ADR's ensure sequence
(§3.1: resolve → launch → new-session) is preserved verbatim. Amend ADR-002
§3.1 at adoption.

---

## 6. Card Projection & AgentOrDefault

`agent` stays store-free; `session` maps a `store.Card` into an `agent.Card`
that carries the already-resolved agent name:

```go
// agent/card.go
type Card struct {
    ID    string
    Title string
    Description string
    Objective string
    AcceptanceCriteria string
    Agent string // already resolved: card.agent ?? [agent] default; "" never sent here
}

// store/cards.go — the DB row carries a nullable column.
type Card struct {
    ID, ColumnID, BoardID, WorkspaceID string
    CodebaseID *string
    Title, Description, Objective, AcceptanceCriteria string
    Priority string
    Labels string
    Agent *string // NEW: NULL = follow [agent] default at launch time
    Position int
    CreatedAt, UpdatedAt string
}

// AgentOrDefault resolves NULL at launch time (not write time) so a later
// config change re-defaults NULL cards — the expected behavior (ADR-002 §6).
func (c Card) AgentOrDefault(def string) string {
    if c.Agent != nil && *c.Agent != "" { return *c.Agent }
    return def
}
```

`session.Manager` builds the projection once:

```go
func (m *Manager) cardForAgent(c store.Card) agent.Card {
    return agent.Card{
        ID: c.ID, Title: c.Title, Description: c.Description,
        Objective: c.Objective, AcceptanceCriteria: c.AcceptanceCriteria,
        Agent: c.AgentOrDefault(m.cfg.Agent.Default),
    }
}
```

---

## 7. Shared Prompt Construction (ADR-001 §4.5, generalized)

Both drivers call the same builder. Template identical to ADR-001 §4.5
(title always present; description/objective/acceptance_criteria omitted when
empty). Stored in `agent/prompt.go` so `session` never builds prompts.

```go
func BuildPrompt(c Card) string {
    var b strings.Builder
    b.WriteString(c.Title)
    if s := strings.TrimSpace(c.Description); s != "" {
        b.WriteString("\n\n## Description\n" + s)
    }
    if s := strings.TrimSpace(c.Objective); s != "" {
        b.WriteString("\n\n## Objective\n" + s)
    }
    if s := strings.TrimSpace(c.AcceptanceCriteria); s != "" {
        b.WriteString("\n\n## Acceptance Criteria\n" + s)
    }
    return b.String()
}
```

---

## 8. Quoting (generalized to every argv element)

tmux runs the session command via `$SHELL -c`, so every argv element is
POSIX single-quoted. This is ADR-002 §4.4's generalization: the old
"quote the prompt" rule becomes "quote all elements". `SendKeys` is never
quoted (it is a tmux key name, not part of the commandline).

```go
// escape.go
func PosixEscape(s string) string {
    return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// CommandLine joins every argv element with PosixEscape.
func CommandLine(argv []string) string {
    out := make([]string, len(argv))
    for i, a := range argv { out[i] = PosixEscape(a) }
    return strings.Join(out, " ")
}
```

---

## 9. Driver Implementations

### 9.1 claudeDriver — ADR-001 behavior, moved behind the driver

```go
func (claudeDriver) Name() string          { return "claude" }
func (claudeDriver) LaunchMode() LaunchMode { return LaunchModeInteractive }

func (claudeDriver) Resolve(cfg *config.Config) (string, error) {
    return exec.LookPath(cfg.Agent.Claude.Binary) // default "claude"
}

func (claudeDriver) Launch(exe string, card Card, cfg *config.Config) (SessionSpec, error) {
    argv := []string{exe, BuildPrompt(card)}
    if m := cfg.Agent.Claude.Model; m != "" {           // parity with generic `model`
        argv = append(argv, "--model", m)
    }
    return SessionSpec{Argv: argv, SendKeys: ""}, nil
}
```

- Interactive argv: `["<abs-claude>", "<context>"]` — exactly today's command.
- `SendKeys` empty; completion == the user quits the claude REPL.
- `prompt_model` from ADR-001 §5 is renamed `model`; behavior unchanged.

### 9.2 opencodeDriver — interactive (ships)

```go
func (opencodeDriver) Name() string          { return "opencode" }
func (opencodeDriver) LaunchMode() LaunchMode { return LaunchModeInteractive }

func (opencodeDriver) Resolve(cfg *config.Config) (string, error) {
    return exec.LookPath(cfg.Agent.Opencode.Binary) // default "opencode"
}

func (opencodeDriver) Launch(exe string, card Card, cfg *config.Config) (SessionSpec, error) {
    o := cfg.Agent.Opencode
    argv := []string{exe}
    switch o.Interface {
    case "full":
        argv = append(argv, "--prompt", BuildPrompt(card))   // full TUI
    default: // "mini" (default)
        argv = append(argv, "--mini", "--prompt", BuildPrompt(card)) // split-footer REPL
    }
    if m := o.Model; m != ""               { argv = append(argv, "--model", m) }
    if a := o.OpencodeAgent; a != ""       { argv = append(argv, "--agent", a) }
    if o.AutoApprove                       { argv = append(argv, "--auto") }
    return SessionSpec{Argv: argv, SendKeys: ""}, nil
}
```

- `interface = "mini"` (default) → `["<abs-opencode>", "--mini", "--prompt", "<ctx>"]` (P1: auto-submits).
- `interface = "full"` → `["<abs-opencode>", "--prompt", "<ctx>"]` (P6; auto-submit assumed shared — **hard-gated** on the Phase 0 full-TUI probe (§3.2.3) before this value ships; until verified, a config setting of `"full"` fails startup validation).
- `SendKeys = ""` (P1/P2: `--prompt` auto-submits; the REPL stays alive after a turn). If a future opencode version regresses to prefill-only, flip to `"Enter"`. **Scope of the visibility guarantee:** while attached, a prefill regression is visible; `--detach` is NOT — a detached session would idle at its prompt as a live-but-empty `● running` run. The §16 auto-submit canary (post-probe `capture-pane` asserting the pane shows the model's response, not the prefilled input) makes a regression fail a test instead of silently idling detached runs (review finding F5).
- Pass-throughs appended after the prompt, matching P6 flag names.

### 9.3 Completion semantics (unchanged mechanism, per agent)

| Agent / mode | argv (excerpt) | Session disappears when | Recorded as |
|---|---|---|---|
| claude interactive | `<abs-claude> '<ctx>'` | user quits REPL | `trace_end`, reconcile |
| opencode `--mini` / TUI | `<abs-opencode> --mini --prompt '<ctx>'` | user quits REPL/TUI (P2) | `trace_end`, reconcile |
| opencode `run` (future §12) | `<abs-opencode> run '<ctx>'` | task completes / idle | `trace_end`, reconcile ("done") |

Detection is identical in all cases — the session vanishes — so the poll,
probe, and reconcile-on-startup are agent-agnostic. Only the human reading
`● running` infers *why*.

---

## 10. SessionManager Refactor

### 10.1 tmux client wrapper (`session/tmux.go`)

Thin, testable wrapper bound to the configured server (`-L loom`):

```go
type Tmux struct { Server string; bin string } // bin resolved once at startup

func (t Tmux) NewSession(name, cwd, command string) error // new-session -d -s <name> -c <cwd> "<command>"
func (t Tmux) HasSession(name string) (bool, error)       // has-session -t <name>
func (t Tmux) CapturePane(name string) string             // capture-pane -p -t <name>
func (t Tmux) SendKeys(name, keys string) error           // send-keys -t <name> <keys>
func (t Tmux) KillSession(name string) error              // kill-session -t <name>
func (t Tmux) ListSessions() ([]string, error)            // list-sessions -F '#{session_name}'

func SessionName(id string) string { return "loom-" + id }
```

### 10.2 `ensure` — the only changed method (ADR-002 §3.1)

```go
// ensure creates-or-reuses the card's session. Called by Enter and
// `loom card open [--detach]`. Only steps 1–3 and 7 touch the driver.
//
// Refinement over ADR-001 §4.1 step 1 (write-then-probe): the run is
// RECORDED AFTER the probe passes — the git baseline is snapshotted before
// launch and held, the trace_start row is written only once the session is
// proven alive. This closes the C7 race where a concurrent reconcile-on-
// startup could finalize a failed launch inside the write→probe window, and
// it means a failed launch never writes a row at all (§10.2 invariants).
// A second refinement: ensure KILLS any session it created on every error
// path after creation — a session is never left alive without a trace_start
// row (the poll/reconcile can only complete runs they can see).
func (m *Manager) ensure(ctx context.Context, c store.Card) error {
    name := SessionName(c.ID)
    if ok, _ := m.tmux.HasSession(name); ok { return nil } // reuse, no new run

    ac := m.cardForAgent(c)                                 // §6
    driver, err := agent.Get(ac.Agent)                      // 1
    if err != nil { return err }                            //   (CHECK'd; programmer error)

    exe, err := driver.Resolve(m.cfg)                       // 2 — fail the open, no trace_start (C7)
    if err != nil {
        return fmt.Errorf("%s: not found in PATH (install it or set [agent.%s] binary)",
            ac.Agent, ac.Agent)
    }
    spec, err := driver.Launch(exe, ac, m.cfg)              // 3
    if err != nil { return err }

    root := m.watchRoot(c)                                  // codebase path ?? workspace root (ADR-001 §4.6)
    baseline := m.trace.SnapshotBaseline(root)              // git pair captured BEFORE launch, held
    if err := m.tmux.NewSession(name, root, agent.CommandLine(spec.Argv)); err != nil { return err } // 4

    var runID string
    success := false
    defer func() {                                          // close the leak on ANY post-create error
        if !success {
            _ = m.tmux.KillSession(name)                    // idempotent; no-op if already gone
            if runID != "" {
                m.trace.AbortRun(ctx, runID)                // DELETE whole run + stop watcher; idempotent
            }
        }
    }()

    probePane := m.tmux.CapturePane(name)                   // 6a — capture DURING the window (F2:
    alive := m.probeAlive(name)                             //      after absence is known the -L server
    if !alive {                                             //      may already be gone with exit-empty)
        if p := m.tmux.CapturePane(name); p != "" { probePane = p } // last-chance scrollback, else ""
        return fmt.Errorf("%s session failed to start: %s", ac.Agent, probePane) // defer kills; no row ever written
    }

    runID, err = m.trace.StartRun(ctx, c.ID, root, baseline) // 5 — recorded AFTER the probe (F3/C7)
    if err != nil { return err }                            //     defer kills session; no partial run
    m.trace.Watch(ctx, c, root, runID)                      //     fsnotify watcher scoped to runID

    if spec.SendKeys != "" {                                // 7 — key name, never quoted
        if err := m.tmux.SendKeys(name, spec.SendKeys); err != nil { return err } // defer aborts run + kills
    }
    success = true
    return nil
}
```

**Invariants this section guarantees (regression-test these):**
1. A session created by `ensure` is always either (a) returned as successfully
   running with a live `trace_start` row and watcher, or (b) killed by the
   defer — no live session exists without its run.
2. A launch that never got off the ground writes **no** trace rows at all
   (probe precedes `StartRun`), so no concurrent reconcile can finalize it,
   and C7 holds by construction.
3. `probeAlive` waits ~500ms then re-checks `HasSession`; a transient tmux
   client error (server socket absent) is retried once before declaring
   failure, so a one-off `tmux` hiccup cannot kill a booting session.
4. `AbortRun` deletes the **whole run** (`DELETE FROM traces WHERE run_id = ?`)
   and stops the watcher — it never leaves an orphaned `trace_end` or
   `file_change` behind.

`attach`, `kill`, `status`, `reconcile-on-startup` are byte-for-byte
unchanged from ADR-001 (they operate on session names and traces only, never
on the driver). The `LaunchMode()` value is informational in v0.1; it is
logged and will drive the card-detail view when §12 ships.

### 10.3 What is deliberately NOT in the driver

- cwd selection (`watchRoot`) — tmux `-c`, SessionManager-owned.
- Session naming, probe timing, poll cadence — SessionManager-owned.
- Prompt construction — `agent.BuildPrompt`, shared.
- `--dir`/`--title`/`-c`/`-s`/`--fork` (opencode session flags) — **not**
  passed. Resume within a session is the tmux session's job (ADR-001 §4.4,
  §8 "Lost context"); loom never manages resume. These belong to the §12
  future `run` mode.

---

## 11. Config Schema (ADR-002 §5)

`~/.config/loom/config.toml` — resolved via `os.UserConfigDir()` + `"/loom/config.toml"`.

```go
type Config struct {
    Agent    AgentConfig    `toml:"agent"`
    Session  SessionConfig  `toml:"session"`
    Database DatabaseConfig `toml:"database"`
}
type AgentConfig struct {
    Default  string         `toml:"default"`              // "claude" | "opencode"; validated
    Claude   ClaudeConfig   `toml:"claude"`
    Opencode OpencodeConfig `toml:"opencode"`
}
type ClaudeConfig struct {
    Binary string `toml:"binary"` // resolved at open (§10.2 step 2); default "claude"
    Model  string `toml:"model"`  // --model if set; empty = agent default
}
type OpencodeConfig struct {
    Binary       string `toml:"binary"`       // default "opencode"
    Model        string `toml:"model"`        // --model (provider/model)
    OpencodeAgent string `toml:"opencode_agent"` // --agent (named opencode agent)
    Interface    string `toml:"interface"`    // "mini" (default) | "full"
    AutoApprove  bool   `toml:"auto_approve"` // --auto
}
type SessionConfig struct {
    TmuxServer string `toml:"tmux_server"` // default "loom"
    Prefix     string `toml:"prefix"`      // default "C-a"
}
type DatabaseConfig struct {
    Path string `toml:"path"` // ~/.config/loom/loom.db; ~ expanded at load
}
```

**Validation (fail fast with accepted values, C8):** missing file → `Default()`
(claude/opencode binaries, `interface = "mini"`, `default = "claude"`); present
file → parse then `Validate()`. `config.Validate()` checks only **config-local**
values: `Opencode.Interface` ∈ `{"mini", "full"}`. The cross-package check
`Agent.Default` ∈ `agent.Known()` lives in `agent.Validate(cfg)` (called from
`main` at startup) — see the §4.2 dependency rule, which forbids `config` from
importing `agent` (review finding F4). `[claude]`/`prompt_model` keys from
ADR-001 §5 are superseded — a `prompt_model` key still present is a config
validation error naming `model` (loud migration, no silent default).

---

## 12. Schema Migration (ADR-002 §6 — verified legal, P-section 3.1)

```sql
-- 00002_card_agent.sql
-- +goose Up
ALTER TABLE cards ADD COLUMN agent TEXT
    CHECK (agent IN ('claude', 'opencode'));

-- +goose Down
ALTER TABLE cards DROP COLUMN agent;
```

Verified against the installed sqlite: `ADD COLUMN ... CHECK` accepts valid +
NULL rows, rejects `bogus` with `CHECK constraint failed` (exit 19); `DROP
COLUMN` restores the original schema. `00001_initial.sql` carries the full
ADR-001 §3.3 DDL unchanged. No new tables; `traces` untouched. Existing rows
migrate `agent = NULL` → follow the new global default (`claude`), preserving
ADR-001 behavior for untouched configs.

### 12.1 Data-model surface (what ADR-002 touches)

| Table | Change | Notes |
|-------|--------|-------|
| `cards` | **+ `agent TEXT NULL CHECK(agent IN ('claude','opencode'))`** | `NULL` = late-bound to `[agent] default` at launch (§6). Only the store's `cards` CRUD + a new `Agent *string` field change. |
| `traces` | None | `run_id`/`seq` ordering, `data_json` shapes, and the run-lifecycle partial UNIQUE index all untouched (ADR-002 §2.2). |
| `workspaces` · `boards` · `columns` · `codebases` | None | Untouched. |
| `ui_state` | None | Untouched. |

No migration touches a `REFERENCES` target ordering concern: `00002` is a
single-column `ALTER` on the existing `cards` table, so the ADR-001
§3.3 table-order rule is unaffected.

---

## 13. CLI Surface (ADR-002 §7.1)

```
loom card add <title> ... [--agent claude|opencode]        # empty = default
loom card update <id> ... [--agent claude|opencode]        # --agent= resets to default (NULL)
loom card list ...                                         # shows an agent badge column
loom config                                                # prints the §11 schema
```

- `--agent` values are validated against `agent.Known()` when non-empty (C8).
- `--agent=` (empty) on `update` clears the column to NULL. Distinguish
  "flag absent" from "flag present/empty" via `flag.Visit` (stdlib `flag`
  cannot tell them apart otherwise).
- `loom card open`, `--detach`, `close`, `attach`, `sessions` — **unchanged**
  (ADR-001 §6). They operate on the card; the card's driver decides the
  command. No `--run` flag (§12).
- CLI dispatch: small stdlib-`flag` router in `internal/cli` (subcommand table
  mirroring ADR-001 §6). No cobra dependency (keeps C1 lean; the surface is
  fixed and fully enumerated). Cobra is an acceptable substitute if the
  implementer prefers.

---

## 14. TUI Surface (ADR-002 §7.2)

- **Card badge:** short agent tag in each column cell next to priority/labels
  — `cl` for claude, `oc` for opencode, resolved from
  `card.AgentOrDefault(cfg.Agent.Default)` via the existing per-card render
  delegate.
- **`n` / `e`:** agent picker in the form — empty = default, plus `claude` /
  `opencode`. Default form value = the card's resolved agent.
- **`d` (detail):** "Agent: claude | opencode (default)" field.
- **Keybindings:** unchanged (ADR-001 §3.5); `Enter` opens via the card's
  agent. Session markers `●`/`◉` unchanged — a running opencode session shows
  exactly like a running claude session.

---

## 15. Risks & Open Questions

| Risk | Severity | Mitigation |
|------|----------|------------|
| opencode CLI churn (`--mini`/`--prompt`/`--auto` semantics shift) | Low–Med | Pin the verified version range in `loom docs`/README; re-run the §3 probes on each opencode major bump. The `SendKeys` field absorbs a prefill-only regression. |
| Full-TUI `--prompt` auto-submit unverified | **Gate** | `interface = "full"` is **hard-gated** on the Phase 0 full-TUI probe (§3.2.3); the value fails startup validation until auto-submit is confirmed. Not shipped unverified. |
| opencode version regresses `--prompt` to prefill-only | Low | §16 auto-submit canary (post-probe `capture-pane` shows the model's response, not the prefilled input) fails a test instead of silently idling a `--detach` run (review finding F5). While attached it is visible; the canary covers the unattached case. |
| opencode permission model ≠ claude | Medium | opencode permissions are config/agent-driven; "ask" rules prompt in the pane while attached; `auto_approve` passes `--auto` (§11). User-owned config, same posture as claude. |
| opencode binary missing / too old | Low | `Resolve` fails the open with an install hint (§10.2). A launch that never gets off the ground writes **no** trace rows (probe precedes `StartRun`, §10.2) — C7 holds by construction. |
| **Live session with no trace row** (post-`NewSession` error path) | Low | `ensure`'s deferred `KillSession` + whole-run `AbortRun` guarantee a session is never left alive without its run (review finding F1; §10.2 invariants 1–2). |
| Nested tmux / `interface = "full"` | Low | Same nested-tmux handling as claude (ADR-001 §4.4); opencode renders in the attach pane. |
| Interface over-abstraction (drivers diverge) | Low | Four small methods; a driver needing more implements a private optional interface (Go idiom). No dynamic registration (YAGNI). |
| **ADR-002 §1.2 factual error** (`run --interactive` "rejected") | Doc | Corrected in §3.2.2; amend ADR-002 at adoption so implementers aren't misled. |
| Mislabeled "done" (run-mode semantics) | Low (future) | Not enabled in this change (§12); when it lands, the card-detail view shows launch mode beside `●`/`◉`. |

**Open questions that do not block implementation:** whether the Phase 0
full-TUI probe confirms auto-submit (blocks `interface = "full"` only, §3.2.3);
whether to expose `run --interactive` as a third interface option (deferred —
ADR chose `--mini`; §12 keeps it).

---

## 16. Verification Matrix

| Layer | What | How |
|-------|------|-----|
| **Unit — driver** | argv per driver/card/config (table-driven: claude positional vs opencode `--mini --prompt`; pass-throughs only when set; `interface = "full"` path); `Resolve` honors `[agent.*] binary`; `AgentOrDefault` (NULL → default, explicit → card value, late-bound to config) | `internal/agent` + `internal/config` table tests |
| **Unit — quoting** | `PosixEscape` idempotent on strings containing `'` and newlines; `CommandLine` joins every element, never the prompt alone | `internal/agent` |
| **Unit — config** | Missing file → defaults; bad `default`/`interface` → fail fast with accepted values; `prompt_model` → error naming `model` | `internal/config` |
| **Unit — schema** | Migration up/down; `agent` default NULL; CHECK rejects unknown names; existing rows migrate NULL | `internal/store` |
| **Integration — per driver** | Parametrized stub `claude` + stub `opencode` scripts (ADR-001 §10): `loom-<id>` session name, cwd, quoted argv visible in `capture-pane`; session-end → `trace_end`; `loom card close` kills + finalizes; git reconciliation attributes changes | `internal/session` against real `tmux` |
| **Failure paths** | **Nonexistent binary** per driver → open fails at `Resolve` with `not found in PATH`, **no** session created, no pane to capture (assert the message; don't assert pane text); **resolved-but-runtime-failing stub** (prints to stderr, exits 1) → probe fails, session killed, **no** `trace_start` row (C7); missed-completion (<2s stub) → reconcile-on-startup finalizes exactly one `trace_end`; **post-create error** (force `StartRun` failure) → deferred `KillSession` leaves no session and no trace rows (§10.2 invariants) | `internal/session` |
| **Regression canary** | Auto-submit still works | Post-probe `capture-pane` in the integration test asserts the first pane line is the **model's response**, not the prefilled input — a future opencode prefill-only regression fails this test instead of silently idling detached runs (review finding F5) | `internal/session` |
| **TUI** | Badge rendering, `n`/`e` picker, detail field, keybindings unchanged | BubbleTea test framework |
| **E2E** | Manual: board → card → open with claude → detach → reattach → trace on completion; same with `default = "opencode"` | Manual (coverage-bar exception per ADR-001 §10) |

The ADR-001 coverage bar and its explicit manual-E2E exception are unchanged;
the handoff to a real interactive agent is now exercised for two agents.

---

## 17. Implementation Plan

The ADR-001 phases (scaffold → TUI+launch → CLI+polish) run unchanged; the
agent layer is **designed-in from the start** rather than refactored in,
because the repo is docs-only today. ADR-002 §11's "Phase A is a green-suite
refactor" therefore becomes "Phase 1 builds the abstraction directly".

| Phase | Scope | Days |
|-------|-------|------|
| **0 — Feasibility** | **Done** (§3): `--mini --prompt` auto-submit (P1), REPL stays alive (P2), TTY requirement (P3), `run --mini` rejection (P4), `run --interactive` validity (P5). **Residual (gate):** full-TUI `--prompt` probe — required before `interface = "full"` can ship (§3.2.3). | 0.25 |
| **1 — Scaffold + store + config + agent** | go.mod, deps (BubbleTea v2.0.7, Bubbles, LipGloss, Glamour, modernc sqlite, goose v3, fsnotify — ADR-001 §2.2); `00001` + `00002` migrations; store CRUD with `Agent` column; `config` load/validate; `agent` package (Driver, registry, escape, prompt, claude/opencode drivers); store/agent/config unit tests | 2–3 |
| **2 — SessionManager + TUI** | `tmux` wrapper; `ensure` per §10.2 (steps 1–3, 7 driver-owned); attach/kill/status/probe/reconcile unchanged; board TUI with agent badge; `n`/`e` picker; `d` detail field; trace recorder + watcher + git reconcile | 5–7 |
| **3 — CLI + polish + verification** | `--agent` flags (add/update, `--agent=` reset, badge in list); `loom config`; failure-path + parametrized stub-driver integration tests; docs (ADR-002 → Adopted with §3.2 corrections) | 2–3 |

**Total: ~10–14 days** (ADR-001's range; ADR-002's agent layer adds ≈0.5d net
for the two drivers + config/schema, per ADR-002 §11).

---

## 18. Handoff

Implementation is ordered by §17 and bounded by C1–C8. The driver contract is
`internal/agent` (§5), the only changed lifecycle code is `session.Manager.ensure`
(§10.2), and everything else in ADR-001 is untouched by design. Hand off to
`feature-dev` when the Phase 0 full-TUI probe and this design are approved.
