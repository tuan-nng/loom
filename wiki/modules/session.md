---
title: session (internal/session)
description: The tmux client wrapper and SessionManager — ensure, attach, kill, status, probe, and reconcile-on-startup. The lifecycle core.
type: module
tags: [wiki, module, session, tmux]
---

## Summary

`internal/session` is the lifecycle core. A thin **tmux client wrapper** (`session/tmux.go`) plus the **SessionManager** (`session/manager.go`) that drives the card → tmux session lifecycle: `ensure` (create-or-reuse, driver-aware), attach, kill, status, the ~500ms startup probe, the 2s poll, and reconcile-on-startup. It imports `agent`, `store`, `trace`, `config` (DESIGN-002 §4.2). Spec: ADR-001 §4, DESIGN-002 §10.

## Responsibilities

- Wrap the tmux client for the configured `-L <server>`: `NewSession`, `HasSession`, `CapturePane`, `SendKeys`, `KillSession`, `ListSessions`; `SessionName(id) = "loom-" + id`.
- `ensure`: reuse existing session, else resolve the agent, build argv via the driver, snapshot baseline, `new-session -d`, probe (~500ms), record the run **after** the probe, start the watcher, send `SendKeys` if set. **Kills any session it created on every post-creation error path** and `AbortRun`s — no live session ever exists without its run (DESIGN-002 §10.2 invariants).
- Attach via `tea.ExecProcess("tmux", attach-session)`; detach returns control; completion via session disappearance.
- 2s poll → running/attached markers; reconcile-on-startup finalizes open runs whose session is absent (correctness backstop).

## Public API / entry points

```go
type Tmux struct { Server, bin string }
type Manager struct { ... }
func (m *Manager) Ensure(ctx, card) error
func (m *Manager) Attach(ctx, card) error
func (m *Manager) Kill(ctx, card) error
func (m *Manager) Status(ctx) (map[string]SessionStatus, error)
func (m *Manager) ReconcileOnStartup(ctx) error
func SessionName(id string) string
```

## Key files

- `internal/session/tmux.go` — thin tmux client wrapper
- `internal/session/manager.go` — SessionManager (driver-aware `ensure`)
- `internal/session/session_test.go` — stub-driver integration tests against real tmux

## Dependencies

- `agent`, `store`, `trace`, `config`. External: tmux (3.x).

## Participates in

- The only lifecycle method that touches the [agent driver](../architecture/agent-abstraction.md) is `ensure` (steps 1–3, 7).
- Uses [store](../modules/store.md) run lifecycle + [trace](../modules/trace.md) recorder/watcher.
- Called by the [board TUI](../modules/tui.md) (Enter/`K`/`m`) and the [CLI](../modules/cli.md) (`loom card open/close`, `loom attach`, `loom sessions`).

## Related

- Architecture: [Session Model](../architecture/session-model.md) · [Agent Abstraction](../architecture/agent-abstraction.md)
- Concepts: [session](../concepts/session.md) · [run](../concepts/run.md) · [stage](../concepts/stage.md)
- Flows: [Card open → completion](../flows/card-open-complete.md) · [Attach/detach](../flows/attach-detach.md) · [Failure paths](../flows/failure-paths.md)
