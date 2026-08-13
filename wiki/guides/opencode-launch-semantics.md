---
title: opencode launch semantics
description: The exact argv loom generates for opencode, the auto-submit facts, and how to re-verify them on a version bump.
type: guide
tags: [wiki, guide, opencode, agent]
---

## Goal

Understand (and re-verify) how loom launches opencode: what argv it produces, why `--prompt` needs no Enter, and what to re-probe when opencode bumps majors. Default `interface` is `full` (the standard TUI) since commit `5e39207`.

## The shipped argv (DESIGN-002 §9.2)

| `interface` | argv | When the session ends |
|---|---|---|
| `full` (default since `5e39207`) | `[<abs-opencode>, --prompt, <ctx>]` | user quits the full TUI |
| `mini` | `[<abs-opencode>, --mini, --prompt, <ctx>]` | user quits the split-footer REPL |

Pass-throughs appended when set: `model` → `--model <provider/model>`, `opencode_agent` → `--agent <name>`, `auto_approve` → `--auto`. `SendKeys` is `""` for both interfaces.

## Verified facts (v1.18.15, 2026-08-09)

- `--prompt` **auto-submits** in both mini (P1) and full TUI (P7) — the pane shows the model's response, not a prefilled input.
- The REPL/TUI **stays alive after a turn** (P2/P8) — session existence still means "user is in the REPL", so completion semantics hold.
- `--mini` requires a TTY stdout (P3) — a tmux pane PTY satisfies this.
- `run --mini` is rejected; `run --interactive` is accepted (P4/P5) — but the shipped interactive interface is top-level `--mini`, not the `run` subcommand.
- Full transcript: [PROBE-full-tui.md](../../docs/PROBE-full-tui.md); probe table in DESIGN-002 §3.1.

## Re-verification on a major bump

1. Re-run probes P1/P2 (mini) and P7/P8 (full) exactly as in [PROBE-full-tui.md](../../docs/PROBE-full-tui.md): detached `-L loomprobe` session, wait ~5s, `capture-pane`, assert the model's response is shown and the session is alive at t+30s.
2. If a future version regresses `--prompt` to prefill-only, set the driver's `SendKeys = "Enter"` (post-probe `send-keys`) — the field exists for this.
3. The integration-test auto-submit canary (post-probe `capture-pane` asserting the model's response) fails the suite instead of silently idling `--detach` runs (DESIGN-002 §16).
4. Update `loom docs`/README with the verified version range (ADR-002 §8).

## Related

- Concepts: [agent-driver](../concepts/agent-driver.md)
- Architecture: [Agent Abstraction](../architecture/agent-abstraction.md)
- Modules: [agent](../modules/agent.md) · [config](../modules/config.md)
- Guides: [Add a new agent](./add-a-new-agent.md)
