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

## 2026-08-09: refresh

- Profile: internal/standard
- source_commit: 6053c96 (was 9cc976a)
- Coverage: T7 store traces run lifecycle landed as real Go source since the last stamp — `internal/store/traces.go` (`StartRun(db, cardID, runID, baseHead, porcelain)` with the `data_json` git-pair shape omitted for git-less runs, `RecordChange`/`EndRun` resolving card_id from the run's trace_start in a tx, store-validated `created|modified|deleted` ops via `OpCreated`/`OpModified`/`OpDeleted`, `OpenRuns` → `[]OpenRun{CardID,RunID,BaseHead,Porcelain}` for reconcile-on-startup, whole-run idempotent `AbortRun`), tests in traces_test.go (85.9% store coverage); session/TUI/CLI remain planned; refreshed the store module page (T7 responsibilities + run-lifecycle API + key files), the implementation-state framing in Overview / Architecture Overview / Data Model, and re-stamped source_commit
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [Data Model](./architecture/data-model.md), [store](./modules/store.md)

## 2026-08-09: refresh

- Profile: internal/standard
- source_commit: 2d2b56d (was 6053c96)
- Coverage: T8 trace git-baseline reconciliation landed as real Go source since the last stamp — `internal/trace/git.go` (`Baseline`/`Change` structs, short-lived-exec `SnapshotBaseline` with empty-pair non-repo semantics, exec-free `Reconcile` combining the path-keyed working-tree set with a committed set parsed from injected name-status text — committed op wins on conflict, deterministic path sort, `T`→modified — `Dedup`, `FilesChanged`, porcelain parsers, `gitError` 128 classification), tests in git_test.go; watcher/recorder remain planned T9; refreshed the trace module page (landed API + key files), the trace reconciliation flow (op mapping, committed-wins precedence, T8 status), trace-recording architecture and git-reconciliation concept (landed functions), plus implementation-state framing + re-stamped source_commit in Overview
- Pages: [Overview](./OVERVIEW.md), [trace](./modules/trace.md), [Trace reconciliation](./flows/trace-reconciliation.md), [Trace Recording](./architecture/trace-recording.md), [git-reconciliation](./concepts/git-reconciliation.md)

## 2026-08-09: refresh

- Profile: internal/standard
- source_commit: b0b428e (was 2d2b56d)
- Coverage: T9 trace recorder + watcher landed as real Go source since the last stamp — `internal/trace/recorder.go` (`Recorder` run lifecycle: `StartRun`/`Watch`/`RecordChange`/`LiveChanges`/`EndRun`/`AbortRun`, run-scoped watcher registry with drain+unregister `stopWatcher`), `internal/trace/watcher.go` (per-directory fsnotify walk tracking `w.dirs`, event loop with stop-drain, on-the-fly `Create` dir watches, no rows for dirs/chmod, dir remove/rename classified via the watch set), `internal/trace/ignore.go` (built-in dir ignores + `.loomignore` gitignore-subset matcher), fsnotify added as a dep, watcher tests under `-race`; session/TUI/CLI remain planned; refreshed the trace module page (T9 API + key files), trace recording architecture and trace-reconciliation flow (T9 status, live leg), watch-scope concept (ignore rules live in ignore.go), plus implementation-state framing + re-stamped source_commit in Overview
- Pages: [Overview](./OVERVIEW.md), [trace](./modules/trace.md), [Trace reconciliation](./flows/trace-reconciliation.md), [Trace Recording](./architecture/trace-recording.md), [watch-scope](./concepts/watch-scope.md)

## 2026-08-10: refresh

- Profile: internal/standard
- source_commit: b156ea6 (was 1e545f4)
- Coverage: T12 BoardService landed as real Go source since the last stamp — `internal/board/service.go` (`Service{db, sess}` full-facade seam: card create/get/list/update/move/delete + workspace/board/column/codebase CRUD passthroughs, selection switching persisting `ui_state` — `SwitchWorkspace` clears the board selection, `ShowBoard`/`CreateBoard` persist both, `ResolveSelection` via the ADR-001 §6 fallback chain ui_state → `MostRecentWorkspace` → `FirstBoard` → `ErrNotInitialized` ("run loom init") — session actions `OpenCard` (Ensure + conditional Attach, detach flag), `CloseCard`, `SessionStatus`, `ReconcileOnStartup` delegating to a consumer-side `sessionManager` interface satisfied by `*session.Manager`, and the done-stage rule on `MoveCard`: store.MoveCard commits first, then kill+finalize when the target column's stage is `done`, kill errors surfaced loudly and retry-safe; `board:` error-prefix convention throughout), tests in service_test.go (fake-manager unit suite: done-kill/no-kill matrix, cross-board + partial-anchor rejection, kill-after-commit, selection fallback incl. deleted selection and no-workspaces, ui_state persistence, CRUD round-trips — 82% board coverage) + integration_test.go (real-tmux `-L loomselftest` + stub-agent open/close round-trip: one trace_start, kill, one trace_end); TUI/CLI remain planned; refreshed the board module page (real API surface), the implementation-state framing in Overview / Architecture Overview, and re-stamped source_commit
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [board](./modules/board.md)

## 2026-08-09: refresh

- Profile: internal/standard
- source_commit: 1e545f4 (was c8a36d1)
- Coverage: T11 SessionManager landed as real Go source since the last stamp — `internal/session/manager.go` (`NewManager`, driver-aware `Ensure` per DESIGN-002 §10.2: reuse → resolve agent → `driver.Launch` → baseline snapshot → `new-session` → ~500ms probe → record run after the probe → watcher start, killing any created session + `AbortRun` on every post-creation error path; `Attach` via `exec.Command(attach-session)` with stdio wired; `Kill`; one-tick `Status` using the new `Sessions` live-state listing + finalizing disappeared sessions; `ReconcileOnStartup` finalizing open runs whose session is absent; a single shared `completeRun` path — git-reconcile against the stored baseline, missing `file_change` rows, `trace_end` with `files_changed = unique(live ∪ missing)`, `durationMs=0` for startup-reconciled runs, blind finalize for deleted cards/shared scopes), `internal/trace/git.go` (`gitDiffNameStatus` → exported `GitDiffNameStatus` for the manager), `internal/session/tmux.go` (`SessionState` + `Sessions` via one `-F '#{session_name}\t#{session_attached}'` call, nil-nil on missing/empty server), tests in session_test.go + tmux_test.go + git_test.go; TUI/CLI remain planned; refreshed the session module page (T11 API + invariant story), Session Model architecture (completeRun, status tick, reconcile), the session concept, and the implementation-state framing + re-stamped source_commit in Overview and Architecture Overview
- Pages: [Overview](./OVERVIEW.md), [session](./modules/session.md), [Architecture Overview](./architecture/overview.md), [Session Model](./architecture/session-model.md), [session](./concepts/session.md)

## 2026-08-09: refresh

- Profile: internal/standard
- source_commit: c8a36d1 (was b0b428e)
- Coverage: T10 tmux client wrapper landed as real Go source since the last stamp — `internal/session/tmux.go` (`Tmux` with `Server` + resolved `bin`, `New(server)` resolving the binary once and gating tmux ≥ 3.x with the ADR-001 §8 install hint, value-receiver methods per DESIGN-002 §10.1 — `NewSession`/`HasSession`/`CapturePane`/`SendKeys`/`KillSession`/`ListSessions`, `SessionName` panicking on `:`, all exec funneled through `run()` converting `*exec.ExitError` → typed `tmuxError{code,stderr}`, exported `MissingServer` predicate for the cold-server retry-once path, `NewSession` retrying the transient cold-server startup race), tests in tmux_test.go (unit tables + real-tmux round-trip on an isolated `-L loomselftest` server, 87.5% coverage); SessionManager/TUI/CLI remain planned; refreshed the session module page (T10 API + key files, shipped vs planned split), the implementation-state framing in Overview / Architecture Overview, the module-nav status line, and re-stamped source_commit
- Pages: [Overview](./OVERVIEW.md), [session](./modules/session.md), [Architecture Overview](./architecture/overview.md), [Session Model](./architecture/session-model.md), [session](./concepts/session.md)

## 2026-08-10: refresh

- Profile: internal/standard
- source_commit: d543251 (was b156ea6)
- Coverage: T13 CLI router landed as real Go source since the last stamp — `internal/cli/` (`root.go`: `command` table mirroring ADR-001 §6, `App`/`newApp` deps bundle, `Main(args) int` bootstrap, `finish` exit-code mapping, `parseFlags`/`expectArgs`, `reorderFlags` for positionals-before-flags; `commands.go`: workspace/board/column/codebase handlers with `--stage` CHECK validation defaulting to `todo`; `status.go`: deterministic `key: value` stream — selection, column card counts, `●`/`◉` session markers, `RecentRuns` — degrade-on-no-tmux; `config.go`: effective TOML via BurntSushi encoder; `lazy.go`: `lazySession` proxy deferring `session.New`+`NewManager`, cached success/failure, `probe()` for status degradation) + `cmd/loom/main.go` (config.Load → agent.Validate → MkdirAll → store.Open → dispatch); `board.ResolveWorkspace` (workspace-only §6 fallback, boardless workspace no longer dead-ends `board create`) + `store.RecentRuns` (newest-ended-first by seq) seam additions; card/attach/sessions stubbed (T14/T15); refreshed the cli module page (rewritten to real API), board + store module pages (new API), implementation-state framing in Overview / Architecture Overview (only TUI now design), re-stamped source_commit
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [cli](./modules/cli.md), [board](./modules/board.md), [store](./modules/store.md)

## 2026-08-10: refresh

- Profile: internal/standard
- source_commit: 371a065 (was d543251)
- Coverage: T14 card CLI subtree landed as real Go source since the last stamp — `internal/cli/card.go` already enumerated in full (add/update/list/show/move/delete + `columnOf`/`findCodebase`/`filterCards`/`agentBadge`/`acceptedAgents`/`derefStr`); refreshed the cli module page (card subtree now ends the stub line: only open/close/attach/sessions remain T15, added the card-command invariants section and the flag.Visit merge-patch semantics, badge + search + done-stage-kill notes), the implementation-state framing in Overview / Architecture Overview (only TUI now design), and re-stamped source_commit
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [cli](./modules/cli.md)