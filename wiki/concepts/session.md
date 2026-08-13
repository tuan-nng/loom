---
title: session
description: A tmux session on the dedicated -L loom server that hosts one card's agent; session existence == agent running.
type: concept
tags: [wiki, concept, tmux, session]
---

## Definition

A **session** is a detached tmux session named `loom-<cardid>`, whose command is the card's coding agent with the card context as its prompt. Standalone it lives on the dedicated `-L loom` server; when loom itself runs inside the user's tmux, it lives on that **enclosing server** (`-S <socket>`) and opens as a plain linked window. Because a session command ends when the agent exits, **session existence == agent running** — loom never parents the agent, needs no PID tracking, and no output parsing (ADR-001 §4).

## Why it matters

- **Completion detection is just "the session disappeared"** — identical for claude and opencode, which is what makes the agent layer interchangeable (ADR-002 §4.3).
- **The human is in the loop by choice** — attach to steer, detach to let it run; the session survives loom exiting (ADR-001 §4.3).
- **The session IS the resume mechanism** — Claude Code's `--resume`/opencode's session flags are never managed by loom; the live tmux conversation is resume.
- The dedicated server is private (`-L loom`), exits when its last session ends (`exit-empty on`), and applies the loom-owned settings — `prefix C-a`, `status off`, `detach-on-destroy off` — for nested-tmux safety, but only on its own `-L` server, never the user's enclosing tmux globals (ADR-001 §4.4).

## Where it lives

- `session.Tmux` wrapper (`session/tmux.go`) ships as real Go source (T10): `New(server)` resolves tmux and gates ≥ 3.x, and `SessionName(id) = "loom-" + id` rejects `:`; `Sessions` (T11) adds live-state listing for the status markers. `session.Manager` (`manager.go`) now ships too (T11): driver-aware `Ensure`, `Attach`, `Kill`, one-tick `Status`, and `ReconcileOnStartup`. Since the restyle, `New` also captures the enclosing tmux (`$TMUX` → `EnclosingSocket`, `$TMUX_PANE`), `AttachCommand`/`AttachCommandFor` build the link-as-tab vs attach handoff, and `KillSession` kills a linked window first on an enclosing server.
- Managed lifecycle in [Session Model](../architecture/session-model.md).

## Related

- Concepts: [run](./run.md) · [stage](./stage.md)
- Architecture: [Session Model](../architecture/session-model.md)
- Flows: [Card open → completion](../flows/card-open-complete.md) · [Attach/detach](../flows/attach-detach.md)
