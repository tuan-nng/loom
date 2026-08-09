-- +goose Up

-- Workspaces
CREATE TABLE workspaces (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    root_path TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now')),
    archived_at TEXT
);

-- Boards
CREATE TABLE boards (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now'))
);

-- Columns
-- stage carries a real behavior, not just a label: moving a card into a
-- column whose stage is 'done' auto-kills the card's session and finalizes
-- its trace (§4.1). Extra columns may reuse a stage, so the trigger is
-- 'target column's stage == done', never a column id.
CREATE TABLE columns (
    id TEXT PRIMARY KEY,
    board_id TEXT NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    stage TEXT NOT NULL DEFAULT 'dev'
        CHECK (stage IN ('backlog', 'todo', 'dev', 'review', 'done')),
    position INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now'))
);

-- Codebases (registered project directories)
-- Declared before `cards` because cards.codebase_id references it.
CREATE TABLE codebases (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    label TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now')),
    UNIQUE(workspace_id, path)
);

-- Cards
-- board_id and workspace_id are denormalized off column_id to avoid a join
-- on every "list cards in board/workspace" query. The store layer's card-move
-- function is the only writer of column_id and MUST keep board_id in sync;
-- moving a card to a column in a different board is rejected at the store
-- layer (v0.1 has no cross-board move — see §6 CLI surface).
-- codebase_id optionally binds a card to a registered codebase; when set it
-- selects the session's cwd and the fsnotify watch scope (§4.6). There is no
-- `status` column: workflow state is expressed by the card's column (stage);
-- removal is an explicit delete, archival is out of scope for v0.1.
CREATE TABLE cards (
    id TEXT PRIMARY KEY,
    column_id TEXT NOT NULL REFERENCES columns(id) ON DELETE CASCADE,
    board_id TEXT NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    codebase_id TEXT REFERENCES codebases(id) ON DELETE SET NULL,
    title TEXT NOT NULL,
    description TEXT,
    objective TEXT,
    -- verification/test content is NOT a separate column: it is folded into
    -- `description` (a card's prompt is built from title/description/objective/
    -- acceptance_criteria only, §4.5). A dedicated column whose only reader is
    -- the prompt concatenator is `description` with extra steps.
    acceptance_criteria TEXT,
    priority TEXT NOT NULL DEFAULT 'medium'
        CHECK (priority IN ('low', 'medium', 'high')),
    labels TEXT,                -- comma-separated, e.g. "frontend, auth, urgent"
    position INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now'))
);

-- Traces (file change events during Claude sessions)
-- A "run" is one open→complete cycle of a card's session: trace_start opens a
-- run, file_change/trace_end events for that run share its run_id. run_id
-- (16 hex bytes, same generator as table IDs) lets the card-detail view
-- compute "Files Changed (last session)" and per-run duration even when a card
-- is opened many times over its life.
-- data_json shapes by event_type:
--   trace_start:  {"git": {"base_head": "<40-hex sha>", "porcelain": "<git status --porcelain output>"}}
--                 (git fields present only when the watch scope is inside a git repo)
--   file_change: {"path": "<watch-scope-relative path>", "operation": "created|modified|deleted"}
--   trace_end:   {"duration_ms": <int>, "files_changed": <int>}   -- files_changed = unique paths in this run
CREATE TABLE traces (
    -- seq is the ordering key for events within a run (see "Ordering is not
    -- the timestamp's job" above). AUTOINCREMENT, not a bare rowid: VACUUM
    -- renumbers plain rowids and would silently reorder trace history.
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    card_id TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    run_id TEXT NOT NULL,
    event_type TEXT NOT NULL
        CHECK (event_type IN ('trace_start', 'file_change', 'trace_end')),
    data_json TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now'))
);
CREATE INDEX idx_traces_card_run ON traces(card_id, run_id, seq);

-- A run has exactly one trace_start and at most one trace_end. Without this,
-- a double-Enter race (two `ensure` calls before the first session appears in
-- the poll) opens two concurrent runs for one card and "last session" becomes
-- ambiguous. file_change is deliberately excluded — a run has many.
CREATE UNIQUE INDEX idx_traces_run_lifecycle
    ON traces(card_id, run_id, event_type)
    WHERE event_type IN ('trace_start', 'trace_end');

-- Open-run lookup for the reconcile-on-startup pass (§4.1 step 5).
CREATE INDEX idx_traces_open_runs
    ON traces(event_type, card_id) WHERE event_type = 'trace_start';

-- artifacts and notes are v0.2 (see §9): they had zero integration with the
-- core loop (board → card → claude → trace) and were cut from v0.1. No rows
-- exist here until v0.2 re-adds their tables.

-- ui_state: runtime selection state, not a domain table (§5). Single row.
CREATE TABLE ui_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),   -- single row, enforced
    last_workspace_id TEXT REFERENCES workspaces(id) ON DELETE SET NULL,
    last_board_id TEXT REFERENCES boards(id) ON DELETE SET NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now'))
);
INSERT INTO ui_state (id) VALUES (1);

-- +goose Down
DROP TABLE IF EXISTS traces;
DROP TABLE IF EXISTS cards;
DROP TABLE IF EXISTS codebases;
DROP TABLE IF EXISTS columns;
DROP TABLE IF EXISTS boards;
DROP TABLE IF EXISTS workspaces;
DROP TABLE IF EXISTS ui_state;
