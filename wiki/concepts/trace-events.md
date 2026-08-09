---
title: trace-events
description: The three trace event types (trace_start, file_change, trace_end), their data_json shapes, and the seq ordering key.
type: concept
tags: [wiki, concept, trace]
---

## Definition

Trace events are the rows in the `traces` table that record a run's lifecycle and the files the agent changed. Three `event_type`s: **`trace_start`** (opens a run; stores the git baseline pair), **`file_change`** (a path + created/modified/deleted), **`trace_end`** (closes the run; stores `duration_ms` and `files_changed`). All events in one cycle share a `run_id` (ADR-001 §3.3).

## Why it matters

- **Event order is `seq`, not timestamps.** `traces.seq` (`INTEGER PRIMARY KEY AUTOINCREMENT`) is the sole ordering key — `datetime('now')` ties at whole seconds and even `strftime(...%f...)` ties within a millisecond on consecutive inserts. `AUTOINCREMENT` (not a bare rowid) survives `VACUUM`, so history order is stable.
- **`data_json` shapes are prescribed** (ADR-001 §3.3): `trace_start` carries `{"git": {"base_head", "porcelain"}}`; `file_change` carries `{"path", "operation"}`; `trace_end` carries `{"duration_ms", "files_changed"}`.
- **Lifecycle is enforced**: exactly one `trace_start` and at most one `trace_end` per run via the partial UNIQUE index `idx_traces_run_lifecycle` (double-Enter race).
- `files_changed` = unique paths in the run, computed by [git-reconciliation](./git-reconciliation.md).

## Where it lives

- `traces` table DDL + index + `data_json` shapes: ADR-001 §3.3.
- Written by `internal/trace` (recorder/watcher/git) and the store's `StartRun`/`AbortRun`/record APIs.

## Related

- Concepts: [run](./run.md) · [watch-scope](./watch-scope.md) · [git-reconciliation](./git-reconciliation.md)
- Architecture: [Trace Recording](../architecture/trace-recording.md) · [Data Model](../architecture/data-model.md)
- Flows: [Trace reconciliation](../flows/trace-reconciliation.md)
