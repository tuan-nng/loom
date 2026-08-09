---
title: watch-scope
description: The directory fsnotify watches during a run — the card's codebase path or workspace root, with ignore rules.
type: concept
tags: [wiki, concept, trace, fsnotify]
---

## Definition

The **watch scope** is the directory loom watches for file changes during a run: the card's `codebase_id` path when set, otherwise the workspace `root_path`. This makes "Codebase: <path>" in the card-detail view real and keeps watching bounded to what the card is actually about (ADR-001 §4.6).

## Why it matters

- `fsnotify` does not watch recursively on its own: loom walks the scope and registers one watch per directory, adds watches on `Create` events, and skips ignored dirs. Without the ignore rules, watching a real repo root would record every compiler/VCS artifact as a `file_change` event.
- The **same scope root** is the tmux session's cwd (`-c <root>`), so the agent works in the watched tree.
- `file_change.path` is stored relative to the watch scope.
- The session cwd + watch scope is what `cards.codebase_id` selects (ADR-001 §4.6).

## Where it lives

- `cards.codebase_id` → `codebases.path` (ADR-001 §3.3); `session.Manager.watchRoot` selects it (DESIGN-002 §10.2).
- Ignore rules in `trace/watcher.go`: built-in defaults (`.git`, `node_modules`, `target`, `dist`, `build`, `vendor`, `.venv`, `__pycache__` — always skipped) + `.loomignore` at the scope top (gitignore-style, merged on top).

## Related

- Concepts: [run](./run.md) · [trace-events](./trace-events.md)
- Architecture: [Trace Recording](../architecture/trace-recording.md) · [Data Model](../architecture/data-model.md)
