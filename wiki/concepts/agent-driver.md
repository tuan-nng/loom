---
title: agent-driver
description: The AgentDriver interface — the loom-owned launch contract (resolve, launch-mode, argv) that makes agents interchangeable.
type: concept
tags: [wiki, concept, agent]
---

## Definition

An **agent driver** is an implementation of the `AgentDriver` interface — one per coding agent (`claude`, `opencode`). It produces the argv that runs inside the card's tmux session and describes what the session's end means; it does **not** own the session lifecycle. Everything above the driver (tmux server, probe, poll, reconcile, tracing) is agent-agnostic and unchanged (ADR-002 §3.1).

## Why it matters

- The **launch contract is a loom guarantee** — absolute-path resolution in loom's env, POSIX quoting, startup probe, completion semantics — and must not be delegated to user scripts (rejected option C, ADR-002 §2.3).
- It makes a card (or the whole board) run its agent as either `claude` or `opencode` with **identical** lifecycle, completion detection, tracing, and attach/detach.
- `LaunchMode` encodes what "the session disappeared" means: `Interactive` (user quits) vs future `Run` (task completes).
- Two signature refinements over the ADR-as-drawn: `Resolve(cfg)` (binary is per-agent config) and `Launch(exe, card, cfg)` (avoids double `LookPath`) — DESIGN-002 §5.2.

## Where it lives

- `internal/agent`: `driver.go` (interface + registry), `claude.go`, `opencode.go` (DESIGN-002 §4.2).
- Called from `session.Manager.ensure`; validated at startup via `agent.Validate(cfg)`.
- See [Add a new agent](../guides/add-a-new-agent.md).

## Related

- Concepts: [session](./session.md) · [run](./run.md)
- Architecture: [Agent Abstraction](../architecture/agent-abstraction.md)
- Guides: [Add a new agent](../guides/add-a-new-agent.md)
