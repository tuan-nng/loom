---
title: Attach/detach — human in the loop
description: How the human steers an agent and walks away — full-terminal handoff, detach, reattach, and sessions that outlive loom.
type: flow
tags: [wiki, flow, session, tmux]
---

## Summary

The agent picks up the task in a background session; the human is **in the loop by choice**. Attach to watch, steer, answer permission prompts, or interrupt; detach to let it run while the board stays usable. The session survives loom exiting entirely. When loom runs inside the user's own tmux, opening a card links it into that session as a plain tab — a pane, not a nested tmux client. Spec: ADR-001 §4.3–§4.4.

## Trigger

- `Enter`/`loom card open <id>` on a running card (reattach), or `loom attach <id>`.
- Detach via `prefix d` (default `C-a d` on the loom server; inside the user's tmux the standard `prefix d` detaches the linked tab).

## Sequence diagram

```mermaid
sequenceDiagram
    participant U as User
    participant SM as SessionManager
    participant T as tmux -L loom
    participant A as Agent
    U->>SM: Enter on running card
    SM->>T: ensure (reuse existing session)
    SM->>T: AttachCommand(loom-<id>) — attach-session standalone / link-window into enclosing tmux
    T-->>U: agent's native TUI (full terminal)
    U->>A: watch / steer / answer prompts
    U->>T: prefix-d detach
    T-->>U: back at board
    Note over T,A: session keeps running detached
    U->>SM: later: loom attach <id>
    SM->>T: attach-session again
```

## Steps

1. `ensure` reuses an existing session if present (no new run).
2. `tea.ExecProcess(session.AttachCommandFor(...))` — the lazygit-pattern full-terminal handoff; the child is the tmux client. The command is mode-dependent: on an enclosing tmux server it is `link-window` (linking the card's window in as a plain tab, so the handoff returns almost immediately) or `select-window` when the tab is already linked; standalone it is `attach-session -t loom-<id>` on the dedicated `-L` server.
3. On detach (`prefix d`), loom's board returns; the session keeps running.
4. If loom exits entirely, the session persists — a later loom invocation redisovers `loom-*` sessions and marks those cards running (ADR-001 §4.4).
5. When the user quits the agent (or `K`/`close`/done-move kills it), the session ends and the run finalizes.

## Failure modes

- **Nested tmux** — loom inside the user's own tmux: the enclosing-server model manages the card's session on the user's server and opens it as a linked tab (never a nested client); the loom-owned `-L` server remaps prefix to `C-a` and sets `status off`/`detach-on-destroy off` so a standalone attach renders as a plain pane (ADR-001 §4.4).
- **Agent left running and forgotten** — visible via `loom sessions`, board `●` markers, or `tmux -L loom ls`; kill with `K`/`loom card close <id>`.
- **Detached prefill regression** (future opencode): a `--detach` run could idle at its prompt as a live-but-empty run — the auto-submit canary guards this (DESIGN-002 §16).

## Related

- Architecture: [Session Model](../architecture/session-model.md)
- Concepts: [session](../concepts/session.md) · [run](../concepts/run.md)
- Flows: [Card open → completion](./card-open-complete.md)
