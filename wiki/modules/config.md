---
title: config (internal/config)
description: Leaf package that loads, defaults, and validates the user's config.toml — user intent only, never rewritten by loom.
type: module
tags: [wiki, module, config]
---

## Summary

The `internal/config` package is a **leaf** (imports nothing internal). It loads `~/.config/loom/config.toml` (resolved via `os.UserConfigDir()`), supplies defaults when the file is missing, and validates config-local values. The config file holds **user intent only** — loom never rewrites it (ADR-001 §5; C6). Missing file = defaults; a present file is parsed then validated.

## Responsibilities

- Load and parse TOML config from the user config dir.
- Provide a `Default()` config (claude/opencode binaries, `interface = "mini"`, `default = "claude"`).
- Validate config-local values: `Opencode.Interface` ∈ `{"mini", "full"}`; error on a lingering ADR-001 `prompt_model` key (loud migration naming `model`).
- Leave the cross-package check (`Agent.Default` known) to `agent.Validate(cfg)` — `config` must not import `agent` (C8, dependency rule).

## Public API / entry points

```go
func Load() (*Config, error)
func Default() *Config
func (c *Config) Validate() error
```

Config shape (DESIGN-002 §11): `Agent{Default, Claude{Binary, Model}, Opencode{Binary, Model, OpencodeAgent, Interface, AutoApprove}}`, `Session{TmuxServer, Prefix}`, `Database{Path}`.

## Key files

- `internal/config/config.go` — Config structs, Default, Load, Validate (DESIGN-002 §4.2)
- `internal/config/config_test.go` — missing file → defaults; bad `default`/`interface` → fail fast; `prompt_model` → error

## Dependencies

- None internal (leaf). External: TOML parsing only.

## Participates in

- Consumed by `agent` (driver `Resolve`/`Launch` take `cfg`), `session` (SessionManager), `cli`, `tui`.
- Validated at startup by `agent.Validate(cfg)` (main).

## Related

- Architecture: [Agent Abstraction](../architecture/agent-abstraction.md) · [Data Model](../architecture/data-model.md)
- Guides: [Add a new agent](../guides/add-a-new-agent.md)
