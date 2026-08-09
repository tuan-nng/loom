---
title: Change the schema
description: How to add or alter a SQLite table/migration in loom, with the invariants that keep cascades and ordering correct.
type: guide
tags: [wiki, guide, schema, store]
---

## Goal

Safely add or modify a table/column in loom's SQLite schema (e.g. the ADR-002 `cards.agent` migration), without breaking cascades, ordering, or the trace run lifecycle.

## Steps

1. **Write a new goose migration** `internal/store/migrate/0000N_<name>.sql` with `-- +goose Up` and `-- +goose Down` (embed via `embed.FS`). See `00001_initial.sql` (ADR-001 §3.3 DDL) and `00002_card_agent.sql` (ADR-002 §6: `ALTER TABLE cards ADD COLUMN agent TEXT CHECK (...)`).
2. **Respect the table-order rule** — every `REFERENCES` target must exist before its referent in the DDL (workspaces → boards → columns → codebases → cards → traces); with `foreign_keys=ON`, goose fails a forward reference (ADR-001 §3.3).
3. **Preserve the invariants**: `traces` keeps `seq` as the sole ordering key and the partial UNIQUE run-lifecycle index; timestamps remain display-only.
4. **Add the store field + CRUD** in the matching `internal/store/*.go` file; wire any new behavior through [BoardService](../modules/board.md) (e.g. the `done`-stage auto-kill, ADR-001 §4.1).
5. **Test**: migration up/down; the CHECK rejects invalid values; existing rows migrate with the expected default (NULL → late-bound default); `goose down` restores.
6. **Update the data-model docs** — the DDL here is the source of truth ([ADR-001 §3.3](../../docs/ADR-001-loom-architecture.md)).

## Relevant code

- `internal/store/migrate/` (embed.go + SQL), `internal/store/store.go` (open + pragmas), `cards.go`/`traces.go` — see [store](../modules/store.md).

## Gotchas

- **Never drop `PRAGMA foreign_keys = ON`** — without it every `ON DELETE CASCADE` is inert (silent orphaned rows). Assert it per connection, with a test (ADR-001 §3.3).
- **Don't renumber the ordering key** — a bare rowid (not `AUTOINCREMENT`) is renumbered by `VACUUM`; `traces.seq` must stay `AUTOINCREMENT`.
- **Keep `cards.board_id`/`workspace_id` in sync** — `MoveCard` is the only writer of `column_id` and must update the denormalized ids; cross-board moves are rejected.
- **v0.2 cut content stays cut** — `notes`/`artifacts` tables return in v0.2, not in v0.1 (ADR-001 §9).

## Related

- Architecture: [Data Model](../architecture/data-model.md) · [Trace Recording](../architecture/trace-recording.md)
- Concepts: [run](../concepts/run.md) · [trace-events](../concepts/trace-events.md)
- Modules: [store](../modules/store.md) · [board](../modules/board.md)
