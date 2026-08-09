---
title: Architecture Overview
description: System boundaries, layers, and technology choices for loom — a CLI-native Kanban task tracker that launches coding agents in tmux sessions.
type: architecture
tags: [wiki, architecture, overview]
---

## Summary

Loom is a **CLI-native Kanban task tracker with agent launch**: a single Go binary that shows a Kanban board in the terminal, and opening a card launches that card's coding agent (Claude Code or opencode) inside a detached tmux session on a dedicated `-L loom` server. The human stays in the loop by attaching and detaching; the board stays usable while the agent works in the background. The canonical design is [ADR-001](../../docs/ADR-001-loom-architecture.md), extended for multiple agents by [ADR-002](../../docs/ADR-002-loom-multi-agent.md) and the [implementation blueprint](../../docs/DESIGN-002-loom-multi-agent.md). The repo is **partially implemented**: the T1 [config](../modules/config.md) package, the T2 [agent](../modules/agent.md) driver contract (interface, registry, prompt, quoting), and the T3 [claude + opencode drivers](../modules/agent.md) are real Go source; the T4 [store](../modules/store.md) migrations + pragma layer (`Open`, per-connection pragma hook, goose migrations) and the T5 [store kanban CRUD](../modules/store.md) (workspaces/boards/columns/codebases, `ui_state`, `loom init`) the T6 [store card CRUD + reorder](../modules/store.md) (`CreateCard`/`UpdateCard`/`MoveCard`/`DeleteCard`, `(prev+next)/2` midpoints, pre-write renumber), and the T7 [store traces lifecycle](../modules/store.md) (`StartRun`/`RecordChange`/`EndRun`/`OpenRuns`/`AbortRun`), the T8/T9 [trace layer](../modules/trace.md) (git-baseline reconcile + fsnotify recorder/watcher), the T10 [session tmux wrapper](../modules/session.md) (`Tmux`/`New`, tmux ≥ 3.x gate, typed `MissingServer`), and the T11 [SessionManager](../modules/session.md) (`Ensure`/`Attach`/`Kill`/`Status`/`ReconcileOnStartup`, shared `completeRun` finalize) are also real; the TUI and CLI are still design — the wiki describes the designed system.

## Diagram

```mermaid
flowchart TB
    TUI --> CORE
    CORE --> STORE
    CORE --> AGENT
    CORE --> TMUX
    TMUX --> AGENT
```

## Key components

- **Terminal tier** — BubbleTea v2 TUI with board, card-detail, forms, and help views. Canonical keybindings in ADR-001 §3.5 (board render model from [kancli](../../docs/ADR-001-loom-architecture.md)).
- **Application Core** — three services: `BoardService` (kanban CRUD + card movement), `SessionManager` (the tmux lifecycle), `TraceRecorder` (file-change events).
- **Agent layer** — `AgentDriver` interface with two implementations (`claude`, `opencode`); produces the argv that runs inside the session. See [Agent Abstraction](./agent-abstraction.md).
- **tmux substrate** — the dedicated `-L loom` server owns the PTY, signals, and child cleanup; loom never parents the agent process. The `-L` client wrapper ships (T10): `New` resolves the binary and gates tmux ≥ 3.x; all failures are typed, `MissingServer` flags the cold server. The [SessionManager](../modules/session.md) that drives the lifecycle ships (T11): `Ensure` launches/reuses, `Status`/`ReconcileOnStartup` finalize disappeared sessions via the shared `completeRun` path. See [Session Model](./session-model.md).
- **SQLite store** — `modernc.org/sqlite` (pure Go, no CGO), WAL mode, `foreign_keys=ON`, goose migrations. T4 landed the connection pragmas + migrations (`Open`); T5 landed the kanban CRUD (workspaces/boards/columns/codebases, `ui_state`, `loom init`); T6 landed the card CRUD + reorder (`CreateCard`/`UpdateCard`/`MoveCard`, `(prev+next)/2`, pre-write renumber); T7 landed the trace run lifecycle (`StartRun`/`RecordChange`/`EndRun`/`OpenRuns`/`AbortRun`). See [Data Model](./data-model.md).

## Design decisions

| Decision | Rationale | Source |
|---|---|---|
| Go + BubbleTea | Best terminal-UI ecosystem in any language; Rust/Ratatui and Python/Textual rejected (ecosystem depth, single binary) | ADR-001 §2.1 |
| tmux session model (not `tea.ExecProcess` pop-over) | Human in the loop by attach/detach; sessions survive loom; N concurrent cards; tmux owns cleanup | ADR-001 §2.3 |
| Single binary | SQLite direct, no HTTP/SSE/React/Vite/CORS; external deps limited to `tmux` + the user's agent | ADR-001 §1.3 |
| Simplicity over automation | User drives the workflow; no automated agents, no lifecycle supervision; interactive launch is the only shipped mode | ADR-001 §1.3, ADR-002 §1.3 |
| Driver interface for agents | Clean N-agent extension, per-driver test isolation; loom owns the launch contract (path resolution, quoting, probe) | ADR-002 §2.3, DESIGN-002 §4.4 |

## Related

- [Session Model](./session-model.md) · [Data Model](./data-model.md) · [Agent Abstraction](./agent-abstraction.md) · [Trace Recording](./trace-recording.md)
- Concepts: [run](../concepts/run.md) · [session](../concepts/session.md) · [stage](../concepts/stage.md) · [watch-scope](../concepts/watch-scope.md)
- Flow: [Card open → completion](../flows/card-open-complete.md)
