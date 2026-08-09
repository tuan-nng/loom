---
title: session (internal/session)
description: The tmux client wrapper and SessionManager — ensure, attach, kill, status, probe, and reconcile-on-startup. The lifecycle core.
type: module
tags: [wiki, module, session, tmux]
---

## Summary

`internal/session` is the lifecycle core. The **tmux client wrapper** (`session/tmux.go`) ships as real Go source (T10): a thin client bound to the dedicated `-L <server>` — `NewSession`, `HasSession`, `CapturePane`, `SendKeys`, `KillSession`, `ListSessions` — built by `New(server)`, which resolves the binary once and gates on tmux ≥ 3.x with an install hint; every failure surfaces as a typed `tmuxError` (never a raw `*exec.ExitError`), and the exported `MissingServer` predicate lets the manager tell a cold/missing server from a real failure. The **SessionManager** (`session/manager.go`) — `ensure` (create-or-reuse, driver-aware), attach, kill, status, the ~500ms startup probe, the 2s poll, and reconcile-on-startup — is still planned (T11). It imports `agent`, `store`, `trace`, `config` (DESIGN-002 §4.2). Spec: ADR-001 §4, DESIGN-002 §10.

## Responsibilities

- Wrap the tmux client for the configured `-L <server>`: `NewSession`, `HasSession`, `CapturePane`, `SendKeys`, `KillSession`, `ListSessions`; `SessionName(id) = "loom-" + id`, rejecting `:` (tmux parses it as `session:window`). **Implemented (T10).**
- `New(server)` resolves the binary once and gates on tmux ≥ 3.x with an `apt install tmux`/`brew install tmux` hint; all failures surface as a typed `tmuxError`, and `MissingServer(err)` flags the cold/missing-server state the manager retries once (ADR-001 §8, DESIGN-002 §10.2 invariant 3). **Implemented (T10).**
- `ensure`: reuse existing session, else resolve the agent, build argv via the driver, snapshot baseline, `new-session -d`, probe (~500ms), record the run **after** the probe, start the watcher, send `SendKeys` if set. **Kills any session it created on every post-creation error path** and `AbortRun`s — no live session ever exists without its run (DESIGN-002 §10.2 invariants). *(planned — T11)*
- Attach via `tea.ExecProcess("tmux", attach-session)`; detach returns control; completion via session disappearance. *(planned)*
- 2s poll → running/attached markers; reconcile-on-startup finalizes open runs whose session is absent (correctness backstop). *(planned)*

## Public API / entry points

**Implemented (T10):**

```go
type Tmux struct { Server string; bin string }
func New(server string) (Tmux, error)
func (t Tmux) NewSession(name, cwd, command string) error
func (t Tmux) HasSession(name string) (bool, error)
func (t Tmux) CapturePane(name string) string
func (t Tmux) SendKeys(name, keys string) error
func (t Tmux) KillSession(name string) error
func (t Tmux) ListSessions() ([]string, error)
func SessionName(id string) string
func MissingServer(err error) bool
```

**Planned (T11):**

```go
type Manager struct { ... }
func (m *Manager) Ensure(ctx, card) error
func (m *Manager) Attach(ctx, card) error
func (m *Manager) Kill(ctx, card) error
func (m *Manager) Status(ctx) (map[string]SessionStatus, error)
func (m *Manager) ReconcileOnStartup(ctx) error
```

## Key files

- `internal/session/tmux.go` — thin tmux client wrapper (**implemented, T10**)
- `internal/session/tmux_test.go` — unit tables (naming/`:` panic, name parse, version gate, typed error) + real-tmux round-trip on an isolated `-L loomselftest` server; `New` install-hint failure (**implemented, T10**)
- `internal/session/manager.go` — SessionManager (driver-aware `ensure`) *(planned)*
- `internal/session/session_test.go` — stub-driver integration tests against real tmux *(planned)*

## Dependencies

- **Implemented wrapper:** stdlib only (`os/exec`, `strconv`, `strings`, `time`). **Planned Manager:** `agent`, `store`, `trace`, `config`. External: tmux (3.x), resolved at startup via `New`.

## Participates in

- The only lifecycle method that touches the [agent driver](../architecture/agent-abstraction.md) is `ensure` (steps 1–3, 7).
- Uses [store](../modules/store.md) run lifecycle + [trace](../modules/trace.md) recorder/watcher.
- Called by the [board TUI](../modules/tui.md) (Enter/`K`/`m`) and the [CLI](../modules/cli.md) (`loom card open/close`, `loom attach`, `loom sessions`).

## Related

- Architecture: [Session Model](../architecture/session-model.md) · [Agent Abstraction](../architecture/agent-abstraction.md)
- Concepts: [session](../concepts/session.md) · [run](../concepts/run.md) · [stage](../concepts/stage.md)
- Flows: [Card open → completion](../flows/card-open-complete.md) · [Attach/detach](../flows/attach-detach.md) · [Failure paths](../flows/failure-paths.md)
