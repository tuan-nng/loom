---
title: Wiki Log
description: Append-only audit trail of wiki generation and refresh runs.
---

# Wiki Log

Append-only audit trail. Add one dated entry per generation or refresh run, recording the profile, the `source_commit` it was anchored to, and the coverage. The codebase-wiki skill describes the entry shape.

## 2026-08-09: generate

- Profile: internal/standard
- source_commit: adcc17df (initial generation)
- Coverage: full wiki from docs-only design repo — 5 architecture pages, 8 module pages, 4 flow pages, 7 concept pages, 4 guide pages
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [Session Model](./architecture/session-model.md), [Data Model](./architecture/data-model.md), [Agent Abstraction](./architecture/agent-abstraction.md), [Trace Recording](./architecture/trace-recording.md), [config](./modules/config.md), [agent](./modules/agent.md), [store](./modules/store.md), [trace](./modules/trace.md), [session](./modules/session.md), [board](./modules/board.md), [cli](./modules/cli.md), [tui](./modules/tui.md), [Card open to completion](./flows/card-open-complete.md), [Attach/detach](./flows/attach-detach.md), [Trace reconciliation](./flows/trace-reconciliation.md), [Failure paths](./flows/failure-paths.md), [run](./concepts/run.md), [session](./concepts/session.md), [stage](./concepts/stage.md), [agent-driver](./concepts/agent-driver.md), [watch-scope](./concepts/watch-scope.md), [trace-events](./concepts/trace-events.md), [git-reconciliation](./concepts/git-reconciliation.md), [Add a new agent](./guides/add-a-new-agent.md), [opencode-launch-semantics](./guides/opencode-launch-semantics.md), [change-the-schema](./guides/change-the-schema.md), [session-command](./guides/session-command.md)

## 2026-08-09: refresh

- Profile: internal/standard
- source_commit: 724b0dd (was adcc17df)
- Coverage: T1 config package landed as real Go source since the last stamp — `internal/config` (Default/Load/Validate) + go.mod; refreshed the config module page and the docs-only/planned framing in Overview and Architecture Overview
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [config](./modules/config.md)

## 2026-08-09: refresh

- Profile: internal/standard
- source_commit: e2ebff6 (was 724b0dd)
- Coverage: T2 agent contract landed as real Go source since the last stamp — `internal/agent` (Driver/LaunchMode/SessionSpec, registry mechanism, Card+AgentOrDefault, BuildPrompt, PosixEscape/CommandLine, agent.Validate); refreshed the agent module page and the config+agent/planned framing in Overview and Architecture Overview; claude/opencode drivers remain T3 (registry empty)
- Pages: [Overview](./OVERVIEW.md), [Architecture Overview](./architecture/overview.md), [agent](./modules/agent.md)
