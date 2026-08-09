# ADR-002: Multi-Agent Support — opencode as a Second Coding Agent

**Status:** Proposed
**Date:** 2026-08-09
**Author:** MiMo Agent
**Area:** Architecture, Terminal UI, Agent Abstraction

---

## Amendment Log

| Date | Change |
|------|--------|
| 2026-08-09 | Pre-adoption design review (`./DESIGN-002-loom-multi-agent.md`). **Verified facts corrected** (§1.2): `opencode run --interactive` **is** a valid interactive split-footer mode in v1.18.15 (probe: accepted, enters the REPL) — the ADR's original claim that it is rejected is wrong; the rejected combination is `run --mini` (its error message is the one originally quoted). Phase 0 also resolved the auto-submit question: `opencode --mini --prompt '<ctx>'` **auto-submits**, and the mini REPL **stays alive after a turn** (session does not exit on idle) — so the §4.2 `SendKeys` default is `""`, not `"Enter"`. **Driver signatures corrected** (§3.1): `Resolve(cfg *Config)` (the binary is per-agent config) and `Launch(exe string, card Card, cfg *Config)` (avoids a double `LookPath`; `argv[0] = exe`). Migration (§6) verified legal against SQLite (`ADD COLUMN ... CHECK` + `DROP COLUMN`). |
| 2026-08-09 | Code-review challenge (DESIGN-002) closed five findings, refining the §3.1 ensure sequence at the blueprint level (no driver/contract change): **(1)** `ensure` records the run **after** the startup probe passes (baseline snapshotted before launch, `trace_start` written after) — a failed launch writes no trace row at all, closing the race where a concurrent reconcile-on-startup could finalize a never-launched session as a completed run; **(2)** `ensure` kills any session it created on every post-creation error path, so a session is never left alive without its `trace_start`; **(3)** the startup probe captures pane text **during** the probe window (after absence is known, the `-L loom` server may already be gone); **(4)** agent-name validation lives in `agent.Validate(cfg)`, called at startup — `config` stays a leaf (no `config → agent` import cycle); **(5)** `interface = "full"` is hard-gated on the Phase 0 full-TUI auto-submit probe until confirmed. Full detail in DESIGN-002 §10.2, §11, §15, §16. |

---

## 1. Context

### 1.1 The Change

[ADR-001](./ADR-001-loom-architecture.md) designs loom as a CLI-native Kanban
task tracker that launches exactly one coding agent — Claude Code (`claude`) —
as the command of a detached `-L loom` tmux session per card. The user drives
the agent by attach/detach; the human is in the loop by choice.

This ADR generalizes the agent layer so that **opencode** is a first-class,
drop-in second agent. The goal: a card (or, by default, the whole board) can
run its agent as either `claude` or `opencode`, with the same tmux session
lifecycle, the same completion detection, the same file-change tracing, and the
same attach/detach human-in-the-loop — with **no change to the data-flow core**
in ADR-001 §3.2.

### 1.2 Facts Verified About opencode (v1.18.15 installed, docs + source)

- `opencode` (no subcommand, optional `[project]` positional) starts the full
  TUI; `--mini` starts a minimal split-footer interactive interface; both accept
  a global `--prompt <text>` to seed the first message.
- `opencode run [message..]` is **non-interactive**: it sends a prompt, streams
  formatted events to stdout, and **exits when the session goes idle** — i.e.
  when the task completes. This is a full agent loop, *not* a one-shot
  `claude -p`-style single turn.
- `opencode run --interactive` **is** a valid interactive mode in v1.18.15
  (verified by probe: accepted, enters the split-footer REPL) — the rejected
  combination is `run --mini`, whose error is `--mini must be used without the
  run subcommand`. The shipped interactive interface is the top-level `--mini`
  (§4.2), not the `run` subcommand.
- Session model: `-c/--continue`, `-s/--session <id>`, `--fork`,
  `--title <name>`, `--dir <path>`, `--agent <name>`, `-m/--model
  <provider/model>`, `--auto` (approve permissions not explicitly denied).
- Permissions are config/agent-driven; "ask" rules prompt interactively in the
  pane, which works while attached.
- `--mini` requires a TTY stdout; a tmux session pane is a PTY, so this holds.
- `--prompt` sets the TUI's input value (`route.prompt` → `r.set(...)`); **it
  auto-submits in mini mode** — verified by probe (2026-08-09, v1.18.15), and
  the mini REPL stays alive after a turn. Residual: full-TUI (`interface =
  "full"`) auto-submit is assumed to share this and is confirmed in Phase 0
  (§11).

### 1.3 Guiding Principles Affected

| Principle (ADR-001 §1.3) | Impact |
|--------------------------|--------|
| **Single binary** | Unchanged. No new dependencies. opencode joins `claude` as a user-owned external runtime dep, not loom code. |
| **Kanban as task tracking** | Unchanged. Opening a card launches *its agent* (per-card, §2.1). |
| **Simplicity over automation** | Preserved. Interactive launch remains the only shipped mode in this change; opencode's non-interactive `run` mode is designed as an extension point (§9) but not enabled. |
| **Terminal-native / Go for TUI** | Unchanged. |

---

## 2. Decision

### 2.1 Primary Decision

**Introduce an `AgentDriver` interface, two implementations (`claude`,
`opencode`), a per-card agent selection with a global default, and route all
launch/tracing through the driver.** Cards gain a nullable `agent` column
(`NULL` = the global `[agent] default`). The tmux session lifecycle, startup
probe, 2s poll, reconcile-on-startup, fsnotify watcher, and git-baseline
reconciliation in ADR-001 §4–§5 are **agent-agnostic and unchanged**; only the
session command's argv (and its completion *meaning*) is driver-owned.

Confirmed scope decisions (2026-08-09):
- **Per-card agent with global default** — a card remembers its agent; `NULL`
  falls back to `[agent] default`. Small schema change (§6), badge in the TUI
  (§7).
- **Autonomous mode designed, not shipped** — opencode's `run` mode is captured
  as a `LaunchMode` extension point (§9) with a future `loom card open --run`
  flag, but v0.1.x ships only interactive opencode, consistent with
  "Simplicity over automation".
- **Captured as ADR-002** — supersedes the claude-specific agent touchpoints of
  ADR-001 listed in §2.2, rather than amending ADR-001 in place.

### 2.2 What This Supersedes in ADR-001

| ADR-001 location | Change |
|------------------|--------|
| §1.3 "Single binary" | External runtime deps are now `tmux` plus **the configured coding agent (claude and/or opencode)**. |
| §2.1 Primary Decision | "launches Claude Code" → "launches the card's agent (claude or opencode)". |
| §3.2 data-flow diagram | "claude TUI" → "agent's native TUI"; the session command is built by the card's driver (§4 of this ADR). |
| §3.3 `traces` comments | "during Claude sessions" → "during agent sessions". No schema change to `traces`. |
| §3.5 keybindings | `Enter`/`loom card open` launch the card's agent; no key change. |
| §4.1 step 0 | `config.claude.binary` → the card's resolved agent binary (per-card, §5 config). |
| §4.5 Prompt Construction & Quoting | Prompt fields unchanged; quoting generalized from "the prompt" to "every argv element" (§4.4 of this ADR). |
| §5 config schema `[claude]` block | Superseded by the `[agent]` + per-agent tables (§5 of this ADR). `prompt_model` → `model`. |
| §7 comparison | "Agent model" row covers both agents. |
| §8 "Lost context" risk | opencode's `--continue`/`-s` mapped in; loom still never manages resume. |
| §10 verification | Stub agent parametrized per driver (§11 of this ADR). |

### 2.3 Options Considered (with trade-off table)

| Option | How it works | Cost | Buys | First to break |
|--------|--------------|------|------|----------------|
| **A. Driver interface** (chosen) | `AgentDriver` interface (`Name`/`Resolve`/`LaunchMode`/`Launch`), one impl per agent; SessionManager calls the card's driver. | One interface + two impls + a ~1-line SessionManager refactor. | Clean N-agent extension; per-driver test isolation; loom owns the launch contract (absolute-path resolution, quoting, probe) instead of delegating it. | If agents diverge beyond the 4 methods, the interface grows — guarded by §10 risk row and YAGNI. |
| **B. Config-driven branch** | A `switch agent.Name` inside the session-command builder; no new types. | Lowest; no interface. | No ceremony. | The branch grows per agent; launch semantics (probe/probe-binary-resolution) and tests duplicate; second agent is exactly where this stops paying. |
| **C. External launcher scripts** | Loom shells out to a per-agent script the user maintains. | No loom code per agent. | Users can add any agent without recompiling. | Pushes tmux/quoting/probe/absolute-PATH guarantees into user scripts — every script must re-implement loom's failure-path handling, and a bad script silently mis-records runs. Violates "Single binary" (agents become loom-adjacent code). |

**Decision rationale:** Option A — the launch contract (binary resolution in
loom's own environment, POSIX quoting, startup probe, completion semantics) is
a loom guarantee that must not be delegated to scripts (C), and a second agent
is the threshold where a branch (B) becomes the thing it would replace. A is
small: four methods, two of which are one-liners, and it pays for itself at
agent #2.

---

## 3. Agent Abstraction

### 3.1 The Driver Interface

Everything above the driver is shared and unchanged from ADR-001: the tmux
`-L loom` server, session naming (`loom-<cardid>`), `ensure`/attach/`kill`,
the ~500ms startup probe, the 2s poll, reconcile-on-startup, `run_id`/`seq`
trace ordering, and the fsnotify + git-baseline reconciliation.

```go
// AgentDriver is the contract loom needs from a coding agent. Drivers do not
// own the session lifecycle; they only produce the command that runs inside it
// and describe what that command's end means.
type AgentDriver interface {
    // Name is the agent identifier used in cards.agent and config.toml.
    Name() string // "claude" | "opencode"

    // Resolve returns the absolute path to the agent binary via
    // exec.LookPath in loom's own environment (ADR-001 §4.1 step 0). The
    // tmux server inherits its client's environment, which is frequently not
    // the shell loom was launched from. The binary is per-agent config
    // ([agent.<name>] binary), hence the cfg argument.
    Resolve(cfg *Config) (string, error)

    // LaunchMode is the agent's completion semantics, which dictates what
    // "the session disappeared" means:
    //   Interactive: session ends when the user quits the agent
    //                (claude REPL; opencode --mini / TUI). Default.
    //   Run:         session ends when the task completes
    //                (opencode run; future, §9).
    LaunchMode() LaunchMode

    // Launch returns the argv for the card's tmux session command. exe is the
    // absolute path Resolve returned (argv[0]), so the driver does not resolve
    // again. Loom joins each element with the POSIX single-quote escaper
    // (§4.4). Cwd is set by tmux -c to the watch-scope root (ADR-001 §4.6),
    // never by the driver.
    Launch(exe string, card Card, cfg *Config) (SessionSpec, error)
}

// SessionSpec is the driver's contribution to the session command.
type SessionSpec struct {
    Argv     []string // e.g. ["/abs/opencode", "--mini", "--prompt", "<context>"]
    SendKeys string   // optional literal keys to send AFTER the startup probe
                      // confirms the session is alive (e.g. "Enter" when the
                      // agent only pre-fills its prompt). "" = nothing to send.
}
```

`SessionManager.ensure` becomes:

1. `driver := agents.Get(card.AgentOrDefault(cfg))`
2. `exe, err := driver.Resolve()` — fail the open, no `trace_start`, on error.
3. `spec, err := driver.Launch(card, cfg)` — build argv; each element
   single-quoted (§4.4).
4. `tmux -L loom new-session -d -s loom-<id> -c <root> "<joined argv>"`
5. `trace_start`, watcher, startup probe as today.
6. After the probe passes, if `spec.SendKeys != ""`:
   `tmux -L loom send-keys -t loom-<id> Enter` (the key name, never quoted).

### 3.2 Registry

`agents.Get(name)` is a static map `"claude" → claudeDriver`,
`"opencode" → opencodeDriver`. Unknown names fail at card-add/edit time
(CHECK constraint, §6) and at `Get` (programmer error otherwise). No dynamic
agent registration in v0.1.x — that is what the launcher-script option (C)
would buy and it is rejected.

---

## 4. Launch Semantics

### 4.1 claude (unchanged behavior, moved behind the driver)

- Interactive argv: `["<abs-claude>", "<card context>"]` — exactly today's
  command, with the prompt as a POSIX single-quoted positional argument
  (ADR-001 §4.5). `SendKeys` empty.
- `LaunchMode: Interactive`. Completion == the user quits the claude REPL.
- `--model <v>` appended when `[agent.claude] model` is set (parity with the
  generic `model` knob; maps to claude's `--model` flag).
- `prompt_model` from ADR-001 §5 is renamed `model`; behavior unchanged.

### 4.2 opencode — interactive (ships in this change)

Default interface is the minimal split-footer REPL (`--mini`), selected via
`[agent.opencode] interface = "mini"`:

```
["<abs-opencode>", "--mini", "--prompt", "<card context>"]
```

- `interface = "full"` selects the full TUI instead: `["<abs-opencode>",
  "--prompt", "<card context>"]`.
- `LaunchMode: Interactive` — the REPL/TUI stays alive until the user quits
  (`/exit`, `ctrl+x q`), so session-existence == the user is still in the
  session, identical to claude. The 2s poll and reconcile-on-startup need no
  change.
- **Auto-submit caveat (resolved).** opencode's `--prompt` sets the input
  value and **auto-submits it in mini mode** — verified by probe (2026-08-09,
  v1.18.15), and the mini REPL stays alive after a turn. The driver ships with
  `SendKeys = ""`. The `SendKeys` field is retained on the contract: the
  full-TUI (`interface = "full"`) auto-submit is assumed to share this
  behavior but is confirmed in Phase 0 (§11), and the field absorbs a
  prefill-only regression in a future opencode version. The failure mode
  remains visible (the agent waits at its prompt), never a silently
  mis-recorded run.
- Config pass-throughs, appended to argv when set (validated in Phase 0):
  - `model` → `--model <provider/model>`
  - `opencode_agent` → `--agent <name>` (a named opencode agent/subagent)
  - `auto_approve = true` → `--auto` (approve permissions not explicitly
    denied; defaults to opencode's own config/ask behavior otherwise, which
    prompts in the pane while attached).

### 4.3 Completion semantics table

| Mode | Session command (excerpt) | Session disappears when | Recorded as |
|------|---------------------------|-------------------------|-------------|
| claude (interactive) | `<abs-claude> '<context>'` | user quits the REPL | `trace_end`, reconcile |
| opencode `--mini` / TUI (interactive) | `<abs-opencode> --mini --prompt '<context>'` | user quits the REPL/TUI | `trace_end`, reconcile |
| opencode `run` (future, §9) | `<abs-opencode> run '<context>'` | task completes / session idle | `trace_end`, reconcile — now meaning "done", not "closed" |

In all three the detection mechanism is identical — the session vanishes — so
the poll, the probe, and reconcile-on-startup are agent-agnostic. Only the
human reading `● running` infers *why*.

### 4.4 Prompt Construction & Quoting (generalized)

The prompt body is unchanged: title, description, objective, acceptance
criteria (ADR-001 §4.5). What generalizes is the quoting rule — from "the
prompt is one single-quoted arg" to "**every argv element is single-quoted**":

```
escape(s) = "'" + s.replace("'", "'\\''") + "'"
commandline = join(escape(a) for a in spec.Argv, " ")
```

tmux still executes via `$SHELL -c`, and the escaper is the same ~5-line
function, now applied uniformly. `SendKeys` values are tmux key names (e.g.
`Enter`), passed as-is via `tmux -L loom send-keys -t loom-<id> Enter` — never
shell-quoted (they are not part of the `$SHELL -c` commandline).

---

## 5. Config Schema

`~/.config/loom/config.toml`. Supersedes the `[claude]` block in ADR-001 §5
(`[claude]` → `[agent.claude]`, `prompt_model` → `model`). User intent only;
loom never rewrites the file (ADR-001 §5).

```toml
[agent]
default = "claude"          # "claude" | "opencode"; overridden per card (cards.agent)

[agent.claude]
binary = "claude"           # resolved to an absolute path at open (ADR-001 §4.1 step 0)
model = ""                  # --model if set (empty = agent's default)

[agent.opencode]
binary = "opencode"         # same absolute-path resolution
model = ""                  # --model (provider/model) if set
opencode_agent = ""         # --agent (named opencode agent) if set
interface = "mini"          # "mini" (split-footer REPL, default) | "full" (TUI)
auto_approve = false        # --auto: approve permissions not explicitly denied

[session]
tmux_server = "loom"        # unchanged
prefix = "C-a"              # unchanged

[database]
path = "~/.config/loom/loom.db"
```

Unknown values for `default`/`interface` are config-validation errors at
startup (fail fast with the accepted values), mirroring the `CHECK` on
`cards.agent`.

---

## 6. Schema Migration

```sql
-- goose: up
ALTER TABLE cards ADD COLUMN agent TEXT
    CHECK (agent IN ('claude', 'opencode'));

-- goose: down
ALTER TABLE cards DROP COLUMN agent;
```

- `NULL` = use `[agent] default` at launch time (not at write time — a later
  config change re-defaults NULL cards, which is the expected behavior).
- No new tables; `traces` is untouched. `codebases`/`boards`/etc. untouched.
- `Card.AgentOrDefault(cfg)` resolves `card.agent ?? cfg.Agent.Default`.
- Existing cards migrate with `agent = NULL` → they follow the new global
  default (`claude`, preserving today's behavior for anyone who does not touch
  config).

---

## 7. CLI and TUI Surface

### 7.1 CLI

```
loom card add <title> ... [--agent <claude|opencode>]
loom card update <id> ... [--agent <claude|opencode>]   # or "" to reset to default
loom card list ...         # shows agent badge per card
loom config                # shows the §5 schema
```

`loom card open` / `--detach`, `loom card close`, `loom attach`, `loom sessions`
are unchanged — they operate on the card, and the card's driver decides the
command. There is **no** `--run` flag in this change (§9).

### 7.2 TUI

- **Card badge:** a short agent tag next to priority/labels in each column
  cell (e.g. `cl` / `oc`, resolved from `cards.agent ?? default`). Ties into
  the existing per-card render delegate (ADR-001 §5, Phase 2).
- **`n` (new card) / `e` (edit):** agent picker in the form; empty = default.
- **`d` (detail):** "Agent: claude | opencode (default)" field.
- **Keybindings:** unchanged (ADR-001 §3.5). `Enter` opens the card via its
  agent.

---

## 8. Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| **opencode version regression: `--mini --prompt` stops auto-submitting** | Low | Resolved by probe for v1.18.15 (auto-submits; REPL stays alive). If a future opencode version regresses to prefill-only, the driver sets `SendKeys = "Enter"` (post-probe `send-keys`); the `SendKeys` field is retained on the contract for this. Failure is visible, never silent. Re-run the Phase 0 probes (§11) on each opencode major bump. |
| **opencode CLI churn** (`--mini`, `--prompt`, permission semantics shift across versions) | Low–Med | Pin a verified opencode version range in `loom docs`/README; re-run Phase 0 verification on each opencode major bump. |
| **opencode permission model differs from claude** | Medium | opencode permissions are config/agent-driven; "ask" rules prompt in the pane (works while attached), `auto_approve` passes `--auto`. User-owned configuration, same posture as claude's own permissions. |
| **opencode binary missing / too old** | Low | Same as claude: `Resolve()` fails the open with an install hint; the startup probe deletes the `trace_start` of a session that never launched (ADR-001 §4.1). |
| **`interface = "full"` TUI nested inside loom's tmux** | Low | Same nested-tmux handling as claude (ADR-001 §4.4); opencode renders in the attach pane. |
| **Interface over-abstraction (drivers diverge)** | Low | Interface is four small methods; a driver that needs more implements a private optional interface (Go idiom). YAGNI guard: no dynamic registration. |
| **Mislabeled "done"** — `run` mode semantics are "task complete", interactive is "user closed" | Low (future) | Not enabled in this change; when §9 ships, the card detail view shows the launch mode alongside `●`/`◉`. |

---

## 9. Future Considerations (deferred by this ADR)

| Feature | Why deferred | Shape when it lands |
|---------|--------------|---------------------|
| **Autonomous `run` mode** (`loom card open <id> --run`, `--detach --run`) | "Simplicity over automation"; more failure paths (permissions unattended, auto-approve policy). opencode is the only agent that supports it cleanly (claude has no equivalent of `run`). | `LaunchMode: Run`; argv `["<abs-opencode>", "run", "<context>"]`; session-end now means "task done"; optional `--continue` on reopen of a completed card (opencode session ids) to resume the same conversation. Trace semantics unchanged. |
| **Session attribution** | Not needed while interactive (the tmux session IS the run). | `--title loom-<cardid>` on `opencode run` so `opencode session list` correlates sessions to cards. |
| **More agents** (aider, etc.) | No demand signal. | A third `AgentDriver` impl; no interface change expected. |
| **tmux-less fallback / daemon mode** | Already v0.2 in ADR-001 §9; orthogonal to agents. | — |

---

## 10. Verification Strategy

| Layer | What | How |
|-------|------|-----|
| **Phase 0 (tmux feasibility, extended)** | opencode interactive launch mechanics | **`--mini --prompt '<ctx>'` auto-submits (verified, v1.18.15) and the split-footer REPL stays alive until quit (verified).** `capture-pane` shows the quoted prompt. `SendKeys` default is `""` (was `"Enter"`). Residual: full-TUI `--prompt` auto-submit. |
| **Unit** | Driver contract | Table-driven: each driver returns the expected argv for a given card + config (claude positional vs opencode `--mini --prompt`); quoting is idempotent on strings containing `'` and newlines; `AgentOrDefault` resolution (`NULL` → `[agent] default`, explicit → card value); `cards.agent` CHECK rejects unknown names. |
| **Integration** | Card open → session → completion → trace, per driver | Parametrize the existing stub-agent test (ADR-001 §10) for stub `claude` and stub `opencode`: assert `loom-<id>` session name, cwd, and the quoted argv in the pane; session-end → `trace_end`; `loom card close` kills + finalizes; git reconciliation still attributes changes (§5). |
| **Failure paths** | Bad binary per driver | Point each driver's `binary` at a nonexistent path → open fails, `capture-pane` text surfaced, **no** `trace_start` row (ADR-001 §4.1 steps 0–1). |
| **Schema** | Migration | `cards.agent` default NULL, CHECK enforced, existing rows migrate NULL, `goose down` restores. |

**Project coverage bar (ADR-001 §10) unchanged;** the tmux attach handoff to a
real interactive agent remains a manual E2E step, now for two agents.

---

## 11. Implementation Plan

Total ~2 days, all behind the existing Phase 2–3 SessionManager work:

| Phase | Scope | Days |
|-------|-------|------|
| **A — Driver refactor** | Extract `AgentDriver`; move claude argv into `claudeDriver`; generalize the escaper to all argv elements; `SessionManager.ensure` calls the driver; stub-driver unit test. No behavior change — a green suite proves it. | 0.5 |
| **B — opencode driver + config + schema** | `opencodeDriver` (interactive mini/full, pass-throughs, `SendKeys`); `[agent]`/`[agent.*]` config; `cards.agent` migration + `AgentOrDefault`; `--agent` CLI flags. Phase 0 verification of `--mini --prompt`. | 1.0 |
| **C — TUI + verification** | Card agent badge, picker in `n`/`e`, detail field; parametrized stub tests + bad-binary tests; docs (this ADR → Adopted). | 0.5 |

---

## 12. References

- **ADR-001** (`./ADR-001-loom-architecture.md`) — base architecture; this ADR
  supersedes its agent touchpoints per §2.2.
- **opencode CLI** — `opencode --help`, `opencode run --help`,
  https://opencode.ai/docs/cli/ (v1.18.15 installed).
- **opencode TUI** — https://opencode.ai/docs/tui/ (`--mini`, `--prompt`).
- **opencode source (verified, dev @ 2026-08-09)** —
  `packages/opencode/src/cli/cmd/run.ts` (non-interactive exits on idle;
  `--interactive` rejected on `run`), `cli/cmd/tui.ts` and
  `packages/tui/src/routes/session/index.tsx` (`--prompt` sets the input value).
