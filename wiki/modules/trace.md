---
title: trace (internal/trace)
description: The trace recorder — fsnotify watcher, .loomignore rules, run start/end, and path-keyed git-baseline reconciliation.
type: module
tags: [wiki, module, trace]
---

## Summary

`internal/trace` records what a coding agent changed during a card session. It owns the **fsnotify watcher** (scoped to the watch scope, with ignore rules) and the **git-baseline reconciliation** that attributes file changes even when the session outlives loom. It imports `store` only (DESIGN-002 §4.2). Spec: ADR-001 §4.6, §5.

**Status (T9 landed):** the full recording path is real Go source. `git.go` (T8) holds the baseline/reconcile logic (`SnapshotBaseline`/`Reconcile`/`Dedup`/`FilesChanged`, porcelain/name-status parsers, `gitError` 128 classification). `recorder.go` (T9) wires the run lifecycle through the store — `StartRun`/`Watch`/`RecordChange`/`LiveChanges`/`EndRun`/`AbortRun`, with a run-scoped watcher registry (`stopWatcher` drains and unregisters). `watcher.go` (T9) walks the scope and runs the fsnotify event loop. `ignore.go` (T9) implements the built-in default dir ignores + the `.loomignore` gitignore-subset matcher. Still design: the `session` package that drives the recorder at open/close (SessionManager wiring, finalize/reconcile orchestration).

## Responsibilities

- Snapshot a git baseline pair on run start: `git rev-parse HEAD` + `git status --porcelain` (only when the watch scope is inside a git repo).
- Walk the watch scope and register one fsnotify watch per directory (not recursive); add watches on `Create` events; skip ignored dirs.
- Record `file_change` events (watch-scope-relative path + created/modified/deleted) for the active `run_id`.
- On completion, compute the authoritative change set from baseline vs completion porcelain, **keyed on path**, deduped against live fsnotify events, and write missing `file_change` rows + `trace_end` (with `files_changed` = unique paths).

## Public API / entry points

Landed T8 + T9 (`internal/trace`):

```go
type Baseline struct { BaseHead, Porcelain string } // empty pair outside a git repo
func SnapshotBaseline(root string) (Baseline, error) // non-repo → empty Baseline, nil error
func Reconcile(baseline, current Baseline, diffOut string) ([]Change, error)
type Change struct { Path, Operation string }        // Operation: store.Op* const
func Dedup(live []Change, changes []Change) []Change
func FilesChanged(changes []Change) int
```

Recorder + watcher (landed T9):

```go
func NewRecorder(db *sql.DB) *Recorder
func (r *Recorder) StartRun(cardID, root string, baseline Baseline) (string, error) // writes trace_start, returns runID
func (r *Recorder) Watch(runID, root string) error                                 // fsnotify + ignore rules; also rejects a duplicate watch
func (r *Recorder) RecordChange(runID, path, operation string) error               // store-validated op; unknown/stopped run silently dropped
func (r *Recorder) LiveChanges(runID string) ([]Change, error)                     // path-keyed dedup, sorted, store-backed
func (r *Recorder) EndRun(runID string, durationMs, filesChanged int) error        // stop watcher, write trace_end
func (r *Recorder) AbortRun(runID string) error                                    // stop watcher, delete the whole run
```

## Key files

- `internal/trace/git.go` — landed T8: `Baseline`/`Change`, `SnapshotBaseline` (short-lived exec git clients, `notARepo` classification), `Reconcile` (working-tree set + committed set from injected name-status text, committed op wins, deterministic path sort), `Dedup`, `FilesChanged`
- `internal/trace/recorder.go` — landed T9: `Recorder` + run-scoped watcher registry; `StartRun`/`Watch`/`RecordChange`/`LiveChanges`/`EndRun`/`AbortRun`; `stopWatcher` drain + unregister
- `internal/trace/watcher.go` — landed T9: per-directory fsnotify walk, event loop with stop-drain, `Create` on-the-fly dir watches, no rows for dirs/chmod
- `internal/trace/ignore.go` — landed T9: built-in dir ignores + `.loomignore` matcher (`parseIgnorePattern`, last-match-wins)

## Dependencies

- `store` only (writes trace rows). External: fsnotify, git CLI.

## Participates in

- Will be driven by the `session` package (planned): baseline snapshotted **before** launch, `StartRun` called **after** the startup probe passes; `AbortRun` deletes the whole run on error (never leaves an orphaned `trace_end`); `EndRun` + `Reconcile`/`Dedup` run at completion, `K`, `loom card close`, done-stage move, and reconcile-on-startup.
- `LiveChanges` feeds `trace.Dedup` (the live leg); its unique-path count feeds `trace_end.files_changed`.

## Related

- Architecture: [Trace Recording](../architecture/trace-recording.md) · [Session Model](../architecture/session-model.md)
- Concepts: [run](../concepts/run.md) · [trace-events](../concepts/trace-events.md) · [watch-scope](../concepts/watch-scope.md) · [git-reconciliation](../concepts/git-reconciliation.md)
- Flows: [Trace reconciliation](../flows/trace-reconciliation.md) · [Failure paths](../flows/failure-paths.md)
