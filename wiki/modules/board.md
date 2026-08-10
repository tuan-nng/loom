---
title: board (internal/board)
description: BoardService — the orchestration layer that ties kanban CRUD, session lifecycle, and trace recording together.
type: module
tags: [wiki, module, board]
---

## Summary

`internal/board` provides **BoardService**, the application-core orchestration layer between the [TUI/CLI](../modules/tui.md) and the store/session services. It wires kanban CRUD, card movement, session open/close, and trace finalization into higher-level operations the UI surfaces call. Spec: ADR-001 §3.1; DESIGN-002 §4.2. Landed as real Go source (T12).

## Responsibilities

- Compose store CRUD (workspaces/boards/columns/cards/codebases) with the session manager.
- Enforce move semantics: reject cross-board moves; auto-kill session + finalize trace when moving a card to a `done`-stage column (ADR-001 §4.1 step 4).
- Resolve current workspace/board from `ui_state` with fallback (most-recently-created, then first board; error only if none exists — `run loom init`).
- Persist the selection on workspace switch / board show-create.

## Public API / entry points

```go
func NewService(db *sql.DB, sess sessionManager) *Service // sessionManager = Ensure/Attach/Kill/Status/ReconcileOnStartup (satisfied by *session.Manager)

// Card ops
func (s *Service) CreateCard(in store.CardInput) (store.Card, error)
func (s *Service) UpdateCard(id string, u store.CardUpdate) (store.Card, error)
func (s *Service) GetCard(id string) (store.Card, error)
func (s *Service) ListCardsByBoard(boardID string) ([]store.Card, error)
func (s *Service) ListCardsByColumn(columnID string) ([]store.Card, error)
func (s *Service) MoveCard(ctx, cardID, toColumnID string, beforeID, afterID *string) (store.Card, error)
func (s *Service) DeleteCard(id string) error

// Workspace / board / column / codebase CRUD
func (s *Service) CreateWorkspace(name, rootPath string) (store.Workspace, error)
func (s *Service) GetWorkspace(id string) (store.Workspace, error)
func (s *Service) ListWorkspaces() ([]store.Workspace, error)
func (s *Service) DeleteWorkspace(id string) error
func (s *Service) CreateBoard(workspaceID, name string) (store.Board, error)
func (s *Service) GetBoard(id string) (store.Board, error)
func (s *Service) ListBoards(workspaceID string) ([]store.Board, error)
func (s *Service) DeleteBoard(id string) error
func (s *Service) CreateColumn(boardID, name, stage string) (store.Column, error)
func (s *Service) GetColumn(id string) (store.Column, error)
func (s *Service) ListColumns(boardID string) ([]store.Column, error)
func (s *Service) DeleteColumn(id string) error
func (s *Service) CreateCodebase(workspaceID, path string) (store.Codebase, error)
func (s *Service) GetCodebase(id string) (store.Codebase, error)
func (s *Service) ListCodebases(workspaceID string) ([]store.Codebase, error)
func (s *Service) DeleteCodebase(id string) error

// Selection switching (persists ui_state)
func (s *Service) SwitchWorkspace(workspaceID string) (store.Workspace, error) // clears board selection
func (s *Service) ShowBoard(boardID string) (store.Board, error)
func (s *Service) CreateBoard(workspaceID, name string) (store.Board, error)

// Session actions
func (s *Service) OpenCard(ctx, cardID string, detach bool) error   // Ensure (+ Attach unless detach)
func (s *Service) CloseCard(ctx, cardID string) error               // Kill + finalize trace
func (s *Service) SessionStatus(ctx) (map[string]session.SessionStatus, error)
func (s *Service) ReconcileOnStartup(ctx) error

// Selection resolution
func (s *Service) ResolveSelection() (store.Workspace, store.Board, error) // ErrNotInitialized = "run loom init"
```

## Key files

- `internal/board/service.go` — BoardService orchestration
- `internal/board/service_test.go` — unit suite (fake manager + real temp DB, 20 tests)
- `internal/board/integration_test.go` — real-tmux `-L loomselftest` open/close round-trip

## Dependencies

- `store` (all CRUD), `session` (only `session.SessionStatus` + the `sessionManager` interface, satisfied by `*session.Manager`). Consumed by `cli` and `tui`.

## Participates in

- Every user-facing card mutation (TUI keys `m`/`K`/Enter, CLI `loom card ...`) routes through BoardService so the **done-stage auto-kill** behavior can never drift from the move operation (ADR-001 §4.1).

## Related

- Architecture: [Architecture Overview](../architecture/overview.md) · [Data Model](../architecture/data-model.md)
- Concepts: [stage](../concepts/stage.md) · [run](../concepts/run.md)
- Flows: [Card open → completion](../flows/card-open-complete.md)
