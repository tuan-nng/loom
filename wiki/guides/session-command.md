---
title: Session command construction
description: How a card's tmux session command is built — prompt template, POSIX quoting of every argv element, and why SendKeys is never quoted.
type: guide
tags: [wiki, guide, session, tmux]
---

## Goal

Understand exactly how the command that runs inside a card's tmux session is assembled — from card fields to the `$SHELL -c` string — so you can debug a session that fails to launch or mis-quotes.

## The pipeline

1. **`BuildPrompt(card)`** (`agent/prompt.go`) — the title always, then `## Description`, `## Objective`, `## Acceptance Criteria` sections (each omitted when empty). Template: ADR-001 §4.5. No structure is inferred from markdown headings — sections pass through verbatim.
2. **Driver builds argv** (`driver.Launch`) — e.g. claude: `[abs-claude, <prompt>]` (+`--model`); opencode: `[abs-opencode, --mini, --prompt, <prompt>]` (+ pass-throughs). argv[0] is the absolute path from `Resolve`.
3. **`CommandLine(argv)`** (`agent/escape.go`) — every element is POSIX single-quoted (`'` → `'\''`), joined with spaces. tmux executes the session command via `$SHELL -c`, so this is what survives shell parsing. Newlines inside single quotes are fine.
4. **tmux `new-session -d -s loom-<id> -n loom-<id> -c <root> <joined>`** — cwd set by `-c`, never by the driver; `-n` names the session's first window so a tab linked into the user's tmux reads `loom-<id>`.
5. **`SendKeys`** (optional) — a literal tmux key name (e.g. `Enter`) sent via `tmux send-keys` **after** the probe passes. Never part of the `$SHELL -c` string, so never shell-quoted. Ships as `""` for both drivers (opencode auto-submits).

## Debugging a session that dies instantly

- Check the pane: `tmux -L loom capture-pane -p -t loom-<id>` — the captured text becomes the error toast if the probe fails.
- Check the binary resolved: loom uses `exec.LookPath` in **its own** env; the tmux server inherits whichever client first started it (ADR-001 §4.1 step 0).
- A session that never launches writes **no** `trace_start` row — absence alone doesn't mean "done"; the ~500ms probe distinguishes (ADR-001 §4.1).

## Related

- Concepts: [session](../concepts/session.md) · [agent-driver](../concepts/agent-driver.md)
- Architecture: [Session Model](../architecture/session-model.md) · [Agent Abstraction](../architecture/agent-abstraction.md)
- Modules: [session](../modules/session.md) · [agent](../modules/agent.md)
