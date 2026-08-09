---
title: run
description: One open→complete cycle of a card's session; every trace event for the cycle shares its run_id.
type: concept
tags: [wiki, concept, trace]
---

## Definition

A **run** is one open→complete cycle of a card's tmux session. Each run gets a fresh `run_id` (16 random bytes, hex-encoded) at creation, and every trace event for that cycle — `trace_start`, `file_change`, `trace_end` — carries it. It lets the card-detail view compute "Files Changed (last session)" and per-run duration even when a card is opened many times over its life (ADR-001 §3.3, §4.1).

## Why it matters

- A run has exactly one `trace_start` and at most one `trace_end`; the partial UNIQUE index `idx_traces_run_lifecycle` on `(card_id, run_id, event_type)` closes the double-Enter double-run race.
- "Last session" and per-run duration are only computable because events are grouped by `run_id`, not just by card.
- A failed launch (probe fails) **never** writes a run at all — the `trace_start` row is deleted, not finalized (ADR-001 §4.1 step 1).
- Runs that end unseen are finalized by reconcile-on-startup (the correctness backstop).

## Where it lives

- `traces.run_id` column + `trace_start`/`trace_end` lifecycle (ADR-001 §3.3).
- Created in `session.Manager.ensure` via `trace.StartRun` **after** the startup probe passes (DESIGN-002 §10.2).
- Event ordering within a run is `traces.seq`, not timestamps — see [trace-events](./trace-events.md).

## Related

- Concepts: [trace-events](./trace-events.md) · [session](./session.md)
- Architecture: [Trace Recording](../architecture/trace-recording.md) · [Session Model](../architecture/session-model.md)
- Flows: [Card open → completion](../flows/card-open-complete.md) · [Failure paths](../flows/failure-paths.md)
