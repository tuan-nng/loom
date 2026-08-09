# Probe: Full-TUI `--prompt` auto-submit (T0)

**Task:** `TASKS-loom-v0.1.md` §T0 — the last open Phase 0 question: does
`opencode --prompt '<ctx>'` (no `--mini`) auto-submit in the **full TUI**, and
does the TUI stay alive after a turn?

**Verdict: CONFIRMED** (2026-08-09) — the full-TUI gate is **lifted**.

## Environment

| | |
|---|---|
| opencode | `1.18.15` (`/home/novpla/.opencode/bin/opencode`) |
| tmux | `3.6` |
| Pane | `120x40` (detached `-L loomprobe`, `tmux new-session -d -x 120 -y 40 -s probe`) |
| cwd | `/mnt/data/works/loom` |
| Model | DeepSeek V4 Flash (user's default config) |

## Method

Identical to P1/P2 (§3.1): a detached `-L loomprobe` tmux session running the
bare full-TUI argv loom would generate for `interface = "full"` —
`opencode --prompt "Reply with exactly: OK"` (no `--mini`, no pass-throughs).
Wait ~5s, `capture-pane`; assert the pane shows the model's response, not the
prefilled input. At t+30s, assert the session is still alive. Probe session
killed afterwards.

## Timeline

- **t0** 09:30:33.9 — `new-session` launched `opencode --prompt "Reply with exactly: OK"`.
- **t+5s** 09:30:43.1 — capture; pane shows the submitted message, the model's
  `OK`, and the full-TUI status bar (below). Session alive.
- **t+30s** 09:31:13.2 (measured t+39.3s) — capture unchanged; session still
  alive. **No Enter was sent; the prompt auto-submitted.**

## Captures (ANSI-stripped, elided)

t+5s and t+30s rendered identically (top → bottom):

```
  ┃
  ┃  Reply with exactly: OK
  ┃
     OK
     ▣  Build · DeepSeek V4 Flash · 5.4s
  ┃
  ┃
  ┃  Build · DeepSeek V4 Flash DeepSeek
  ╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
   /mnt/data/works/loom                                                            52.7K (5%) · $0.01  ctrl+p commands
```

The bottom chrome (`/mnt/data/works/loom 52.7K (5%) · $0.01  ctrl+p commands`)
is the **full TUI** status bar — not the `--mini` split-footer — confirming the
`interface = "full"` render path is what auto-submitted. `▣ Build ·
DeepSeek V4 Flash · 5.4s` mirrors P1's latency signature (5.0s for mini).

## Verdict and consequences

- **Auto-submit: confirmed.** The full TUI submits `--prompt` itself; the pane
  shows the model's response, not a prefilled input line (F5 failure mode absent).
- **Stays alive: confirmed.** The full TUI does not exit on idle; session
  existence still means "user is in the TUI" (completion semantics hold).
- **Gate lifted.** `interface = "full"` no longer fails startup validation.
  T2's `agent.Validate` accepts `{"mini","full"}`; T3's `full` argv path
  (`["<abs-opencode>", "--prompt", "<ctx>"]`) ships with a table case. The §16
  regression canary covers **both** interfaces against a future prefill-only
  regression.

Pinned for v1.18.15; re-run the §3 probes (incl. P7/P8) on each opencode major
bump (§15).
