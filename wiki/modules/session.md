---
title: session (internal/session)
description: The tmux client wrapper and SessionManager — ensure, attach, kill, status, probe, and reconcile-on-startup. The lifecycle core.
type: module
tags: [wiki, module, session, tmux]
---

## Summary

`internal/session` is the lifecycle core. Verified against real tmux (T21): per-driver session naming/cwd/argv-quoting, reconcile-on-startup git attribution for a run that outlives loom, and an opt-in live regression canary for opencode's auto-submit (both interfaces). The **tmux client wrapper** (`session/tmux.go`) ships as real Go source (T10): a thin client bound to the dedicated `-L <server>` (or, since the restyle, the enclosing server when loom runs inside the user's tmux) — `NewSession`, `HasSession`, `CapturePane`, `SendKeys`, `KillSession`, `ListSessions`, `Sessions` — built by `New(server)`, which resolves the binary once and gates on tmux ≥ 3.x with an install hint; every failure surfaces as a typed `tmuxError` (never a raw `*exec.ExitError`), and the exported `MissingServer` predicate lets the manager tell a cold/missing server from a real failure. The **SessionManager** (`session/manager.go`, T11) ships: `Ensure` (create-or-reuse, driver-aware, per DESIGN-002 §10.2), `Attach`, `Kill`, `Status` (one synchronous tick: running/attached markers + finalizing disappeared sessions), and `ReconcileOnStartup`. A single `completeRun` path — git-reconcile the run against its stored baseline, emit missing `file_change` rows, write `trace_end` with `files_changed = unique(live ∪ missing)` — is shared by status/kill/reconcile. Startup-reconciled runs record `durationMs=0` (no start timestamp); deleted cards/shared scopes finalize blind. The only test seam is the private `runRecorder` interface satisfied by `*trace.Recorder`. Deps: `agent`, `store`, `trace`, `config` (DESIGN-002 §4.2). Spec: ADR-001 §4, DESIGN-002 §10.

## Responsibilities

- Wrap the tmux client for the configured `-L <server>`: `NewSession`, `HasSession`, `CapturePane`, `SendKeys`, `KillSession`, `ListSessions`; `SessionName(id) = "loom-" + id`, rejecting `:` (tmux parses it as `session:window`). **Implemented (T10).**
- `New(server)` resolves the binary once and gates on tmux ≥ 3.x with an `apt install tmux`/`brew install tmux` hint; all failures surface as a typed `tmuxError`, and `MissingServer(err)` flags the cold/missing-server state the manager retries once (ADR-001 §8, DESIGN-002 §10.2 invariant 3). **Implemented (T10).**
- **Enclosing-server model (post-T22):** `New` also captures the enclosing tmux from `$TMUX` (`EnclosingSocket`) and `$TMUX_PANE`; every subcommand is targeted via `targetArgs` (`-S <socket>` when loom runs inside the user's tmux, else `-L <server>`); `configureServer` applies the loom-owned settings (`prefix`, `status off`, `detach-on-destroy off`) on the dedicated `-L` server only (no-op on an enclosing server — the user's own tmux globals are never rewritten), re-applied on session reuse and attach; `NewSession` names the first window (`-n`) so a linked tab reads `loom-<id>`; `AttachCommand`/`AttachCommandFor` build the handoff (`link-window`/`select-window` on the enclosing server so a card opens as a plain tab, `attach-session` standalone); `KillSession` kills the linked window first on an enclosing server (a linked window would otherwise outlive `kill-session`), with a `kill-session` fallback.
- `ensure`: reuse existing session, else resolve the agent, build argv via the driver, snapshot baseline, `new-session -d`, probe (~500ms), record the run **after** the probe, start the watcher, send `SendKeys` if set. **Kills any session it created on every post-creation error path** and `AbortRun`s — no live session ever exists without its run (DESIGN-002 §10.2 invariants). **Implemented (T11).**
- Attach via `AttachCommand(name)` with stdio wired; detach returns control; completion via session disappearance (Status/ReconcileOnStartup). **Implemented (T11).**
- `Status` is one synchronous tick → running/attached markers (`Sessions`), finalizing runs whose session is gone; the caller owns cadence (TUI 2s poll, one-shot CLI). `ReconcileOnStartup` finalizes open runs whose session is absent (correctness backstop). **Implemented (T11).**

## Public API / entry points

**Implemented (T10):**

```go
type Tmux struct { Server string; Prefix string; bin string; enclosing string; pane string }
func New(server string) (Tmux, error)
func EnclosingSocket() string
func (t Tmux) NewSession(name, cwd, command string) error
func (t Tmux) HasSession(name string) (bool, error)
func (t Tmux) CapturePane(name string) string
func (t Tmux) SendKeys(name, keys string) error
func (t Tmux) KillSession(name string) error
func (t Tmux) ListSessions() ([]string, error)
type SessionState struct { Name string; Attached bool }
func (t Tmux) Sessions() ([]SessionState, error)
func (t Tmux) AttachCommand(name string) *exec.Cmd
func AttachCommandFor(server, name string) (*exec.Cmd, error)
func SessionName(id string) string
func MissingServer(err error) bool
```

**Implemented (T11) — SessionManager:**

```go
type Manager struct { ... }
func NewManager(tm Tmux, cfg *config.Config, db *sql.DB) *Manager
func (m *Manager) Ensure(ctx context.Context, card store.Card) error
func (m *Manager) Attach(ctx context.Context, card store.Card) error
func (m *Manager) Kill(ctx context.Context, card store.Card) error
func (m *Manager) Status(ctx context.Context) (map[string]SessionStatus, error)
type SessionStatus struct { Running, Attached bool }
func (m *Manager) ReconcileOnStartup(ctx context.Context) error
```

## Key files

- `internal/session/tmux.go` — thin tmux client wrapper (**implemented, T10**); since the restyle it owns the enclosing-server model: `EnclosingSocket`, `configureServer`, `AttachCommand`/`AttachCommandFor`, `enclosingSession`/`linkedWindow`, `targetArgs`, and the `-n` first-window naming + linked-window `KillSession` (post-T22)
- `internal/session/tmux_test.go` — unit tables (naming/`:` panic, name parse, version gate, typed error) + real-tmux round-trip on an isolated `-L loomselftest` server; `New` install-hint failure (**implemented, T10**); plus the enclosing tests (post-T22): `TestServerConfigured` (prefix C-a / status off / detach-on-destroy off applied to the `-L` server), `TestEnclosingSocket`, `TestAttachCommandEnclosingLinksAsTab` (link vs re-open select, on a fixture with a decoy session guarding against tmux's implicit current-session fallback), `TestKillSessionEnclosingKillsLinkedWindow`, `TestConfigureServerSkippedWhenEnclosing`
- `internal/session/manager.go` — SessionManager: driver-aware `Ensure` (reuse→resolve→launch→baseline→probe→record→watch), `Attach`, `Kill`, one-tick `Status`, `ReconcileOnStartup`, shared `completeRun` finalize (**implemented, T11**)
- `internal/session/session_test.go` — stub-driver tests against real tmux on the isolated server: reuse-no-new-run, nonexistent binary, probe-fail, post-create StartRun/Watch failures, status markers + completion finalize, reconcile-on-startup, deleted-card blind finalize, kill (**implemented, T11**); plus the T21 verification-matrix rows — `TestEnsurePerDriverSessionNameCwdAndArgv` (claude + opencode mini/full, parametrized: `loom-<id>` naming, launch cwd, and the quoted argv reconstructed byte-for-byte through tmux's `$SHELL -c` re-parse), `TestReconcileOnStartupAttributesGitChanges` (a run with no live watcher — "outlives loom" — still git-reconciled on `ReconcileOnStartup`), and `TestOpencodeFullTUIAutoSubmitCanary` (the DESIGN-002 §16 regression canary: automates `docs/PROBE-full-tui.md` against the real opencode CLI for both interfaces; opt-in via `LOOM_TEST_LIVE_OPENCODE=1` since it makes a live LLM call) (**implemented, T21**)

## Dependencies

- **Implemented wrapper:** stdlib only (`os/exec`, `strconv`, `strings`, `time`). **Implemented Manager:** `agent`, `store`, `trace`, `config`. External: tmux (3.x), resolved at startup via `New`.

## Participates in

- The only lifecycle method that touches the [agent driver](../architecture/agent-abstraction.md) is `ensure` (steps 1–3, 7).
- Uses [store](../modules/store.md) run lifecycle + [trace](../modules/trace.md) recorder/watcher.
- Called by the [board TUI](../modules/tui.md) (Enter/`K`/`m`) and the [CLI](../modules/cli.md) (`loom card open/close`, `loom attach`, `loom sessions`).

## Related

- Architecture: [Session Model](../architecture/session-model.md) · [Agent Abstraction](../architecture/agent-abstraction.md)
- Concepts: [session](../concepts/session.md) · [run](../concepts/run.md) · [stage](../concepts/stage.md)
- Flows: [Card open → completion](../flows/card-open-complete.md) · [Attach/detach](../flows/attach-detach.md) · [Failure paths](../flows/failure-paths.md)
