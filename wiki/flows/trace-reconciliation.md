---
title: Trace reconciliation
description: How file changes are attributed at run completion — path-keyed porcelain diff, committed-set diff, and dedup against live fsnotify events.
type: flow
tags: [wiki, flow, trace, git]
---

## Summary

At completion, loom computes the authoritative set of files the agent changed, combining the **git-baseline pair** with the **live fsnotify events** already recorded, keyed on path so nothing is double-counted and already-dirty files are over-attributed. This is what makes a session that outlives loom still attributable. Spec: ADR-001 §5.

## Trigger

- Completion (session disappears), `K`/`loom card close <id>`, a move into a `done`-stage column, or reconcile-on-startup.

## Steps

1. **Baseline pair** (from `trace_start`): `git rev-parse HEAD` + `git status --porcelain`, parsed into a `path → status-letter` map.
2. **Completion pair**: take a second porcelain snapshot (+ HEAD).
3. **Working-tree set** — path in completion map and either (a) absent from baseline (newly dirtied/untracked) or (b) present with a different status letter; plus any baseline path absent from completion (staged/committed/reverted) as `modified`. Already-dirty paths with an identical letter are **ambiguous** and included as `modified` — deliberately biased toward over-attribution.
4. **Committed set** — if HEAD moved, `git diff --name-status <base_head> HEAD` contributes paths (`A`→created, `M`→modified, `D`→deleted, `R`→old deleted + new created).
5. **Union + dedup** — emit a `file_change` row only for paths fsnotify did not already record; `trace_end.files_changed` = unique paths across both.

## Failure modes

- **Line-wise diff bug** — a status-letter change alone (staging moves ` M` → `M `) was counted as an edit, and an already-dirty file yielded a byte-identical line and vanished (under-attribution, the worse failure). Fixed by keying on **path** (ADR-001 §5, 2026-08-08 review).
- **Non-git watch scope** — trace fidelity is fsnotify-only.
- **Already-dirty file** — attributed to the run whether or not the agent touched it (documented limitation).

## Related

- Architecture: [Trace Recording](../architecture/trace-recording.md)
- Concepts: [git-reconciliation](../concepts/git-reconciliation.md) · [run](../concepts/run.md) · [trace-events](../concepts/trace-events.md)
- Flows: [Card open → completion](./card-open-complete.md) · [Failure paths](./failure-paths.md)
