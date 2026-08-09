---
title: Add a new agent
description: How to add a third coding agent driver to loom — interface, registry, config, validation, tests.
type: guide
tags: [wiki, guide, agent]
---

## Goal

Add a new coding agent (e.g. `aider`) as a first-class loom agent driver, with the same lifecycle, tracing, and attach/detach as claude and opencode. Per ADR-002 §9, "more agents" needs just a third `AgentDriver` impl — no interface change expected.

## Steps

1. **Implement the driver** in `internal/agent/<name>.go` (DESIGN-002 §4.2): `Name()`, `Resolve(cfg)` (return `exec.LookPath(cfg.Agent.<X>.Binary)`), `LaunchMode()` (Interactive unless it supports autonomous run), `Launch(exe, card, cfg)` returning `SessionSpec{Argv, SendKeys}`.
2. **Add config** in `internal/config`: a `<X>Config{Binary, Model, ...}` block + fields in `AgentConfig`, plus the defaults in `Default()` and config-local validation.
3. **Register** by adding `func init() { drivers["<name>"] = <name>Driver{} }` in the driver's own file — the `claude`/`opencode` drivers follow this self-registration pattern (T3), and `driver.go`'s map literal stays untouched. `Known()`/`IsKnown()`/`Get` then include it.
4. **Add validation** — if the agent name is user-facing in config, ensure `Agent.Default` ∈ `Known()` (this is why `agent.Validate(cfg)` exists — the cross-package check can't live in `config`, C8).
5. **Prompt/quoting come free** — `BuildPrompt` and `PosixEscape`/`CommandLine` are shared; only add pass-through flags if the agent needs them.
6. **Tests** — add table cases to the argv unit tests (positional vs flag-based prompt), a `Resolve` test, and a parametrized stub-agent integration case in `internal/session` (ADR-001 §10 / ADR-002 §10).
7. **Phase 0 probe** (if an interactive agent): verify in a detached `-L loom` session that your launch argv actually works and that completion semantics hold; record the transcript like [PROBE-full-tui.md](../../docs/PROBE-full-tui.md).

## Relevant code

- `internal/agent/driver.go` (interface, registry), `claude.go`, `opencode.go`, `escape.go`, `prompt.go` — see [agent](../modules/agent.md).
- `internal/config/config.go` — config blocks + defaults + validation.
- `internal/session/manager.go` — `ensure` steps 1–3, 7 (driver touchpoints) — DESIGN-002 §10.2.

## Gotchas

- **Never resolve the binary twice** — `Launch` takes `exe` from `Resolve` (DESIGN-002 §5.2).
- **argv[0] must be the absolute path** — tmux inherits a different env (ADR-001 §4.1 step 0).
- **Quote every element** — tmux runs via `$SHELL -c`; only `SendKeys` values are never quoted.
- **Keep `agent` store-free** — take the `agent.Card` projection, not a `store.Card`.
- **`LaunchMode: Run` is designed, not wired** — interactive launch is the only shipped mode in v0.1 (ADR-002 §2.1, §9).

## Related

- Concepts: [agent-driver](../concepts/agent-driver.md) · [session](../concepts/session.md)
- Architecture: [Agent Abstraction](../architecture/agent-abstraction.md)
- Modules: [agent](../modules/agent.md) · [config](../modules/config.md)
