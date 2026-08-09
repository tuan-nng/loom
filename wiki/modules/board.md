---
title: board (internal/board)
description: BoardService — the orchestration layer that ties kanban CRUD, session lifecycle, and trace recording together.
type: module
tags: [wiki, module, board]
---

## Summary

`internal/board` provides **BoardService**, the application-core orchestration layer between the [TUI/CLI](../modules/tui.md) and the store/session/trace services. It wires kanban CRUD, card movement, session open/close, and trace finalization into higher-level operations the UI surfaces call. Spec: ADR-001 §3.1; DESIGN-002 §4.2.

## Responsibilities

- Compose store CRUD (workspaces/boards/columns/cards) with the SessionManager + TraceRecorder.
- Enforce move semantics: reject cross-board moves; auto-kill session + finalize trace when moving a card to a `done`-stage column.
- Resolve current workspace/board from `ui_state` with fallback (most-recently-created, then first board; error only if none exists — `run loom init`).

## Public API / entry points

```go
type Service struct { ... }
func (s *Service) MoveCard(ctx, cardID, targetColumnID string) error
func (s *Service) OpenCard(ctx, cardID string, detach bool) error
func (s *Service) CloseCard(ctx, cardID string) error
func (s *Service) ResolveSelection(ctx) (workspaceID, boardID string, err error)
```

## Key files

- `internal/board/service.go` — BoardService orchestration

## Dependencies

- `store`, `session`, `trace`. Consumed by `cli` and `tui`.

## Participates in

- Every user-facing card mutation (TUI keys `m`/`K`/Enter, CLI `loom card ...`) routes through BoardService so the **done-stage auto-kill** behavior can never drift from the move operation (ADR-001 §4.1).

## Related

- Architecture: [Architecture Overview](../architecture/overview.md) · [Data Model](../architecture/data-model.md)
- Concepts: [stage](../concepts/stage.md) · [run](../concepts/run.md)
- Flows: [Card open → completion](../flows/card-open-complete.md)
