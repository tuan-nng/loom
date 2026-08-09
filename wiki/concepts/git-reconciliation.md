---
title: git-reconciliation
description: The path-keyed porcelain-diff algorithm that attributes file changes for a run, robust across loom restarts.
type: concept
tags: [wiki, concept, trace, git]
---

## Definition

**Git reconciliation** is the algorithm that computes the authoritative set of files an agent changed during a run, from a baseline porcelain snapshot (captured at `trace_start`) vs a completion snapshot — combined with live fsnotify events and deduped on path. It exists because fsnotify only records while a loom process is alive, but a tmux session can outlive loom (ADR-001 §4.4, §5).

## Why it matters

- **Keyed on path, never on the raw porcelain line.** Line-wise set difference is wrong in both directions: a status-letter change alone (staging moves ` M` → `M `) is not an edit, and a file already dirty at baseline yields a byte-identical line and vanishes. Under-attribution — silently dropping a file the agent really changed — is the worse failure, so the algorithm is deliberately biased toward **over-attribution**.
- Three inputs combine: the **working-tree set** (completion paths not in baseline, or with a different status letter; plus baseline-only paths as `modified`), the **committed set** (if HEAD moved, `git diff --name-status <base_head> HEAD`), and the **live fsnotify events** (a path is emitted only if fsnotify didn't already record it — never counted twice).
- `trace_end.files_changed` = unique paths across both sources.

## Where it lives

- `internal/trace/git.go` (porcelain/path-keyed reconcile) — DESIGN-002 §4.2. **Landed T8:** `Baseline`/`Change`, `SnapshotBaseline(root)` (pure exec-git; non-repo → empty pair, nil error), `Reconcile(baseline, current, diffOut)` (exec-free — the committed-set text is injected; committed op wins on conflict; output sorted by path), `Dedup(live, changes)`, `FilesChanged(changes)` — table-tested in git_test.go.
- The fsnotify watcher and the recorder that wires the deduped rows into the store are T9.
- Runs at completion, `K`/`close`, done-stage move, and reconcile-on-startup.
- Documented limitations: non-git watch scope → fsnotify-only fidelity; already-dirty files are attributed to the run whether or not touched.

## Related

- Concepts: [run](./run.md) · [trace-events](./trace-events.md)
- Architecture: [Trace Recording](../architecture/trace-recording.md)
- Flows: [Trace reconciliation](../flows/trace-reconciliation.md)
