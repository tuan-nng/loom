---
title: store (internal/store)
description: The SQLite store layer — migrations, pragmas, kanban CRUD, card reorder, and the trace run lifecycle.
type: module
tags: [wiki, module, store, sqlite]
---

## Summary

`internal/store` is a **leaf** over `modernc.org/sqlite` (pure Go, no CGO). T4 landed the **schema + connection layer**: `Open(path)` opens the database with the mandated per-connection pragmas and runs the goose migrations from an `embed.FS` (`migrate/00001_initial.sql` = the full ADR-001 §3.3 schema incl. `ui_state`, `migrate/00002_card_agent.sql` = the ADR-002 §6 `cards.agent` migration). The kanban CRUD, card reorder, and trace run lifecycle remain **planned** (T5–T7) on top of this foundation.

## Responsibilities

**Implemented (T4):**

- Open the DB and assert the four pragmas on **every physical connection** — WAL, `foreign_keys=ON`, `busy_timeout=5000`, `synchronous=NORMAL` — via a `sqlite.RegisterConnectionHook` registered in `init()` (a missing `foreign_keys` silently inert-ifies every cascade, so the hook fails the connection open on any pragma error). `SetMaxOpenConns(1)` makes the store a single writer.
- Run goose migrations idempotently via `migrateUp(db, migrate.FS)` (`-- +goose Up`/`Down` markers): `00001_initial.sql` carries the 7-table schema + its Down (reverse REFERENCES order), `00002_card_agent.sql` adds the nullable `cards.agent` CHECK and drops it on down.

**Planned (T5–T7):** kanban CRUD; cross-board move rejection + `board_id`/`workspace_id` sync; position/reorder with the pre-write rebalance at `next - prev <= 1` (ADR-001 §3.4); trace run lifecycle (`StartRun`/`AbortRun` — delete whole run, open-run lookup); `ui_state` get/set.

## Public API / entry points

```go
func Open(path string) (*sql.DB, error)
```

- `Open` opens `"sqlite"` at `path`, sets `SetMaxOpenConns(1)`, then applies the goose migrations; returns a ready `*sql.DB` with pragmas enforced on every pooled connection by the hook. Third-party errors are returned bare; `db.Close()` runs on any migration failure.
- `migrateUp(db *sql.DB, fsys fs.FS) error` (internal) runs the goose `Up`.

A `Store` struct wrapping `*sql.DB` with the CRUD/traces methods is planned with T5–T7.

## Key files

- `internal/store/store.go` — connection-hook pragmas, `Open`, `migrateUp` (implemented)
- `internal/store/migrate/embed.go`, `00001_initial.sql`, `00002_card_agent.sql` (implemented)
- `internal/store/workspaces.go` `boards.go` `columns.go` `codebases.go` `cards.go` `traces.go` — CRUD + trace lifecycle (planned, T5–T7)

## Dependencies

- None internal (leaf). External: `modernc.org/sqlite v1.33.1` (the C1 pin was v1.33.0, retracted by the maintainer — it dropped the external `modernc.org/libc` and broke clients; v1.33.1 restores it), `github.com/pressly/goose/v3 v3.21.0`, stdlib `embed.FS`.

## Participates in

- Consumed by `trace` (recorder), `session` (manager), `board` (BoardService), `cli`, `tui` once their store-facing APIs land (T5–T7).
- The trace run-lifecycle API (`StartRun`/`AbortRun`) is what [SessionManager.ensure](../architecture/session-model.md) uses to make a failed launch leave **no** trace row.

## Related

- Architecture: [Data Model](../architecture/data-model.md) · [Trace Recording](../architecture/trace-recording.md)
- Concepts: [run](../concepts/run.md) · [trace-events](../concepts/trace-events.md) · [stage](../concepts/stage.md)
- Guides: [Change the schema](../guides/change-the-schema.md)
