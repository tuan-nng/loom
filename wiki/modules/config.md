---
title: config (internal/config)
description: Leaf package that loads, defaults, and validates the user's config.toml — user intent only, never rewritten by loom.
type: module
tags: [wiki, module, config]
---

## Summary

The `internal/config` package is a **leaf** (imports nothing internal) and the **first implemented package** (T1, commit `724b0dd`). It loads `~/.config/loom/config.toml` (resolved via `os.UserConfigDir()`), supplies defaults when the file is missing, and validates config-local values. The config file holds **user intent only** — loom never rewrites it (ADR-001 §5; C6). Missing file = defaults; a present file is parsed then validated.

## Responsibilities

- Load and parse TOML config from the user config dir.
- Provide a `Default()` config (claude/opencode binaries, `interface = "full"` — the default flipped from `mini` to `full` in commit `5e39207` so opencode cards launch the standard TUI — `default = "claude"`).
- Validate config-local values: `Opencode.Interface` ∈ `{"mini", "full"}` (`Validate()`).
- Reject a lingering ADR-001 `prompt_model` key at load time (`stalePromptModel`) — a loud migration to `model`; both the legacy top-level `[claude]` and `[agent.claude]` spellings are caught.
- Leave the cross-package check (`Agent.Default` known) to `agent.Validate(cfg)` — `config` must not import `agent` (C8, dependency rule).

## Public API / entry points

```go
func Load() (*Config, error)
func Default() *Config
func (c *Config) Validate() error
```

Config shape (DESIGN-002 §11): `Agent{Default, Claude{Binary, Model}, Opencode{Binary, Model, OpencodeAgent, Interface, AutoApprove}}`, `Session{TmuxServer, Prefix}`, `Database{Path}`.

## Key files

- [../../internal/config/config.go](../../internal/config/config.go) — `Config`, `AgentConfig`, `ClaudeConfig`, `OpencodeConfig`, `SessionConfig`, `DatabaseConfig` structs; `Default()`, `Load()`, `Validate()`, `expandPath()`, `stalePromptModel()`
- [../../internal/config/config_test.go](../../internal/config/config_test.go) — `TestDefault`, `TestExpandTilde`, `TestValidate`, `TestLoad` (missing file → defaults; present file parsed; stale `prompt_model` → error; `~` expansion; invalid TOML)

## Implementation notes (T1, commit `724b0dd`)

- Module `loom` (go 1.23); the only external dependency is `github.com/BurntSushi/toml v1.4.0`.
- `Load()` resolves `<user-config-dir>/loom/config.toml`. Missing file → `Default()` with `Database.Path` `~`-expanded; present file → decode, reject stale `prompt_model`, expand `~` in `Database.Path`, then `Validate()`.
- `expandPath()` expands a leading `~` / `~/` in `Database.Path` only; bare `~` → home, while `~user/` and mid-string `~` are left alone.
- `Default()`: agent `claude`, claude binary `claude`, opencode binary `opencode`, interface `full` (was `mini` until commit `5e39207`), tmux server `loom`, prefix `C-a`, db path `~/.config/loom/loom.db`.

## Dependencies

- None internal (leaf). External: TOML parsing only.

## Participates in

- Consumed by `agent` (driver `Resolve`/`Launch` take `cfg`), `session` (SessionManager), `cli`, `tui`.
- Validated at startup by `agent.Validate(cfg)` (main).

## Related

- Architecture: [Agent Abstraction](../architecture/agent-abstraction.md) · [Data Model](../architecture/data-model.md)
- Guides: [Add a new agent](../guides/add-a-new-agent.md)
