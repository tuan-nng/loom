---
title: cli (internal/cli)
description: The subcommand router and stdlib-flag command surface — the scriptable half of loom (CLI/TUI parity).
type: module
tags: [wiki, module, cli]
---

## Summary

`internal/cli` is the **non-TUI command surface** — a small stdlib-`flag` subcommand router mirroring ADR-001 §6 (no cobra dependency; the surface is fixed and fully enumerated). Every TUI action is scriptable: add/move/update/delete/close/open-`--detach`/sessions. `loom` alone launches the interactive TUI; everything else is CLI. Spec: ADR-001 §6, DESIGN-002 §13.

## Responsibilities

- Route subcommands: `init`, `config`, `workspace`, `board`, `card`, `column`, `attach`, `sessions`, `status`, `version`, `help`.
- Implement CLI/TUI parity — every state mutation (add/move/update/delete/close) works without a terminal for scripting.
- `loom card open <id>` is interactive attach; `--detach` creates the session and returns ("start the agent and leave it running"). No non-interactive card-execution command in v0.1.
- Persist current workspace/board to `ui_state` on `workspace switch` / `board show`/`create` / TUI launch.

## Public API / entry points

```go
func Main(args []string) int
// per-command handlers: cmdInit, cmdConfig, cmdWorkspace, cmdBoard, cmdCard, cmdColumn, cmdAttach, cmdSessions, cmdStatus
```

## Key files

- `internal/cli/root.go` — subcommand table + dispatch
- `internal/cli/card.go` — card commands, incl. `--agent` flag validated against `agent.Known()`; `--agent=` (empty) resets to default via `flag.Visit` (stdlib flag can't distinguish absent vs empty otherwise)
- `internal/cli/config.go` — `loom config` prints the [config schema](../architecture/agent-abstraction.md)
- `internal/cli/session.go` — open/close/attach/sessions
- `internal/cli/status.go` — overall status (runs reconcile-on-startup)

## Dependencies

- `config`, `agent`, `store`, `session`, `trace`, `board`. No cobra.

## Participates in

- Entry point dispatch from `cmd/loom/main.go` (TUI vs CLI).
- Calls [BoardService](../modules/board.md) + [SessionManager](../modules/session.md) for card operations.

## Related

- Architecture: [Architecture Overview](../architecture/overview.md) · [Agent Abstraction](../architecture/agent-abstraction.md)
- Flows: [Card open → completion](../flows/card-open-complete.md) · [Attach/detach](../flows/attach-detach.md)
