---
title: store (internal/store)
description: The SQLite store layer — migrations, pragmas, kanban CRUD, card reorder, and the trace run lifecycle.
type: module
tags: [wiki, module, store, sqlite]
---

## Summary

`internal/store` is a **leaf** over `modernc.org/sqlite` (pure Go, no CGO). It opens the database with the mandated per-connection pragmas, runs goose migrations from an `embed.FS`, and owns all CRUD: workspaces, boards, columns, codebases, cards (with the `agent` column), traces (run lifecycle), and `ui_state`. The full DDL is ADR-001 §3.3 + the ADR-002 §6 `cards.agent` migration.

## Responsibilities

- Open the DB and assert pragmas on every connection: WAL, `foreign_keys=ON`, `busy_timeout=5000`, `synchronous=NORMAL` — **before any query** (a missing `foreign_keys` silently inert-ifies every cascade).
- Run goose migrations (`migrate/00001_initial.sql`, `migrate/00002_card_agent.sql`).
- Kanban CRUD: card add/list/show/move/update/delete; enforce cross-board move rejection; keep denormalized `board_id` in sync.
- Position/reorder with the pre-write rebalance trigger at `next - prev <= 1` (ADR-001 §3.4).
- Trace run lifecycle: `StartRun`/`AbortRun` (delete whole run), trace event inserts, open-run lookup for reconcile.
- Persist current workspace/board in the single-row `ui_state` table.

## Public API / entry points

```go
type Store struct { ... }
func Open(path string) (*Store, error)
// Workspace/Board/Column/Codebase/Card CRUD methods
func (s *Store) MoveCard(id, targetColumnID string) error
func (s *Store) StartRun(cardID, root string, baseline map[string]string) (runID string, err error)
func (s *Store) AbortRun(runID string) error
func (s *Store) RecordTraceEvent(...) error
```

## Key files

- `internal/store/store.go` — open, pragmas, migration runner
- `internal/store/workspaces.go` `boards.go` `columns.go` `codebases.go` — CRUD
- `internal/store/cards.go` — Card CRUD + `Agent *string` column + `AgentOrDefault`
- `internal/store/traces.go` — trace events, run lifecycle, open-run lookup
- `internal/store/migrate/embed.go`, `00001_initial.sql`, `00002_card_agent.sql`

## Dependencies

- None internal (leaf). External: `modernc.org/sqlite`, pressly/goose, `embed.FS`.

## Participates in

- Consumed by `trace` (recorder), `session` (manager), `board` (BoardService), `cli`, `tui`.
- The trace run-lifecycle API (`StartRun`/`AbortRun`) is what [SessionManager.ensure](../architecture/session-model.md) uses to make a failed launch leave **no** trace row.

## Related

- Architecture: [Data Model](../architecture/data-model.md) · [Trace Recording](../architecture/trace-recording.md)
- Concepts: [run](../concepts/run.md) · [trace-events](../concepts/trace-events.md) · [stage](../concepts/stage.md)
- Guides: [Change the schema](../guides/change-the-schema.md)
