---
title: Wiki Log
description: Append-only audit trail of wiki generation and refresh runs.
---

# Wiki Log

Append-only audit trail. Add one dated entry per generation or refresh run, recording the profile, the `source_commit` it was anchored to, and the coverage. The codebase-wiki skill describes the entry shape.

## 2026-08-09: generate

- Profile: internal/standard
- source_commit: adcc17df (initial generation)
- Coverage: full wiki from docs-only design repo — 5 architecture pages, 8 module pages, 4 flow pages, 7 concept pages, 4 guide pages
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [Session Model](./architecture/session-model.md), [Data Model](./architecture/data-model.md), [Agent Abstraction](./architecture/agent-abstraction.md), [Trace Recording](./architecture/trace-recording.md), [config](./modules/config.md), [agent](./modules/agent.md), [store](./modules/store.md), [trace](./modules/trace.md), [session](./modules/session.md), [board](./modules/board.md), [cli](./modules/cli.md), [tui](./modules/tui.md), [Card open to completion](./flows/card-open-complete.md), [Attach/detach](./flows/attach-detach.md), [Trace reconciliation](./flows/trace-reconciliation.md), [Failure paths](./flows/failure-paths.md), [run](./concepts/run.md), [session](./concepts/session.md), [stage](./concepts/stage.md), [agent-driver](./concepts/agent-driver.md), [watch-scope](./concepts/watch-scope.md), [trace-events](./concepts/trace-events.md), [git-reconciliation](./concepts/git-reconciliation.md), [Add a new agent](./guides/add-a-new-agent.md), [opencode-launch-semantics](./guides/opencode-launch-semantics.md), [change-the-schema](./guides/change-the-schema.md), [session-command](./guides/session-command.md)

## 2026-08-09: refresh

- Profile: internal/standard
- source_commit: 724b0dd (was adcc17df)
- Coverage: T1 config package landed as real Go source since the last stamp — `internal/config` (Default/Load/Validate) + go.mod; refreshed the config module page and the docs-only/planned framing in Overview and Architecture Overview
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [config](./modules/config.md)

## 2026-08-09: refresh

- Profile: internal/standard
- source_commit: e2ebff6 (was 724b0dd)
- Coverage: T2 agent contract landed as real Go source since the last stamp — `internal/agent` (Driver/LaunchMode/SessionSpec, registry mechanism, Card+AgentOrDefault, BuildPrompt, PosixEscape/CommandLine, agent.Validate); refreshed the agent module page and the config+agent/planned framing in Overview and Architecture Overview; claude/opencode drivers remain T3 (registry empty)
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [agent](./modules/agent.md)

## 2026-08-09: refresh

- Profile: internal/standard
- source_commit: f729599 (was e2ebff6)
- Coverage: T3 claude + opencode drivers landed as real Go source since the last stamp — `internal/agent` claude.go/opencode.go (argv builders per DESIGN-002 §9.1/§9.2, `init()` self-registration into the registry, PATH-independent Resolve); refreshed the agent module page (registry now populated), the implementation-state framing in Overview / Architecture Overview / Agent Abstraction, and corrected the registration step in the add-a-new-agent guide (init() in the driver file, not the driver.go map literal); claude/opencode drivers no longer design
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [Agent Abstraction](./architecture/agent-abstraction.md), [agent](./modules/agent.md), [Add a new agent](./guides/add-a-new-agent.md)

## 2026-08-09: refresh

- Profile: internal/standard
- source_commit: 4fdc915 (was f729599)
- Coverage: T4 store migrations + pragmas landed as real Go source since the last stamp — `internal/store` store.go (`RegisterConnectionHook` per-connection pragmas, `Open(path) (*sql.DB, error)`, `migrateUp`) + `migrate/` (`embed.FS`, `00001_initial.sql` full §3.3 schema + `ui_state` + Down, `00002_card_agent.sql`); CRUD/reorder/trace lifecycle remain planned (T5–T7); deps now pin `modernc.org/sqlite v1.33.1` (v1.33.0 retracted) + `pressly/goose v3.21.0`; refreshed the store module page, the implementation-state framing in Overview / Architecture Overview / Data Model, and the change-the-schema pragma gotcha
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [Data Model](./architecture/data-model.md), [store](./modules/store.md), [Change the schema](./guides/change-the-schema.md)

## 2026-08-09: refresh

- Profile: internal/standard
- source_commit: a390749 (was 4fdc915)
- Coverage: T5 store kanban CRUD landed as real Go source since the last stamp — `internal/store` workspaces.go/boards.go/columns.go/codebases.go (entity CRUD, `CreateBoard` seeds the 5 default columns atomically via a shared `execer`, `CreateColumn` appends at max+1000), ui_state.go (single-row get/set), init.go (`InitWorkspace`, idempotent keyed on root_path), ids.go (`NewID()` 16 crypto/rand bytes → 32 hex); cards.go reorder + traces.go lifecycle remain planned (T6–T7); refreshed the store module page (full public API), and the implementation-state framing in Overview / Architecture Overview / Data Model
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [Data Model](./architecture/data-model.md), [store](./modules/store.md)

## 2026-08-09: refresh

- Profile: internal/standard
- source_commit: 9cc976a (was a390749)
- Coverage: T6 store cards CRUD + reorder landed as real Go source since the last stamp — `internal/store/cards.go` (`Card` with `*string` nullable fields + `AgentOrDefault`, `CreateCard` append at max+1000, partial `UpdateCard` where non-nil `""` clears a nullable col, `GetCard`/`DeleteCard`, `ListCardsByBoard`/`ListCardsByColumn`, `MoveCard` anchored `(prev+next)/2` with pre-write whole-column renumber, `ErrPartialAnchors`/`ErrCrossBoardMove`), tests in cards_test.go (85.8% store coverage); boards.go `execer` gained `QueryContext`; traces.go lifecycle remains planned (T7); refreshed the store module page (card CRUD + public API + key files), and the implementation-state framing in Overview / Architecture Overview / Data Model
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [Data Model](./architecture/data-model.md), [store](./modules/store.md)
