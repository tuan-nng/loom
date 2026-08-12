---
title: Wiki Log
description: Append-only audit trail of wiki generation and refresh runs.
---

# Wiki Log

Append-only audit trail. Add one dated entry per generation or refresh run, recording the profile, the `source_commit` it was anchored to, and the coverage. The codebase-wiki skill describes the entry shape.

## 2026-08-12: refresh

- Profile: internal/standard
- source_commit: f24463f (was 5b99765)
- Coverage: T20 TUI extras (search, board/workspace switch, help overlay) landed as real Go source since the last stamp — `internal/tui/search.go` (the `/` overlay: `searchMsg`, `filterCards`/`visibleCards` — case-insensitive title/description substring mirroring `loom card list --search` — `openSearch` seeded with the active filter, `afterSearch` applying it client-side with no re-fetch; an empty submit clears the filter, esc preserves the prior filter, later re-fetches preserve it), `internal/tui/switch.go` (the `s`/`w` pickers: `switchForm` as a single cycle field seeded via `indexOf`, `openBoardPicker`/`openWorkspacePicker` doing synchronous list reads that degrade to a status-bar notice on error/empty, submit routing through `ShowBoard`/`SwitchWorkspace` — persisting `{workspace, board}` / clearing the board per ADR-001 §6 — folded by `afterBoardSwitched`/`afterWorkspaceSwitched` into a re-fetch + toast), `internal/tui/help.go` (the `?` overlay: `helpOverlay` with esc/q close + full key swallowing, `helpRows`/`keyNames` derived from `defaultKeyMap()` so the help can never drift from the bindings), plus the `applyFetch` board-transition reset of kill-suppression and `pendingFocus` (both board-scoped). Widened the `tui.Service` seam with `ListBoards`/`ShowBoard`/`ListWorkspaces`/`SwitchWorkspace` (auto-satisfied by the embedded `*board.Service`); `fakeService` mirrors all four. Tests: search (narrows the board + shrinks header counts, description/case matching, esc restores the filter, empty clears, survives refetch), switch (open/cycle/submit + re-fetch, esc cancels without a service write, list-error/empty degrade to a notice, submit-error toasts, board-scoped kill-suppression cleared on transition), help (renders the canonical row set, esc/q close, keys swallowed) — replacing the old `TestStubKeysSetNotice`. Full `go build`/`go vet`/`go test ./...` green, TUI ~87% coverage. Updated the tui module page (summary, responsibilities, widened Service, the search.go/switch.go/help.go + test key files, duplicate app_test bullet retired), the implementation-state framing in Overview + Architecture Overview (no TUI keys left stubbed), and re-stamped source_commit.
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [tui](./modules/tui.md)

## 2026-08-11: refresh

- Profile: internal/standard
- source_commit: 5b99765 (was 8fd9dcc)
- Coverage: T19 card detail pane landed as real Go source since the last stamp — `internal/store/traces.go` (`CardRun{RunID, StartedAt, EndedAt *string, DurationMs, Files []string, FilesChanged}` + `RunsForCard(db, cardID)` — every run of one card newest-first by the seq of each run's `trace_start` (never `created_at`; pinned by `TestRunsForCardSeqNotTimestampOrder`), a single scan grouped in Go over rows ordered by `(run_id, seq)` served by `idx_traces_card_run`, `EndedAt` nil + `DurationMs` 0 iff the run is still open, duration = `trace_end.created_at − trace_start.created_at`, files deduped + lexicographically sorted, corrupt `data_json`/`created_at` surface as errors; exported `TraceTimeLayout` for the TUI), `internal/board/service.go` (`RunsForCard` passthrough, `board:` wrap), `internal/tui/card_detail.go` (the `d` overlay: `cardDetail` struct + `openCardDetail`/`detailUpdate` esc-q-close keyPress guard/`detailView` via `lipgloss.Place` centered box, `renderMarkdown` tolerant plain-text renderer, `detailRunLine` with ●/◉ open-run markers + friendly clock) wired into `app.go` (`detail` field, `d` route after the form guard, `noteDetail` const retired) + `board.go` layout. Widened the `tui.Service` seam with `GetCodebase` + `RunsForCard` (auto-satisfied by the embedded `*board.Service`); `fakeService` mirrors both (`runs`/`runsErr`/`runsCalled`, `codebases`/`cbErr`). Glamour added to go.mod as an indirect dep but the renderer is plain-text for now (full Glamour later). Tests: 10 `RunsForCard*` store tests + a `TestSeqBeatsTimestamp` discrimination drive-by (2030→2000), `TestRunsForCardPassthrough` in board, 8 card_detail tests (open/no-card/esc-close/fields/agent-(default)-suffix/codebase-path/runs+errors/fetch) — full `go build`/`go vet`/`go test ./...` green. T20 (search + board/workspace switch + help) remains stubbed. Updated the tui module page (T19 overlay responsibility + responsibilities + widened Service + card_detail.go/card_detail_test.go key files + Glamour note), store + board module pages (`RunsForCard`/`CardRun` API), the implementation-state framing in Overview + Architecture Overview, and re-stamped source_commit.
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [tui](./modules/tui.md), [store](./modules/store.md), [board](./modules/board.md)

## 2026-08-11: refresh

- Profile: internal/standard
- source_commit: 8fd9dcc (was 1eee0fe)
- Coverage: T18 TUI forms landed as real Go source since the last stamp — `internal/tui/forms.go` (the four overlays: `n` new card title/column/priority/agent, `e` edit title/description/objective/acceptance/priority/labels/agent, `N` new column name/stage, `m` move — one concrete `form` struct with text (`textinput`) + in-place cycle fields, tab/shift+tab/enter/esc, centered `lipgloss.Place` box that owns every key, form-local error line; create keeps empty optionals nil, edit value-diffs and writes `&""` to clear nullable cols, agent picker empty-default → NULL on create / `&""` reset on edit via a `touched` flag, post-submit refocus via `pendingFocus`/`applyPendingFocus`). Widened the `tui.Service` seam with `CreateCard`/`UpdateCard`/`GetCard`/`CreateColumn`/`MoveCard` (satisfied by the embedded `*board.Service`; done-stage auto-kill on `m` routes through `MoveCard`). Promoted the closed validation sets out of the CLI into `store.ValidPriorities` + `store.ValidStages` (stages derived from `DefaultColumns`), the CLI now validates with `slices.Contains` and byte-identical messages; T19 (card detail `d`) and T20 (search/switch/help) remain stubbed. Updated the tui module page (forms responsibilities, widened Service, forms.go/forms_test.go key files), the implementation-state framing in Overview + Architecture Overview, the cli + store module pages (validation-set promotion), and re-stamped source_commit.
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [tui](./modules/tui.md), [cli](./modules/cli.md), [store](./modules/store.md)

## 2026-08-11: refresh

- Profile: internal/standard
- source_commit: 1eee0fe (was 08913fd)
- Coverage: T16/T17 TUI landed as real Go source since the last stamp — `internal/tui` (BubbleTea v2, bubbles v2, lipgloss v2; app.go/keymap.go/board.go/app_test.go): the board shell (canonical §3.5 keymap, five-column layout, navigation, quit confirm/force) plus live session control (per-card cl/oc agent badges, ●/◉ session markers on a 2s poll with re-arm, Enter = ensure + `tea.ExecProcess` tmux attach, K = kill + finalize with toast suppression). Bare `loom` on a TTY now routes to the TUI via `internal/cli/tui.go` (tuiService adapter: default agent + `tmux -L <server> attach-session` argv); v2 charm deps in go.mod. Rewrote the tui module page design placeholder as real source, updated the implementation-state framing in Overview + Architecture Overview, the bare-loom routing in the cli module page, and the Enter trigger/attach handoff in the card-open-complete flow; T18–T20 (forms, card detail, search/switch/help) remain stubbed.
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [cli](./modules/cli.md), [tui](./modules/tui.md), [Card open → completion](./flows/card-open-complete.md)

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
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [cli](./modules/cli.md)

## 2026-08-10: refresh

- Profile: internal/standard
- source_commit: 08913fd (was 371a065)
- Coverage: T15 CLI session commands landed as real Go source since the last stamp — `internal/cli/session.go` (`runCardOpen` ensure + conditional attach, `--detach` = ensure-and-return, the first bool flag in the codebase; `runCardClose` kill + finalize, idempotent on a never-opened card; `runAttach` pure attach — the card must already have a live session, `session: card <id> has no live session` otherwise; `runSessions` reuses status's `renderSessions` verbatim so the `●`/`◉` output can never drift, probe-first degrade notice on missing tmux with the same exit-0 tradeoff as `loom status`) plus `board.Service.Attach` (`GetCard` + `sess.Attach` passthrough, `board:` error wrap convention); tests in session_test.go (stubSess-driven: ensure/attach/kill/reconcile counters, detach ordering, missing-card gate, no-tmux loud-vs-degrade split for open-vs-sessions, sorted `◉`/`●` output, usage errors) + stubSess attach tracking in cli_test.go; the cli module page's stub line is retired outright — there are no stubbed leaves left in the CLI, only the TUI is still design; refreshed the cli module page (session subtree replaces the T15 stub note: session-command invariants section, `session.go` + `session_test.go` key files, 80.0 → 80.5% cli coverage), the implementation-state framing in Overview / Architecture Overview, and re-stamped source_commit
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [cli](./modules/cli.md), [board](./modules/board.md), [store](./modules/store.md)

## 2026-08-10: refresh

- Profile: internal/standard
- source_commit: 371a065 (was d543251)
- Coverage: T14 card CLI subtree landed as real Go source since the last stamp — `internal/cli/card.go` already enumerated in full (add/update/list/show/move/delete + `columnOf`/`findCodebase`/`filterCards`/`agentBadge`/`acceptedAgents`/`derefStr`); refreshed the cli module page (card subtree now ends the stub line: only open/close/attach/sessions remain T15, added the card-command invariants section and the flag.Visit merge-patch semantics, badge + search + done-stage-kill notes), the implementation-state framing in Overview / Architecture Overview (only TUI now design), and re-stamped source_commit
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [cli](./modules/cli.md)

## 2026-08-12: refresh

- Profile: internal/standard
- source_commit: b22a5a4 (was f24463f)
- Coverage: T21 integration tests + T22 docs adoption landed since the last stamp — `internal/session/session_test.go` gained the DESIGN-002 §16 verification-matrix rows: `TestEnsurePerDriverSessionNameCwdAndArgv` (parametrized claude/opencode-mini/opencode-full stub tests against real tmux — `loom-<id>` session naming, launch cwd, and the prompt's argv-quoting round-trip through tmux's `$SHELL -c` re-parse), `TestReconcileOnStartupAttributesGitChanges` (a run opened with no live fsnotify watcher — simulating a run that outlives the loom process — still git-reconciled correctly by `ReconcileOnStartup`), and `TestOpencodeFullTUIAutoSubmitCanary` (automates the `docs/PROBE-full-tui.md` method against the real opencode CLI for both `mini` and `full`; opt-in via `LOOM_TEST_LIVE_OPENCODE=1` since it makes a live LLM call, so a default `go test ./...` never spends it); `docs/ADR-002-loom-multi-agent.md` flipped **Status: Adopted**, folded the T0 full-TUI verdict into its §8 Risks / §10 Verification Strategy, and corrected a stale post-adoption reference (§12 still claimed `run --interactive` was rejected); a new project `README.md` landed (build, config, both agent drivers, verified opencode version, testing incl. the live-canary opt-in); a stale doc comment in `cmd/loom/main.go` was fixed (bare `loom` already launches the TUI since T16, not "prints help"). This closes `docs/TASKS-loom-v0.1.md` — all of T0–T22 are now real Go source / adopted docs; only the manual E2E step (ADR-001 §10's coverage-bar exception) remains unautomated. Refreshed the session module page (T21 key files + verification note), Overview and Architecture Overview (v0.1-backlog-complete framing, README link), and re-stamped source_commit.
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [session](./modules/session.md)
