---
title: Failure paths
description: The two ways a run is silently mis-recorded (failed launch, missed completion) plus the concurrency and ordering guards — and how each is handled.
type: flow
tags: [wiki, flow, failure, correctness]
---

## Summary

The design treats silent mis-recording as the cardinal sin: **plausible-looking wrong data rather than an error**. This page collects the failure paths — a launch that never starts, a completion nobody observes, concurrent runs, unorderable events — and the mechanisms that turn each into a visible error or a corrected value. Spec: ADR-001 §4.1, §8; DESIGN-002 §10.2.

## Failure modes

### Failed launch (bad binary, bad cwd, non-executable)

A session whose command never got off the ground is *absent* for the same reason a completed one is. The ~500ms **startup probe** distinguishes them: probe fails → `capture-pane` the scrollback, kill the session, **delete** (not finalize) the `trace_start` row, surface the pane as an error toast. Combined with `exec.LookPath` resolution in loom's env (the tmux server inherits a different env), a failed launch never appears as a successful zero-file run (ADR-001 §4.1 steps 0–1).

### Missed completion (run ends between polls or while loom isn't running)

The 2s poll is a liveness *indicator*, not the completion detector. **Reconcile-on-startup** queries for runs with a `trace_start` and no `trace_end`, cross-references the live `loom-*` sessions, and finalizes every such run whose session is absent — exactly once. This is also the path that attributes sessions outliving loom, so it costs nothing extra (ADR-001 §4.1 step 5).

### Double-Enter race

Two `ensure` calls before the first session appears in the poll would open two concurrent runs. The partial UNIQUE index `idx_traces_run_lifecycle` on `(card_id, run_id, event_type)` for `trace_start`/`trace_end` rejects the second (ADR-001 §3.3).

### Unorderable trace events

Timestamps tie even at millisecond precision. `traces.seq` (`INTEGER PRIMARY KEY AUTOINCREMENT`) is the sole ordering key — `AUTOINCREMENT` so `VACUUM` can't renumber history. A `file_change` recorded before its run's `trace_start` would be a corrupt run; `seq` gives a guaranteed total order (ADR-001 §3.3).

### Live session with no trace row

If `StartRun` fails after the session was created, the deferred `KillSession` + whole-run `AbortRun` guarantee a session is never left alive without its run — the poll/reconcile can only complete runs they can see (DESIGN-002 §10.2 invariants).

### opencode prefill-only regression (future)

The auto-submit canary — a post-probe `capture-pane` asserting the pane shows the model's response, not the prefilled input — fails a test instead of silently idling a `--detach` run (DESIGN-002 §16, finding F5).

## Related

- Architecture: [Session Model](../architecture/session-model.md) · [Trace Recording](../architecture/trace-recording.md)
- Concepts: [run](../concepts/run.md) · [trace-events](../concepts/trace-events.md)
- Flows: [Card open → completion](./card-open-complete.md) · [Trace reconciliation](./trace-reconciliation.md)
