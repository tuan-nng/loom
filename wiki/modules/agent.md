---
title: agent (internal/agent)
description: The store-free agent layer — Driver interface, registry, prompt builder, POSIX quoting, and the claude/opencode drivers. The heart of ADR-002.
type: module
tags: [wiki, module, agent]
---

## Summary

The `internal/agent` package is a **leaf** (imports `config` only, deliberately store-free) and the **second implemented package** (T2, commit `e2ebff6`; drivers T3, commit `f729599`). It owns everything agent-shaped: the `Driver` interface and registry, the shared prompt builder, the POSIX argv escaper, and the `agent.Card` projection. Both shipped drivers — `claude` and `opencode` — are implemented and self-register into the registry via `init()` (T3). Sessions, tracing, and the TUI never build agent commands themselves — they call the driver. Spec: ADR-002 §3–§4, DESIGN-002 §5–§9.

## Responsibilities

- Define the `Driver` interface (`Name`, `Resolve`, `LaunchMode`, `Launch`) and the `SessionSpec` value.
- Provide the static registry (`Get`, `Known`, `IsKnown`); no dynamic registration in v0.1.x.
- Build the card prompt (`BuildPrompt`) from title/description/objective/acceptance-criteria (ADR-001 §4.5 template).
- Single-quote every argv element (`PosixEscape`/`CommandLine`) — tmux runs the session command via `$SHELL -c`.
- Resolve the agent binary to an absolute path (`exec.LookPath`) in loom's own environment.
- Validate agent names across config + registry via `agent.Validate(cfg)` (C8).

## Public API / entry points

```go
type Driver interface { Name() string; Resolve(*config.Config) (string, error); LaunchMode() LaunchMode; Launch(exe string, Card, *config.Config) (SessionSpec, error) }
type SessionSpec struct { Argv []string; SendKeys string }
func Get(name string) (Driver, error)
func Known() []string
func IsKnown(name string) bool
func BuildPrompt(c Card) string
func PosixEscape(s string) string
func CommandLine(argv []string) string
func Validate(cfg *config.Config) error
```

## Key files

- [../../internal/agent/driver.go](../../internal/agent/driver.go) — Driver, LaunchMode, SessionSpec, registry mechanism
- [../../internal/agent/card.go](../../internal/agent/card.go) — `agent.Card` projection + `AgentOrDefault` (late-bound NULL resolution)
- [../../internal/agent/prompt.go](../../internal/agent/prompt.go) — `BuildPrompt` (ADR-001 §4.5 template)
- [../../internal/agent/escape.go](../../internal/agent/escape.go) — `PosixEscape`, `CommandLine`
- [../../internal/agent/agent_test.go](../../internal/agent/agent_test.go) — table-driven quoting/AgentOrDefault/Validate/registry tests
- `internal/agent/claude.go` — `claudeDriver` (ADR-001 behavior, moved behind the driver): `[exe, prompt]` + `--model` after the prompt — **implemented (T3)**
- `internal/agent/opencode.go` — `opencodeDriver` (`--mini`/`--prompt` default, `full` optional, pass-throughs after the prompt) — **implemented (T3)**

## Implementation notes (T2, commit `e2ebff6`; drivers T3, commit `f729599`)

- The driver contract + helpers land as real Go source: `Driver` (signatures per DESIGN-002 §5.2 — `Resolve(cfg *config.Config)`, `Launch(exe, card, cfg)`), `LaunchMode` (`interactive`/`run`), `SessionSpec{Argv, SendKeys}`, the registry mechanism (`Get`/`Known`/`IsKnown`, `Known()` sorted and non-nil), `agent.Card` + `AgentOrDefault`, `BuildPrompt` (§7), `PosixEscape`/`CommandLine` (§8), and `agent.Validate(cfg)`.
- `claudeDriver`/`opencodeDriver` land in T3 as field-less value-receiver structs, each self-registering with `func init() { drivers["claude"] = claudeDriver{} }` in its own file — `driver.go` and its map literal stay untouched. The registry is populated (`Known()` = `["claude", "opencode"]`), so `agent.Validate`'s default check now passes on the shipped default (`claude`).
- claude `Launch`: `[exe, BuildPrompt(card)]` then `--model m` iff `[agent.claude] model` is set — prompt first (DESIGN-002 §9.1).
- opencode `Launch`: `[exe]`, then per `interface` — `"full"` → `--prompt <ctx>`, default `"mini"` → `--mini --prompt <ctx>` (switch default); pass-throughs appended **after** the prompt only when set, in order: `--model m`, `--agent a`, `--auto` (flag-only). No session flags (`--dir`/`--title`/`-c`/`-s`/`--fork`). `SendKeys` is `""` for both drivers (§9.2, §10.3).
- `Resolve` returns `exec.LookPath(cfg.Agent.<X>.Binary)`'s error verbatim.
- Tests: table-driven `Launch` argv cases per driver (claude positional; opencode mini-default/mini-explicit/full; pass-throughs only-when-set incl. all-flags rows), PATH-independent `Resolve` tests (absolute paths `/bin/sh` + nonexistent), and `TestKnownRealDrivers` asserting the **real** registry (`Known()` = `["claude", "opencode"]`, per-driver `Name`/`LaunchMode`) unseeded.
- Verification: `go build`, `go vet`, `go test ./...` green; 100% statement coverage.

## Dependencies

- `config` only. Deliberately **store-free** — takes an `agent.Card` projection so driver unit tests never touch a database (DESIGN-002 §4.2, §6).

## Participates in

- `session.Manager.ensure` calls `agent.Get(card.AgentOrDefault)` then `driver.Resolve`/`driver.Launch` (DESIGN-002 §10.2).
- Validated from `main` at startup.
- CLI `--agent` flags validated against `Known()`.

## Related

- Architecture: [Agent Abstraction](../architecture/agent-abstraction.md) · [Session Model](../architecture/session-model.md)
- Concepts: [agent-driver](../concepts/agent-driver.md) · [run](../concepts/run.md)
- Guides: [Add a new agent](../guides/add-a-new-agent.md)
- Flows: [Card open → completion](../flows/card-open-complete.md)
