---
title: agent (internal/agent)
description: The store-free agent layer — Driver interface, registry, prompt builder, POSIX quoting, and the claude/opencode drivers. The heart of ADR-002.
type: module
tags: [wiki, module, agent]
---

## Summary

`internal/agent` is a **leaf** (imports `config` only, deliberately store-free). It owns everything agent-shaped: the `Driver` interface and registry, the shared prompt builder, the POSIX argv escaper, the `agent.Card` projection, and the two shipped drivers (`claude`, `opencode`). Sessions, tracing, and the TUI never build agent commands themselves — they call the driver. Spec: ADR-002 §3–§4, DESIGN-002 §5–§9.

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

- `internal/agent/driver.go` — Driver, LaunchMode, SessionSpec, registry
- `internal/agent/card.go` — `agent.Card` projection + `AgentOrDefault` (late-bound NULL resolution)
- `internal/agent/prompt.go` — `BuildPrompt` (ADR-001 §4.5 template)
- `internal/agent/escape.go` — `PosixEscape`, `CommandLine`
- `internal/agent/claude.go` — claudeDriver (ADR-001 behavior, moved behind the driver)
- `internal/agent/opencode.go` — opencodeDriver (`--mini`/`--prompt` default, `full` optional, pass-throughs)
- `internal/agent/agent_test.go` — table-driven argv/quoting/AgentOrDefault tests

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
