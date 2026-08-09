---
title: trace (internal/trace)
description: The trace recorder — fsnotify watcher, .loomignore rules, run start/end, and path-keyed git-baseline reconciliation.
type: module
tags: [wiki, module, trace]
---

## Summary

`internal/trace` records what a coding agent changed during a card session. It owns the **fsnotify watcher** (scoped to the watch scope, with ignore rules) and the **git-baseline reconciliation** that attributes file changes even when the session outlives loom. It imports `store` only (DESIGN-002 §4.2). Spec: ADR-001 §4.6, §5.

**Status (T8 landed):** the git-baseline reconciliation layer is real Go source in `internal/trace/git.go` — the `Baseline`/`Change` structs, `SnapshotBaseline` (short-lived exec-git client), `Reconcile` (exec-free, table-tested path-keyed reconcile), `Dedup`, and `FilesChanged`, plus unexported porcelain/name-status parsers and `gitError` 128 classification. The fsnotify watcher (`watcher.go`) and the recorder that wires trace rows (`recorder.go`) remain planned T9.

## Responsibilities

- Snapshot a git baseline pair on run start: `git rev-parse HEAD` + `git status --porcelain` (only when the watch scope is inside a git repo).
- Walk the watch scope and register one fsnotify watch per directory (not recursive); add watches on `Create` events; skip ignored dirs.
- Record `file_change` events (watch-scope-relative path + created/modified/deleted) for the active `run_id`.
- On completion, compute the authoritative change set from baseline vs completion porcelain, **keyed on path**, deduped against live fsnotify events, and write missing `file_change` rows + `trace_end` (with `files_changed` = unique paths).

## Public API / entry points

Landed in T8 (`internal/trace/git.go`):

```go
type Baseline struct { BaseHead, Porcelain string } // empty pair outside a git repo
func SnapshotBaseline(root string) (Baseline, error) // non-repo → empty Baseline, nil error
func Reconcile(baseline, current Baseline, diffOut string) ([]Change, error)
type Change struct { Path, Operation string }        // Operation: store.Op* const
func Dedup(live []Change, changes []Change) []Change
func FilesChanged(changes []Change) int
```

Planned T9 (recorder + watcher — design-shape only):

```go
type Recorder struct { ... }
func (r *Recorder) SnapshotBaseline(root string) map[string]string
func (r *Recorder) StartRun(ctx, cardID, root string, baseline map[string]string) (runID string, err error)
func (r *Recorder) Watch(ctx, card, root, runID string)
func (r *Recorder) AbortRun(ctx, runID string) error
func (r *Recorder) Finalize(ctx, runID string, baseline map[string]string) error
```

## Key files

- `internal/trace/git.go` — landed T8: `Baseline`/`Change`, `SnapshotBaseline` (short-lived exec git clients, `notARepo` classification), `Reconcile` (working-tree set + committed set from injected name-status text, committed op wins, deterministic path sort), `Dedup`, `FilesChanged`
- `internal/trace/recorder.go` — planned T9: trace_start/end wiring, `files_changed`
- `internal/trace/watcher.go` — planned T9: fsnotify + `.loomignore` + built-in ignore defaults

## Dependencies

- `store` only (writes trace rows). External: fsnotify, git CLI.

## Participates in

- Driven by [SessionManager.ensure](../architecture/session-model.md): baseline snapshotted **before** launch, `StartRun` called **after** the startup probe passes; `AbortRun` deletes the whole run on error (never leaves an orphaned `trace_end`).
- `Finalize`/reconcile runs at completion, `K`, `loom card close`, done-stage move, and reconcile-on-startup.

## Related

- Architecture: [Trace Recording](../architecture/trace-recording.md) · [Session Model](../architecture/session-model.md)
- Concepts: [run](../concepts/run.md) · [trace-events](../concepts/trace-events.md) · [watch-scope](../concepts/watch-scope.md) · [git-reconciliation](../concepts/git-reconciliation.md)
- Flows: [Trace reconciliation](../flows/trace-reconciliation.md) · [Failure paths](../flows/failure-paths.md)
