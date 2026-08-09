---
title: store (internal/store)
description: The SQLite store layer — migrations, pragmas, kanban CRUD, card reorder, and the trace run lifecycle.
type: module
tags: [wiki, module, store, sqlite]
---

## Summary

`internal/store` is a **leaf** over `modernc.org/sqlite` (pure Go, no CGO). T4 landed the **schema + connection layer**: `Open(path)` opens the database with the mandated per-connection pragmas and runs the goose migrations from an `embed.FS` (`migrate/00001_initial.sql` = the full ADR-001 §3.3 schema incl. `ui_state`, `migrate/00002_card_agent.sql` = the ADR-002 §6 `cards.agent` migration). **T5 landed the kanban CRUD** for the four domain entities plus the `ui_state` single-row get/set and the `loom init` helper — all package-level functions over a `*sql.DB`, sharing one `NewID()` generator. **T6 landed the card CRUD + reorder** (`cards.go`) — create / partial-update / move / list / delete with anchored `(prev+next)/2` repositioning and the pre-write whole-column renumber. The trace run lifecycle remains **planned** (T7).

## Responsibilities

**Implemented (T4):**

- Open the DB and assert the four pragmas on **every physical connection** — WAL, `foreign_keys=ON`, `busy_timeout=5000`, `synchronous=NORMAL` — via a `sqlite.RegisterConnectionHook` registered in `init()` (a missing `foreign_keys` silently inert-ifies every cascade, so the hook fails the connection open on any pragma error). `SetMaxOpenConns(1)` makes the store a single writer.
- Run goose migrations idempotently via `migrateUp(db, migrate.FS)` (`-- +goose Up`/`Down` markers): `00001_initial.sql` carries the 7-table schema + its Down (reverse REFERENCES order), `00002_card_agent.sql` adds the nullable `cards.agent` CHECK and drops it on down.

**Implemented (T5):**

- **Entity CRUD** — `workspaces.go`, `boards.go`, `columns.go`, `codebases.go`: `Create`/`List`/`Get`/`Delete` per entity with parameterized queries; ordering by `position` (boards/columns) or `created_at` (workspaces/codebases). Errors surface raw (`sql.ErrNoRows`, the schema's `CHECK`/`UNIQUE` constraint messages) — validation is the DB's job.
- **Board + column seeding** — `CreateBoard` always seeds the five [default columns](../concepts/stage.md) (`Backlog`/`To Do`/`In Progress`/`Review`/`Done` @ positions 0,1000,2000,3000,4000, one per stage) inside **one transaction** via a shared `execer` (`*sql.DB`/`*sql.Tx`) so a board never exists with an empty template (ADR-001 §6). `CreateColumn` appends at `max(position)+1000`.
- **`ui_state`** — single-row (`CHECK (id = 1)`) get/set with nullable `*string` selections; `SetUIState` only UPDATEs `id = 1`, never inserts (ADR-001 §5). See [Data Model](../architecture/data-model.md).
- **`InitWorkspace`** — the `loom init` helper (ADR-001 §6): workspace named after the dir + board `"Board"` + the five columns, all in one transaction; **idempotent keyed on `root_path`** — an already-registered directory returns the existing workspace untouched.
- **`NewID()`** — the shared 16 `crypto/rand` bytes → 32 hex chars generator (ADR-001 §3.3), used by every writer. Panics only on `crypto/rand` failure (broken OS entropy, unrecoverable).

**Implemented (T6):**

- **Card CRUD** — `cards.go`: `CreateCard` appends at `max(position)+1000` in one transaction, denormalizing `board_id`/`workspace_id` off the column's board (priority defaults to `"medium"`); `UpdateCard` is a **partial** update — nil pointer = untouched, non-nil sets, and on nullable columns a non-nil `""` clears to NULL (how the CLI expresses `--agent=` reset, DESIGN-002 §13); `GetCard`/`DeleteCard`; `ListCardsByBoard`/`ListCardsByColumn` `ORDER BY position`.
- **`Card` + `AgentOrDefault`** — nullable fields are `*string` (NULL carries meaning); `AgentOrDefault(def)` resolves launch agent: explicit card value wins, else the config default (DESIGN-002 §6).
- **MoveCard** — the **only writer of `column_id`**, keeping `board_id`/`workspace_id` in sync (ADR-001 §3.3). `(nil, nil)` appends at `max(position)+1000`; two anchors land the card at `(prev+next)/2`; when the gap is exhausted (`next-prev <= 1`) the whole column renumbers to `0,1000,2000,…` in display order **before** the midpoint is computed — one transaction, so no reader sees the intermediate renumber (ADR-001 §3.4). Exactly one anchor → `ErrPartialAnchors`; a target column on another board → `ErrCrossBoardMove`.

**Planned (T7):** trace run lifecycle (`StartRun`/`AbortRun` — delete whole run, open-run lookup).

## Public API / entry points

```go
func Open(path string) (*sql.DB, error)
func NewID() string

// workspaces (ORDER BY created_at)
func CreateWorkspace(db *sql.DB, name, rootPath string) (Workspace, error)
func ListWorkspaces(db *sql.DB) ([]Workspace, error)
func GetWorkspace(db *sql.DB, id string) (Workspace, error)
func WorkspaceByRootPath(db *sql.DB, rootPath string) (Workspace, error)
func MostRecentWorkspace(db *sql.DB) (Workspace, error)
func DeleteWorkspace(db *sql.DB, id string) error

// boards (ORDER BY position; CreateBoard seeds the 5 default columns)
func CreateBoard(db *sql.DB, workspaceID, name string) (Board, error)
func ListBoards(db *sql.DB, workspaceID string) ([]Board, error)
func GetBoard(db *sql.DB, id string) (Board, error)
func FirstBoard(db *sql.DB, workspaceID string) (Board, error)
func DeleteBoard(db *sql.DB, id string) error

// columns (ORDER BY position)
func CreateColumn(db *sql.DB, boardID, name, stage string) (Column, error)
func ListColumns(db *sql.DB, boardID string) ([]Column, error)
func GetColumn(db *sql.DB, id string) (Column, error)
func DeleteColumn(db *sql.DB, id string) error

// codebases (ORDER BY created_at; UNIQUE(workspace_id, path))
func CreateCodebase(db *sql.DB, workspaceID, path string) (Codebase, error)
func ListCodebases(db *sql.DB, workspaceID string) ([]Codebase, error)
func GetCodebase(db *sql.DB, id string) (Codebase, error)
func DeleteCodebase(db *sql.DB, id string) error

// ui_state (single row, id = 1)
func GetUIState(db *sql.DB) (UIState, error)
func SetUIState(db *sql.DB, lastWorkspaceID, lastBoardID *string) error

// loom init helper (idempotent on root_path)
func InitWorkspace(db *sql.DB, rootPath string) (Workspace, error)

// cards (nullable fields are *string; UpdateCard is partial, "" clears a nullable col)
func CreateCard(db *sql.DB, in CardInput) (Card, error)
func UpdateCard(db *sql.DB, id string, u CardUpdate) (Card, error)
func GetCard(db *sql.DB, id string) (Card, error)
func DeleteCard(db *sql.DB, id string) error
func ListCardsByBoard(db *sql.DB, boardID string) ([]Card, error)
func ListCardsByColumn(db *sql.DB, columnID string) ([]Card, error)
func MoveCard(db *sql.DB, cardID, toColumnID string, beforeID, afterID *string) error
```

- `Open` opens `"sqlite"` at `path`, sets `SetMaxOpenConns(1)`, then applies the goose migrations; returns a ready `*sql.DB` with pragmas enforced on every pooled connection by the hook. Third-party errors are returned bare; `db.Close()` runs on any migration failure.
- `MostRecentWorkspace`/`FirstBoard` are the store primitives behind the fallback chain (ADR-001 §6: `ui_state` → most-recent workspace → first board) — [BoardService](../modules/board.md) composes them.
- `MoveCard` append mode is `(nil, nil)`; exactly one anchor returns [ErrPartialAnchors]() and a target column on another board returns [ErrCrossBoardMove]().
- `IsNotFound(err)` wraps `errors.Is(err, sql.ErrNoRows)` for callers.

## Key files

- `internal/store/store.go` — connection-hook pragmas, `Open`, `migrateUp` (implemented)
- `internal/store/migrate/embed.go`, `00001_initial.sql`, `00002_card_agent.sql` (implemented)
- `internal/store/ids.go` — `NewID()` (implemented, T5)
- `internal/store/workspaces.go` `boards.go` `columns.go` `codebases.go` — entity CRUD + `DefaultColumns` seed (implemented, T5)
- `internal/store/ui_state.go` — `UIState` + get/set (implemented, T5)
- `internal/store/init.go` — `InitWorkspace` (implemented, T5)
- `internal/store/cards.go` — card CRUD + reorder (implemented, T6)
- `internal/store/traces.go` — trace run lifecycle (planned, T7)

## Dependencies

- None internal (leaf). External: `modernc.org/sqlite v1.33.1` (the C1 pin was v1.33.0, retracted by the maintainer — it dropped the external `modernc.org/libc` and broke clients; v1.33.1 restores it), `github.com/pressly/goose/v3 v3.21.0`, stdlib `embed.FS`.

## Participates in

- Consumed by `trace` (recorder), `session` (manager), `board` (BoardService), `cli`, `tui` — the card CRUD/reorder API is ready for them; the trace run-lifecycle API lands in T7.
- The trace run-lifecycle API (`StartRun`/`AbortRun`) is what [SessionManager.ensure](../architecture/session-model.md) uses to make a failed launch leave **no** trace row.

## Related

- Architecture: [Data Model](../architecture/data-model.md) · [Trace Recording](../architecture/trace-recording.md)
- Concepts: [run](../concepts/run.md) · [trace-events](../concepts/trace-events.md) · [stage](../concepts/stage.md)
- Guides: [Change the schema](../guides/change-the-schema.md)